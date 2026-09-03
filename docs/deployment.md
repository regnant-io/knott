# Deploying KNOTT

KNOTT is one process. Most of this page is about what to set before other people
can reach it, not about assembling infrastructure.

---

## The short version

```bash
API_KEYS="$(openssl rand -hex 32):admin" \
WEBHOOK_SECRET="$(openssl rand -hex 32)" \
KNOTT_SECRET_KEY="$(openssl rand -hex 32)" \
knott serve --host 0.0.0.0
```

Put TLS in front of it. Back up the state directory. That is a production
install.

---

## Choosing a shape

**One node.** One binary, SQLite on local disk. This is the default and it is
not a compromise: SQLite in WAL mode handles thousands of runs a day on
unremarkable hardware, and a single process removes an entire category of
operational problems. Start here.

**Several nodes.** The four services also build separately
(`make services` → `knott-registry`, `knott-engine`, `knott-tasks`,
`knott-agents`). Engine replicas coordinate through run leases, so several can
share a queue safely — each run is claimed by exactly one replica, and a replica
that dies has its runs reclaimed after `RUN_LEASE_TTL_SECONDS`.

You need this when a single node cannot keep up, or when you want the engine to
scale independently of the console. You do not need it to be "production".

**Containers.** One image, `ghcr.io/regnant/knott`. Mount a volume at
`/var/lib/knott` and set `KNOTT_SECRET_KEY` so stored credentials survive a
rebuild.

---

## Before you expose it

KNOTT executes workflows an operator authors, holds credentials for the services
those workflows call, and pauses for human approval. Anyone who can reach an
unauthenticated KNOTT can do all of that.

**1. Set `API_KEYS`.**

```
API_KEYS=<random>:admin,<random>:operator,<random>:viewer
```

Roles are enforced per request. `viewer` may only read. `operator` may create
and run workflows but cannot write credentials or change the AI provider. Give
people the narrowest role that works — an approver only needs `viewer` plus the
task queue.

KNOTT logs a warning at startup when it is bound off-loopback with no keys.

**2. Set `KNOTT_SECRET_KEY`.** Stored connector credentials are encrypted with
it. The binary generates one on first run and writes it to `secret.key` in the
state directory, owner-readable only. A container needs it supplied explicitly,
or a volume rebuild loses every stored credential.

**3. Set `WEBHOOK_SECRET`** if anything triggers workflows over HTTP. Callers
then have to sign the body:

```bash
BODY='{"invoice_id":"INV-42"}'
SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" -r | cut -d' ' -f1)
curl -X POST https://knott.example.com/api/v1/hooks/<workflow-id> \
     -H "X-KNOTT-Signature: $SIG" \
     -H 'Content-Type: application/json' \
     -d "$BODY"
```

Add an `Idempotency-Key` header and a repeated delivery returns the original run
instead of starting a second one.

**4. Set `CORS_ORIGINS`** to your console's origin.

**5. Terminate TLS in front of it.** KNOTT speaks plain HTTP.
`infra/nginx/nginx.conf` is a working configuration — note that it routes
*everything* through the engine port rather than exposing internal services,
which is the point.

**6. Think about egress.** A workflow author can make KNOTT issue HTTP requests
to anything it can route to — your internal network and your cloud provider's
metadata endpoint included. Restrict KNOTT's outbound access to what its
workflows need, and treat authoring access as privileged.

---

## systemd

The `.deb` and `.rpm` install a unit but do not enable it. After setting
`API_KEYS` in `/etc/knott/knott.env`:

```bash
sudo systemctl enable --now knott
sudo journalctl -u knott -f
```

The unit runs as an unprivileged `knott` user with `ProtectSystem=strict` and a
`StateDirectory`, so KNOTT can write to `/var/lib/knott` and nowhere else.

---

## Backups

Everything KNOTT owns lives in one directory (`knott home` prints it):

```
data/workflows.db    definitions and their version history
data/runs.db         runs, events, AI decisions, credentials, schedules
data/tasks.db        the approval queue
data/agents.db       registered agents
secret.key           the credential encryption key
```

A live SQLite file should not be copied with `cp`. Use the backup API:

```bash
for db in workflows runs tasks agents; do
  sqlite3 "$(knott home)/data/$db.db" ".backup '/backup/$db.db'"
done
cp "$(knott home)/secret.key" /backup/
```

**A backup without `secret.key` cannot decrypt its own credentials.** Store the
key with the same care as the databases, and separately if your threat model
calls for it.

To restore: stop KNOTT, put the files back, start it. Runs that were mid-flight
resume from their last checkpoint.

---

## Watching it

`/metrics` (Prometheus):

| Metric | |
|---|---|
| `knott_runs_total`, `knott_runs_by_status` | throughput and the failure rate |
| `knott_ai_decisions_total`, `knott_ai_confidence_avg` | how often the model is unsure |
| `knott_connectors_ready`, `knott_connectors_need_credentials` | a credential expired |
| `knott_build_info` | version and instance |

Gate it with `METRICS_TOKEN` if the path is reachable from outside.

`/api/v1/system-health` checks every service from inside the engine, so it
reports correctly behind any proxy topology. `/api/v1/health` is a liveness
probe.

Worth alerting on: failed runs over a window, and
`knott_connectors_need_credentials` going above zero — that is usually a token
that expired, and the workflows depending on it will be failing.

---

## Upgrading

Replace the binary and restart. Databases migrate forward on startup; the
connector catalogue reconciles additively, so new integrations appear and copy
is refreshed without disturbing which connectors you have enabled or the
credentials you stored.

Runs that were executing resume from their checkpoints, so a step whose side
effect already happened is not repeated.

Take a backup first anyway. KNOTT is before 1.0.
