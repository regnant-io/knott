# Contributing to KNOTT

Thanks for wanting to help. KNOTT is maintained by [Regnant](https://regnant.io)
and released under the Apache License 2.0.

## Before you start

**Small fixes** — typos, a broken link, an obviously wrong default, a failing
edge case with a test — just send a pull request. No issue needed.

**Anything larger** — a new node type, a connector, a change to how runs are
scheduled or stored — open an issue first and describe the problem you hit. It
is much easier to agree on an approach in an issue than to ask you to rewrite
work you have already finished.

**Security issues** do not go in the tracker. See [SECURITY.md](SECURITY.md).

## Getting set up

You need Go 1.25+ and Node 18+. Python 3.9+ is optional — without it KNOTT falls
back to deterministic rule-based decisions, which is enough to develop against.

```bash
git clone https://github.com/regnant/knott
cd knott
make ui      # build the web console
make run     # build and start KNOTT, opening the console
```

While working on the console, run Vite instead so changes reload:

```bash
make build && ./bin/knott serve &     # the API on :8002
npm --prefix apps/designer run dev    # the console on :3000, proxying to it
```

Run everything CI runs before you push:

```bash
make check   # gofmt, go vet, Go tests, console tests
```

## How the code is laid out

```
cmd/knott              the all-in-one binary — this is what people download
cmd/knott-{registry,engine,tasks,agents}
                       the same services as separate binaries, for scaled deploys
internal/registry      workflow definitions, versions, validation
internal/execution     the run loop, scheduling, triggers, the HTTP front door
internal/execution/engine
                       node execution and the connector implementations
internal/humantask     the approval queue
internal/agents        the external agent registry
internal/ui            the console, compiled in with go:embed
apps/designer          the console source (React + React Flow)
services/ai-decision-engine
                       the optional Python decision engine
tools/brand            generates every brand asset from the mark's geometry
```

## What good looks like here

**Explain why, not what.** The code says what it does. A comment earns its place
by saying why it does that — the constraint, the failure it avoids, the thing
that was tried and did not work. Look at `internal/execution/store/redact.go`
or `internal/execution/engine/subworkflow.go` for the register.

**Cover the behaviour, not the function.** A test that pins the reason a change
was made is worth ten that assert a getter returns what was set. When you fix a
bug, add the test that would have caught it.

**Errors are for the person reading them.** `connector 'stripe' needs
STRIPE_SECRET_KEY` beats `configuration error`. Say what failed and what to do.

**Match the surrounding code.** Go is `gofmt`-clean, with `golangci-lint`
advisory. The console has no formatter enforced — follow the file you are in.

## Adding a connector

Connectors are the most common contribution, and there is a well-worn path:

1. Add a `CatalogEntry` to `internal/execution/store/catalog.go`, with a slug,
   category, and a `CredentialSpec` for each secret — including the one-line
   `Help` saying where in the vendor's UI to find it. That help text is the
   whole onboarding experience for that connector; write it as if for someone
   who has never opened that product's settings.
2. Implement `call<Name>` in `internal/execution/engine/connectors_more.go`,
   using `connectorJSON` / `doRequest` so retries, timeouts and secret handling
   come for free.
3. Add the case to `callConnector` in `executor.go`.
4. Add a schema entry to `CONNECTOR_SCHEMA` in
   `apps/designer/src/pages/WorkflowDesigner.jsx` so the designer renders the
   right fields.
5. Add a test in `internal/execution/engine/` against an `httptest` server —
   assert the request shape, not the vendor's response.

Do not add a connector that needs a credential KNOTT cannot store, or one whose
free tier cannot be exercised in a test.

## Pull requests

- Branch from `main`.
- Keep one concern per pull request. Two unrelated fixes are two pull requests.
- Write the commit message for someone reading `git log` in a year: what was
  wrong, and what you changed about it. The body matters more than the subject.
- Include the reasoning for any non-obvious decision, in the code as a comment
  rather than only in the pull request — the pull request will not be there when
  someone is reading the file.

## Contributor licence

By submitting a pull request you agree that your contribution is licensed under
the Apache License 2.0, and that you have the right to license it — under
[section 5](LICENSE) of that licence, contributions are accepted under its
terms. There is no separate CLA to sign.

Add yourself to the git history and, if you like, to a `Co-Authored-By` line.
There is no `AUTHORS` file to keep in sync.

## Trademarks

The Apache License 2.0 covers the code. It does not grant rights to the KNOTT
name or mark, which belong to Regnant.

You may, without asking:

- say your project works with, integrates with, or is built on KNOTT;
- redistribute unmodified KNOTT builds under the name KNOTT;
- use the mark to link to this project.

Please ask (opensource@regnant.io) before:

- naming a modified build, a fork, or a hosted service "KNOTT" or a name
  containing it;
- using the mark in a way that suggests Regnant produced or endorsed your work.

Renaming a fork is not a hostile request — it is how anyone downstream can tell
whose software they are running when something goes wrong.

## Code of conduct

Participation is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
