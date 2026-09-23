# Changelog

Notable changes to KNOTT. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) once
1.0 is reached — until then, minor versions may change behaviour.

## [Unreleased]

## [1.0.0] — 2026-09-23

A native desktop app, a rebuilt workflow builder, local AI that works out of
the box, 113 new integrations, and a security pass.

### Added

- **A native desktop app** (`desktop/`, Wails). A real window over the
  operating system's own web view — WebView2, WKWebView, WebKitGTK — with
  native menus, a single-instance lock, logs in the data folder, and a clean
  shutdown that lets in-flight runs finish. It replaces launching Chrome or
  Edge in `--app` mode, which depended on a browser being installed and
  looked like one.
- **Installers.** Windows: a configurable installer (per-user or all users,
  install folder, `knott` command and PATH, shortcuts, start at sign-in,
  WebView2 bootstrap, silent `/S` installs) for x64 and ARM64, plus a portable
  zip. macOS: `KNOTT.app` in a drag-to-install disk image for Apple silicon and
  Intel. Linux: `knott-desktop` .deb/.rpm for x64 and ARM64, alongside the
  server package. Built on each platform by the release workflow.
- **A redesigned builder.** Full-bleed canvas; a node creator that slides in
  beside the canvas instead of a modal, browsing by category, drilling from an
  app into its actions, searching steps, apps and actions together, and
  dragging results onto the canvas; + and × on every connection to insert or
  remove a step; an inspector with Setup, Settings and Output tabs; drafting a
  workflow from a description on the empty canvas.
- **New steps:** AI Prompt (free-form prompts with text or JSON output and a
  Test button), Sort, Limit, Remove duplicates, Filter items, Map items,
  Aggregate (with group by), Date & time, Crypto (hash, HMAC, base64, UUID),
  Stop and error, and presets for webhook, schedule and polling triggers and
  HTTP Request.
- **Declarative connectors.** Integrations are JSON definitions the engine
  runs generically and the console renders; see `docs/connectors.md`.
  113 new connectors across CRM, marketing, e-commerce, logistics, finance,
  developer tools, databases, AI, communication, support, productivity, HR,
  healthcare (FHIR), education, legal, maps and data, smart home and social
  media — 177 in total.
- **Workflow generation and a prompt playground in the binary** — no Python.
- `GET /api/v1/info` reports the public URL (for webhook addresses), runtime
  and whether a key is required.

### Changed

- **Local AI works without configuration.** KNOTT detects a running Ollama
  (honouring `OLLAMA_HOST`), uses an installed model when the configured one
  is missing, talks to it over `/api/chat`, keeps it loaded between steps, and
  gives AI steps five minutes rather than 45 seconds. A model failure that
  falls back to rules is now recorded as such on the decision.
- The Python AI sidecar is opt-in (`--ai-sidecar`); the container image no
  longer carries Python.
- The Connectors page is a searchable grid with a category rail and a drawer
  per app.
- Moved to `@xyflow/react` 12 (React Flow 11 is superseded); the builder and
  the overview load on demand, cutting the initial bundle from 988 kB to 289 kB.

### Security

- **Browser-origin protection.** An unauthenticated loopback API could be
  driven by any web page the user visited — create a workflow that posts the
  stored credentials elsewhere, then run it. Cross-origin requests are now
  refused and, on a loopback bind, so are foreign host names (DNS rebinding).
  CORS is off unless origins are listed.
- **Workflows can no longer read the platform's own secrets** from the
  environment (`KNOTT_SECRET_KEY`, `API_KEYS`, …) by naming them as a
  credential.
- **Human-task callbacks must target the engine**, closing a server-side
  request forgery through `callback_url`.
- The internal services refuse browser-made requests, and every listener has
  header and idle timeouts.
- `golang.org/x/sys` 0.46 (GO-2026-5024).
- The dev start scripts no longer disable Go module checksum verification.

### Fixed

- Clicking a step marked the workflow as unsaved; drags created an undo step
  even when nothing moved.
- Per-run locks were never released, growing memory for the life of the
  process.
- Registry calls had no timeout and leaked response bodies when a workflow was
  missing.
- Settings reported Ollama as configured when the engine was not using it.

### Removed

- The legacy per-service Go modules under `services/` (superseded by
  `internal/` and missing its fixes), the WiX v3 MSI, and the browser-based
  AppImage.

## [0.9.0] — 2026-09-22

The release that makes KNOTT something you can hand to someone else.

