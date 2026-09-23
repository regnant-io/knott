<div align="center">

<img src="brand/knott-icon.svg" width="72" height="72" alt="">

# KNOTT

**Workflow orchestration that stays on your infrastructure.**

Design workflows visually, run them durably, put a human in the loop where it
matters, and keep an audit trail of every decision — from one binary you own.

[![CI](https://github.com/regnant-io/knott/actions/workflows/ci.yml/badge.svg)](https://github.com/regnant-io/knott/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](https://go.dev)

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

**Windows**

Run `KNOTT-…-windows-x64-setup.exe` from
[Releases](https://github.com/regnant-io/knott/releases).
Choose per-user or all users, the folder,
shortcuts and the `knott` command.
A portable `.zip` needs no install.

</td><td width="33%">

**macOS**

Open `KNOTT-…-macos-arm64.dmg`
(or `-amd64` for Intel) and drag
KNOTT to Applications.

</td><td width="33%">

**Linux**

`knott-desktop` `.deb` / `.rpm` for the
app, `knott` for servers.

</td></tr>
</table>

KNOTT is a native desktop application — a real window over the operating
system's own web view (WebView2, WKWebView, WebKitGTK), with menus, a
single-instance lock and a clean shutdown that lets running workflows finish.
It is not a browser tab, and it does not need a browser installed.

On a server, the same platform is one binary with no window:

```bash
# Docker
docker run -p 8002:8002 -v knott-data:/var/lib/knott ghcr.io/regnant-io/knott

# The server binary (any OS)
knott serve --open           # console at http://localhost:8002

# From source (Go 1.26.8+, Node 22+)
git clone https://github.com/regnant-io/knott && cd knott
make ui && make run          # server + browser
make desktop-run             # the native desktop app
```

On first run, open **Workflows → Examples** for ten complete starter workflows
covering finance, support, supply chain and HR — or describe what you want on
an empty canvas and let the AI draft it.

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

**Building it.** Click the **+** on any step — or on a connection, to insert a
step between two others — and the node creator slides in beside the canvas.
Search steps, apps and app actions at once ("send slack message", "dedupe",
"approval"), browse by category, or drag any result to exactly where you want
it. `Tab` opens it from anywhere; double-click the canvas to add a step there.
The inspector configures the selected step in three tabs — Setup, Settings
(retries, timeouts, error routing) and Output (what it produced in the run on
screen).

**Steps you can use**

| | |
|---|---|
| **Triggers** | Manual, webhook, schedule (interval, daily, cron), polling for new items |
| **AI** | AI Prompt (write, summarise, extract JSON), AI Decision (with a confidence threshold), external agents |
| **Apps** | 177 connectors, HTTP Request for any API, Run workflow (sub-workflows) |
| **Flow** | If / Switch, Filter, Loop, Parallel, Merge, Wait, Stop and error, End |
| **Data** | Set fields, Expression, Transform, Sort, Limit, Remove duplicates, Filter items, Map items, Aggregate, Date & time, Crypto |
| **Human** | Review tasks — approve, reject or fill in a form, with SLAs |

**When something fails.** Steps that can fail have a second, red output. Draw a
line from it and that is where the run goes when the step fails — after its
retries, with the failure available to the branch as `error`. Retries back off
exponentially with jitter, so a rate-limited API is not hammered and replicas do
not retry in lockstep.

**Runs are durable.** Each step checkpoints its resolved forward edge, so a
restart resumes where it left off without re-firing a side effect that already
happened. A distributed lease means exactly one replica executes a given run.

**AI, wherever you want it.** Install [Ollama](https://ollama.com), pull any
model, and KNOTT uses it — no configuration: it finds the local server
(honouring `OLLAMA_HOST`), picks an installed model, and keeps it loaded
between steps. Or add an Anthropic API key. With neither, AI Decision steps
fall back to deterministic rules that escalate anything they cannot clear, and
the audit log says so. Everything runs inside the binary; Python is not needed.

---

## Connectors

**177 integrations** across CRM, marketing and analytics, e-commerce and
logistics, finance, developer tools, databases and vector stores, AI models,
communication, customer support, productivity, HR, healthcare (FHIR), education,
legal and e-signature, maps and data, smart home and social media — plus HTTP
Request and GraphQL for everything else.

The Connectors page lists them all, filterable by category and status. Each app
opens a drawer with the exact credentials it needs — with a line telling you
where to find each value — a button that makes a harmless live call to check
them, and the actions it offers. Credentials are encrypted at rest and never
shown again once saved.

**Adding one is a JSON entry.** A connector is a definition in
[`internal/connectors/defs`](internal/connectors/defs): its credentials, how it
authenticates, and the HTTP request each action makes. The engine runs it and
the console renders its forms from the same definition — no Go, no React. See
[docs/connectors.md](docs/connectors.md).

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
| `KNOTT_ALLOWED_ORIGINS` | Extra browser origins allowed to call the API (the console's own origin always is). Alias: `CORS_ORIGINS` |
| `KNOTT_ALLOWED_HOSTS` | Extra host names a loopback-bound server answers to, e.g. behind a local proxy |
| `KNOTT_HOME` | State directory. Defaults to the per-OS application data path |
| `PORT`, `KNOTT_BIND_HOST` | Where to listen. Defaults to `127.0.0.1:8002` |
| `OLLAMA_HOST` / `OLLAMA_BASE_URL` | Where Ollama listens (detected automatically on `127.0.0.1:11434`). `KNOTT_DETECT_OLLAMA=0` turns detection off |
| `ANTHROPIC_API_KEY` | Use Anthropic Claude. Also settable in Settings → AI |
| `KNOTT_ENV_SECRETS` | `off` stops workflows reading credentials from environment variables (stored credentials only) |
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
