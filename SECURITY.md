# Security policy

## Reporting a vulnerability

Please do not open a public issue.

Report privately through GitHub's [security advisory
form](https://github.com/regnant-io/knott/security/advisories/new), or by email to
**security@regnant.io**.

Useful to include: what an attacker can do, the version or commit you tested,
and the smallest set of steps that shows it. A proof of concept helps but is not
required — a clear description of the flaw is enough to start.

**What to expect**

| | |
|---|---|
| Acknowledgement | within 3 working days |
| Initial assessment | within 10 working days |
| Fix or mitigation for a confirmed high-severity issue | target 30 days |
| Public advisory | after a fix ships, or 90 days, whichever is first |

We will keep you updated as the assessment proceeds, credit you in the advisory
unless you would rather we did not, and tell you before we publish.

## Supported versions

The latest minor release receives security fixes. KNOTT has not reached 1.0; if
you are running it in production, track releases.

## What is in scope

- The KNOTT server and its API, including authentication and role enforcement
- Credential storage and encryption at rest
- Webhook signature verification
- Expression evaluation and workflow definition parsing
- The web console (XSS, CSRF, and similar)
- The published binaries and container images

Out of scope: findings that require an attacker to already hold an admin API
key; vulnerabilities in a third-party service a connector talks to (report those
to that vendor); denial of service through deliberately expensive workflows an
authorised operator wrote; and results from automated scanners without a
demonstrated impact.

## Running KNOTT safely

KNOTT executes workflows an operator authors, calls services with credentials it
holds, and pauses for human approval. Treat it as infrastructure, not as a page.

**Authentication is off by default and binds to loopback.** That is the right
default for a laptop and the wrong one for a server. Before binding anywhere
else, set `API_KEYS`:

```
API_KEYS=long-random-admin-key:admin,another-key:operator,readonly:viewer
```

Roles are enforced on every request: `viewer` may only read, `operator` may
create and run workflows, and only `admin` may write credentials or change AI
provider configuration. KNOTT logs a warning at startup when it is bound
off-loopback with no keys set.

**Set `KNOTT_SECRET_KEY`.** Stored connector credentials are encrypted at rest
with it. The all-in-one binary generates one on first run and writes it to
`secret.key` in the state directory with owner-only permissions; a container or
orchestrated deployment should supply its own so the key survives a rebuild.

**Sign your webhooks.** With `WEBHOOK_SECRET` set, inbound webhook calls must
carry a matching `X-KNOTT-Signature` HMAC. Without it, anyone who can reach the
port can start a run.

**Browser origins are checked.** A page on another site cannot drive the API,
even though it can reach a loopback port: requests carrying another origin are
refused, and on a loopback bind only loopback host names are answered, which
defeats DNS rebinding. The console's own origin always works. To serve the
console from somewhere else, list it in `KNOTT_ALLOWED_ORIGINS`; to reach a
loopback-bound server through a local proxy name, list the name in
`KNOTT_ALLOWED_HOSTS`. The internal services (registry, tasks, agents) refuse
browser requests outright.

**Put TLS in front of it.** KNOTT speaks plain HTTP; terminate TLS at a reverse
proxy. `infra/nginx/nginx.conf` is a working starting point.

**Watch what workflows can reach.** A workflow author can make the server issue
HTTP requests to any address it can route to, which includes your internal
network and cloud metadata endpoints. Give KNOTT only the network access its
workflows need, and treat authoring access as the privileged thing it is.

**Workflows cannot read the platform's own secrets.** A step names the
credential it uses, and stored credentials are available to any workflow — an
admin put them there for that. Environment variables are narrower: KNOTT's own
keys (`KNOTT_SECRET_KEY`, `API_KEYS`, `WEBHOOK_SECRET`, `METRICS_TOKEN`, any
`KNOTT_*`) are never readable, only UPPER_SNAKE names are, and
`KNOTT_ENV_SECRETS=off` restricts workflows to stored credentials entirely.
Declarative connectors send a credential only to the host their definition
names.

## What KNOTT does on your behalf

Worth knowing before an audit:

- **Secrets are redacted from the audit trail.** Run events are walked before
  they are written, so a credential echoed back in a node's output or an error
  message does not come to rest in the event log.
- **Credential values are write-only over the API.** They can be set and
  deleted, never read back.
- **Task-completion callbacks are HMAC-signed** by the engine, so the
  auth-exempt callback endpoint cannot be used to forge a human approval, and
  the task service only calls back to the engine's own task-complete endpoint.
- **Every listener has header and idle timeouts**, so slow-drip connections
  cannot exhaust them.
- **Run leases** stop two replicas executing the same run, and checkpoints stop
  a restart re-firing a side effect that already happened.
- **Every AI decision and human approval is recorded** — model, confidence,
  reasoning, actor and timestamp — in an append-only audit log.
