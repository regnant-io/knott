# Connectors

A connector is **data**. Every integration KNOTT ships is described in
[`internal/connectors/defs/*.json`](../internal/connectors/defs): what it is
called, the credentials it needs, how it authenticates, and — for each action —
the HTTP request to make. The engine executes that request generically, and the
console renders the action's form from the same definition.

Adding an integration is one JSON entry. No Go, no React, no second list.

```
defs/crm.json  ──►  engine: builds and sends the request      ──► Salesforce
               ──►  API: GET /api/v1/connectors (actions, fields, credentials)
               ──►  console: node creator, inspector form, Connectors page
```

A few connectors need real code — OAuth refresh, multi-step calls, provider
quirks. Those are **native**: their definition (in `native.json`) supplies the
form, and the engine supplies the implementation in
`internal/execution/engine/`.

## A complete example

```json
{
  "slug": "pipedrive",
  "name": "Pipedrive",
  "category": "CRM",
  "color": "#1A1A1A",
  "description": "Manage people, deals, activities and notes in Pipedrive",
  "docs_url": "https://developers.pipedrive.com/docs/api/v1",
  "credentials": [
    {"name": "PIPEDRIVE_API_TOKEN", "label": "API token", "secret": true,
     "help": "Pipedrive → Personal preferences → API."}
  ],
  "http": {
    "base_url": "https://api.pipedrive.com/v1",
    "auth": {"type": "query", "param": "api_token", "credential": "PIPEDRIVE_API_TOKEN"},
    "test": {"id": "test", "label": "Current user", "method": "GET", "path": "/users/me", "fields": []}
  },
  "actions": [
    {"id": "create_deal", "label": "Create deal", "method": "POST", "path": "/deals",
     "body": {"title": "{title}", "value": "{value}", "currency": "{currency}"},
     "fields": [
       {"name": "title", "label": "Title", "required": true},
       {"name": "value", "label": "Value", "type": "number"},
       {"name": "currency", "label": "Currency", "placeholder": "USD"}
     ]},
    {"id": "list_deals", "label": "List deals", "method": "GET", "path": "/deals",
     "query": {"status": "{status}"}, "items_path": "data",
     "fields": [
       {"name": "status", "label": "Status", "type": "select", "options": ["open", "won", "lost"]}
     ]}
  ]
}
```

## Reference

### Connector

| Field | |
|---|---|
| `slug` | Stable id, lower-case. Workflows store it — never rename. |
| `name`, `category`, `description` | Shown in the creator and on the Connectors page. Reuse an existing category where one fits. |
| `color` | Brand colour for the app tile (hex). |
| `docs_url` | Where to read about the API / find credentials. |
| `credentials` | Secrets and settings the connector needs (below). |
| `http` | Base URL, auth, shared headers and the connection test. |
| `actions` | What the connector can do. The first is the default. |

### Credentials

| Field | |
|---|---|
| `name` | UPPER_SNAKE key the value is stored under, unique across connectors (prefix it with the app). |
| `label`, `help`, `placeholder` | Shown next to the input. `help` should say *where to find* the value. |
| `secret` | `true` hides the value. Use `false` for URLs, account ids, e-mails. |
| `optional`, `alt_of` | Optional credentials, or alternatives to another credential. |

### Auth

| `type` | Sends |
|---|---|
| `bearer` | `Authorization: Bearer <credential>` (`prefix` overrides `Bearer `) |
| `header` | `<header>: <prefix><credential>` — e.g. `X-API-Key`, `Authorization: Token …` |
| `query` | `?<param>=<credential>` |
| `basic` | Basic auth: `credential` (or fixed `user_value`) as the user, `password` credential (or fixed `password_value`) as the password |
| `none` | Nothing — for webhooks, or APIs that take the key in the body |

### Actions and templates

| Field | |
|---|---|
| `id`, `label`, `description` | `id` is stored in workflows — never rename. |
| `method`, `path` | The request. `path` is appended to `base_url`. |
| `query`, `headers` | Template maps. Empty values are dropped. |
| `body` | Template object (or array, or a single `"{field}"`). Omit it to send every unused field as JSON. |
| `body_type` | `json` (default), `form`, or `none`. |
| `items_path` | Where the list is in the response; exposed to later steps as `output.items`. |
| `fields` | The form (below). |

Templates:

- `{field}` inserts an action field. In a path it is URL-escaped.
- `{field*}` inserts a sub-path as typed (for APIs addressed by path, such as
  Firebase); `..` is refused.
- `{cred:NAME}` inserts a stored credential — for tenant URLs
  (`https://{cred:ZENDESK_SUBDOMAIN}.zendesk.com`) or keys that go in the body.
- In a body, a string that is exactly `"{field}"` keeps the field's type: a
  `number` field stays a number, a `json` field is parsed. Keys that resolve to
  nothing are dropped, so optional fields stay optional.

### Fields

| Field | |
|---|---|
| `name`, `label` | |
| `type` | `text` (default), `textarea`, `number`, `boolean`, `select` (with `options`), `json` |
| `required`, `default`, `placeholder`, `help` | |

Values can be expressions — `{{ input.email }}`, `{{ steps.fetch.output.items }}`
— and are resolved against the run before the request is built.

## Security properties

- A declarative connector sends its credentials **only** to the host its
  definition names. A workflow cannot override `base_url` and redirect a stored
  token somewhere else.
- The connection test must be harmless — "who am I", "list one item" — never a
  business action. It runs when someone clicks *Test connection*.
- Credentials are stored encrypted (AES-256-GCM) and are never returned by the
  API.

## Checklist

1. Add the entry to the right file in `internal/connectors/defs/` (or a new
   file — every `*.json` there is loaded).
2. `go test ./internal/connectors/` — validates every definition: templates
   name real fields and credentials, auth is complete, ids are unique.
3. Run KNOTT, open **Connectors**, save credentials, press **Test connection**,
   then add an action in the builder and run it.
