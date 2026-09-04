package engine

import (
	"fmt"
	"strings"
)

// ─── Additional connectors (broadening coverage toward n8n) ────────────────────
// Each connector follows the established pattern: resolve a credential (stored
// credential → env var), build the request, and return a normalized result via
// connectorJSON/connectorBasic. All accept a base_url override for testing and
// self-hosted instances. They are dispatched from callConnector in executor.go.

// Linear (GraphQL issue tracker). Secret: LINEAR_API_KEY.
//   create_issue (default): team_id, title, description
func (e *Executor) callLinear(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("LINEAR_API_KEY"))
	if token == "" {
		return nil, fmt.Errorf("linear requires LINEAR_API_KEY (store it in Credentials)")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.linear.app")
	headers := map[string]string{"Authorization": token}
	switch defaultAction(action, "create_issue") {
	case "create_issue", "create":
		team := str(in["team_id"])
		title := str(in["title"])
		if team == "" || title == "" {
			return nil, fmt.Errorf("linear create_issue requires 'team_id' and 'title'")
		}
		desc := firstNonEmpty(str(in["description"]), str(in["body"]))
		mutation := `mutation IssueCreate($t:String!,$ti:String!,$d:String){issueCreate(input:{teamId:$t,title:$ti,description:$d}){success issue{id identifier url}}}`
		payload := map[string]any{"query": mutation, "variables": map[string]any{"t": team, "ti": title, "d": desc}}
		out, err := e.connectorJSON("linear", "POST", base+"/graphql", headers, payload, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			if data, ok := m["data"].(map[string]any); ok {
				if ic, ok := data["issueCreate"].(map[string]any); ok {
					if iss, ok := ic["issue"].(map[string]any); ok {
						out["issue_id"] = iss["id"]
						out["identifier"] = iss["identifier"]
						out["url"] = iss["url"]
					}
				}
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("linear: unknown action %q (supported: create_issue)", action)
	}
}

// Trello. Secrets: TRELLO_KEY + TRELLO_TOKEN (query-param auth).
//   create_card (default): list_id, name, desc
func (e *Executor) callTrello(action string, in map[string]any) (map[string]any, error) {
	key := firstNonEmpty(str(in["key"]), e.secret("TRELLO_KEY"))
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("TRELLO_TOKEN"))
	if key == "" || token == "" {
		return nil, fmt.Errorf("trello requires TRELLO_KEY and TRELLO_TOKEN")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.trello.com")
	auth := "key=" + key + "&token=" + token
	switch defaultAction(action, "create_card") {
	case "create_card", "create":
		list := str(in["list_id"])
		name := str(in["name"])
		if list == "" || name == "" {
			return nil, fmt.Errorf("trello create_card requires 'list_id' and 'name'")
		}
		endpoint := base + "/1/cards?" + auth + "&idList=" + url_QueryEscape(list) +
			"&name=" + url_QueryEscape(name) + "&desc=" + url_QueryEscape(firstNonEmpty(str(in["desc"]), str(in["description"])))
		out, err := e.connectorJSON("trello", "POST", endpoint, nil, nil, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["card_id"] = m["id"]
			out["url"] = m["shortUrl"]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("trello: unknown action %q (supported: create_card)", action)
	}
}

// Asana. Secret: ASANA_TOKEN (PAT).
//   create_task (default): project_id, name, notes
func (e *Executor) callAsana(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("ASANA_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("asana requires ASANA_TOKEN (a personal access token)")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://app.asana.com")
	headers := map[string]string{"Authorization": "Bearer " + token}
	switch defaultAction(action, "create_task") {
	case "create_task", "create":
		name := str(in["name"])
		if name == "" {
			return nil, fmt.Errorf("asana create_task requires 'name'")
		}
		data := map[string]any{"name": name}
		if notes := firstNonEmpty(str(in["notes"]), str(in["description"])); notes != "" {
			data["notes"] = notes
		}
		if proj := str(in["project_id"]); proj != "" {
			data["projects"] = []any{proj}
		}
		out, err := e.connectorJSON("asana", "POST", base+"/api/1.0/tasks", headers, map[string]any{"data": data}, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			if d, ok := m["data"].(map[string]any); ok {
				out["task_id"] = d["gid"]
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("asana: unknown action %q (supported: create_task)", action)
	}
}

// ClickUp. Secret: CLICKUP_TOKEN.
//   create_task (default): list_id, name, description
func (e *Executor) callClickUp(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("CLICKUP_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("clickup requires CLICKUP_TOKEN")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.clickup.com")
	headers := map[string]string{"Authorization": token}
	switch defaultAction(action, "create_task") {
	case "create_task", "create":
		list := str(in["list_id"])
		name := str(in["name"])
		if list == "" || name == "" {
			return nil, fmt.Errorf("clickup create_task requires 'list_id' and 'name'")
		}
		payload := map[string]any{"name": name}
		if d := firstNonEmpty(str(in["description"]), str(in["notes"])); d != "" {
			payload["description"] = d
		}
		out, err := e.connectorJSON("clickup", "POST", base+"/api/v2/list/"+list+"/task", headers, payload, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["task_id"] = m["id"]
			out["url"] = m["url"]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("clickup: unknown action %q (supported: create_task)", action)
	}
}

// PagerDuty Events API v2 (trigger an incident). Secret: PAGERDUTY_ROUTING_KEY.
//   trigger (default): summary, source, severity (critical|error|warning|info)
func (e *Executor) callPagerDuty(action string, in map[string]any) (map[string]any, error) {
	routingKey := firstNonEmpty(e.resolveSecretRef(in["routing_key"]), e.secret("PAGERDUTY_ROUTING_KEY"))
	if routingKey == "" {
		return nil, fmt.Errorf("pagerduty requires PAGERDUTY_ROUTING_KEY (an Events API v2 integration key)")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://events.pagerduty.com")
	switch defaultAction(action, "trigger") {
	case "trigger", "create":
		summary := firstNonEmpty(str(in["summary"]), str(in["text"]), str(in["message"]))
		if summary == "" {
			return nil, fmt.Errorf("pagerduty trigger requires 'summary'")
		}
		payload := map[string]any{
			"routing_key":  routingKey,
			"event_action": "trigger",
			"payload": map[string]any{
				"summary":  summary,
				"source":   firstNonEmpty(str(in["source"]), "KNOTT"),
				"severity": firstNonEmpty(str(in["severity"]), "error"),
			},
		}
		out, err := e.connectorJSON("pagerduty", "POST", base+"/v2/enqueue", nil, payload, map[string]any{"triggered": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["dedup_key"] = m["dedup_key"]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("pagerduty: unknown action %q (supported: trigger)", action)
	}
}

// Mattermost: post a message via an incoming webhook. Secret: MATTERMOST_WEBHOOK_URL.
func (e *Executor) callMattermost(action string, in map[string]any) (map[string]any, error) {
	hook := firstNonEmpty(str(in["webhook"]), str(in["url"]), e.secret("MATTERMOST_WEBHOOK_URL"))
	if hook == "" {
		return nil, fmt.Errorf("mattermost requires MATTERMOST_WEBHOOK_URL")
	}
	text := firstNonEmpty(str(in["text"]), str(in["message"]), str(in["body"]))
	if text == "" {
		return nil, fmt.Errorf("mattermost send_message requires 'text'")
	}
	body := map[string]any{"text": text}
	if ch := str(in["channel"]); ch != "" {
		body["channel"] = ch
	}
	return e.connectorJSON("mattermost", "POST", hook, nil, body, map[string]any{"sent": true})
}

// Zendesk: create a support ticket. Secrets: ZENDESK_EMAIL + ZENDESK_API_TOKEN.
// Requires base_url (your subdomain, e.g. https://acme.zendesk.com).
//   create_ticket (default): subject, comment (body), priority
func (e *Executor) callZendesk(action string, in map[string]any) (map[string]any, error) {
	email := firstNonEmpty(str(in["email"]), e.secret("ZENDESK_EMAIL"))
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("ZENDESK_API_TOKEN"))
	site := firstNonEmpty(str(in["base_url"]), e.secret("ZENDESK_BASE_URL"))
	if email == "" || token == "" || site == "" {
		return nil, fmt.Errorf("zendesk requires ZENDESK_EMAIL, ZENDESK_API_TOKEN and a base_url (https://acme.zendesk.com)")
	}
	site = strings.TrimRight(site, "/")
	switch defaultAction(action, "create_ticket") {
	case "create_ticket", "create":
		subject := str(in["subject"])
		body := firstNonEmpty(str(in["comment"]), str(in["body"]), str(in["text"]))
		if subject == "" || body == "" {
			return nil, fmt.Errorf("zendesk create_ticket requires 'subject' and 'comment'")
		}
		ticket := map[string]any{"subject": subject, "comment": map[string]any{"body": body}}
		if p := str(in["priority"]); p != "" {
			ticket["priority"] = p
		}
		// Zendesk uses basic auth as "email/token:api_token".
		out, err := e.connectorBasic("zendesk", "POST", site+"/api/v2/tickets.json", email+"/token", token, map[string]any{"ticket": ticket}, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			if t, ok := m["ticket"].(map[string]any); ok {
				out["ticket_id"] = t["id"]
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("zendesk: unknown action %q (supported: create_ticket)", action)
	}
}

// Shopify Admin API. Secret: SHOPIFY_ACCESS_TOKEN. Requires base_url (store URL).
//   create_order_note / list_products (default): list_products
func (e *Executor) callShopify(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("SHOPIFY_ACCESS_TOKEN"))
	site := firstNonEmpty(str(in["base_url"]), e.secret("SHOPIFY_STORE_URL"))
	if token == "" || site == "" {
		return nil, fmt.Errorf("shopify requires SHOPIFY_ACCESS_TOKEN and a base_url (https://your-store.myshopify.com)")
	}
	site = strings.TrimRight(site, "/")
	headers := map[string]string{"X-Shopify-Access-Token": token}
	version := firstNonEmpty(str(in["api_version"]), "2024-04")
	switch defaultAction(action, "list_products") {
	case "list_products", "list":
		out, err := e.connectorJSON("shopify", "GET", site+"/admin/api/"+version+"/products.json?limit=50", headers, nil, map[string]any{"listed": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["products"] = m["products"]
		}
		return out, nil
	case "create_customer":
		email := str(in["email"])
		if email == "" {
			return nil, fmt.Errorf("shopify create_customer requires 'email'")
		}
		cust := map[string]any{"email": email}
		if v := str(in["firstname"]); v != "" {
			cust["first_name"] = v
		}
		if v := str(in["lastname"]); v != "" {
			cust["last_name"] = v
		}
		return e.connectorJSON("shopify", "POST", site+"/admin/api/"+version+"/customers.json", headers, map[string]any{"customer": cust}, map[string]any{"created": true})
	default:
		return nil, fmt.Errorf("shopify: unknown action %q (supported: list_products, create_customer)", action)
	}
}

// Mailchimp Marketing: add/subscribe a member to an audience. Secret: MAILCHIMP_API_KEY.
// The API key embeds the datacenter suffix (e.g. -us21); we parse it for the base URL.
//   add_member (default): list_id, email, status (subscribed|pending)
func (e *Executor) callMailchimp(action string, in map[string]any) (map[string]any, error) {
	key := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("MAILCHIMP_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("mailchimp requires MAILCHIMP_API_KEY")
	}
	dc := ""
	if i := strings.LastIndex(key, "-"); i >= 0 {
		dc = key[i+1:]
	}
	if dc == "" {
		return nil, fmt.Errorf("mailchimp API key missing datacenter suffix (e.g. -us21)")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://"+dc+".api.mailchimp.com")
	switch defaultAction(action, "add_member") {
	case "add_member", "subscribe", "create":
		list := str(in["list_id"])
		email := str(in["email"])
		if list == "" || email == "" {
			return nil, fmt.Errorf("mailchimp add_member requires 'list_id' and 'email'")
		}
		payload := map[string]any{
			"email_address": email,
			"status":        firstNonEmpty(str(in["status"]), "subscribed"),
		}
		out, err := e.connectorBasic("mailchimp", "POST", base+"/3.0/lists/"+list+"/members", "anystring", key, payload, map[string]any{"added": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["member_id"] = m["id"]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("mailchimp: unknown action %q (supported: add_member)", action)
	}
}

// OpenAI Chat Completions (a cloud LLM connector for text generation inside a
// workflow). Secret: OPENAI_API_KEY.
//   chat (default): prompt (or messages), model, system
func (e *Executor) callOpenAI(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("OPENAI_API_KEY"))
	if token == "" {
		return nil, fmt.Errorf("openai requires OPENAI_API_KEY")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.openai.com")
	headers := map[string]string{"Authorization": "Bearer " + token}
	switch defaultAction(action, "chat") {
	case "chat", "complete", "generate":
		model := firstNonEmpty(str(in["model"]), "gpt-4o-mini")
		var messages []any
		if ms := toAnyList(in["messages"]); len(ms) > 0 {
			messages = ms
		} else {
			prompt := firstNonEmpty(str(in["prompt"]), str(in["text"]), str(in["input"]))
			if prompt == "" {
				return nil, fmt.Errorf("openai chat requires 'prompt' or 'messages'")
			}
			if sys := str(in["system"]); sys != "" {
				messages = append(messages, map[string]any{"role": "system", "content": sys})
			}
			messages = append(messages, map[string]any{"role": "user", "content": prompt})
		}
		payload := map[string]any{"model": model, "messages": messages}
		if t := str(in["temperature"]); t != "" {
			payload["temperature"] = toFloat(in["temperature"])
		}
		out, err := e.connectorJSON("openai", "POST", base+"/v1/chat/completions", headers, payload, map[string]any{"ok": true})
		if err != nil {
			return nil, err
		}
		// Surface the assistant text directly for easy downstream use.
		if m, ok := out["response"].(map[string]any); ok {
			if choices := toAnyList(m["choices"]); len(choices) > 0 {
				if c0, ok := choices[0].(map[string]any); ok {
					if msg, ok := c0["message"].(map[string]any); ok {
						out["text"] = msg["content"]
					}
				}
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("openai: unknown action %q (supported: chat)", action)
	}
}

// Pushover: send a push notification. Secrets: PUSHOVER_TOKEN + PUSHOVER_USER.
func (e *Executor) callPushover(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("PUSHOVER_TOKEN"))
	user := firstNonEmpty(str(in["user"]), e.secret("PUSHOVER_USER"))
	if token == "" || user == "" {
		return nil, fmt.Errorf("pushover requires PUSHOVER_TOKEN and PUSHOVER_USER")
	}
	msg := firstNonEmpty(str(in["message"]), str(in["text"]), str(in["body"]))
	if msg == "" {
		return nil, fmt.Errorf("pushover send requires 'message'")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.pushover.net")
	payload := map[string]any{"token": token, "user": user, "message": msg}
	if t := str(in["title"]); t != "" {
		payload["title"] = t
	}
	return e.connectorJSON("pushover", "POST", base+"/1/messages.json", nil, payload, map[string]any{"sent": true})
}

// GraphQL: call any GraphQL endpoint. Generic, like the HTTP connector but for GraphQL.
//   query (default): url, query, variables, auth_token (optional bearer)
func (e *Executor) callGraphQL(action string, in map[string]any) (map[string]any, error) {
	endpoint := firstNonEmpty(str(in["url"]), str(in["endpoint"]))
	query := str(in["query"])
	if endpoint == "" || query == "" {
		return nil, fmt.Errorf("graphql requires 'url' and 'query'")
	}
	headers := map[string]string{}
	if tok := firstNonEmpty(e.resolveSecretRef(in["auth_token"]), str(in["auth_credential"])); tok != "" {
		headers["Authorization"] = "Bearer " + tok
	}
	payload := map[string]any{"query": query}
	if vars := asMap(in["variables"]); vars != nil {
		payload["variables"] = vars
	}
	return e.connectorJSON("graphql", "POST", endpoint, headers, payload, map[string]any{"ok": true})
}

// url_QueryEscape is a tiny wrapper so this file doesn't need to import net/url
// directly (executor.go already imports it); kept local for clarity.
func url_QueryEscape(s string) string {
	r := strings.NewReplacer(" ", "%20", "&", "%26", "?", "%3F", "#", "%23",
		"=", "%3D", "+", "%2B", "/", "%2F")
	return r.Replace(s)
}

// ─── Connector coverage — wave 2 ───────────────────────────────────────────────

// GitLab. Secret: GITLAB_TOKEN. base_url defaults to gitlab.com.
//   create_issue (default): project_id, title, description
func (e *Executor) callGitLab(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("GITLAB_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("gitlab requires GITLAB_TOKEN")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://gitlab.com")
	headers := map[string]string{"PRIVATE-TOKEN": token}
	switch defaultAction(action, "create_issue") {
	case "create_issue", "create":
		project := str(in["project_id"])
		title := str(in["title"])
		if project == "" || title == "" {
			return nil, fmt.Errorf("gitlab create_issue requires 'project_id' and 'title'")
		}
		endpoint := base + "/api/v4/projects/" + url_QueryEscape(project) + "/issues?title=" + url_QueryEscape(title) +
			"&description=" + url_QueryEscape(firstNonEmpty(str(in["description"]), str(in["body"])))
		out, err := e.connectorJSON("gitlab", "POST", endpoint, headers, nil, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["issue_iid"] = m["iid"]
			out["url"] = m["web_url"]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("gitlab: unknown action %q (supported: create_issue)", action)
	}
}

// Monday.com (GraphQL). Secret: MONDAY_TOKEN.
//   create_item (default): board_id, item_name
func (e *Executor) callMonday(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("MONDAY_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("monday requires MONDAY_TOKEN")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.monday.com")
	headers := map[string]string{"Authorization": token}
	switch defaultAction(action, "create_item") {
	case "create_item", "create":
		board := str(in["board_id"])
		name := firstNonEmpty(str(in["item_name"]), str(in["name"]))
		if board == "" || name == "" {
			return nil, fmt.Errorf("monday create_item requires 'board_id' and 'item_name'")
		}
		q := `mutation($b:ID!,$n:String!){create_item(board_id:$b,item_name:$n){id}}`
		payload := map[string]any{"query": q, "variables": map[string]any{"b": board, "n": name}}
		return e.connectorJSON("monday", "POST", base+"/v2", headers, payload, map[string]any{"created": true})
	default:
		return nil, fmt.Errorf("monday: unknown action %q (supported: create_item)", action)
	}
}

// Freshdesk. Secret: FRESHDESK_API_KEY. Requires base_url (https://acme.freshdesk.com).
//   create_ticket (default): subject, description, email, priority(1-4)
func (e *Executor) callFreshdesk(action string, in map[string]any) (map[string]any, error) {
	key := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("FRESHDESK_API_KEY"))
	site := firstNonEmpty(str(in["base_url"]), e.secret("FRESHDESK_BASE_URL"))
	if key == "" || site == "" {
		return nil, fmt.Errorf("freshdesk requires FRESHDESK_API_KEY and a base_url (https://acme.freshdesk.com)")
	}
	site = strings.TrimRight(site, "/")
	switch defaultAction(action, "create_ticket") {
	case "create_ticket", "create":
		subject := str(in["subject"])
		desc := firstNonEmpty(str(in["description"]), str(in["body"]))
		email := str(in["email"])
		if subject == "" || desc == "" || email == "" {
			return nil, fmt.Errorf("freshdesk create_ticket requires 'subject', 'description', and 'email'")
		}
		priority := 2
		if p := int(toFloat(in["priority"])); p >= 1 && p <= 4 {
			priority = p
		}
		payload := map[string]any{"subject": subject, "description": desc, "email": email, "priority": priority, "status": 2}
		// Freshdesk uses basic auth: APIKEY as username, any password.
		out, err := e.connectorBasic("freshdesk", "POST", site+"/api/v2/tickets", key, "X", payload, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["ticket_id"] = m["id"]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("freshdesk: unknown action %q (supported: create_ticket)", action)
	}
}

// Intercom. Secret: INTERCOM_TOKEN.
//   create_contact (default): email, name
func (e *Executor) callIntercom(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("INTERCOM_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("intercom requires INTERCOM_TOKEN")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.intercom.io")
	headers := map[string]string{"Authorization": "Bearer " + token, "Intercom-Version": "2.11"}
	switch defaultAction(action, "create_contact") {
	case "create_contact", "create":
		email := str(in["email"])
		if email == "" {
			return nil, fmt.Errorf("intercom create_contact requires 'email'")
		}
		payload := map[string]any{"role": "user", "email": email}
		if n := str(in["name"]); n != "" {
			payload["name"] = n
		}
		out, err := e.connectorJSON("intercom", "POST", base+"/contacts", headers, payload, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["contact_id"] = m["id"]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("intercom: unknown action %q (supported: create_contact)", action)
	}
}

// Microsoft Graph — send an email as the authenticated user/app. Secret:
// MS_GRAPH_TOKEN (an OAuth2 access token). send_mail (default): to, subject, body
func (e *Executor) callMSGraph(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("MS_GRAPH_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("ms_graph requires MS_GRAPH_TOKEN (an OAuth2 access token)")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://graph.microsoft.com")
	headers := map[string]string{"Authorization": "Bearer " + token}
	switch defaultAction(action, "send_mail") {
	case "send_mail", "send":
		to := str(in["to"])
		subject := str(in["subject"])
		body := firstNonEmpty(str(in["body"]), str(in["text"]))
		if to == "" || subject == "" {
			return nil, fmt.Errorf("ms_graph send_mail requires 'to' and 'subject'")
		}
		payload := map[string]any{
			"message": map[string]any{
				"subject":      subject,
				"body":         map[string]any{"contentType": "Text", "content": body},
				"toRecipients": []any{map[string]any{"emailAddress": map[string]any{"address": to}}},
			},
			"saveToSentItems": true,
		}
		return e.connectorJSON("ms_graph", "POST", base+"/v1.0/me/sendMail", headers, payload, map[string]any{"sent": true})
	default:
		return nil, fmt.Errorf("ms_graph: unknown action %q (supported: send_mail)", action)
	}
}

// WhatsApp Cloud API (Meta). Secret: WHATSAPP_TOKEN + phone number id (in config).
//   send_message (default): phone_number_id, to, text
func (e *Executor) callWhatsApp(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("WHATSAPP_TOKEN"))
	phoneID := firstNonEmpty(str(in["phone_number_id"]), e.secret("WHATSAPP_PHONE_ID"))
	if token == "" || phoneID == "" {
		return nil, fmt.Errorf("whatsapp requires WHATSAPP_TOKEN and a phone_number_id")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://graph.facebook.com")
	headers := map[string]string{"Authorization": "Bearer " + token}
	to := str(in["to"])
	text := firstNonEmpty(str(in["text"]), str(in["message"]), str(in["body"]))
	if to == "" || text == "" {
		return nil, fmt.Errorf("whatsapp send_message requires 'to' and 'text'")
	}
	payload := map[string]any{
		"messaging_product": "whatsapp", "to": to, "type": "text",
		"text": map[string]any{"body": text},
	}
	return e.connectorJSON("whatsapp", "POST", base+"/v19.0/"+phoneID+"/messages", headers, payload, map[string]any{"sent": true})
}

// Coda. Secret: CODA_TOKEN. insert_row (default): doc_id, table_id, cells(JSON map col→val)
func (e *Executor) callCoda(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("CODA_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("coda requires CODA_TOKEN")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://coda.io")
	headers := map[string]string{"Authorization": "Bearer " + token}
	switch defaultAction(action, "insert_row") {
	case "insert_row", "create":
		doc := str(in["doc_id"])
		table := str(in["table_id"])
		cells := asMap(in["cells"])
		if doc == "" || table == "" || len(cells) == 0 {
			return nil, fmt.Errorf("coda insert_row requires 'doc_id', 'table_id', and 'cells'")
		}
		var cellArr []any
		for col, val := range cells {
			cellArr = append(cellArr, map[string]any{"column": col, "value": val})
		}
		payload := map[string]any{"rows": []any{map[string]any{"cells": cellArr}}}
		return e.connectorJSON("coda", "POST", base+"/apis/v1/docs/"+doc+"/tables/"+table+"/rows", headers, payload, map[string]any{"inserted": true})
	default:
		return nil, fmt.Errorf("coda: unknown action %q (supported: insert_row)", action)
	}
}

// Close CRM. Secret: CLOSE_API_KEY. create_lead (default): name
func (e *Executor) callClose(action string, in map[string]any) (map[string]any, error) {
	key := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("CLOSE_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("close requires CLOSE_API_KEY")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.close.com")
	switch defaultAction(action, "create_lead") {
	case "create_lead", "create":
		name := str(in["name"])
		if name == "" {
			return nil, fmt.Errorf("close create_lead requires 'name'")
		}
		payload := map[string]any{"name": name}
		out, err := e.connectorBasic("close", "POST", base+"/api/v1/lead/", key, "", payload, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["lead_id"] = m["id"]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("close: unknown action %q (supported: create_lead)", action)
	}
}

// Calendly (read). Secret: CALENDLY_TOKEN. me (default): returns current user.
func (e *Executor) callCalendly(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("CALENDLY_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("calendly requires CALENDLY_TOKEN")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.calendly.com")
	headers := map[string]string{"Authorization": "Bearer " + token}
	switch defaultAction(action, "me") {
	case "me", "get":
		return e.connectorJSON("calendly", "GET", base+"/users/me", headers, nil, map[string]any{"ok": true})
	default:
		return nil, fmt.Errorf("calendly: unknown action %q (supported: me)", action)
	}
}

// ServiceNow. Secrets: SERVICENOW_USER + SERVICENOW_PASSWORD. Requires base_url.
//   create_incident (default): short_description, description
func (e *Executor) callServiceNow(action string, in map[string]any) (map[string]any, error) {
	user := firstNonEmpty(str(in["user"]), e.secret("SERVICENOW_USER"))
	pass := firstNonEmpty(e.resolveSecretRef(in["password"]), e.secret("SERVICENOW_PASSWORD"))
	site := firstNonEmpty(str(in["base_url"]), e.secret("SERVICENOW_BASE_URL"))
	if user == "" || pass == "" || site == "" {
		return nil, fmt.Errorf("servicenow requires SERVICENOW_USER, SERVICENOW_PASSWORD and a base_url")
	}
	site = strings.TrimRight(site, "/")
	switch defaultAction(action, "create_incident") {
	case "create_incident", "create":
		short := firstNonEmpty(str(in["short_description"]), str(in["summary"]))
		if short == "" {
			return nil, fmt.Errorf("servicenow create_incident requires 'short_description'")
		}
		payload := map[string]any{"short_description": short}
		if d := str(in["description"]); d != "" {
			payload["description"] = d
		}
		out, err := e.connectorBasic("servicenow", "POST", site+"/api/now/table/incident", user, pass, payload, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			if res, ok := m["result"].(map[string]any); ok {
				out["sys_id"] = res["sys_id"]
				out["number"] = res["number"]
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("servicenow: unknown action %q (supported: create_incident)", action)
	}
}