### Added

- **One binary.** `knott` runs the registry, execution engine, task service and
  agent registry in a single process, with the web console compiled in. Internal
  services bind to loopback ports chosen at startup, so only the console port is
  reachable and two instances can share a machine. `knott desktop` opens it in
  its own window.
- **Installers for every platform** — `.msi` (Windows), `.dmg` with a proper app
  bundle (macOS), `.deb`, `.rpm` and `.AppImage` (Linux), a multi-arch container
  image, and a self-contained archive per platform. The Linux packages install a
  hardened systemd unit but do not enable it: a workflow engine that starts
  listening the moment it is unpacked is a surprise, not a convenience.
- **Error outputs.** `config.on_error` names the step to route to when one fails,
  after its retries, with the failure available to that branch as `error`. In
  the designer it is a red handle you draw a line from.
- **Sub-workflows.** A `sub_workflow` step runs another workflow — synchronously
  or fire-and-forget — and returns its output, so a reusable sequence is written
  once. Recursion is bounded at eight levels.
- **A built-in decision engine.** Anthropic and Ollama providers plus the
  deterministic rules, compiled into the binary. AI decisions no longer require
  the optional Python sidecar, and a provider that goes down no longer stops a
  run — the rules answer and the audit log records that they did.
- **A real KNOTT mark.** A trefoil generated from its own geometry, with every
  asset — SVG, favicon, and the PNG/ICO/ICNS platform icons — derived from the
  same source by `tools/brand/generate.py`.
- **Run overlay in the designer.** Watch a run play out on the graph you built
  it on, with each step's state and its error on hover.
- **Building by keyboard.** A searchable node picker on <kbd>Tab</kbd> and on
  every step's **+**, undo/redo, copy/paste/duplicate, and an auto-layout that
  orders layers to cut edge crossings.
- **A concurrency ceiling** (`MAX_CONCURRENT_RUNS`). A burst of webhooks now
  queues instead of starting a goroutine per run until the process runs out of
  memory. Queue depth is exported to `/metrics`.
- **Secret redaction in the audit trail.** Every run event is walked before it is
  written, so a credential echoed back in a node's output or an error message
  does not come to rest in the event log.
- **A generated encryption key on first run.** Stored credentials previously
  fell back to a well-known default key when `KNOTT_SECRET_KEY` was unset, which
  is obfuscation rather than encryption.
- Apache License 2.0, a NOTICE with the third-party inventory, CONTRIBUTING,
  SECURITY, a code of conduct, issue and pull request templates, and CI covering
  formatting, vet, race-enabled tests, a six-platform cross-compile matrix, a
  container build and an end-to-end smoke test.

### Changed

- **Connectors are one section.** Each connector has its own card carrying its
  switch, the exact credentials it needs — each with a line saying where to find
  the value — and a live connection test. This replaces a flat list of ~60 secret
  names sitting above a grid of toggles, with nothing linking the two.
- **Connectors have a stable slug.** Workflows and the executor dispatch on it,
  so renaming a connector no longer risks breaking saved workflows. The console
  previously guessed the slug from the display name with a forty-branch string
  match.
- **Retry backoff is exponential with jitter**, capped by `max_retry_delay`. It
  was linear, which is the wrong shape for the failures it exists to survive, and
  made every replica retry an outage in lockstep.
- **Conditions have one output per branch.** Routing is drawn on the canvas and
  round-trips through a save.
- Four Go modules became one (`github.com/regnant/knott`), with services as
  packages under `internal/` and binaries under `cmd/`.
- The container image is a single service rather than five wired together.

### Fixed

- **An edge drawn from a condition was silently dropped on save.** With one
  output there was no way to know which branch it belonged to.
- **Deleting an edge left the routing behind**, so a run still went to a step the
  author had disconnected.
- **`tool_call` branched on its first failure**, so a step with retries
  configured never retried. Error routing now lives in the run loop, applies to
  every step type, and runs only after retries are exhausted.
- The Settings page crashed with `Cloud is not defined` after the connector
  rework moved its icon import.
- `make release` ran `zip` unconditionally and deleted the staging directory
  regardless, so on a machine without zip the Windows build vanished.
- `Dockerfile.go` — a Dockerfile with a `.go` extension — made `go build ./...`
  fail across the whole repository.
- `start.sh` and `start.bat` built from `services/` directories that no longer
  exist; the Vite dev proxy pointed at ports that are no longer fixed.
