# ⊗ KNOTT
### Sovereign Workflow Orchestration Platform

**Self-hosted, AI-powered workflow automation with human-in-the-loop decisions, a visual designer, real-time execution monitoring, and a full audit trail — with zero cloud lock-in.**

KNOTT is built for organizations that want the power of an automation platform like n8n, but with **data sovereignty**: run every AI decision locally with Ollama, keep all data on your own infrastructure, and expose a single port to the outside world.

---

## Why KNOTT

- **Sovereign by default** — run 100% offline with local Ollama models. No data leaves your network.
- **AI with a human in the loop** — automatic escalation to human reviewers when AI confidence is low, with a required justification on every decision for compliance.
- **Immutable audit trail** — every AI decision, human approval, and node transition is recorded.
- **Runtime-configurable AI** — switch providers, set models, and test connectivity from the Settings page. No restart, no editing files on the server.
- **Real-world triggers** — start workflows from inbound webhooks, not just manual runs.
- **No heavy runtime** — Go services use a pure-Go SQLite driver (no CGO); the AI engine uses only the Python standard library (no `pip install`).

---

## 🚀 Quick Start

### Prerequisites
- **Go 1.22+** — only to build the backend services
- **Python 3.9+** — runs the AI engine (standard library only)
- **Node.js 18+** — only to rebuild the frontend
- **AI provider (optional)** — Anthropic Claude (cloud) or Ollama (local). Falls back to a rule-based simulation if neither is configured.

### 1. Configure environment

```bash
cp .env.example .env
# Optionally set ANTHROPIC_API_KEY or OLLAMA_BASE_URL — or configure later in the UI.
```

You no longer have to choose the AI provider in `.env`. The `.env` values are just
the initial defaults — everything can be changed at runtime on the **Settings** page.

> **Tip:** on first launch, open **Workflows → Examples** to load ready-made starter
> workflows for finance, marketing, supply chain, and HR/IT. Each is a complete
> trigger → AI decision → human-review → outcome graph you can run immediately.

### 2. Start the platform

**Windows:** `start.bat`
**Linux/macOS:** `chmod +x start.sh stop.sh && ./start.sh`

### 3. Open the platform

```
→ http://localhost:8002
```

### 4. Configure AI in the UI

