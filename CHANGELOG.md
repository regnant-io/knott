# Changelog

Notable changes to KNOTT. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) once
1.0 is reached — until then, minor versions may change behaviour.

## [Unreleased]

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
