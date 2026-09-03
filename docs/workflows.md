# Building workflows

A workflow is a graph of steps. The run enters at the trigger and follows edges
until it reaches an end.

---

## The build loop

Click the **+** on any step and type what the next one should do. The picker
searches names, descriptions and intent keywords, so "slack", "delay" and
"approval" all find the right step. Pick it and it arrives already connected and
positioned.

| | |
|---|---|
| <kbd>Tab</kbd> | Open the picker from anywhere |
| Double-click the canvas | Add a step where you clicked |
| <kbd>Ctrl</kbd>+<kbd>Z</kbd> / <kbd>Shift</kbd>+<kbd>Ctrl</kbd>+<kbd>Z</kbd> | Undo, redo |
| <kbd>Ctrl</kbd>+<kbd>D</kbd> | Duplicate the selection |
| <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>L</kbd> | Tidy the layout |
| <kbd>?</kbd> | The full list |

---

## Expressions

Anywhere a field accepts text, `{{ ... }}` interpolates from the run's context.

```
{{ input.customer.email }}
{{ steps.risk_check.output.decision }}
{{ upper(input.name) }}
{{ input.amount > 1000 ? "high" : "normal" }}
```

**What is in context**

| | |
|---|---|
| `input` | The payload the run started with |
| `steps.<id>.output` | What a completed step produced |
| `error` | The failure, on an error branch: `.node`, `.message`, `.type` |

Conditions use the same language without the braces:
`input.amount > 1000 && input.region == "EU"`.

Functions available: `upper`, `lower`, `trim`, `len`, `contains`, `startsWith`,
`endsWith`, `split`, `join`, `replace`, `default`, `int`, `float`, `round`,
`abs`, `min`, `max`, `now`, `date`, `json`, `keys`, `values`.

Fill in **Test data** in the properties panel and every expression on screen
shows what it resolves to as you type. That is the fastest way to find the
mistake in a path.

---

## Handling failure

Every step that can fail has three ways to respond, in this order:

**Retry.** Network-bound steps retry twice by default, with delays that double
and carry jitter. Adjust under *Reliability*.

**Route the failure.** Drag from the red handle to the step that should handle
it — notify someone, compensate, escalate. The failure is available there as
`error`:

```
Payment charge  ──▶  Confirm order
      │
      │ on error
      ▼
Notify billing  ──▶  Hold for review
```

**Carry on.** *Carry on down the normal path if this step fails* suits a step
whose failure should not stop the run — a notification, say. The run finishes
with the outcome `COMPLETED_WITH_ERRORS`.

Nothing set means the run fails, with the failure recorded on the run.

**Timeouts on side-effecting steps are not retried by default.** A timed-out
call may or may not have been delivered; retrying could send the message twice
or charge the card twice. Set `retry_on_timeout` when the target is idempotent.

---

## Human review

A **Human Task** pauses the run and puts an item in the Task Inbox. The run
holds no resources while it waits — it can sit for days.

Configure who it goes to, an SLA, and what happens on each outcome. A required
justification on every decision is what makes the audit trail worth having.

The usual pattern is an **AI Decision** with a confidence threshold, escalating
to a person below it: the model handles the clear cases, a person handles the
rest, and every decision of both kinds is recorded.

With no AI provider configured, decisions are made by rule instead — the same
task specs, deterministic thresholds, and a bias towards escalating anything the
rules cannot clear. That is also what answers when a configured provider is
down, so an outage at your model vendor slows a workflow down rather than
stopping it. The audit log records `simulation` as the model for those, so a
rule-based decision is never mistaken for a model's judgement.

---

## Reusing work

A **Sub-workflow** step runs another workflow and hands back its output — so
"notify the on-call rota" or "enrich a customer record" is written once and
fixed once.

By default the parent waits. For fan-out, set it to start the child and carry
on. A child that pauses for a human fails the step rather than holding the
parent open, unless you say otherwise — a parent waiting three days on an
approval is almost never what was intended.

Nesting stops at eight levels; a workflow that calls itself in a circle fails
that step with a message saying so.

---

## Triggers

| | |
|---|---|
| **Manual / API** | `POST /api/v1/runs` |
| **Webhook** | `POST /api/v1/hooks/<workflow-id>`, HMAC-signed |
| **Schedule** | Interval, daily at a time, or a five-field cron |
| **Poll** | KNOTT checks a source and starts a run per new item |

Give the trigger an input schema and KNOTT validates required fields and applies
defaults before the run starts — a bad payload fails at the door with a message
naming the missing field, instead of three steps in as a null.

---

## Things worth doing

**Name steps for what they do.** "Check fraud score" beats "AI Decision 2" in
the run monitor at two in the morning.

**Give end nodes outcomes.** `APPROVED`, `REJECTED`, `ESCALATED` are what the
dashboard groups by and what a downstream system reads.

**Set an input schema on the trigger.** It is the cheapest validation you will
ever write.

**Pin down what "unsure" means.** A confidence threshold is a policy decision,
not a tuning parameter. 0.85 is a reasonable start; watch the AI Decisions page
and move it.

**Run it before you activate it.** The Run button takes a JSON payload; the run
monitor shows every step's input and output.