Go to **Settings → AI Provider Configuration**:
1. Pick a provider: **Auto**, **Anthropic**, **Ollama**, or **Simulation**.
2. For Ollama, set the base URL, click **refresh** to list installed models, and pick one.
3. Click **Test Connection** to verify, then **Save Configuration**. The choice is persisted and survives restarts.

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                          KNOTT Platform                          │
├──────────────────┬──────────────────┬───────────────────────────┤
│  Workflow        │  Execution       │  AI Decision Engine        │
│  Registry        │  Engine          │  Python stdlib             │
│  Go + Chi        │  Go + Goroutines │  Anthropic / Ollama / Sim  │
│  :8001           │  :8002           │  :8003                     │
├──────────────────┼──────────────────┼───────────────────────────┤
│  Human Task      │  Agent           │  React Frontend            │
│  Service         │  Integration     │  Vite + React Flow         │
│  Go + Chi :8004  │  Go + Chi :8005  │  → served on :8002         │
└──────────────────┴──────────────────┴───────────────────────────┘
                         │
                    SQLite Databases  (./data/*.db)
```

> **Single-port deployment:** in production the Execution Engine (8002) serves the
> built UI *and* reverse-proxies the registry, task, agent, and AI-engine APIs.
> Clients expose only **port 8002**; 8001/8003/8004/8005 stay internal.

| Service | Port | Purpose |
|---------|------|---------|
| **Workflow Registry** | 8001 | Definitions, versioning, validation |
| **Execution Engine** | 8002 | Orchestration, state, webhooks, UI hosting, single-port proxy |
| **AI Decision Engine** | 8003 | Anthropic / Ollama / simulation, runtime config, task specs |
| **Human Task Service** | 8004 | HITL queue, approvals, SLA tracking |
| **Agent Integration** | 8005 | External agent registry & health |

---

## 🔌 Triggering workflows from the real world

Any external system can start a workflow run by POSTing JSON to the inbound webhook:

```bash
curl -X POST http://localhost:8002/api/v1/hooks/<workflow_id> \
  -H "Content-Type: application/json" \
  -d '{ "transaction_id": "TXN-12345", "amount": 5000 }'
```

The request body becomes the run input, available downstream as `{{ input.* }}`.
Returns `202 Accepted` with the new `run_id`.

You can also start runs from the UI, or via the API:

```bash
curl -X POST http://localhost:8002/api/v1/runs \
  -H "Content-Type: application/json" \
  -d '{ "workflow_id": "...", "input_data": { "amount": 5000 } }'
```

---

## 🤖 AI providers

| Provider | Best for | Notes |
|----------|----------|-------|
| **Ollama** | Sovereignty, offline, cost control | Local models (Llama 3.1/3.2, Mistral, …). No data egress. GPU recommended. |
| **Anthropic** | Highest quality | Cloud Claude models. Requires API key + internet. |
| **Simulation** | Testing / demos | Deterministic rule-based fallback. Always available. |

Configure and test all of this at runtime from **Settings**. The active provider
is shown live in the sidebar status and on the Settings page.

### Runtime AI configuration API

```bash
GET  /internal/v1/config             # current provider config
PUT  /internal/v1/config             # update provider / key / ollama url+model (persisted)
POST /internal/v1/config/test        # test connectivity without saving
GET  /internal/v1/ollama/models      # list models installed in Ollama
```

(These are proxied through the engine on `:8002` for single-port deployments.)

---

## ⚡ Triggers — how workflows start

The **Trigger node is the source of truth** for how a workflow runs. Pick a
`trigger_type` in the node and the engine reconciles it from the saved workflow
(self-healing, survives restarts — no manual wiring):

| Trigger type | How it starts | Config |
|---|---|---|
| **Manual** | Operator clicks Run, or `POST /api/v1/runs` | — |
| **Webhook** | External system POSTs JSON | shows the live URL; optional HMAC signing |
| **Schedule** | interval / daily / cron | auto-registers a schedule (also on the Schedules page) |
| **Polling** | engine checks a source on an interval, fires a run per *new* item | source (HTTP or connector), items path, dedup key, interval, max/poll |
| **Email** | inbound email (via your mail provider's inbound-parse webhook) | points at the webhook URL; native IMAP polling on the roadmap |

**Polling** is the key autonomous capability for systems that don't push
webhooks (most enterprise apps). On each interval the engine fetches the source,
extracts items via `items_path`, dedups by `dedup_key`, and fires one run per
new item with the item available as `{{ input.item }}`. On first activation it
records existing items as seen *without* replaying them (toggle with
`fire_on_first`). A **Test Poll** button shows exactly what would be processed.

Trigger management API:
```
GET  /api/v1/triggers/polls       # active polling triggers + cursor state
POST /api/v1/triggers/test-poll   # dry poll: returns items + dedup keys, fires nothing
GET  /api/v1/schedules            # registered schedules
```

## 🔌 Connectors & operations

Tool Call nodes ship with first-class connectors. Pick a connector, choose an
operation, fill a few fields — credentials are referenced by name from the
encrypted store, never inline.

| Connector | Operations | Credential(s) |
|-----------|-----------|---------------|
| HTTP / Webhook | any method, query, headers, auth (bearer/basic/api-key), JSON/form/raw body | per-request |
| Slack | post message | `SLACK_WEBHOOK_URL` or `SLACK_BOT_TOKEN` |
| Telegram | send message | `TELEGRAM_BOT_TOKEN` |
| Discord | send message | `DISCORD_WEBHOOK_URL` |
| GitHub | create issue, comment, close, get, list issues | `GITHUB_TOKEN` |
| Jira | create issue, comment issue | `JIRA_EMAIL`, `JIRA_API_TOKEN`, `JIRA_BASE_URL` |
| Airtable | create / update / list records | `AIRTABLE_TOKEN` |
| Notion | create page, query database | `NOTION_TOKEN` |
| HubSpot | create contact, create deal | `HUBSPOT_TOKEN` |
| Google Sheets | append row, read range | OAuth: `GOOGLE_CLIENT_ID` + `GOOGLE_CLIENT_SECRET` + `GOOGLE_REFRESH_TOKEN` (auto-refreshed), or a short-lived `GOOGLE_ACCESS_TOKEN` |
| Google Calendar | create event | same Google OAuth as Sheets |
| Microsoft Teams | send message | `TEAMS_WEBHOOK_URL` |
| Stripe | create customer, create charge | `STRIPE_SECRET_KEY` |
| Database (SQL) | query, exec | `DATABASE_DSN` (SQLite built-in; Postgres/MySQL need a driver build) |
| SendGrid | send email | `SENDGRID_API_KEY`, `SENDGRID_FROM` |
| Twilio | send SMS | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM_NUMBER` |

All connectors accept an optional `base_url` to target self-hosted/enterprise
instances (e.g. GitHub Enterprise, a Jira data-center site). Tool Call and Agent
Call nodes support an **Output Path** (e.g. `response.data.0.id`) to extract a
clean value for downstream steps, plus per-node timeout and on-error routing.

Every Tool Call node has a **Test Connector** button: it runs the connector live
with the current config and sample Test Data, showing the result or the exact
error — so you validate an integration before wiring it into a workflow.

The **Trigger** node can declare an **input schema** (field types, required
flags, defaults). The engine validates inbound run input against it and applies
defaults, so a workflow fully describes and guards the data it expects.

## 📈 Observability

The **Observability** page surfaces, across all runs:
- recent node/run **failures with their exact error text**,
- **retry volume** and tasks **awaiting humans**,
- a **per-node health table** (completed / failed / retries / failure rate).

Backed by `GET /api/v1/diagnostics`, computed live from the immutable event log.

## 🎨 Workflow node types

| Node | Purpose |
|------|---------|
| **Trigger** | Entry point (manual / webhook / schedule / polling / email) |
| **AI Decision** | Confidence-scored decision; Anthropic *or* Ollama model profiles |
| **Human Task** | Pause for review/approval with SLA |
| **Condition** | Branch on expressions (multi-case + default) |
| **Filter** | Pass when a condition is true; drop or route the rest |
| **Loop** | Iterate a list, running a body sub-path once per item (`{{ item }}`) |
| **Set Fields** | Build/modify a data object from templates |
| **Code** | Compute fields with the full expression language (no external runtime) |
| **Tool Call** | Real connectors with operations + Test button (HTTP, Slack, Telegram, Discord, GitHub, Jira, Airtable, Notion, HubSpot, Google Sheets/Calendar, Database, Stripe, SendGrid, Twilio, Teams) |
| **Agent Call** | Call a registered external agent (timeout, output path) |
| **Wait** | Durable timed delay (duration or until a timestamp) — survives restarts |
| **Parallel** | Fan-out branches |
| **Merge** | Combine outputs of multiple upstream nodes |
| **Transform** | Map outputs via templates |
| **End** | Terminal outcome (Approve/Reject/Complete/Escalate) |

Every node can be **disabled** (skipped at runtime) and annotated with **notes**.

## 🧮 Expressions

Fields support a real expression language inside `{{ }}`:
- **Paths:** `{{ input.amount }}`, `{{ steps.assess.output.decision }}`
- **Operators:** `+ - * /`, `== != > < >= <=`, `&& ||`, `??` (default), `!`
- **Functions:** `upper, lower, trim, len, concat, replace, split, substring, contains, number, round, abs, min, max, default, coalesce, if, json, jsonparse, now, today, dateadd`
- **Special vars:** `$now`, `$today`, `$timestamp`
- Examples: `{{ upper(input.name) }}` · `{{ input.email ?? 'none@x.com' }}` · `{{ if(input.score > 80, 'HOT', 'COLD') }}` · `{{ dateadd($now, 3, 'days') }}`

---

## 🛠️ Designer experience

- **Live expression preview:** any field using `{{ input.* }}` or `{{ steps.<id>.output.* }}` shows its resolved value as you type, evaluated against editable **Test Data** in the properties panel. Unresolved references are flagged so you catch typos before running.
- **Run replay:** from Run Monitor, replay any completed/failed run with one click — it starts a fresh run of the same workflow with the same input, so you can iterate quickly.
- **Per-node reliability:** retries, retry delay, timeout, and continue-on-error are editable per node.

## 🎨 Theming

KNOTT ships with full light/dark theming plus a **System** mode that follows the
operating system preference live. Switch from the sidebar (cycles System → Light →
Dark) or pick explicitly on the Settings page. Your choice is saved per device.

---

## 🔒 Security & deployment notes

- **Single port:** only expose `:8002`. The Execution Engine is the **only** trust boundary — it enforces auth and reverse-proxies the sibling services. Set `BIND_HOST=127.0.0.1` so the registry/AI/task/agent services bind to loopback only and cannot be reached directly (which would bypass auth). The included `infra/nginx/nginx.conf` routes *all* traffic through the engine over TLS.
- **API token:** set `API_TOKEN` to require authentication on every API call. The UI prompts for it once and stores it in the browser (sent as `X-API-Key`). When unset, the engine runs open and logs a warning — never expose KNOTT publicly without it.
- **Webhook signing:** set `WEBHOOK_SECRET` to require an HMAC-SHA256 signature on inbound webhooks. Callers send `X-KNOTT-Signature: sha256=<hex>` where the hex is `HMAC_SHA256(secret, raw_body)`. Missing/forged signatures are rejected with 401.
- **Secrets:** connector credentials (Slack/SendGrid/Twilio) and agent tokens are read from environment variables at runtime and are **never** stored in a workflow definition. Never commit real secrets to `.env` (a `.gitignore` excludes it).
- **UI credential store:** operators can manage connector secrets from **Connectors → Connector Credentials**. Values are encrypted at rest with AES-256-GCM (key derived from `KNOTT_SECRET_KEY`), are **write-only** (never returned by the API), and override the matching environment variable at runtime. Set a strong `KNOTT_SECRET_KEY` in production.
- **TLS:** put KNOTT behind a reverse proxy (see `infra/nginx/nginx.conf`) terminating TLS.
- **Audit:** every AI decision and human approval is recorded with timestamps and justification.

### Signing a webhook (example)

```bash
BODY='{"amount":5000}'
SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | sed 's/^.* //')
curl -X POST "https://your-host/api/v1/hooks/<workflow_id>" \
  -H "Content-Type: application/json" \
  -H "X-KNOTT-Signature: sha256=$SIG" \
  -d "$BODY"
```

---

## 🐳 Docker

```bash
cp .env.example .env
docker-compose up --build -d
# open http://localhost:8002
```

Ollama on the host is reached from containers via `host.docker.internal:11434`
(configured in `docker-compose.yml`).

---

## 🛠️ Development

```bash
# Rebuild Go services
cd services/workflow-registry  && go build -o ../../bin/workflow-registry .
cd services/execution-engine   && go build -o ../../bin/execution-engine .
cd services/human-task-service && go build -o ../../bin/human-task-service .
cd services/agent-integration  && go build -o ../../bin/agent-integration .

# Frontend
cd apps/designer && npm install && npm run build
```

Data lives in `./data/*.db` (SQLite). Logs in `./logs/`.

---

## 🆘 Troubleshooting

**AI engine shows simulation / offline** — open Settings, pick your provider, click Test Connection. For Ollama, ensure `ollama serve` is running and the model is pulled (`ollama pull llama3.1`).

**Workflow stuck in RUNNING** — check the run's event timeline in Run Monitor and the service logs in `./logs/`.

**Ports busy** — change them in `.env` (`REGISTRY_PORT`, `ENGINE_PORT`, …).

---

## 📄 License

**Proprietary Software** — © 2026 KNOTT. All rights reserved.

*KNOTT — workflows that tie your systems, your AI, and your people together, on your own terms.*
