<div align="center">

<img src="brand/knott-icon.svg" width="72" height="72" alt="">

# KNOTT

**Workflow orchestration that stays on your infrastructure.**

Design workflows visually, run them durably, put a human in the loop where it
matters, and keep an audit trail of every decision — from one binary you own.

[![CI](https://github.com/regnant/knott/actions/workflows/ci.yml/badge.svg)](https://github.com/regnant/knott/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.25+-00ADD8.svg)](https://go.dev)

[Install](#install) · [How it works](#how-it-works) · [Connectors](#connectors) · [Building workflows](docs/workflows.md) · [Deploying](docs/deployment.md) · [Contributing](CONTRIBUTING.md)

</div>

---

## Why KNOTT exists

Most automation platforms ask you to send your data through them. That is fine
until the data is a loan application, a patient record, or a payment you have to
be able to explain to an auditor three years later.

KNOTT runs entirely on your own machines. Workflows execute in your network,
credentials are encrypted in your database, and AI decisions can run against a
local model so nothing leaves the building. Every decision — which model made
it, how confident it was, what it reasoned, who approved it — is written to an
append-only log.

It is a single binary. No cluster, no message broker, no managed service.

---

## Install

<table>
<tr><td width="33%">

**macOS**

Download the `.dmg` from
[Releases](https://github.com/regnant/knott/releases),
drag KNOTT to Applications.

</td><td width="33%">

**Windows**

Download the `.msi` from
[Releases](https://github.com/regnant/knott/releases)
and run it.

</td><td width="33%">

**Linux**

`.deb`, `.rpm`, or the `.AppImage`
to run without installing.

</td></tr>
</table>

```bash
# Docker
docker run -p 8002:8002 -v knott-data:/var/lib/knott ghcr.io/regnant/knott

# Homebrew
brew install regnant/tap/knott

# From source (Go 1.25+, Node 18+)
git clone https://github.com/regnant/knott && cd knott
make ui && make run
```

Then:

```bash
knott desktop          # opens KNOTT in its own window
knott serve --open     # or just serve, and open a browser
```

The console is at **http://localhost:8002**. On first run, open
**Workflows → Examples** for ten complete starter workflows covering finance,
support, supply chain and HR — each a working trigger → decision → review →
outcome graph you can run immediately.

> KNOTT binds to loopback with authentication off, which is right for a laptop
> and wrong for a server. Set `API_KEYS` before exposing it — see
> [SECURITY.md](SECURITY.md).

---

## How it works

A workflow is a graph of steps. You draw it; KNOTT runs it.

```
   ┌─────────┐    ┌──────────────┐    ┌───────────────┐    ┌──────────┐
   │ Trigger │───▶│ AI Decision  │───▶│  Human Review │───▶│  Action  │
   │ webhook │    │  confidence  │    │  when unsure  │    │  Slack,  │
   │ schedule│    │  threshold   │    │               │    │  Jira, … │
   └─────────┘    └──────┬───────┘    └───────────────┘    └──────────┘
                         │ on error
                         ▼
                   ┌───────────┐
                   │ Escalate  │
                   └───────────┘
```

**Building it.** Click the **+** on any step and search for what you want the
next one to do — "slack", "wait", "approval". The step arrives already
connected. `Tab` opens the same search from anywhere. Undo, copy/paste and a
tidy-layout button work the way you expect.

**Steps you can use**

| | |
|---|---|
| **Trigger** | Webhook, schedule, polled source, or a manual run |
| **AI Decision** | A model decides, with a confidence threshold and a fallback |
| **Human Task** | Pauses for a person; approval, rejection and justification are recorded |
| **Condition** | One labelled output per branch, plus a default |
| **Connector** | Call an app or any HTTP endpoint |
| **Sub-workflow** | Run another workflow and use its result |
| **Loop / Parallel / Merge** | Iterate, fan out, fan back in |
| **Set / Expression / Filter / Wait** | Shape data, compute values, gate, pause |
| **Agent** | Hand work to a registered external agent |

**When something fails.** Steps that can fail have a second, red output. Draw a
line from it and that is where the run goes when the step fails — after its
retries, with the failure available to the branch as `error`. Retries back off
exponentially with jitter, so a rate-limited API is not hammered and replicas do
not retry in lockstep.

**Runs are durable.** Each step checkpoints its resolved forward edge, so a
restart resumes where it left off without re-firing a side effect that already
happened. A distributed lease means exactly one replica executes a given run.

**AI, wherever you want it.** Anthropic Claude, a local Ollama model, or a
deterministic rule-based simulation. Switch provider and model from the Settings
page — no restart, no editing files on the server. Set a confidence threshold
per step and low-confidence decisions escalate to a person automatically.

---

## Connectors

Around forty integrations ship in the box: Slack, Microsoft Teams, Discord,
Telegram, WhatsApp, SendGrid, Twilio, Outlook, Pushover, Mattermost · GitHub,
GitLab, Linear, Jira, Zendesk, Freshdesk, ServiceNow, PagerDuty · HubSpot,
Intercom, Close · Notion, Google Sheets, Google Calendar, Airtable, Trello,
Asana, ClickUp, Monday, Coda, Calendly · Stripe, Shopify, Mailchimp · OpenAI ·
SQL (SQLite, PostgreSQL, MySQL) · and generic HTTP and GraphQL for everything
else.

Each connector has its own card on the Connectors page: a switch, the exact
credentials it needs — each with a line telling you where to find the value —
and a button that makes a real call to check them. Credentials are encrypted at
rest and never shown again once saved.

Missing one? [Ask for it](https://github.com/regnant/knott/issues/new?template=connector.yml),
or add it — CONTRIBUTING.md has a walkthrough.

---

## Deploying

[docs/deployment.md](docs/deployment.md) covers this properly — backups,
monitoring, systemd, upgrades. The essentials:

### One node

The default. One binary, one port, SQLite on local disk. This comfortably runs
thousands of workflows a day.

```bash
API_KEYS='long-random-key:admin,readonly:viewer' \
KNOTT_SECRET_KEY="$(openssl rand -hex 32)" \
WEBHOOK_SECRET="$(openssl rand -hex 32)" \
knott serve --host 0.0.0.0
```

Put TLS in front of it — `infra/nginx/nginx.conf` is a working starting point.

### Several nodes

The four services also build as separate binaries (`knott-registry`,
`knott-engine`, `knott-tasks`, `knott-agents`) and can be scaled independently.
Run leases mean several engine replicas can share a queue safely.

```bash
make services
REGISTRY_URL=http://registry:8001 HUMAN_TASK_URL=http://tasks:8004 \
AGENT_URL=http://agents:8005 knott-engine
```

### Configuration

| Variable | What it does |
|---|---|
| `API_KEYS` | `key:role` pairs. Roles: `admin`, `operator`, `viewer` |
| `KNOTT_SECRET_KEY` | Encrypts stored credentials. Generated on first run if unset |
| `WEBHOOK_SECRET` | Requires an HMAC signature on inbound webhooks |
| `CORS_ORIGINS` | Restricts browser origins. Permissive by default |
| `KNOTT_HOME` | State directory. Defaults to the per-OS application data path |
| `PORT`, `KNOTT_BIND_HOST` | Where to listen. Defaults to `127.0.0.1:8002` |
| `ANTHROPIC_API_KEY` / `OLLAMA_BASE_URL` | AI provider. Also settable in the UI |
| `RUN_RETENTION_DAYS` | Prunes finished runs after this many days |
| `METRICS_TOKEN` | Gates `/metrics` behind a bearer token |

### Operating it

- `/metrics` — Prometheus: runs by status, decision counts and confidence,
  connector readiness, build info
- `/api/v1/system-health` — every service, checked server-side, so it works
  behind any proxy topology
- **Observability** page — run volume, failure rates, decision confidence
- **AI Decisions** page — the full audit log, searchable, with reasoning and
  confidence per decision

---

## Triggering a run

```bash
curl -X POST http://localhost:8002/api/v1/hooks/<workflow-id> \
  -H 'Content-Type: application/json' \
  -H "X-KNOTT-Signature: $(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" -r | cut -d' ' -f1)" \
  -d "$BODY"
```

Pass an `Idempotency-Key` header and a repeated delivery returns the original
run rather than starting a second one.

---

## Contributing

Issues and pull requests are welcome — [CONTRIBUTING.md](CONTRIBUTING.md) covers
the layout, the house style, and a walkthrough for adding a connector.

```bash
make check     # gofmt, vet, Go tests, console tests — what CI runs
```

---

## Licence

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).

"KNOTT" and the KNOTT mark are trademarks of Regnant; the licence covers the
code, not the marks. See the trademark section of
[CONTRIBUTING.md](CONTRIBUTING.md#trademarks) for what you may do without asking
(which is most things).

<div align="center">
<br>
Built by <a href="https://regnant.io">Regnant</a>
</div>
