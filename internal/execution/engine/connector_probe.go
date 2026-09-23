// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"github.com/regnant/knott/internal/connectors"
	"net/url"
	"strings"
)

// TestConnection validates a connector without performing its business action.
// This is deliberately separate from TestToolCall: a credentials screen must
// not create a ticket, charge a card or send a message merely to prove auth.
func (e *Executor) TestConnection(connectorID string) (map[string]any, error) {
	id := strings.ToLower(strings.TrimSpace(connectorID))
	need := func(names ...string) error {
		for _, name := range names {
			if e.secret(name) == "" {
				return fmt.Errorf("%s is not configured", name)
			}
		}
		return nil
	}
	configuredOnly := func(detail string, names ...string) (map[string]any, error) {
		if err := need(names...); err != nil {
			return nil, err
		}
		return map[string]any{"validated": "configuration", "detail": detail}, nil
	}
	getBearer := func(name, target string, headers map[string]string) (map[string]any, error) {
		token := e.secret(name)
		if token == "" {
			return nil, fmt.Errorf("%s is not configured", name)
		}
		if headers == nil {
			headers = map[string]string{}
		}
		headers["Authorization"] = "Bearer " + token
		return e.connectorJSON(id, "GET", target, headers, nil, map[string]any{"validated": "live"})
	}
	validURL := func(name string) (string, error) {
		raw := normalizeBaseURL(e.secret(name))
		if raw == "" {
			return "", fmt.Errorf("%s is not configured", name)
		}
		u, err := url.ParseRequestURI(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", fmt.Errorf("%s must be a valid http(s) URL", name)
		}
		return strings.TrimRight(raw, "/"), nil
	}

	switch id {
	case "webhook", "graphql":
		return map[string]any{"validated": "configuration", "detail": "No global credentials are required; configure and test the endpoint in the workflow builder."}, nil
	case "slack":
		if token := e.secret("SLACK_BOT_TOKEN"); token != "" {
			return e.connectorJSON(id, "POST", "https://slack.com/api/auth.test", map[string]string{"Authorization": "Bearer " + token}, map[string]any{}, map[string]any{"validated": "live"})
		}
		_, err := validURL("SLACK_WEBHOOK_URL")
		if err != nil {
			return nil, fmt.Errorf("configure SLACK_BOT_TOKEN or SLACK_WEBHOOK_URL: %w", err)
		}
		return map[string]any{"validated": "configuration", "detail": "Webhook URL is valid. KNOTT does not send a test message from the credentials screen."}, nil
	case "sendgrid":
		return getBearer("SENDGRID_API_KEY", "https://api.sendgrid.com/v3/user/profile", nil)
	case "twilio":
		if err := need("TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_FROM_NUMBER"); err != nil {
			return nil, err
		}
		sid := e.secret("TWILIO_ACCOUNT_SID")
		return e.connectorBasic(id, "GET", "https://api.twilio.com/2010-04-01/Accounts/"+url.PathEscape(sid)+".json", sid, e.secret("TWILIO_AUTH_TOKEN"), nil, map[string]any{"validated": "live"})
	case "telegram":
		token := e.secret("TELEGRAM_BOT_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is not configured")
		}
		return e.connectorJSON(id, "GET", "https://api.telegram.org/bot"+token+"/getMe", nil, nil, map[string]any{"validated": "live"})
	case "discord":
		hook, err := validURL("DISCORD_WEBHOOK_URL")
		if err != nil {
			return nil, err
		}
		return e.connectorJSON(id, "GET", hook, nil, nil, map[string]any{"validated": "live"})
	case "teams":
		_, err := validURL("TEAMS_WEBHOOK_URL")
		if err != nil {
			return nil, err
		}
		return map[string]any{"validated": "configuration", "detail": "Webhook URL is valid. No message was sent."}, nil
	case "mattermost":
		_, err := validURL("MATTERMOST_WEBHOOK_URL")
		if err != nil {
			return nil, err
		}
		return map[string]any{"validated": "configuration", "detail": "Webhook URL is valid. No message was sent."}, nil
	case "whatsapp":
		if err := need("WHATSAPP_TOKEN", "WHATSAPP_PHONE_ID"); err != nil {
			return nil, err
		}
		return getBearer("WHATSAPP_TOKEN", "https://graph.facebook.com/v19.0/"+url.PathEscape(e.secret("WHATSAPP_PHONE_ID")), nil)
	case "ms_graph", "onedrive":
		path := "/v1.0/me"
		if id == "onedrive" {
			path = "/v1.0/me/drive"
		}
		return getBearer("MS_GRAPH_TOKEN", "https://graph.microsoft.com"+path, nil)
	case "pushover":
		return configuredOnly("Pushover credentials are present. Validation does not send a push notification.", "PUSHOVER_TOKEN", "PUSHOVER_USER")

	case "github":
		return getBearer("GITHUB_TOKEN", "https://api.github.com/user", map[string]string{"Accept": "application/vnd.github+json"})
	case "gitlab":
		token := e.secret("GITLAB_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("GITLAB_TOKEN is not configured")
		}
		return e.connectorJSON(id, "GET", "https://gitlab.com/api/v4/user", map[string]string{"PRIVATE-TOKEN": token}, nil, map[string]any{"validated": "live"})
	case "linear":
		token := e.secret("LINEAR_API_KEY")
		if token == "" {
			return nil, fmt.Errorf("LINEAR_API_KEY is not configured")
		}
		return e.connectorJSON(id, "POST", "https://api.linear.app/graphql", map[string]string{"Authorization": token}, map[string]any{"query": "{ viewer { id name } }"}, map[string]any{"validated": "live"})
	case "jira":
		if err := need("JIRA_BASE_URL", "JIRA_EMAIL", "JIRA_API_TOKEN"); err != nil {
			return nil, err
		}
		return e.connectorBasic(id, "GET", normalizeBaseURL(e.secret("JIRA_BASE_URL"))+"/rest/api/3/myself", e.secret("JIRA_EMAIL"), e.secret("JIRA_API_TOKEN"), nil, map[string]any{"validated": "live"})
	case "zendesk":
		if err := need("ZENDESK_BASE_URL", "ZENDESK_EMAIL", "ZENDESK_API_TOKEN"); err != nil {
			return nil, err
		}
		return e.connectorBasic(id, "GET", normalizeBaseURL(e.secret("ZENDESK_BASE_URL"))+"/api/v2/users/me.json", e.secret("ZENDESK_EMAIL")+"/token", e.secret("ZENDESK_API_TOKEN"), nil, map[string]any{"validated": "live"})
	case "freshdesk":
		if err := need("FRESHDESK_BASE_URL", "FRESHDESK_API_KEY"); err != nil {
			return nil, err
		}
		return e.connectorBasic(id, "GET", normalizeBaseURL(e.secret("FRESHDESK_BASE_URL"))+"/api/v2/agents/me", e.secret("FRESHDESK_API_KEY"), "X", nil, map[string]any{"validated": "live"})
	case "servicenow":
		if err := need("SERVICENOW_BASE_URL", "SERVICENOW_USER", "SERVICENOW_PASSWORD"); err != nil {
			return nil, err
		}
		return e.connectorBasic(id, "GET", normalizeBaseURL(e.secret("SERVICENOW_BASE_URL"))+"/api/now/table/sys_user?sysparm_limit=1", e.secret("SERVICENOW_USER"), e.secret("SERVICENOW_PASSWORD"), nil, map[string]any{"validated": "live"})
	case "pagerduty":
		return configuredOnly("Routing key is present. KNOTT does not create a test incident from the credentials screen.", "PAGERDUTY_ROUTING_KEY")

	case "hubspot":
		return getBearer("HUBSPOT_TOKEN", "https://api.hubapi.com/crm/v3/objects/contacts?limit=1", nil)
	case "intercom":
		return getBearer("INTERCOM_TOKEN", "https://api.intercom.io/me", map[string]string{"Intercom-Version": "2.11"})
	case "close":
		if err := need("CLOSE_API_KEY"); err != nil {
			return nil, err
		}
		return e.connectorBasic(id, "GET", "https://api.close.com/api/v1/me/", e.secret("CLOSE_API_KEY"), "", nil, map[string]any{"validated": "live"})

	case "notion":
		return getBearer("NOTION_TOKEN", "https://api.notion.com/v1/users/me", map[string]string{"Notion-Version": "2022-06-28"})
	case "google_sheets", "google_calendar", "google_drive":
		token, err := e.googleAccessToken(map[string]any{})
		if err != nil {
			return nil, err
		}
		return e.connectorJSON(id, "GET", "https://www.googleapis.com/oauth2/v3/tokeninfo?access_token="+url.QueryEscape(token), nil, nil, map[string]any{"validated": "live"})
	case "trello":
		if err := need("TRELLO_KEY", "TRELLO_TOKEN"); err != nil {
			return nil, err
		}
		target := "https://api.trello.com/1/members/me?key=" + url.QueryEscape(e.secret("TRELLO_KEY")) + "&token=" + url.QueryEscape(e.secret("TRELLO_TOKEN"))
		return e.connectorJSON(id, "GET", target, nil, nil, map[string]any{"validated": "live"})
	case "asana":
		return getBearer("ASANA_TOKEN", "https://app.asana.com/api/1.0/users/me", nil)
	case "clickup":
		token := e.secret("CLICKUP_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("CLICKUP_TOKEN is not configured")
		}
		return e.connectorJSON(id, "GET", "https://api.clickup.com/api/v2/user", map[string]string{"Authorization": token}, nil, map[string]any{"validated": "live"})
	case "monday":
		token := e.secret("MONDAY_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("MONDAY_TOKEN is not configured")
		}
		return e.connectorJSON(id, "POST", "https://api.monday.com/v2", map[string]string{"Authorization": token}, map[string]any{"query": "{ me { id name } }"}, map[string]any{"validated": "live"})
	case "coda":
		return getBearer("CODA_TOKEN", "https://coda.io/apis/v1/whoami", nil)
	case "calendly":
		return getBearer("CALENDLY_TOKEN", "https://api.calendly.com/users/me", nil)

	case "airtable":
		return getBearer("AIRTABLE_TOKEN", "https://api.airtable.com/v0/meta/whoami", nil)
	case "database":
		return configuredOnly("Connection string is present. Run SELECT 1 in the builder to validate the selected database driver.", "DATABASE_DSN")
	case "stripe":
		if err := need("STRIPE_SECRET_KEY"); err != nil {
			return nil, err
		}
		return e.connectorBasic(id, "GET", "https://api.stripe.com/v1/account", e.secret("STRIPE_SECRET_KEY"), "", nil, map[string]any{"validated": "live"})
	case "shopify":
		if err := need("SHOPIFY_STORE_URL", "SHOPIFY_ACCESS_TOKEN"); err != nil {
			return nil, err
		}
		return e.connectorJSON(id, "GET", normalizeBaseURL(e.secret("SHOPIFY_STORE_URL"))+"/admin/api/2024-04/shop.json", map[string]string{"X-Shopify-Access-Token": e.secret("SHOPIFY_ACCESS_TOKEN")}, nil, map[string]any{"validated": "live"})
	case "mailchimp":
		key := e.secret("MAILCHIMP_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("MAILCHIMP_API_KEY is not configured")
		}
		parts := strings.Split(key, "-")
		if len(parts) < 2 {
			return nil, fmt.Errorf("MAILCHIMP_API_KEY is missing its datacenter suffix (for example -us21)")
		}
		return e.connectorBasic(id, "GET", "https://"+parts[len(parts)-1]+".api.mailchimp.com/3.0/ping", "anystring", key, nil, map[string]any{"validated": "live"})

	case "openai":
		return getBearer("OPENAI_API_KEY", "https://api.openai.com/v1/models", nil)
	case "anthropic":
		// Anthropic has no no-cost identity endpoint; models is the least invasive auth check.
		key := e.secret("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is not configured")
		}
		return e.connectorJSON(id, "GET", "https://api.anthropic.com/v1/models?limit=1", map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}, nil, map[string]any{"validated": "live"})
	case "gemini":
		key := e.secret("GEMINI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY is not configured")
		}
		return e.connectorJSON(id, "GET", "https://generativelanguage.googleapis.com/v1beta/models?key="+url.QueryEscape(key), nil, nil, map[string]any{"validated": "live"})
	case "groq":
		return getBearer("GROQ_API_KEY", "https://api.groq.com/openai/v1/models", nil)
	case "cohere":
		return getBearer("COHERE_API_KEY", "https://api.cohere.com/v1/models", nil)

	case "dropbox":
		token := e.secret("DROPBOX_ACCESS_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("DROPBOX_ACCESS_TOKEN is not configured")
		}
		return e.connectorJSON(id, "POST", "https://api.dropboxapi.com/2/users/get_current_account", map[string]string{"Authorization": "Bearer " + token}, map[string]any{}, map[string]any{"validated": "live"})
	case "box":
		return getBearer("BOX_ACCESS_TOKEN", "https://api.box.com/2.0/users/me", nil)
	case "cloudflare":
		return getBearer("CLOUDFLARE_API_TOKEN", "https://api.cloudflare.com/client/v4/user/tokens/verify", nil)
	case "digitalocean":
		return getBearer("DIGITALOCEAN_TOKEN", "https://api.digitalocean.com/v2/account", nil)
	case "datadog":
		if err := need("DATADOG_API_KEY", "DATADOG_APP_KEY"); err != nil {
			return nil, err
		}
		site := firstNonEmpty(e.secret("DATADOG_SITE"), "datadoghq.com")
		return e.connectorJSON(id, "GET", "https://api."+site+"/api/v1/validate", map[string]string{"DD-API-KEY": e.secret("DATADOG_API_KEY"), "DD-APPLICATION-KEY": e.secret("DATADOG_APP_KEY")}, nil, map[string]any{"validated": "live"})
	case "newrelic":
		key := e.secret("NEW_RELIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("NEW_RELIC_API_KEY is not configured")
		}
		return e.connectorJSON(id, "POST", "https://api.newrelic.com/graphql", map[string]string{"API-Key": key}, map[string]any{"query": "{ actor { user { name } } }"}, map[string]any{"validated": "live"})
	case "sentry":
		return getBearer("SENTRY_AUTH_TOKEN", "https://sentry.io/api/0/", nil)
	case "grafana":
		base, err := validURL("GRAFANA_URL")
		if err != nil {
			return nil, err
		}
		if err := need("GRAFANA_TOKEN"); err != nil {
			return nil, err
		}
		return e.connectorJSON(id, "GET", base+"/api/org", map[string]string{"Authorization": "Bearer " + e.secret("GRAFANA_TOKEN")}, nil, map[string]any{"validated": "live"})
	case "elasticsearch":
		base, err := validURL("ELASTICSEARCH_URL")
		if err != nil {
			return nil, err
		}
		if err := need("ELASTICSEARCH_API_KEY"); err != nil {
			return nil, err
		}
		return e.connectorJSON(id, "GET", base+"/_cluster/health", map[string]string{"Authorization": "ApiKey " + e.secret("ELASTICSEARCH_API_KEY")}, nil, map[string]any{"validated": "live"})
	case "supabase":
		base, err := validURL("SUPABASE_URL")
		if err != nil {
			return nil, err
		}
		if err := need("SUPABASE_SERVICE_KEY"); err != nil {
			return nil, err
		}
		return e.connectorJSON(id, "GET", base+"/rest/v1/", map[string]string{"apikey": e.secret("SUPABASE_SERVICE_KEY"), "Authorization": "Bearer " + e.secret("SUPABASE_SERVICE_KEY")}, nil, map[string]any{"validated": "live"})
	case "mongodb_atlas":
		return configuredOnly("Data API endpoint and key are present. Select a data source/database/collection in the builder for a live query.", "MONGODB_DATA_API_URL", "MONGODB_DATA_API_KEY")
	case "rabbitmq":
		base, err := validURL("RABBITMQ_URL")
		if err != nil {
			return nil, err
		}
		if err := need("RABBITMQ_USER", "RABBITMQ_PASSWORD"); err != nil {
			return nil, err
		}
		return e.connectorBasic(id, "GET", base+"/api/whoami", e.secret("RABBITMQ_USER"), e.secret("RABBITMQ_PASSWORD"), nil, map[string]any{"validated": "live"})
	case "kafka_rest":
		base, err := validURL("KAFKA_REST_URL")
		if err != nil {
			return nil, err
		}
		headers := map[string]string{}
		if token := e.secret("KAFKA_REST_TOKEN"); token != "" {
			headers["Authorization"] = "Bearer " + token
		}
		return e.connectorJSON(id, "GET", base+"/topics", headers, nil, map[string]any{"validated": "live"})
	case "zoom":
		return getBearer("ZOOM_ACCESS_TOKEN", "https://api.zoom.us/v2/users/me", nil)
	case "typeform":
		return getBearer("TYPEFORM_TOKEN", "https://api.typeform.com/me", nil)
	case "surveymonkey":
		return getBearer("SURVEYMONKEY_TOKEN", "https://api.surveymonkey.com/v3/users/me", nil)
	case "wordpress":
		base, err := validURL("WORDPRESS_URL")
		if err != nil {
			return nil, err
		}
		if err := need("WORDPRESS_USER", "WORDPRESS_APP_PASSWORD"); err != nil {
			return nil, err
		}
		return e.connectorBasic(id, "GET", base+"/wp-json/wp/v2/users/me?context=edit", e.secret("WORDPRESS_USER"), e.secret("WORDPRESS_APP_PASSWORD"), nil, map[string]any{"validated": "live"})
	case "woocommerce":
		base, err := validURL("WOOCOMMERCE_URL")
		if err != nil {
			return nil, err
		}
		if err := need("WOOCOMMERCE_KEY", "WOOCOMMERCE_SECRET"); err != nil {
			return nil, err
		}
		return e.connectorBasic(id, "GET", base+"/wp-json/wc/v3/system_status", e.secret("WOOCOMMERCE_KEY"), e.secret("WOOCOMMERCE_SECRET"), nil, map[string]any{"validated": "live"})
	case "quickbooks":
		return configuredOnly("OAuth token and company realm are present. Use a workflow query for a live company-data test.", "QUICKBOOKS_ACCESS_TOKEN", "QUICKBOOKS_REALM_ID")
	case "x_twitter":
		return getBearer("X_BEARER_TOKEN", "https://api.x.com/2/users/me", nil)
	}
	if def, ok := connectors.Get(id); ok && !def.Native && def.HTTP != nil {
		return e.testDeclarative(def)
	}
	return nil, fmt.Errorf("unknown connector %q", connectorID)
}
