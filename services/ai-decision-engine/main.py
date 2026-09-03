# Copyright 2026 Regnant
# SPDX-License-Identifier: Apache-2.0

"""
KNOTT — AI Decision Engine
Pure standard-library HTTP service (no third-party dependencies) so it runs on
any Python 3.9+ interpreter without `pip install`. This guarantees the AI engine
starts on a client machine even when wheels are unavailable (e.g. Python 3.14).

Providers (selected via AI_PROVIDER env / runtime config: auto | anthropic | ollama | simulation):
  - Anthropic Claude  (cloud, high quality) — HTTPS via urllib
  - Ollama            (local, private)      — HTTP via urllib
  - Rule-based        (deterministic fallback when no provider is reachable)

Runtime configuration:
  Provider settings can be changed at runtime via PUT /internal/v1/config and are
  persisted to a JSON file (AI_CONFIG_PATH, default ./data/ai-config.json) so the
  choice survives restarts. Environment variables seed the initial defaults.
"""

import os
import re
import json
import time
import base64
import hashlib
import hmac
import logging
import secrets
import stat
import threading
import urllib.request
import urllib.error
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

logging.basicConfig(level=logging.INFO, format="[AI Engine] %(levelname)s %(message)s")
log = logging.getLogger(__name__)

CONFIG_PATH = os.environ.get("AI_CONFIG_PATH", os.path.join("data", "ai-config.json"))


# ─── Secret sealing (stdlib-only) ──────────────────────────────────────────────
# The Anthropic API key entered in the Settings UI is persisted to CONFIG_PATH.
# It used to be written in plaintext; it is now sealed with an HMAC-SHA256 CTR
# keystream + encrypt-then-MAC, keyed from KNOTT_SECRET_KEY (mirroring the Go
# engine's encrypted credential store). Stdlib only — no pip installs.

def _seal_keys():
    secret = os.environ.get("KNOTT_SECRET_KEY", "") or "knott-dev-default-secret-key"
    enc = hashlib.sha256(("knott-ai-config-enc:" + secret).encode()).digest()
    mac = hashlib.sha256(("knott-ai-config-mac:" + secret).encode()).digest()
    return enc, mac


def _keystream(key, nonce, length):
    out = b""
    counter = 0
    while len(out) < length:
        out += hmac.new(key, nonce + counter.to_bytes(8, "big"), hashlib.sha256).digest()
        counter += 1
    return out[:length]


def _seal(plaintext):
    """Encrypt-then-MAC a secret string → 'enc1:<base64>'. Empty stays empty."""
    if not plaintext:
        return ""
    enc_key, mac_key = _seal_keys()
    nonce = secrets.token_bytes(16)
    data = plaintext.encode("utf-8")
    ct = bytes(a ^ b for a, b in zip(data, _keystream(enc_key, nonce, len(data))))
    tag = hmac.new(mac_key, nonce + ct, hashlib.sha256).digest()
    return "enc1:" + base64.b64encode(nonce + ct + tag).decode("ascii")


def _unseal(value):
    """Reverse _seal. Returns '' on tamper/format/key mismatch. Plaintext
    (legacy configs) passes through unchanged for backward compatibility."""
    if not value:
        return ""
    if not value.startswith("enc1:"):
        return value  # legacy plaintext config — re-sealed on next save
    try:
        raw = base64.b64decode(value[len("enc1:"):])
        nonce, ct, tag = raw[:16], raw[16:-32], raw[-32:]
        enc_key, mac_key = _seal_keys()
        if not hmac.compare_digest(tag, hmac.new(mac_key, nonce + ct, hashlib.sha256).digest()):
            log.warning("Sealed AI config value failed authentication (changed KNOTT_SECRET_KEY?) — ignoring it")
            return ""
        return bytes(a ^ b for a, b in zip(ct, _keystream(enc_key, nonce, len(ct)))).decode("utf-8")
    except Exception:
        return ""

# ─── Task Specifications ───────────────────────────────────────────────────────

