// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"net/url"
	"strings"
)

// callWave3Connector contains the compact REST adapters used by the expanded
// catalog. Every adapter validates its operation inputs, obtains secrets from
// encrypted storage/environment, supports a base_url override for tests and
// returns the same normalized {status,response,...} shape as older connectors.
func (e *Executor) callWave3Connector(id, action string, in map[string]any) (map[string]any, error) {
	bearer := func(key string) (string, error) {
		v := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret(key))
		if v == "" {
			return "", fmt.Errorf("%s requires %s", id, key)
		}
		return v, nil
	}
	prompt := func() (string, error) {
		v := firstNonEmpty(str(in["prompt"]), str(in["text"]), str(in["message"]))
		if v == "" {
			return "", fmt.Errorf("%s requires 'prompt'", id)
		}
		return v, nil
	}

	switch id {
	case "anthropic":
		key, err := bearer("ANTHROPIC_API_KEY")
		if err != nil {
			return nil, err
		}
		p, err := prompt()
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.anthropic.com")
		body := map[string]any{"model": firstNonEmpty(str(in["model"]), "claude-3-5-haiku-latest"), "max_tokens": intOr(in["max_tokens"], 1024), "messages": []any{map[string]any{"role": "user", "content": p}}}
		if s := str(in["system"]); s != "" {
			body["system"] = s
		}
		return e.connectorJSON(id, "POST", base+"/v1/messages", map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}, body, map[string]any{"generated": true})

	case "gemini":
		key, err := bearer("GEMINI_API_KEY")
		if err != nil {
			return nil, err
		}
		p, err := prompt()
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://generativelanguage.googleapis.com")
		model := firstNonEmpty(str(in["model"]), "gemini-2.0-flash")
		body := map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]any{"text": p}}}}}
		return e.connectorJSON(id, "POST", base+"/v1beta/models/"+url.PathEscape(model)+":generateContent?key="+url.QueryEscape(key), nil, body, map[string]any{"generated": true})

	case "groq":
		key, err := bearer("GROQ_API_KEY")
		if err != nil {
			return nil, err
		}
		p, err := prompt()
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.groq.com/openai")
		body := map[string]any{"model": firstNonEmpty(str(in["model"]), "llama-3.3-70b-versatile"), "messages": []any{map[string]any{"role": "user", "content": p}}}
		return e.connectorJSON(id, "POST", base+"/v1/chat/completions", map[string]string{"Authorization": "Bearer " + key}, body, map[string]any{"generated": true})

	case "cohere":
		key, err := bearer("COHERE_API_KEY")
		if err != nil {
			return nil, err
		}
		p, err := prompt()
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.cohere.com")
		body := map[string]any{"model": firstNonEmpty(str(in["model"]), "command-r-plus"), "messages": []any{map[string]any{"role": "user", "content": p}}}
		return e.connectorJSON(id, "POST", base+"/v2/chat", map[string]string{"Authorization": "Bearer " + key}, body, map[string]any{"generated": true})

	case "dropbox":
		token, err := bearer("DROPBOX_ACCESS_TOKEN")
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.dropboxapi.com")
		body := map[string]any{"path": str(in["path"])}
		return e.connectorJSON(id, "POST", base+"/2/files/list_folder", map[string]string{"Authorization": "Bearer " + token}, body, map[string]any{"listed": true})

	case "box":
		token, err := bearer("BOX_ACCESS_TOKEN")
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.box.com")
		folder := firstNonEmpty(str(in["folder_id"]), "0")
		return e.connectorJSON(id, "GET", base+"/2.0/folders/"+url.PathEscape(folder)+"/items", map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})

	case "google_drive":
		token, err := e.googleAccessToken(in)
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://www.googleapis.com")
		endpoint := base + "/drive/v3/files?pageSize=100&fields=files(id,name,mimeType,webViewLink)"
		if q := str(in["query"]); q != "" {
			endpoint += "&q=" + url.QueryEscape(q)
		}
		return e.connectorJSON(id, "GET", endpoint, map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})

	case "onedrive":
		token, err := bearer("MS_GRAPH_TOKEN")
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://graph.microsoft.com")
		return e.connectorJSON(id, "GET", base+"/v1.0/me/drive/root/children", map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})

	case "cloudflare":
		token, err := bearer("CLOUDFLARE_API_TOKEN")
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.cloudflare.com/client/v4")
		return e.connectorJSON(id, "GET", base+"/zones?per_page=50", map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})

	case "digitalocean":
		token, err := bearer("DIGITALOCEAN_TOKEN")
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.digitalocean.com")
		return e.connectorJSON(id, "GET", base+"/v2/droplets?per_page=100", map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})

	case "datadog":
		api := e.secret("DATADOG_API_KEY")
		app := e.secret("DATADOG_APP_KEY")
		if api == "" || app == "" {
			return nil, fmt.Errorf("datadog requires DATADOG_API_KEY and DATADOG_APP_KEY")
		}
		text := firstNonEmpty(str(in["text"]), str(in["message"]))
		if text == "" {
			return nil, fmt.Errorf("datadog submit_event requires 'text'")
		}
		site := firstNonEmpty(e.secret("DATADOG_SITE"), "datadoghq.com")
		base := firstNonEmpty(str(in["base_url"]), "https://api."+site)
		body := map[string]any{"title": firstNonEmpty(str(in["title"]), "KNOTT event"), "text": text}
		return e.connectorJSON(id, "POST", base+"/api/v1/events", map[string]string{"DD-API-KEY": api, "DD-APPLICATION-KEY": app}, body, map[string]any{"submitted": true})

	case "newrelic":
		key, err := bearer("NEW_RELIC_API_KEY")
		if err != nil {
			return nil, err
		}
		query := firstNonEmpty(str(in["query"]), `{ actor { user { name email } } }`)
		base := firstNonEmpty(str(in["base_url"]), "https://api.newrelic.com")
		return e.connectorJSON(id, "POST", base+"/graphql", map[string]string{"API-Key": key}, map[string]any{"query": query}, map[string]any{"queried": true})

	case "sentry":
		token, err := bearer("SENTRY_AUTH_TOKEN")
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://sentry.io")
		return e.connectorJSON(id, "GET", base+"/api/0/projects/", map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})

	case "grafana":
		token := e.secret("GRAFANA_TOKEN")
		base := normalizeBaseURL(firstNonEmpty(str(in["base_url"]), e.secret("GRAFANA_URL")))
		if token == "" || base == "" {
			return nil, fmt.Errorf("grafana requires GRAFANA_URL and GRAFANA_TOKEN")
		}
		return e.connectorJSON(id, "GET", strings.TrimRight(base, "/")+"/api/search?query="+url.QueryEscape(str(in["query"])), map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})

	case "elasticsearch":
		key := e.secret("ELASTICSEARCH_API_KEY")
		base := normalizeBaseURL(firstNonEmpty(str(in["base_url"]), e.secret("ELASTICSEARCH_URL")))
		if key == "" || base == "" {
			return nil, fmt.Errorf("elasticsearch requires ELASTICSEARCH_URL and ELASTICSEARCH_API_KEY")
		}
		index := str(in["index"])
		if index == "" {
			return nil, fmt.Errorf("elasticsearch search requires 'index'")
		}
		body := asMap(in["query"])
		if body == nil {
			body = map[string]any{"query": map[string]any{"match_all": map[string]any{}}}
		}
		return e.connectorJSON(id, "POST", strings.TrimRight(base, "/")+"/"+url.PathEscape(index)+"/_search", map[string]string{"Authorization": "ApiKey " + key}, body, map[string]any{"searched": true})

	case "supabase":
		key := e.secret("SUPABASE_SERVICE_KEY")
		base := normalizeBaseURL(firstNonEmpty(str(in["base_url"]), e.secret("SUPABASE_URL")))
		if key == "" || base == "" {
			return nil, fmt.Errorf("supabase requires SUPABASE_URL and SUPABASE_SERVICE_KEY")
		}
		table := str(in["table"])
		if table == "" {
			return nil, fmt.Errorf("supabase requires 'table'")
		}
		headers := map[string]string{"apikey": key, "Authorization": "Bearer " + key}
		if defaultAction(action, "select") == "insert" {
			body := asMap(in["record"])
			if body == nil {
				return nil, fmt.Errorf("supabase insert requires 'record' JSON")
			}
			return e.connectorJSON(id, "POST", strings.TrimRight(base, "/")+"/rest/v1/"+url.PathEscape(table), headers, body, map[string]any{"inserted": true})
		}
		return e.connectorJSON(id, "GET", strings.TrimRight(base, "/")+"/rest/v1/"+url.PathEscape(table)+"?select="+url.QueryEscape(firstNonEmpty(str(in["select"]), "*")), headers, nil, map[string]any{"listed": true})

	case "mongodb_atlas":
		key := e.secret("MONGODB_DATA_API_KEY")
		base := normalizeBaseURL(firstNonEmpty(str(in["base_url"]), e.secret("MONGODB_DATA_API_URL")))
		if key == "" || base == "" {
			return nil, fmt.Errorf("mongodb_atlas requires MONGODB_DATA_API_URL and MONGODB_DATA_API_KEY")
		}
		collection := str(in["collection"])
		db := str(in["database"])
		source := str(in["data_source"])
		if collection == "" || db == "" || source == "" {
			return nil, fmt.Errorf("mongodb_atlas requires 'data_source', 'database', and 'collection'")
		}
		body := map[string]any{"dataSource": source, "database": db, "collection": collection, "filter": asMap(in["filter"])}
		return e.connectorJSON(id, "POST", strings.TrimRight(base, "/")+"/action/findOne", map[string]string{"api-key": key}, body, map[string]any{"found": true})

	case "rabbitmq":
		base := normalizeBaseURL(firstNonEmpty(str(in["base_url"]), e.secret("RABBITMQ_URL")))
		user := e.secret("RABBITMQ_USER")
		pass := e.secret("RABBITMQ_PASSWORD")
		if base == "" || user == "" || pass == "" {
			return nil, fmt.Errorf("rabbitmq requires RABBITMQ_URL, RABBITMQ_USER and RABBITMQ_PASSWORD")
		}
		vhost := firstNonEmpty(str(in["vhost"]), "/")
		exchange := str(in["exchange"])
		body := map[string]any{"properties": map[string]any{}, "routing_key": str(in["routing_key"]), "payload": str(in["payload"]), "payload_encoding": "string"}
		return e.connectorBasic(id, "POST", strings.TrimRight(base, "/")+"/api/exchanges/"+url.PathEscape(vhost)+"/"+url.PathEscape(exchange)+"/publish", user, pass, body, map[string]any{"published": true})

	case "kafka_rest":
		base := normalizeBaseURL(firstNonEmpty(str(in["base_url"]), e.secret("KAFKA_REST_URL")))
		if base == "" {
			return nil, fmt.Errorf("kafka_rest requires KAFKA_REST_URL")
		}
		topic := str(in["topic"])
		if topic == "" {
			return nil, fmt.Errorf("kafka_rest requires 'topic'")
		}
		headers := map[string]string{"Content-Type": "application/vnd.kafka.json.v2+json"}
		if token := e.secret("KAFKA_REST_TOKEN"); token != "" {
			headers["Authorization"] = "Bearer " + token
		}
		body := map[string]any{"records": []any{map[string]any{"key": in["key"], "value": in["value"]}}}
		return e.connectorJSON(id, "POST", strings.TrimRight(base, "/")+"/topics/"+url.PathEscape(topic), headers, body, map[string]any{"published": true})

	case "zoom":
		token, err := bearer("ZOOM_ACCESS_TOKEN")
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.zoom.us")
		if defaultAction(action, "list_users") == "create_meeting" {
			body := map[string]any{"topic": firstNonEmpty(str(in["topic"]), "KNOTT meeting"), "type": 2}
			if start := str(in["start_time"]); start != "" {
				body["start_time"] = start
			}
			user := firstNonEmpty(str(in["user_id"]), "me")
			return e.connectorJSON(id, "POST", base+"/v2/users/"+url.PathEscape(user)+"/meetings", map[string]string{"Authorization": "Bearer " + token}, body, map[string]any{"created": true})
		}
		return e.connectorJSON(id, "GET", base+"/v2/users?page_size=30", map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})

	case "typeform":
		token, err := bearer("TYPEFORM_TOKEN")
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.typeform.com")
		if form := str(in["form_id"]); form != "" {
			return e.connectorJSON(id, "GET", base+"/forms/"+url.PathEscape(form)+"/responses?page_size=100", map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})
		}
		return e.connectorJSON(id, "GET", base+"/forms?page_size=100", map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})

	case "surveymonkey":
		token, err := bearer("SURVEYMONKEY_TOKEN")
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.surveymonkey.com")
		return e.connectorJSON(id, "GET", base+"/v3/surveys?per_page=100", map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"listed": true})

	case "wordpress":
		base := normalizeBaseURL(firstNonEmpty(str(in["base_url"]), e.secret("WORDPRESS_URL")))
		user := e.secret("WORDPRESS_USER")
		pass := e.secret("WORDPRESS_APP_PASSWORD")
		if base == "" || user == "" || pass == "" {
			return nil, fmt.Errorf("wordpress requires WORDPRESS_URL, WORDPRESS_USER and WORDPRESS_APP_PASSWORD")
		}
		if defaultAction(action, "list_posts") == "create_post" {
			title := str(in["title"])
			if title == "" {
				return nil, fmt.Errorf("wordpress create_post requires 'title'")
			}
			body := map[string]any{"title": title, "content": str(in["content"]), "status": firstNonEmpty(str(in["status"]), "draft")}
			return e.connectorBasic(id, "POST", strings.TrimRight(base, "/")+"/wp-json/wp/v2/posts", user, pass, body, map[string]any{"created": true})
		}
		return e.connectorBasic(id, "GET", strings.TrimRight(base, "/")+"/wp-json/wp/v2/posts?per_page=20", user, pass, nil, map[string]any{"listed": true})

	case "woocommerce":
		base := normalizeBaseURL(firstNonEmpty(str(in["base_url"]), e.secret("WOOCOMMERCE_URL")))
		key := e.secret("WOOCOMMERCE_KEY")
		secret := e.secret("WOOCOMMERCE_SECRET")
		if base == "" || key == "" || secret == "" {
			return nil, fmt.Errorf("woocommerce requires WOOCOMMERCE_URL, WOOCOMMERCE_KEY and WOOCOMMERCE_SECRET")
		}
		return e.connectorBasic(id, "GET", strings.TrimRight(base, "/")+"/wp-json/wc/v3/products?per_page=50", key, secret, nil, map[string]any{"listed": true})

	case "quickbooks":
		token, err := bearer("QUICKBOOKS_ACCESS_TOKEN")
		if err != nil {
			return nil, err
		}
		realm := e.secret("QUICKBOOKS_REALM_ID")
		if realm == "" {
			return nil, fmt.Errorf("quickbooks requires QUICKBOOKS_REALM_ID")
		}
		base := firstNonEmpty(str(in["base_url"]), "https://quickbooks.api.intuit.com")
		query := firstNonEmpty(str(in["query"]), "select * from Customer maxresults 100")
		endpoint := base + "/v3/company/" + url.PathEscape(realm) + "/query?minorversion=75&query=" + url.QueryEscape(query)
		return e.connectorJSON(id, "GET", endpoint, map[string]string{"Authorization": "Bearer " + token, "Accept": "application/json"}, nil, map[string]any{"queried": true})

	case "x_twitter":
		token, err := bearer("X_BEARER_TOKEN")
		if err != nil {
			return nil, err
		}
		base := firstNonEmpty(str(in["base_url"]), "https://api.x.com")
		query := str(in["query"])
		if query == "" {
			return nil, fmt.Errorf("x_twitter recent_search requires 'query'")
		}
		return e.connectorJSON(id, "GET", base+"/2/tweets/search/recent?max_results=10&query="+url.QueryEscape(query), map[string]string{"Authorization": "Bearer " + token}, nil, map[string]any{"searched": true})
	}
	return nil, fmt.Errorf("connector %q is not implemented", id)
}
