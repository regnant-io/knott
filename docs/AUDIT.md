# Platform audit — September 2026

Scope: the whole repository — the Go platform (`internal/`, `cmd/`), the console
(`apps/designer`), the desktop runtime, packaging and CI — reviewed for bugs,
performance, security and dependency risk, followed by the fixes that shipped
in 1.0.0. Items marked **Open** are recommendations not yet implemented.

Severity: **Critical** — exploitable without credentials, or silent data loss ·
**High** — a core feature broken, or exploitable with limited access ·
**Medium** — incorrect behaviour or a hardening gap · **Low** — polish.

## Security

| # | Severity | Finding | Where | Status |
|---|---|---|---|---|
| S1 | Critical | The API had no authentication by default and sent `Access-Control-Allow-Origin: *`. Any web page the user visited could create a workflow (e.g. an HTTP step that sends `{{ secret }}`s to the attacker) and run it. CORS stops a page *reading* responses, not *making* requests. DNS rebinding reached the same API through a same-origin page. | `internal/execution/server.go` | **Fixed** — origin/host guard (`guard.go`): foreign origins refused, loopback-only host names on a loopback bind, CORS off unless listed. Tests in `guard_test.go`; smoke test in CI. |
| S2 | High | A workflow could read any process environment variable by naming it as a credential (`auth_credential: "KNOTT_SECRET_KEY"`, `"API_KEYS"`), letting an operator-role key lift admin keys and the master encryption key. | `engine/executor.go` `secret()` | **Fixed** — platform secrets and `KNOTT_*` never readable; only UPPER_SNAKE names; `KNOTT_ENV_SECRETS=off` restricts to stored credentials. |
| S3 | High | Server-side request forgery: `POST /api/v1/tasks` accepted an arbitrary `callback_url`, which the task service later POSTs to (including cloud metadata endpoints). | `internal/humantask/server.go` | **Fixed** — callbacks must target the engine's task-complete endpoint. |
| S4 | Medium | Internal services (registry, tasks, agents) sent CORS `*` on loopback ports a page can scan for, unauthenticated. | `internal/{registry,humantask,agents}` | **Fixed** — they refuse browser-made requests; the engine strips browser headers when proxying. |
| S5 | Medium | No read-header or idle timeouts on any listener (slow-loris). | all `ListenAndServe` | **Fixed** — `internal/httpx.Listen`. |
| S6 | Medium | Dev start scripts set `GONOSUMDB=* GOINSECURE=* GOPROXY=direct`, disabling module checksum verification. | `start.sh` | **Fixed** — scripts rewritten. |
| S7 | Medium | Native connectors accept a `base_url` input, so a workflow author can send a stored token to another host. | `engine/connectors_*.go` | **Mitigated** — the 113 new declarative connectors never accept one. **Open** for the native ones: migrate them to declarative definitions, or allow overrides only for tenant URLs. |
| S8 | Medium | Webhooks are unauthenticated unless `WEBHOOK_SECRET` is set, and one secret covers every workflow. | `server.go triggerWebhook` | **Open** — per-workflow webhook tokens in the URL. |
| S9 | Low | Credentials are AES-GCM encrypted without binding the ciphertext to its name (a value could be moved between names by someone with database write access). | `store/credentials.go` | **Open** — add the name as associated data, with fallback decryption for existing rows. |
| S10 | Low | HTTP steps can reach private networks by design. | engine | **Open** — optional egress policy (`KNOTT_BLOCK_PRIVATE_NETWORKS`). Documented in SECURITY.md. |
| S11 | Info | Release binaries are not code-signed (Authenticode) or notarised (Apple). | release workflow | **Open** — needs certificates; the macOS bundle is ad-hoc signed and the workflow signs with `MACOS_SIGN_IDENTITY` when provided. |

## Bugs and correctness

| # | Severity | Finding | Status |
|---|---|---|---|
| B1 | High | **Local AI appeared broken.** In `auto` mode the engine never looked for Ollama (its URL defaulted to empty), while Settings showed `localhost:11434` as if configured. When set, the default model `llama3.1` was usually not installed; the 404 was swallowed and every decision silently fell back to rules, and *Test* still said OK. On Windows `localhost` can resolve to `::1` while Ollama listens on IPv4. AI steps timed out after 45 s, less than a cold local model needs. Workflow generation only worked with the Python sidecar, which desktop installs rarely have. | **Fixed** — detection (honouring `OLLAMA_HOST`, `127.0.0.1`), model resolution to an installed model, `/api/chat`, `keep_alive`, 5-minute AI timeouts, visible fallbacks (`fallback_reason`, strict mode), a real test prompt, and generation in Go. |
| B2 | Medium | Clicking a node marked the workflow unsaved (drag-stop fired without movement), and drags always pushed an undo step. | **Fixed** |
| B3 | Medium | Deleting a mid-chain step cut the workflow in two. | **Fixed** — neighbours are reconnected. |
| B4 | Medium | `createRun` treated any registry response but 404 as success and leaked the body on 404; registry calls had no timeout, so a wedged registry hung run creation and every run list. | **Fixed** |
| B5 | Low | Webhook URLs in the builder used `window.location.origin`, wrong behind the desktop app. | **Fixed** — `/api/v1/info` public URL. |
| B6 | Low | The AI engine's configuration was written by Settings while runs read it, without synchronisation (a data race). | **Fixed** — guarded, copy-on-read. |
| B7 | Low | Human-task decision routes (`next_map`) are not drawn on the canvas. | **Open** |
| B8 | Low | `/api/v1/runs` returns the latest 100 with no pagination. | **Open** |

## Performance

| # | Finding | Status |
|---|---|---|
| P1 | Per-run mutexes were stored in a `sync.Map` and never removed — memory grew with every run for the life of the process. | **Fixed** — reference-counted, released when idle. |
| P2 | The console shipped as one 988 kB bundle. | **Fixed** — the builder and overview load on demand; initial bundle 289 kB. |
| P3 | The canvas re-created every node's derived presentation on each render. | **Fixed** — memoised node components and derived metadata. |
| P4 | Listing runs fetches every workflow (definitions included) from the registry to attach names. | **Open** — a names-only registry endpoint. |
| P5 | Every AI call reloaded the local model when idle. | **Fixed** — `keep_alive` 15 minutes. |

## Dependencies

| Ecosystem | Result | Action |
|---|---|---|
| Go (govulncheck) | GO-2026-5024 in `golang.org/x/sys` 0.42 (Windows, not reachable from KNOTT's code) | Upgraded to 0.46. The desktop module is audited in CI too. |
| npm (`npm audit`) | 0 vulnerabilities | — |
| `reactflow` 11 | Superseded by `@xyflow/react` | Migrated to `@xyflow/react` 12.11. |
| React 18.3, recharts 2.15, lucide-react 0.453, jsdom 25 | Supported, no advisories; newer majors exist | Safe path: React 19 with `@testing-library/react` 16 (already compatible), then recharts 3 and lucide-react 1.x (a handful of icon renames). Not bundled into this release to keep its blast radius contained. |
| Legacy `services/*` Go modules | Four stale copies of the services, without the fixes above, still audited and built by CI | Removed. |

CI runs `govulncheck` (server and desktop modules) and `npm audit` on every
change.