TASK_SPECS = {
    "fraud_risk_assessment": {
        "name": "Fraud Risk Assessment",
        "description": "Assess transaction fraud risk using behavior and pattern analysis",
        "system_prompt": """You are a senior fraud risk analyst at a major financial institution.
Assess whether a transaction shows signs of fraudulent activity. Consider amount vs. typical
behavior, geographic anomalies, merchant category risk, time patterns, and device anomalies.

Return ONLY a valid JSON object — no other text, no markdown:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

Rules: APPROVE when risk is clearly low (confidence >= 0.85, risk_score < 30).
REJECT when fraud is clearly evident (confidence >= 0.85, risk_score > 70).
ESCALATE for borderline cases. Return ONLY the JSON.""",
    },
    "credit_risk_assessment": {
        "name": "Credit Risk Assessment",
        "description": "Evaluate loan application creditworthiness",
        "system_prompt": """You are a senior credit underwriter. Assess loan creditworthiness from
the applicant data. Consider debt-to-income, credit history, loan purpose, employment stability.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 600 chars>","suggested_amount":<number or null>,"conditions":["<condition>"],
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}""",
    },
    "content_moderation": {
        "name": "Content Moderation",
        "description": "Review content for policy violations",
        "system_prompt": """You are a content policy enforcement specialist. Review content for
hate speech, harassment, violence, spam, misinformation, adult content, privacy violations.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 400 chars>","categories":["<violated_category>"],
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}""",
    },
    "document_classification": {
        "name": "Document Classification",
        "description": "Classify document type and extract key metadata",
        "system_prompt": """You are a document processing specialist. Classify the document type and
extract key metadata.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,
"document_type":"INVOICE|CONTRACT|REPORT|RECEIPT|ID_DOCUMENT|LEGAL|OTHER",
"extracted_data":{"<key>":"<value>"},"reasoning":"<max 300 chars>",
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH"}]}""",
    },
    "sentiment_analysis": {
        "name": "Customer Sentiment Analysis",
        "description": "Analyze customer message sentiment, intent, and urgency",
        "system_prompt": """You are a customer experience analyst. Analyze the customer communication
for sentiment, intent, and urgency.

Return ONLY a valid JSON object:
{"decision":"APPROVE|ESCALATE|REJECT","confidence":<0.0-1.0>,
"sentiment":"POSITIVE|NEUTRAL|NEGATIVE|CRITICAL","urgency":"LOW|MEDIUM|HIGH|CRITICAL",
"intent":"COMPLAINT|INQUIRY|PRAISE|CANCELLATION_RISK|SUPPORT","reasoning":"<max 300 chars>",
"suggested_response_tier":"SELF_SERVICE|STANDARD|PRIORITY|EXECUTIVE","flags":[]}

APPROVE = standard handling, ESCALATE = priority needed.""",
    },
    "general_decision": {
        "name": "General Decision",
        "description": "General-purpose approve/reject/escalate decision for any structured input",
        "system_prompt": """You are an operations decision assistant. Review the provided data and make
a clear, well-reasoned decision.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

ESCALATE when the decision is ambiguous or needs human judgment. Return ONLY the JSON.""",
    },

    # ─── Finance ────────────────────────────────────────────────────────────────
    "invoice_approval": {
        "name": "Invoice Approval",
        "description": "Validate and approve supplier invoices for payment (AP automation)",
        "system_prompt": """You are an accounts-payable specialist. Review a supplier invoice for
approval. Check the amount against any PO/budget, validate vendor details, detect duplicates,
unusual amounts, math errors, and policy breaches (e.g. missing PO above threshold).

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","matched_po":<true|false|null>,
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

APPROVE clean low-value invoices matching a PO. ESCALATE high-value, missing-PO, or anomalous ones.
REJECT clear duplicates or invalid invoices. Return ONLY the JSON.""",
    },
    "expense_audit": {
        "name": "Expense Report Audit",
        "description": "Audit employee expense reports against policy",
        "system_prompt": """You are a corporate expense auditor. Review an expense report for policy
compliance. Check per-category limits, missing receipts, out-of-policy categories, weekend/duplicate
claims, and unusual amounts.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","policy_violations":["<violation>"],
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

ESCALATE borderline or above-threshold reports for manager review. Return ONLY the JSON.""",
    },

    # ─── Marketing & Creative ─────────────────────────────────────────────────────
    "lead_scoring": {
        "name": "Lead Qualification & Scoring",
        "description": "Qualify and score inbound leads (MQL/SQL routing)",
        "system_prompt": """You are a demand-generation analyst. Score and qualify an inbound lead using
firmographic and behavioral signals (company size, role/seniority, industry fit, engagement, budget
signals). Decide routing.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"score":<0-100>,
"tier":"HOT|WARM|COLD|DISQUALIFIED","reasoning":"<max 400 chars>",
"suggested_owner":"SALES|NURTURE|SELF_SERVE","flags":[]}

APPROVE = route to sales now (HOT/WARM), ESCALATE = needs SDR review, REJECT = disqualified.
Return ONLY the JSON.""",
    },

    # ─── Manufacturing / Retail supply chain ───────────────────────────────────────
    "supply_chain_exception": {
        "name": "Supply Chain Exception",
        "description": "Triage supply-chain / inventory / shipment exceptions",
        "system_prompt": """You are a supply-chain operations analyst. Triage an exception event
(stockout risk, delayed shipment, quality hold, demand spike, supplier delay). Assess severity and
recommend an action.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","recommended_action":"<short action>",
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

APPROVE = auto-resolve (reorder/expedite within policy), ESCALATE = planner review, REJECT = no action.
Return ONLY the JSON.""",
    },

    # ─── HR & IT ──────────────────────────────────────────────────────────────────
    "offboarding_review": {
        "name": "Employee Offboarding Review",
        "description": "Review and authorize employee offboarding actions (HR + IT)",
        "system_prompt": """You are an HR/IT offboarding coordinator. Review an offboarding case and
decide whether automated deprovisioning (revoke accounts, reclaim devices, final paperwork) can
proceed or needs human sign-off (e.g. legal hold, disputed termination, pending payroll).

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","required_actions":["<action>"],
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

APPROVE = proceed with automated deprovisioning, ESCALATE = HR/legal sign-off needed. Return ONLY the JSON.""",
    },
}

# Anthropic model aliases (no date suffix — aliases track the latest snapshot).
# The previous defaults (claude-sonnet-4-20250514 / claude-opus-4-20250514) were
# deprecated and retired June 2026.
MODEL_PROFILES = {
    "default":        "claude-sonnet-5",
    "high_accuracy":  "claude-opus-4-8",
    "fast":           "claude-haiku-4-5",
    "ollama_default": "llama3.1:latest",
    "ollama_fast":    "llama3.2:latest",
    "ollama_large":   "llama3.1:70b",
}

# ─── Provider configuration ────────────────────────────────────────────────────

