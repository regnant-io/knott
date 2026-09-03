// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Package decide makes AI decisions inside the KNOTT binary.
//
// The Python decision engine is an optional sidecar. When it is not running —
// which is the common case, because a downloaded binary does not come with a
// Python interpreter — ai_decision nodes used to fail outright, so a workflow
// that ran on a developer's laptop stopped working the moment someone ran the
// release build. This package removes that dependency: it holds the same task
// specifications, calls Anthropic and Ollama directly, and falls back to the
// same deterministic rules.
//
// It stays behaviour-compatible with services/ai-decision-engine/main.py: same
// task ids, same prompts, same rule thresholds, same response shape. When both
// are available the Python service still answers, so the two must not disagree.
package decide

// TaskSpec describes one kind of decision KNOTT knows how to make.
type TaskSpec struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	SystemPrompt string `json:"-"`
}

// Specs returns the task catalogue in a stable order.
func Specs() []TaskSpec { return specs }

// Spec looks up a task by id.
func Spec(id string) (TaskSpec, bool) {
	for _, s := range specs {
		if s.ID == id {
			return s, true
		}
	}
	return TaskSpec{}, false
}

// The prompts below are shared with the Python engine. They all demand a bare
// JSON object because the caller parses the response — a model that wraps its
// answer in prose or a markdown fence produces a decision KNOTT cannot read.
var specs = []TaskSpec{
	{
		ID:          "fraud_risk_assessment",
		Name:        "Fraud Risk Assessment",
		Description: "Assess transaction fraud risk using behaviour and pattern analysis",
		SystemPrompt: `You are a senior fraud risk analyst at a major financial institution.
Assess whether a transaction shows signs of fraudulent activity. Consider amount vs. typical
behavior, geographic anomalies, merchant category risk, time patterns, and device anomalies.

Return ONLY a valid JSON object — no other text, no markdown:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

Rules: APPROVE when risk is clearly low (confidence >= 0.85, risk_score < 30).
REJECT when fraud is clearly evident (confidence >= 0.85, risk_score > 70).
ESCALATE for borderline cases. Return ONLY the JSON.`,
	},
	{
		ID:          "credit_risk_assessment",
		Name:        "Credit Risk Assessment",
		Description: "Evaluate loan application creditworthiness",
		SystemPrompt: `You are a senior credit underwriter. Assess loan creditworthiness from
the applicant data. Consider debt-to-income, credit history, loan purpose, employment stability.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 600 chars>","suggested_amount":<number or null>,"conditions":["<condition>"],
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}`,
	},
	{
		ID:          "content_moderation",
		Name:        "Content Moderation",
		Description: "Review content for policy violations",
		SystemPrompt: `You are a content policy enforcement specialist. Review content for
hate speech, harassment, violence, spam, misinformation, adult content, privacy violations.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 400 chars>","categories":["<violated_category>"],
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}`,
	},
	{
		ID:          "document_classification",
		Name:        "Document Classification",
		Description: "Classify document type and extract key metadata",
		SystemPrompt: `You are a document processing specialist. Classify the document type and
extract key metadata.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,
"document_type":"INVOICE|CONTRACT|REPORT|RECEIPT|ID_DOCUMENT|LEGAL|OTHER",
"extracted_data":{"<key>":"<value>"},"reasoning":"<max 300 chars>",
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH"}]}`,
	},
	{
		ID:          "sentiment_analysis",
		Name:        "Customer Sentiment Analysis",
		Description: "Analyse customer message sentiment, intent and urgency",
		SystemPrompt: `You are a customer experience analyst. Analyze the customer communication
for sentiment, intent, and urgency.

Return ONLY a valid JSON object:
{"decision":"APPROVE|ESCALATE|REJECT","confidence":<0.0-1.0>,
"sentiment":"POSITIVE|NEUTRAL|NEGATIVE|CRITICAL","urgency":"LOW|MEDIUM|HIGH|CRITICAL",
"intent":"COMPLAINT|INQUIRY|PRAISE|CANCELLATION_RISK|SUPPORT","reasoning":"<max 300 chars>",
"suggested_response_tier":"SELF_SERVICE|STANDARD|PRIORITY|EXECUTIVE","flags":[]}

APPROVE = standard handling, ESCALATE = priority needed.`,
	},
	{
		ID:          "general_decision",
		Name:        "General Decision",
		Description: "General-purpose approve/reject/escalate decision for any structured input",
		SystemPrompt: `You are an operations decision assistant. Review the provided data and make
a clear, well-reasoned decision.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

ESCALATE when the decision is ambiguous or needs human judgment. Return ONLY the JSON.`,
	},
	{
		ID:          "invoice_approval",
		Name:        "Invoice Approval",
		Description: "Validate and approve supplier invoices for payment (AP automation)",
		SystemPrompt: `You are an accounts-payable specialist. Review a supplier invoice for
approval. Check the amount against any PO/budget, validate vendor details, detect duplicates,
unusual amounts, math errors, and policy breaches (e.g. missing PO above threshold).

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","matched_po":<true|false|null>,
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

APPROVE clean low-value invoices matching a PO. ESCALATE high-value, missing-PO, or anomalous ones.
REJECT clear duplicates or invalid invoices. Return ONLY the JSON.`,
	},
	{
		ID:          "expense_audit",
		Name:        "Expense Report Audit",
		Description: "Audit employee expense reports against policy",
		SystemPrompt: `You are a corporate expense auditor. Review an expense report for policy
compliance. Check per-category limits, missing receipts, out-of-policy categories, weekend/duplicate
claims, and unusual amounts.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","policy_violations":["<violation>"],
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

ESCALATE borderline or above-threshold reports for manager review. Return ONLY the JSON.`,
	},
	{
		ID:          "lead_scoring",
		Name:        "Lead Qualification & Scoring",
		Description: "Qualify and score inbound leads (MQL/SQL routing)",
		SystemPrompt: `You are a demand-generation analyst. Score and qualify an inbound lead using
firmographic and behavioral signals (company size, role/seniority, industry fit, engagement, budget
signals). Decide routing.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"score":<0-100>,
"tier":"HOT|WARM|COLD|DISQUALIFIED","reasoning":"<max 400 chars>",
"suggested_owner":"SALES|NURTURE|SELF_SERVE","flags":[]}

APPROVE = route to sales now (HOT/WARM), ESCALATE = needs SDR review, REJECT = disqualified.
Return ONLY the JSON.`,
	},
	{
		ID:          "supply_chain_exception",
		Name:        "Supply Chain Exception",
		Description: "Triage supply-chain, inventory and shipment exceptions",
		SystemPrompt: `You are a supply-chain operations analyst. Triage an exception event
(stockout risk, delayed shipment, quality hold, demand spike, supplier delay). Assess severity and
recommend an action.

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","recommended_action":"<short action>",
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

APPROVE = auto-resolve (reorder/expedite within policy), ESCALATE = planner review, REJECT = no action.
Return ONLY the JSON.`,
	},
	{
		ID:          "offboarding_review",
		Name:        "Employee Offboarding Review",
		Description: "Review and authorise employee offboarding actions (HR and IT)",
		SystemPrompt: `You are an HR/IT offboarding coordinator. Review an offboarding case and
decide whether automated deprovisioning (revoke accounts, reclaim devices, final paperwork) can
proceed or needs human sign-off (e.g. legal hold, disputed termination, pending payroll).

Return ONLY a valid JSON object:
{"decision":"APPROVE|REJECT|ESCALATE","confidence":<0.0-1.0>,"risk_score":<0-100>,
"reasoning":"<max 500 chars>","required_actions":["<action>"],
"flags":[{"code":"<CODE>","description":"<desc>","severity":"LOW|MEDIUM|HIGH|CRITICAL"}]}

APPROVE = proceed with automated deprovisioning, ESCALATE = HR/legal sign-off needed. Return ONLY the JSON.`,
	},
}

// ModelProfiles maps the profile a workflow author picks to a concrete model.
// Aliases rather than dated snapshots, so a retired snapshot does not silently
// break every workflow — the previous dated defaults were retired in June 2026.
var ModelProfiles = map[string]string{
	"default":        "claude-sonnet-5",
	"high_accuracy":  "claude-opus-4-8",
	"fast":           "claude-haiku-4-5",
	"ollama_default": "llama3.1:latest",
	"ollama_fast":    "llama3.2:latest",
	"ollama_large":   "llama3.1:70b",
}
