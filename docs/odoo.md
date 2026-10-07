# Odoo

KNOTT talks to Odoo 19 and later through Odoo's **External JSON-2 API**
(`POST /json/2/<model>/<method>`, a bearer API key, and the database named in
`X-Odoo-Database`). The connector is a declarative definition in
[`internal/connectors/defs/erp.json`](../internal/connectors/defs/erp.json);
the purchase approval workflow ships as an example template.

Older Odoo versions (16–18) expose XML-RPC/JSON-RPC rather than JSON-2. They
are not covered by this connector yet; the HTTP Request step can call them in
the meantime.

---

## Connecting

1. **In Odoo**, create a dedicated user for KNOTT (for example *KNOTT bot*)
   with only the rights the workflow needs. For purchase approval that is
   *Purchase: Administrator*, which Odoo requires to approve an order. Give it
   no password, so it can only be used through its API key.
2. Log in as that user → avatar → **My Preferences → Account Security → New
   API Key**. Odoo shows the key once; keys last at most three months, so
   note when to rotate it.
3. **In KNOTT**, open **Connectors → Odoo** and fill in:

   | | |
   |---|---|
   | Odoo address | `https://odoo.example.co.tz` — no `/odoo` or `/web` |
   | Database | the database name, e.g. `chesify_test` |
   | API key | the key from step 2 |

4. Press **Test connection**. It reads the company name, which proves the
   address, database and key without changing anything.

KNOTT sends the key only to the address on this page; a workflow cannot point
it anywhere else.

## Actions

| Action | Odoo call |
|---|---|
| List purchase orders awaiting approval | `purchase.order/search_read`, default domain `state = 'to approve'` |
| Get purchase order lines | `purchase.order.line/search_read` for one order |
| Get a vendor's recent purchase orders | confirmed orders from one vendor, newest first |
| Approve purchase order | `purchase.order/button_approve` |
| Confirm purchase order | `purchase.order/button_confirm` (Odoo applies its own two-step rule) |
| Cancel purchase order | `purchase.order/button_cancel` |
| Log a note on a record | `<model>/message_post` as an internal note (`mail.mt_note`) |
| Search records / Update records | `search_read` / `write` on any model |
| Call a model method | any public method, with a JSON body of `ids`, `context` and named arguments |

Odoo returns references as `[id, name]` pairs. Expressions index them:
`{{ input.item.partner_id[0] }}` is the vendor's id and
`{{ input.item.partner_id[1] }}` its name.

---

## The purchase order approval workflow

**Workflows → Examples → Purchase Order Approval (Odoo).** It is seeded as a
draft, so it does not call Odoo until you activate it.

```
 Odoo: order awaiting approval (polls every minute, one run per order)
        │
 Approval policy ── amount limit, currency, minimum confidence
        │
 Odoo: order lines ──▶ Odoo: vendor's recent orders
        │
 Score the order (AI Decision, task purchase_order_approval)
        │
 Routine, or a person decides?
   ├─ routine ─▶ Odoo: approve ─▶ Odoo: note ─▶ AUTO_APPROVED
   └─ otherwise ─▶ Approver review (Task Inbox)
                    ├─ Approve ─▶ Odoo: approve ─▶ Odoo: note ─▶ APPROVED
                    └─ Reject  ─▶ Odoo: cancel  ─▶ Odoo: note ─▶ REJECTED
 Any Odoo failure ─▶ ODOO_ERROR (order unchanged; Odoo's error on the run)
```

**What "routine" means.** All four must hold, or a person decides:

1. a model answered — not the fallback rules (a provider outage never
   approves anything on its own);
2. the model said `APPROVE`;
3. its confidence is at or above `min_confidence` (0.85 by default);
4. the order total is within `amount_limit` (10,000,000 TZS by default).

Change the numbers in the **Approval policy** step. They are the business's
policy, so agree them with whoever owns purchasing.

**What the approver sees.** The order, vendor, total, the lines, the vendor's
recent orders, and KNOTT's recommendation with its confidence, risk score,
flags and reasoning. A justification is recorded with each decision.

**What lands in Odoo.** Each decision is posted to the order's chatter as an
internal note, e.g.:

> Approved by frank@chesify after KNOTT review: Usual monthly stationery
> order. (KNOTT had recommended APPROVE.) Model cordon:qwen2.5-7b, confidence
> 0.91, risk 12. In line with the vendor's last three orders. KNOTT run
> 7b0f2648-….

**What lands in KNOTT.** The run (every step's input and output), the AI
decision on the **AI Decisions** page (model, confidence, reasoning, inputs),
and the reviewer's decision. With Cordon as the AI provider, the decision also
carries Cordon's receipt — the request ID in Cordon's audit log and the
response signature — under `inference_receipt`.

### In Odoo

Turn on **Purchase → Configuration → Settings → Purchase Order Approval** and
set its minimum amount (0 sends every confirmed order through approval).
Orders a buyer confirms then wait in *To Approve*, which is where KNOTT looks.
To review RFQs before confirmation instead, change the trigger's domain to
`[["state","in",["draft","sent"]]]` and the approve steps to *Confirm purchase
order*.

### Polling and duplicates

The trigger polls every 60 seconds, starts at most 10 runs per poll, and
deduplicates on `id,write_date`: an order that is sent back for approval after
a change is a new decision, an order that has not changed is not. `fire_on_first`
is on, so orders already waiting when the workflow is activated are picked up.

---

## Rehearsing without Odoo

`tools/odoo-sim` is a stand-in that speaks the same JSON-2 calls over an
in-memory purchasing dataset in Tanzanian shillings: four vendors with order
history, and four orders waiting — one routine, one above the limit, one from
a new vendor, one unusually large.

```bash
go run ./tools/odoo-sim                       # http://127.0.0.1:8069, key demo-key, db demo
knott serve
tools/odoo-sim/rehearse.sh                    # credentials, connection test, seed, activate
```

Open `http://127.0.0.1:8069` to watch order states and the notes KNOTT
writes. `POST /sim/order` with `{"vendor_id": 7, "amount": 2500000}` adds a new
order mid-demo; `POST /sim/reset` starts over.

Against a real Odoo, run the same script with `ODOO_URL`, `ODOO_DATABASE` and
`ODOO_API_KEY` set.

## Choosing the model

Purchase decisions need a model that follows instructions and returns JSON.
On a CPU-only machine a 7–8B instruction model (Qwen 2.5 7B, Llama 3.1 8B) at
4-bit quantisation takes tens of seconds per order; a GPU with 8 GB or more
brings that to a few seconds. Sub-1B models are too small: they do not hold the
format, and the workflow will (correctly) send every order to a person.

Run the model behind [Cordon](https://github.com/regnant-io/cordon) to have
each decision signed and audited on the inference side as well, or through
Ollama directly.