class Provider:
    """Holds the active AI provider configuration. Thread-safe for runtime updates."""

    def __init__(self):
        self._lock = threading.RLock()
        self.type = "simulation"           # anthropic | ollama | simulation
        self.preference = "auto"           # auto | anthropic | ollama | simulation
        self.anthropic_key = None
        self.ollama_url = None
        self.ollama_reachable = False
        self.ollama_model = "llama3.1:latest"

    # ── Persistence ────────────────────────────────────────────────────────────
    def _load_saved(self):
        try:
            with open(CONFIG_PATH, "r", encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            return {}

    def _save(self):
        try:
            os.makedirs(os.path.dirname(CONFIG_PATH) or ".", exist_ok=True)
            data = {
                "provider": self.preference,
                # Sealed at rest (was plaintext before) — see _seal/_unseal.
                "anthropic_api_key": _seal(self.anthropic_key or ""),
                "ollama_base_url": (self.ollama_url or os.environ.get("OLLAMA_BASE_URL", "http://localhost:11434")),
                "ollama_model": self.ollama_model,
            }
            with open(CONFIG_PATH, "w", encoding="utf-8") as f:
                json.dump(data, f, indent=2)
            try:  # owner-only permissions (no-op on Windows ACLs, effective on Linux)
                os.chmod(CONFIG_PATH, stat.S_IRUSR | stat.S_IWUSR)
            except OSError:
                pass
        except Exception as e:
            log.warning("Could not persist AI config: %s", e)

    # ── Initialization ───────────────────────────────────────────────────────--
    def initialize(self):
        with self._lock:
            saved = self._load_saved()

            pref = (saved.get("provider") or os.environ.get("AI_PROVIDER", "auto")).lower()
            key = _unseal(saved.get("anthropic_api_key") or "") or os.environ.get("ANTHROPIC_API_KEY", "")
            ollama_url = (saved.get("ollama_base_url") or os.environ.get("OLLAMA_BASE_URL", "http://localhost:11434")).rstrip("/")
            ollama_model = saved.get("ollama_model") or os.environ.get("OLLAMA_MODEL", "llama3.1:latest")

            self._apply(pref, key, ollama_url, ollama_model)

    def _apply(self, pref, key, ollama_url, ollama_model):
        """Recompute the active provider from the supplied settings. Caller holds lock."""
        self.preference = pref if pref in ("auto", "anthropic", "ollama", "simulation") else "auto"
        self.anthropic_key = key if (key and key != "sk-ant-..." and len(key) > 20) else None
        self.ollama_url = (ollama_url or "http://localhost:11434").rstrip("/")
        self.ollama_model = ollama_model or "llama3.1:latest"
        self.ollama_reachable = self._probe_ollama(self.ollama_url)

        # Resolve the active provider from preference + availability.
        if self.preference == "simulation":
            self.type = "simulation"
        elif self.preference == "anthropic":
            self.type = "anthropic" if self.anthropic_key else "simulation"
        elif self.preference == "ollama":
            # Honor explicit preference even if the probe fails; calls surface errors.
            self.type = "ollama"
        else:  # auto
            if self.anthropic_key:
                self.type = "anthropic"
            elif self.ollama_reachable:
                self.type = "ollama"
            else:
                self.type = "simulation"

        log.info("Active AI provider: %s (preference=%s, ollama_reachable=%s)",
                 self.type.upper(), self.preference, self.ollama_reachable)

    @staticmethod
    def _probe_ollama(url):
        try:
            with urllib.request.urlopen(url + "/api/tags", timeout=2.0) as r:
                return r.status == 200
        except Exception:
            return False

    # ── Runtime update ───────────────────────────────────────────────────────--
    def update(self, patch):
        """Apply a partial config update at runtime and persist it."""
        with self._lock:
            pref = (patch.get("provider") or self.preference).lower()
            key = patch.get("anthropic_api_key")
            if key is None:
                key = self.anthropic_key or ""
            ollama_url = patch.get("ollama_base_url") or self.ollama_url
            ollama_model = patch.get("ollama_model") or self.ollama_model
            self._apply(pref, key, ollama_url, ollama_model)
            self._save()
            return self.config_payload()

    def config_payload(self):
        with self._lock:
            return {
                "provider": self.preference,
                "active_provider": self.type,
                "anthropic_configured": self.anthropic_key is not None,
                "ollama_base_url": self.ollama_url,
                "ollama_model": self.ollama_model,
                "ollama_reachable": self.ollama_reachable,
            }

    def list_ollama_models(self):
        with self._lock:
            url = self.ollama_url
        try:
            with urllib.request.urlopen(url + "/api/tags", timeout=4.0) as r:
                data = json.loads(r.read().decode("utf-8"))
            return [m.get("name") for m in data.get("models", []) if m.get("name")]
        except Exception as e:
            raise RuntimeError(f"Could not reach Ollama at {url}: {e}")

    def test(self, patch=None):
        """Test connectivity for the given (or current) provider without persisting."""
        with self._lock:
            pref = (patch or {}).get("provider") or self.preference
            key = (patch or {}).get("anthropic_api_key")
            if key is None:
                key = self.anthropic_key or ""
            ollama_url = ((patch or {}).get("ollama_base_url") or self.ollama_url or "").rstrip("/")
            ollama_model = (patch or {}).get("ollama_model") or self.ollama_model

        target = pref
        if pref == "auto":
            target = "anthropic" if (key and len(key) > 20) else "ollama"

        if target == "simulation":
            return {"ok": True, "provider": "simulation", "detail": "Rule-based simulation is always available."}

        if target == "anthropic":
            if not key or len(key) < 20:
                return {"ok": False, "provider": "anthropic", "detail": "No valid Anthropic API key configured."}
            try:
                body = json.dumps({
                    "model": MODEL_PROFILES["fast"],
                    "max_tokens": 16,
                    "messages": [{"role": "user", "content": "ping"}],
                }).encode("utf-8")
                req = urllib.request.Request(
                    "https://api.anthropic.com/v1/messages", data=body, method="POST",
                    headers={"Content-Type": "application/json", "x-api-key": key,
                             "anthropic-version": "2023-06-01"})
                with urllib.request.urlopen(req, timeout=15) as r:
                    ok = r.status == 200
                return {"ok": ok, "provider": "anthropic", "detail": "Anthropic API reachable and key accepted."}
            except urllib.error.HTTPError as e:
                detail = e.read().decode("utf-8", "ignore")[:200]
                return {"ok": False, "provider": "anthropic", "detail": f"HTTP {e.code}: {detail}"}
            except Exception as e:
                return {"ok": False, "provider": "anthropic", "detail": str(e)}

        # ollama
        try:
            with urllib.request.urlopen(ollama_url + "/api/tags", timeout=4.0) as r:
                data = json.loads(r.read().decode("utf-8"))
            models = [m.get("name") for m in data.get("models", []) if m.get("name")]
            have = ollama_model in models or any((m or "").split(":")[0] == ollama_model.split(":")[0] for m in models)
            detail = f"Ollama reachable. {len(models)} model(s) available."
            if not have:
                detail += f" Warning: '{ollama_model}' not found — run `ollama pull {ollama_model}`."
            return {"ok": True, "provider": "ollama", "detail": detail, "models": models, "model_present": have}
        except Exception as e:
            return {"ok": False, "provider": "ollama", "detail": f"Could not reach Ollama at {ollama_url}: {e}"}


provider = Provider()


# ─── JSON extraction ────────────────────────────────────────────────────────────

def extract_json(text):
    text = (text or "").strip()
    if text.startswith("```"):
        # strip a leading ```json / ``` fence and trailing fence
        text = re.sub(r"^```[a-zA-Z]*\n?", "", text)
        text = re.sub(r"\n?```$", "", text).strip()
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        m = re.search(r"\{[\s\S]*\}", text)
        if m:
            return json.loads(m.group())
        raise ValueError("Could not parse JSON from model response: " + text[:200])


# ─── Providers ──────────────────────────────────────────────────────────────────

def call_anthropic(task, inputs, model_id, overrides=None):
    overrides = overrides or {}
    spec = TASK_SPECS[task]
    system_prompt = overrides.get("system_prompt") or spec["system_prompt"]
    instructions = overrides.get("instructions") or ""
    user_content = "Analyze the following data:\n\n" + json.dumps(inputs, indent=2)
    if instructions:
        user_content += "\n\nAdditional instructions: " + instructions
    user_content += "\n\nReturn ONLY valid JSON as specified."

    body_obj = {
        "model": model_id,
        "max_tokens": int(overrides.get("max_tokens") or 1024),
        "system": system_prompt,
        "messages": [{"role": "user", "content": user_content}],
    }
    if overrides.get("temperature") is not None:
        body_obj["temperature"] = float(overrides["temperature"])
    body = json.dumps(body_obj).encode("utf-8")

    req = urllib.request.Request(
        "https://api.anthropic.com/v1/messages",
        data=body, method="POST",
        headers={
            "Content-Type": "application/json",
            "x-api-key": provider.anthropic_key,
            "anthropic-version": "2023-06-01",
        },
    )
    start = time.time()
    with urllib.request.urlopen(req, timeout=60) as r:
        data = json.loads(r.read().decode("utf-8"))
    latency = int((time.time() - start) * 1000)
    usage = data.get("usage", {})
    tokens = usage.get("input_tokens", 0) + usage.get("output_tokens", 0)
    text = "".join(part.get("text", "") for part in data.get("content", []))
    return extract_json(text), tokens, latency


def call_ollama(task, inputs, model, overrides=None):
    overrides = overrides or {}
    spec = TASK_SPECS[task]
    system_prompt = overrides.get("system_prompt") or spec["system_prompt"]
    instructions = overrides.get("instructions") or ""
    prompt = "Analyze the following data:\n\n" + json.dumps(inputs, indent=2)
    if instructions:
        prompt += "\n\nAdditional instructions: " + instructions
    prompt += "\n\nReturn ONLY valid JSON as specified."

    options = {"temperature": 0.2, "top_p": 0.9}
    if overrides.get("temperature") is not None:
        options["temperature"] = float(overrides["temperature"])
    if overrides.get("max_tokens"):
        options["num_predict"] = int(overrides["max_tokens"])

    body = json.dumps({
        "model": model,
        "system": system_prompt,
        "prompt": prompt,
        "format": "json",
        "stream": False,
        "options": options,
    }).encode("utf-8")

    req = urllib.request.Request(
        provider.ollama_url + "/api/generate",
        data=body, method="POST",
        headers={"Content-Type": "application/json"},
    )
    start = time.time()
    with urllib.request.urlopen(req, timeout=120) as r:
        data = json.loads(r.read().decode("utf-8"))
    latency = int((time.time() - start) * 1000)
    text = data.get("response", "")
    tokens = data.get("prompt_eval_count", 0) + data.get("eval_count", 0)
    return extract_json(text), tokens, latency


def simulate_decision(task, inputs):
    decision, confidence, risk_score = "APPROVE", 0.87, 22
    reasoning = "Rule-based simulation. Configure an Anthropic API key or Ollama in Settings for real AI decisions."
    flags = []

    if task == "fraud_risk_assessment":
        amount = inputs.get("transaction", {}).get("amount") or inputs.get("amount", 0)
        if isinstance(amount, (int, float)):
            if amount > 5000:
                decision, confidence, risk_score = "ESCALATE", 0.71, 68
                flags = [{"code": "HIGH_AMOUNT", "description": f"Transaction amount {amount} exceeds threshold", "severity": "HIGH"}]
            elif amount > 2000:
                decision, confidence, risk_score = "ESCALATE", 0.67, 45
                flags = [{"code": "MEDIUM_AMOUNT", "description": f"Transaction amount {amount} warrants review", "severity": "MEDIUM"}]
        return {"decision": decision, "confidence": confidence, "risk_score": risk_score, "reasoning": reasoning, "flags": flags}

    if task == "credit_risk_assessment":
        amount = inputs.get("loan_amount", 0)
        if isinstance(amount, (int, float)) and amount > 100000:
            decision, confidence, risk_score = "ESCALATE", 0.74, 55
        return {"decision": decision, "confidence": confidence, "risk_score": risk_score, "reasoning": reasoning, "suggested_amount": None, "conditions": [], "flags": flags}

    if task == "content_moderation":
        content = str(inputs.get("content", "")).lower()
        if any(w in content for w in ["spam", "hate", "violence", "scam"]):
            decision, confidence, risk_score = "REJECT", 0.95, 88
            flags = [{"code": "POLICY_VIOLATION", "description": "Content violates community guidelines", "severity": "HIGH"}]
        return {"decision": decision, "confidence": confidence, "risk_score": risk_score, "reasoning": reasoning, "categories": [], "flags": flags}

    if task == "sentiment_analysis":
        return {"decision": "APPROVE", "confidence": 0.88, "sentiment": "NEUTRAL", "urgency": "LOW",
                "intent": "INQUIRY", "reasoning": reasoning, "suggested_response_tier": "STANDARD", "flags": []}

    if task == "invoice_approval":
        amount = inputs.get("amount") or inputs.get("invoice", {}).get("amount", 0)
        has_po = inputs.get("po_number") or inputs.get("matched_po")
        if isinstance(amount, (int, float)) and amount > 10000:
            return {"decision": "ESCALATE", "confidence": 0.72, "risk_score": 60, "matched_po": bool(has_po),
                    "reasoning": reasoning, "flags": [{"code": "HIGH_VALUE", "description": f"Invoice {amount} above auto-approve threshold", "severity": "HIGH"}]}
        if not has_po:
            return {"decision": "ESCALATE", "confidence": 0.66, "risk_score": 45, "matched_po": False,
                    "reasoning": reasoning, "flags": [{"code": "NO_PO", "description": "No matching purchase order", "severity": "MEDIUM"}]}
        return {"decision": "APPROVE", "confidence": 0.9, "risk_score": 15, "matched_po": True, "reasoning": reasoning, "flags": []}

    if task == "expense_audit":
        amount = inputs.get("amount", 0)
        if isinstance(amount, (int, float)) and amount > 1000:
            return {"decision": "ESCALATE", "confidence": 0.7, "risk_score": 50, "policy_violations": ["ABOVE_LIMIT"],
                    "reasoning": reasoning, "flags": [{"code": "ABOVE_LIMIT", "description": f"Expense {amount} exceeds policy limit", "severity": "MEDIUM"}]}
        return {"decision": "APPROVE", "confidence": 0.88, "risk_score": 12, "policy_violations": [], "reasoning": reasoning, "flags": []}

    if task == "lead_scoring":
        score = 30
        seniority = str(inputs.get("seniority", inputs.get("role", ""))).lower()
        if any(w in seniority for w in ["vp", "chief", "head", "director", "c-level", "ceo", "cto", "cfo"]):
            score = 85
        elif any(w in seniority for w in ["manager", "lead", "senior"]):
            score = 60
        tier = "HOT" if score >= 80 else "WARM" if score >= 55 else "COLD" if score >= 30 else "DISQUALIFIED"
        decision = "APPROVE" if score >= 55 else "ESCALATE" if score >= 30 else "REJECT"
        owner = "SALES" if score >= 55 else "NURTURE" if score >= 30 else "SELF_SERVE"
        return {"decision": decision, "confidence": 0.8, "score": score, "tier": tier,
                "reasoning": reasoning, "suggested_owner": owner, "flags": []}

    if task == "supply_chain_exception":
        sev = str(inputs.get("severity", "")).lower()
        if sev in ("critical", "high") or inputs.get("stockout_risk"):
            return {"decision": "ESCALATE", "confidence": 0.73, "risk_score": 70, "recommended_action": "Expedite replenishment / notify planner",
                    "reasoning": reasoning, "flags": [{"code": "STOCKOUT_RISK", "description": "Potential stockout / critical exception", "severity": "HIGH"}]}
        return {"decision": "APPROVE", "confidence": 0.85, "risk_score": 20, "recommended_action": "Auto-reorder within policy", "reasoning": reasoning, "flags": []}

    if task == "offboarding_review":
        if inputs.get("legal_hold") or inputs.get("disputed") or str(inputs.get("status", "")).lower() == "disputed":
            return {"decision": "ESCALATE", "confidence": 0.78, "risk_score": 65, "required_actions": ["HR/legal sign-off"],
                    "reasoning": reasoning, "flags": [{"code": "LEGAL_HOLD", "description": "Case requires human sign-off before deprovisioning", "severity": "HIGH"}]}
        return {"decision": "APPROVE", "confidence": 0.9, "risk_score": 10,
                "required_actions": ["Revoke accounts", "Reclaim devices", "Final paperwork"], "reasoning": reasoning, "flags": []}

    return {"decision": decision, "confidence": confidence, "risk_score": risk_score, "reasoning": reasoning, "flags": flags}


def _simulate_with_meta(task, inputs):
    """Run the rule-based simulator and return it in the (output, tokens, latency, label) shape."""
    start = time.time()
    output = simulate_decision(task, inputs)
    latency = int((time.time() - start) * 1000)
    return output, 0, latency, "simulation"


def _call_ollama_with_retry(task, inputs, model, overrides, attempts=2):
    """Call Ollama, retrying once on an empty/unparseable response. Ollama can return
    an empty 'response' on the first token-stream hiccup; a single retry usually fixes it."""
    last = None
    for i in range(attempts):
        try:
            out, tokens, latency = call_ollama(task, inputs, model, overrides)
            # Guard: an empty dict means the model returned nothing useful.
            if out:
                return out, tokens, latency
            last = ValueError("empty model response")
        except Exception as e:
            last = e
        time.sleep(0.4 * (i + 1))
    raise last if last else RuntimeError("ollama call failed")


def make_decision(req):
    task = req.get("task")
    if task not in TASK_SPECS:
        raise ValueError(f"Unknown task: '{task}'. Available: {list(TASK_SPECS.keys())}")

    inputs = req.get("inputs", {}) or {}
    model_profile = req.get("model_profile", "default")
    threshold = float(req.get("confidence_threshold", 0.8))
    overrides = {
        "system_prompt": req.get("system_prompt"),
        "instructions": req.get("instructions"),
        "temperature": req.get("temperature"),
        "max_tokens": req.get("max_tokens"),
    }

    use = provider.type
    if use == "anthropic":
        # Anthropic profiles only; an ollama_* profile on Anthropic falls back to default.
        model_id = MODEL_PROFILES.get(model_profile, MODEL_PROFILES["default"])
        if model_profile.startswith("ollama_"):
            model_id = MODEL_PROFILES["default"]
        try:
            output, tokens, latency = call_anthropic(task, inputs, model_id, overrides)
            model_label = f"anthropic:{model_id}"
        except Exception as e:
            log.warning("Anthropic call failed (%s); falling back to simulation for task=%s", e, task)
            output, tokens, latency, model_label = _simulate_with_meta(task, inputs)
    elif use == "ollama":
        # FIX: never send an Anthropic model id to Ollama. Map ollama_* profiles to
        # their model, otherwise use the operator-configured default Ollama model.
        if model_profile.startswith("ollama_") and model_profile in MODEL_PROFILES:
            model_id = MODEL_PROFILES[model_profile]
        else:
            model_id = provider.ollama_model
        try:
            output, tokens, latency = _call_ollama_with_retry(task, inputs, model_id, overrides)
            model_label = f"ollama:{model_id}"
        except Exception as e:
            # A slow/empty/unreachable local model must not kill the workflow run.
            log.warning("Ollama call failed (%s); falling back to simulation for task=%s", e, task)
            output, tokens, latency, model_label = _simulate_with_meta(task, inputs)
    else:
        output, tokens, latency, model_label = _simulate_with_meta(task, inputs)

    log.info("Decision: task=%s run=%s provider=%s model=%s", task, req.get("run_id", ""), use, model_label)

    confidence = float(output.get("confidence", 0.5))
    routing = "auto" if confidence >= threshold else "escalate"
    return {
        "output": output,
        "confidence": confidence,
        "reasoning": output.get("reasoning", ""),
        "model_id": model_label,
        "tokens_used": tokens,
        "latency_ms": latency,
        "routing": routing,
    }


# ─── Workflow generation ──────────────────────────────────────────────────────
# Turn a plain-English prompt into a runnable KNOTT workflow definition. This is
# for users who can't yet build graphs by hand: describe the automation in words,
# get a complete trigger → … → end graph back, ready to review and save.

WORKFLOW_BUILDER_SYSTEM = """You are KNOTT's workflow architect. You convert a plain-English automation
description into a STRICT JSON workflow definition that KNOTT's execution engine can run directly.

Output ONLY a single JSON object — no markdown, no commentary — with this exact shape:
{
  "name": "<short title>",
  "description": "<one sentence>",
  "tags": ["generated"],
  "trigger": {"type": "manual|webhook|schedule|polling", "input_schema": { "<field>": {"type":"string|number|boolean","required":true} }},
  "steps": [ <node>, ... ]
}

Each <node> object:
{
  "id": "<unique_snake_id>",
  "type": "<node type, see below>",
  "name": "<human label>",
  "next": "<id of next node, or omit for terminal>",
  "config": { ... type-specific ... },
  "inputs": { ... optional resolved inputs ... },
  "position": {"x": <int>, "y": <int>}
}

AVAILABLE NODE TYPES and their config:
- "trigger": entry point. config.input_schema optional. ALWAYS the first node, id "start".
- "ai_decision": config.task (one of the TASK list below) + config.confidence_threshold (0-1). inputs map field→value. Outputs steps.<id>.output.decision (APPROVE|REJECT|ESCALATE), .confidence, .reasoning.
- "condition": branch. "cases":[{"condition":"<expr>","next":"<id>"}], "default":"<id>".
- "human_task": config.title, config.due_hours, config.assigned_roles (list). next_map {"APPROVE":"<id>","REJECT":"<id>"}.
- "tool_call": call a connector. config.connector (e.g. "slack","telegram","discord","github","jira","airtable","notion","hubspot","google_sheets","google_calendar","teams","stripe","database","http"), config.operation, plus operation params. Use {{ expressions }} for values.
- "set": config.fields {key: value-or-{{expr}}}. Builds/reshapes an object.
- "code": config.assignments {outKey: "<expression>"}. Compute values via the expression language.
- "filter": config.condition "<expr>"; passes through if true, else config.on_false "<id>" or ends.
- "loop": config.items "{{ <list expr> }}", config.body "<first body node id>", config.item_var "item". Iterates; per-item output collected at steps.<id>.output.results.
- "merge": config.sources ["<id>","<id>"], config.mode "object|combine".
- "wait": config.mode "duration", config.seconds N, config.unit "seconds|minutes|hours|days". Durable timer.
- "end": config terminal. "outcome": "<UPPER_LABEL>".

EXPRESSIONS: use {{ ... }} referencing input.<field>, steps.<id>.output.<path>, item (inside loops).
Functions available: upper,lower,trim,len,concat,replace,split,substring,contains,number,round,abs,min,max,default,coalesce,if,json,jsonparse,now,today,dateadd.

AI TASK list for ai_decision config.task: fraud_risk_assessment, credit_risk_assessment, content_moderation,
document_classification, sentiment_analysis, general_decision, invoice_approval, expense_audit, lead_scoring,
supply_chain_exception, offboarding_review. If none fit, use "general_decision".

RULES:
- First node MUST be a "trigger" with id "start".
- Every non-terminal node MUST have a valid "next" (or "cases"/"next_map") pointing to an existing id.
- At least one "end" node.
- Only reference connectors from the AVAILABLE list. If the user names an app we don't list, use "http" with a sensible config or the closest available connector, and note it in the description.
- Lay out nodes left-to-right: x increases ~240 per step, y around 160 (branches offset by ~140).
- Keep it minimal but complete and runnable. Prefer real connector operations over placeholders.
- CHOOSE THE MOST SPECIFIC ai_decision task that fits the domain (e.g. invoice_approval, lead_scoring, sentiment_analysis). Only use "general_decision" when truly nothing else fits.
- When a connector needs an id you cannot know (spreadsheet id, repo, channel), put a clearly-labeled placeholder like "REPLACE_ME_repo" and prefer pulling the value from input via {{ input.<field> }} so the user supplies it at runtime. Add those fields to trigger.input_schema.
Return ONLY the JSON object."""


def _provider_raw(system_prompt, user_prompt, max_tokens=2048):
    """Call the active provider with a free-form system+user prompt; return (text, tokens, latency, model_label)."""
    use = provider.type
    if use == "anthropic":
        body = json.dumps({
            "model": MODEL_PROFILES["default"],
            "max_tokens": max_tokens,
            "system": system_prompt,
            "messages": [{"role": "user", "content": user_prompt}],
        }).encode("utf-8")
        req = urllib.request.Request(
            "https://api.anthropic.com/v1/messages", data=body, method="POST",
            headers={"Content-Type": "application/json", "x-api-key": provider.anthropic_key,
                     "anthropic-version": "2023-06-01"})
        start = time.time()
        with urllib.request.urlopen(req, timeout=90) as r:
            data = json.loads(r.read().decode("utf-8"))
        latency = int((time.time() - start) * 1000)
        usage = data.get("usage", {})
        tokens = usage.get("input_tokens", 0) + usage.get("output_tokens", 0)
        text = "".join(p.get("text", "") for p in data.get("content", []))
        return text, tokens, latency, f"anthropic:{MODEL_PROFILES['default']}"
    if use == "ollama":
        body = json.dumps({
            "model": provider.ollama_model,
            "system": system_prompt,
            "prompt": user_prompt,
            "format": "json",
            "stream": False,
            "options": {"temperature": 0.3, "num_predict": max_tokens},
        }).encode("utf-8")
        req = urllib.request.Request(
            provider.ollama_url + "/api/generate", data=body, method="POST",
            headers={"Content-Type": "application/json"})
        start = time.time()
        with urllib.request.urlopen(req, timeout=180) as r:
            data = json.loads(r.read().decode("utf-8"))
        latency = int((time.time() - start) * 1000)
        text = data.get("response", "")
        tokens = data.get("prompt_eval_count", 0) + data.get("eval_count", 0)
        return text, tokens, latency, f"ollama:{provider.ollama_model}"
    # simulation
    raise RuntimeError("simulation")


def _validate_and_normalize_workflow(wf):
    """Light structural validation + normalization of a generated workflow. Raises ValueError on hard failures."""
    if not isinstance(wf, dict):
        raise ValueError("workflow is not an object")
    steps = wf.get("steps")
    if not isinstance(steps, list) or not steps:
        raise ValueError("workflow has no steps")

    ids = set()
    for s in steps:
        if not isinstance(s, dict) or not s.get("id") or not s.get("type"):
            raise ValueError("each step needs id and type")
        ids.add(s["id"])

    # Ensure a trigger first node.
    if steps[0].get("type") != "trigger":
        # Promote/insert a trigger.
        if not any(s.get("type") == "trigger" for s in steps):
            steps.insert(0, {"id": "start", "type": "trigger", "name": "Start",
                             "next": steps[0]["id"], "position": {"x": 80, "y": 160}})
            ids.add("start")

    # Validate next references; drop dangling ones.
    for s in steps:
        nxt = s.get("next")
        if nxt and nxt not in ids:
            s.pop("next", None)
        for c in s.get("cases", []) or []:
            if isinstance(c, dict) and c.get("next") not in ids:
                c["next"] = None
        if isinstance(s.get("next_map"), dict):
            s["next_map"] = {k: v for k, v in s["next_map"].items() if v in ids}
        # Assign default positions if missing.
        if "position" not in s or not isinstance(s["position"], dict):
            s["position"] = {"x": 80, "y": 160}

    if not any(s.get("type") == "end" for s in steps):
        # Append a terminal end node and point the last non-terminal step at it.
        end = {"id": "end", "type": "end", "name": "Done", "outcome": "DONE",
               "position": {"x": 80 + 240 * len(steps), "y": 160}}
        for s in steps:
            if not s.get("next") and s.get("type") not in ("end", "condition", "human_task"):
                s["next"] = "end"
        steps.append(end)

    wf.setdefault("name", "Generated Workflow")
    wf.setdefault("description", "")
    tags = wf.get("tags")
    if not isinstance(tags, list):
        tags = []
    if "generated" not in tags:
        tags.append("generated")
    wf["tags"] = tags
    wf.setdefault("trigger", {"type": "manual"})
    return wf


def _simulate_workflow(prompt):
    """Deterministic fallback generator when no AI provider is configured. Produces a
    simple but valid trigger → ai_decision → condition → (human_task) → end graph."""
    p = (prompt or "").lower()
    task = "general_decision"
    for key, words in {
        "invoice_approval": ["invoice", "payable", "ap "],
        "expense_audit": ["expense", "reimburse"],
        "lead_scoring": ["lead", "sales", "prospect", "marketing"],
        "fraud_risk_assessment": ["fraud", "transaction", "payment risk"],
        "content_moderation": ["moderat", "content", "comment", "review post"],
        "sentiment_analysis": ["sentiment", "support ticket", "customer message", "feedback"],
        "supply_chain_exception": ["supply", "inventory", "shipment", "stockout", "warehouse"],
        "offboarding_review": ["offboard", "deprovision", "employee leaving", "termination"],
    }.items():
        if any(w in p for w in words):
            task = key
            break
    return {
        "name": "Generated: " + (prompt[:40].strip() or "Automation"),
        "description": "Auto-generated from prompt (rule-based fallback — configure an AI provider for richer graphs).",
        "tags": ["generated"],
        "trigger": {"type": "manual"},
        "steps": [
            {"id": "start", "type": "trigger", "name": "Start", "next": "assess", "position": {"x": 80, "y": 160}},
            {"id": "assess", "type": "ai_decision", "name": "AI Assessment",
             "config": {"task": task, "confidence_threshold": 0.85},
             "inputs": {"data": "{{ input }}"}, "next": "route", "position": {"x": 320, "y": 160}},
            {"id": "route", "type": "condition", "name": "Route on Decision",
             "cases": [{"condition": "steps.assess.output.decision == 'ESCALATE'", "next": "review"},
                       {"condition": "steps.assess.output.decision == 'REJECT'", "next": "rejected"}],
             "default": "approved", "position": {"x": 560, "y": 160}},
            {"id": "review", "type": "human_task", "name": "Human Review",
             "config": {"title": "Review required", "due_hours": 24, "assigned_roles": ["reviewer"]},
             "next_map": {"APPROVE": "approved", "REJECT": "rejected"}, "next": "approved",
             "position": {"x": 800, "y": 60}},
            {"id": "approved", "type": "end", "name": "Approved", "outcome": "APPROVED", "position": {"x": 1040, "y": 160}},
            {"id": "rejected", "type": "end", "name": "Rejected", "outcome": "REJECTED", "position": {"x": 800, "y": 300}},
        ],
    }


def _collect_workflow_warnings(wf):
    """Scan a generated workflow for things a human must fix before it will work in
    the real world: placeholder ids, generic AI tasks, and missing connector params.
    Returns a list of human-readable warnings (empty = ready to run as-is)."""
    warnings = []
    placeholder_markers = ("your-", "your_", "example.com", "xxxx", "<", "changeme",
                           "todo", "placeholder", "sheet-id", "appxxxx", "owner/name")

    def looks_placeholder(v):
        s = str(v).lower()
        return any(m in s for m in placeholder_markers)

    for s in wf.get("steps", []):
        nid = s.get("id", "?")
        cfg = s.get("config", {}) or {}
        if s.get("type") == "ai_decision" and cfg.get("task") == "general_decision":
            warnings.append(f"Node '{nid}' uses the generic 'general_decision' task — pick a more specific AI task if one fits.")
        if s.get("type") == "tool_call":
            for k, v in list(cfg.items()) + list((s.get("inputs") or {}).items()):
                if isinstance(v, (str, int)) and looks_placeholder(v):
                    warnings.append(f"Node '{nid}' has a placeholder value for '{k}' ({v!r}) — replace it before running.")
    return warnings


def generate_workflow(req):
    prompt = (req.get("prompt") or "").strip()
    if not prompt:
        raise ValueError("prompt is required")

    user_prompt = "Build a KNOTT workflow for this automation request:\n\n" + prompt
    ctx = req.get("context")
    if ctx:
        user_prompt += "\n\nAdditional context: " + json.dumps(ctx)[:1500]

    use = provider.type
    if use in ("anthropic", "ollama"):
        try:
            text, tokens, latency, model_label = _provider_raw(WORKFLOW_BUILDER_SYSTEM, user_prompt)
            wf = extract_json(text)
            wf = _validate_and_normalize_workflow(wf)
            return {"workflow": wf, "model_id": model_label, "tokens_used": tokens,
                    "latency_ms": latency, "generator": use,
                    "warnings": _collect_workflow_warnings(wf)}
        except Exception as e:
            log.warning("AI workflow generation failed (%s), falling back to rule-based: %s", use, e)

    wf = _validate_and_normalize_workflow(_simulate_workflow(prompt))
    return {"workflow": wf, "model_id": "simulation", "tokens_used": 0, "latency_ms": 0,
            "generator": "simulation", "warnings": _collect_workflow_warnings(wf)}


# ─── HTTP handler ───────────────────────────────────────────────────────────────

def health_payload():
    cfg = provider.config_payload()
    return {
        "status": "ok",
        "service": "ai-decision-engine",
        "port": os.environ.get("PORT", "8003"),
        "ai_provider": cfg["active_provider"],
        "provider_preference": cfg["provider"],
        "providers_available": {
            "anthropic": cfg["anthropic_configured"],
            "ollama": cfg["ollama_reachable"],
            "simulation": True,
        },
        "ollama_model": cfg["ollama_model"],
        "task_specs": list(TASK_SPECS.keys()),
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        log.info("%s - %s", self.address_string(), fmt % args)

    def _send(self, status, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "*")
        self.end_headers()
        self.wfile.write(body)

    def _read_body(self):
        try:
            length = int(self.headers.get("Content-Length", 0))
            return json.loads(self.rfile.read(length).decode("utf-8")) if length else {}
        except Exception:
            return {}

    def do_OPTIONS(self):
        self._send(204, {})

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path in ("/api/v1/health", "/internal/v1/health"):
            self._send(200, health_payload())
        elif path == "/internal/v1/config":
            self._send(200, provider.config_payload())
        elif path == "/internal/v1/ollama/models":
            try:
                self._send(200, {"data": provider.list_ollama_models()})
            except Exception as e:
                self._send(502, {"error": {"code": "OLLAMA_UNREACHABLE", "message": str(e)}})
        elif path == "/internal/v1/task-specs":
            self._send(200, {"data": [
                {"id": k, "name": v["name"], "description": v["description"]}
                for k, v in TASK_SPECS.items()
            ]})
        else:
            self._send(404, {"error": {"code": "NOT_FOUND", "message": path}})

    def do_PUT(self):
        path = self.path.split("?", 1)[0]
        if path == "/internal/v1/config":
            try:
                self._send(200, provider.update(self._read_body()))
            except Exception as e:
                self._send(400, {"error": {"code": "CONFIG_ERROR", "message": str(e)}})
        else:
            self._send(404, {"error": {"code": "NOT_FOUND", "message": path}})

    def do_POST(self):
        path = self.path.split("?", 1)[0]
        if path == "/internal/v1/config/test":
            self._send(200, provider.test(self._read_body()))
            return
        if path == "/internal/v1/generate-workflow":
            try:
                self._send(200, generate_workflow(self._read_body()))
            except ValueError as e:
                self._send(400, {"error": {"code": "BAD_REQUEST", "message": str(e)}})
            except Exception as e:
                log.error("Workflow generation failed: %s", e)
                self._send(502, {"error": {"code": "GENERATION_ERROR", "message": str(e)}})
            return
        if path != "/internal/v1/decisions":
            self._send(404, {"error": {"code": "NOT_FOUND", "message": path}})
            return
        req = self._read_body()
        try:
            self._send(200, make_decision(req))
        except ValueError as e:
            self._send(400, {"error": {"code": "BAD_TASK", "message": str(e)}})
        except urllib.error.HTTPError as e:
            detail = e.read().decode("utf-8", "ignore")[:300]
            log.error("Provider HTTP error: %s %s", e.code, detail)
            self._send(502, {"error": {"code": "PROVIDER_ERROR", "message": f"{e.code}: {detail}"}})
        except Exception as e:
            log.error("Decision failed: %s", e)
            self._send(502, {"error": {"code": "PROVIDER_ERROR", "message": str(e)}})


def main():
    provider.initialize()
    port = int(os.environ.get("PORT", 8003))
    host = os.environ.get("BIND_HOST", "0.0.0.0")
    log.info("KNOTT AI Decision Engine listening on %s:%d (provider=%s)", host, port, provider.type)
    ThreadingHTTPServer((host, port), Handler).serve_forever()


if __name__ == "__main__":
    main()
