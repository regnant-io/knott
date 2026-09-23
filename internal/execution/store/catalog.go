// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package store

import "github.com/regnant/knott/internal/connectors"

// The connector catalog: one source of truth for every integration KNOTT ships.
//
// A connector entry carries a stable slug, presentation metadata, and the exact
// credentials it needs — each with a human label and a line of help explaining
// where to find it. The slug is what the executor dispatches on and what a
// workflow definition stores, so renaming a connector in the UI never breaks a
// saved workflow. The credential specs are what let the console render the
// connector's own credential form instead of a separate wall of secret names.

// CredentialSpec describes one secret a connector needs.
type CredentialSpec struct {
	// Name is the environment-variable-style key the secret is stored under.
	Name string `json:"name"`
	// Label is what an operator reads next to the field.
	Label string `json:"label"`
	// Help says where to obtain the value. Shown under the field.
	Help string `json:"help"`
	// Optional marks a credential the connector can work without, either because
	// there is an alternative (AltOf) or because it only unlocks extra actions.
	Optional bool `json:"optional"`
	// AltOf names another credential this one substitutes for. A connector is
	// ready when, for each group, at least one alternative is configured.
	AltOf string `json:"alt_of,omitempty"`
	// Secret is false for values that are not sensitive (a site URL, an account
	// email), so the console can show them in the clear and echo them back.
	Secret bool `json:"secret"`
	// Placeholder is an example value.
	Placeholder string `json:"placeholder,omitempty"`
}

// CatalogEntry is one connector as KNOTT ships it.
type CatalogEntry struct {
	Slug        string
	Name        string
	Category    string
	Description string
	Icon        string
	DocsURL     string
	// Enabled is the default on/off state for a fresh install.
	Enabled     bool
	Credentials []CredentialSpec
	// CredentialSets describes valid authentication recipes. Each inner slice is
	// an AND set and the outer slice is OR. It is used for providers such as
	// Google where either one access token OR the client-id/client-secret/refresh-
	// token trio is sufficient. When empty, Credentials/Optional/AltOf apply.
	CredentialSets [][]string
}

func secret(name, label, help string) CredentialSpec {
	return CredentialSpec{Name: name, Label: label, Help: help, Secret: true}
}

func plain(name, label, help, placeholder string) CredentialSpec {
	return CredentialSpec{Name: name, Label: label, Help: help, Placeholder: placeholder}
}

func alt(base CredentialSpec, of string) CredentialSpec {
	base.AltOf = of
	base.Optional = true
	return base
}

func optional(base CredentialSpec) CredentialSpec {
	base.Optional = true
	return base
}

// Catalog returns every connector KNOTT ships with: the native ones below and
// every declarative definition in internal/connectors.
func Catalog() []CatalogEntry {
	return append(nativeCatalog(), declarativeCatalog()...)
}

// declarativeCatalog adapts the data-defined connectors to catalog entries.
func declarativeCatalog() []CatalogEntry {
	defs := connectors.Declarative()
	out := make([]CatalogEntry, 0, len(defs))
	for _, d := range defs {
		creds := make([]CredentialSpec, 0, len(d.Credentials))
		for _, c := range d.Credentials {
			creds = append(creds, CredentialSpec{
				Name: c.Name, Label: c.Label, Help: c.Help, Placeholder: c.Placeholder,
				Secret: c.Secret, Optional: c.Optional, AltOf: c.AltOf,
			})
		}
		out = append(out, CatalogEntry{
			Slug: d.Slug, Name: d.Name, Category: d.Category, Description: d.Description,
			Icon: d.Icon, DocsURL: d.DocsURL, Enabled: d.Enabled, Credentials: creds,
		})
	}
	return out
}

// nativeCatalog lists the connectors implemented in the engine.
func nativeCatalog() []CatalogEntry {
	return []CatalogEntry{
		// ── Communication ────────────────────────────────────────────────────
		{
			Slug: "slack", Name: "Slack", Category: "Communication", Icon: "message-square", Enabled: true,
			Description: "Post messages and notifications to Slack channels",
			DocsURL:     "https://api.slack.com/messaging/webhooks",
			Credentials: []CredentialSpec{
				secret("SLACK_WEBHOOK_URL", "Incoming Webhook URL",
					"Slack app → Incoming Webhooks → Add New Webhook to Workspace. Simplest option; posts to one channel."),
				alt(secret("SLACK_BOT_TOKEN", "Bot User OAuth Token",
					"Slack app → OAuth & Permissions. Starts with xoxb-. Needed to post to any channel."), "SLACK_WEBHOOK_URL"),
			},
		},
		{
			Slug: "sendgrid", Name: "SendGrid Email", Category: "Communication", Icon: "mail", Enabled: true,
			Description: "Send transactional email via SendGrid",
			DocsURL:     "https://app.sendgrid.com/settings/api_keys",
			Credentials: []CredentialSpec{
				secret("SENDGRID_API_KEY", "API Key", "SendGrid → Settings → API Keys. Needs the Mail Send permission."),
				optional(plain("SENDGRID_FROM", "Default From Address",
					"A verified sender. Used when a node does not set its own from address.", "alerts@example.com")),
			},
		},
		{
			Slug: "twilio", Name: "Twilio SMS", Category: "Communication", Icon: "smartphone", Enabled: true,
			Description: "Send SMS notifications via Twilio",
			DocsURL:     "https://console.twilio.com",
			Credentials: []CredentialSpec{
				plain("TWILIO_ACCOUNT_SID", "Account SID", "Twilio Console dashboard. Starts with AC.", "AC…"),
				secret("TWILIO_AUTH_TOKEN", "Auth Token", "Twilio Console dashboard, next to the Account SID."),
				plain("TWILIO_FROM_NUMBER", "From Number", "A Twilio number you own, in E.164 format.", "+15551234567"),
			},
		},
		{
			Slug: "telegram", Name: "Telegram", Category: "Communication", Icon: "message-square", Enabled: true,
			Description: "Send messages and alerts via a Telegram bot",
			DocsURL:     "https://core.telegram.org/bots#botfather",
			Credentials: []CredentialSpec{
				secret("TELEGRAM_BOT_TOKEN", "Bot Token", "Message @BotFather on Telegram and run /newbot."),
			},
		},
		{
			Slug: "discord", Name: "Discord", Category: "Communication", Icon: "message-square", Enabled: true,
			Description: "Post messages to Discord via an incoming webhook",
			DocsURL:     "https://support.discord.com/hc/en-us/articles/228383668",
			Credentials: []CredentialSpec{
				secret("DISCORD_WEBHOOK_URL", "Webhook URL", "Channel → Edit Channel → Integrations → Webhooks."),
			},
		},
		{
			Slug: "teams", Name: "Microsoft Teams", Category: "Communication", Icon: "message-square", Enabled: true,
			Description: "Post messages to Teams via an incoming webhook",
			DocsURL:     "https://learn.microsoft.com/microsoftteams/platform/webhooks-and-connectors/how-to/add-incoming-webhook",
			Credentials: []CredentialSpec{
				secret("TEAMS_WEBHOOK_URL", "Webhook URL", "Channel → ⋯ → Connectors → Incoming Webhook."),
			},
		},
		{
			Slug: "mattermost", Name: "Mattermost", Category: "Communication", Icon: "message-square", Enabled: true,
			Description: "Post messages via a Mattermost incoming webhook",
			Credentials: []CredentialSpec{
				secret("MATTERMOST_WEBHOOK_URL", "Webhook URL", "System Console → Integrations → Incoming Webhooks."),
			},
		},
		{
			Slug: "whatsapp", Name: "WhatsApp", Category: "Communication", Icon: "message-square", Enabled: true,
			Description: "Send WhatsApp messages via the Cloud API",
			DocsURL:     "https://developers.facebook.com/docs/whatsapp/cloud-api",
			Credentials: []CredentialSpec{
				secret("WHATSAPP_TOKEN", "Access Token", "Meta for Developers → your app → WhatsApp → API Setup."),
				plain("WHATSAPP_PHONE_ID", "Phone Number ID", "Shown on the same API Setup page.", "1234567890"),
			},
		},
		{
			Slug: "ms_graph", Name: "Microsoft Outlook", Category: "Communication", Icon: "mail", Enabled: true,
			Description: "Send email through Microsoft Graph",
			DocsURL:     "https://learn.microsoft.com/graph/auth-v2-service",
			Credentials: []CredentialSpec{
				secret("MS_GRAPH_TOKEN", "Access Token", "An OAuth token with the Mail.Send scope."),
			},
		},
		{
			Slug: "pushover", Name: "Pushover", Category: "Communication", Icon: "smartphone", Enabled: true,
			Description: "Send push notifications to phones and desktops",
			DocsURL:     "https://pushover.net/apps/build",
			Credentials: []CredentialSpec{
				secret("PUSHOVER_TOKEN", "Application Token", "Create an application at pushover.net/apps/build."),
				secret("PUSHOVER_USER", "User or Group Key", "Shown on your Pushover dashboard."),
			},
		},

		// ── Developer ────────────────────────────────────────────────────────
		{
			Slug: "github", Name: "GitHub", Category: "Developer Tools", Icon: "layers", Enabled: true,
			Description: "Create, comment on and close GitHub issues",
			DocsURL:     "https://github.com/settings/tokens",
			Credentials: []CredentialSpec{
				secret("GITHUB_TOKEN", "Personal Access Token", "Settings → Developer settings → Tokens. Needs the repo scope."),
			},
		},
		{
			Slug: "gitlab", Name: "GitLab", Category: "Developer Tools", Icon: "layers", Enabled: true,
			Description: "Create issues in GitLab projects",
			DocsURL:     "https://gitlab.com/-/user_settings/personal_access_tokens",
			Credentials: []CredentialSpec{
				secret("GITLAB_TOKEN", "Personal Access Token", "Needs the api scope."),
			},
		},
		{
			Slug: "linear", Name: "Linear", Category: "Developer Tools", Icon: "layers", Enabled: true,
			Description: "Create issues in Linear",
			DocsURL:     "https://linear.app/settings/api",
			Credentials: []CredentialSpec{
				secret("LINEAR_API_KEY", "API Key", "Linear → Settings → API → Personal API keys."),
			},
		},

		// ── Ticketing & support ──────────────────────────────────────────────
		{
			Slug: "jira", Name: "Jira", Category: "Customer Support", Icon: "layers", Enabled: true,
			Description: "Create and comment on Jira issues",
			DocsURL:     "https://id.atlassian.com/manage-profile/security/api-tokens",
			Credentials: []CredentialSpec{
				plain("JIRA_BASE_URL", "Site URL", "Your Atlassian site.", "https://acme.atlassian.net"),
				plain("JIRA_EMAIL", "Account Email", "The Atlassian account the token belongs to.", "you@acme.com"),
				secret("JIRA_API_TOKEN", "API Token", "id.atlassian.com → Security → API tokens."),
			},
		},
		{
			Slug: "zendesk", Name: "Zendesk", Category: "Customer Support", Icon: "layers", Enabled: true,
			Description: "Create support tickets in Zendesk",
			Credentials: []CredentialSpec{
				plain("ZENDESK_BASE_URL", "Site URL", "Your Zendesk subdomain.", "https://acme.zendesk.com"),
				plain("ZENDESK_EMAIL", "Account Email", "The agent account the token belongs to.", "you@acme.com"),
				secret("ZENDESK_API_TOKEN", "API Token", "Admin Center → Apps and integrations → APIs → Zendesk API."),
			},
		},
		{
			Slug: "freshdesk", Name: "Freshdesk", Category: "Customer Support", Icon: "layers", Enabled: true,
			Description: "Create support tickets in Freshdesk",
			Credentials: []CredentialSpec{
				plain("FRESHDESK_BASE_URL", "Site URL", "Your Freshdesk domain.", "https://acme.freshdesk.com"),
				secret("FRESHDESK_API_KEY", "API Key", "Profile settings → Your API Key."),
			},
		},
		{
			Slug: "servicenow", Name: "ServiceNow", Category: "Developer Tools", Icon: "zap", Enabled: true,
			Description: "Create incidents in ServiceNow",
			Credentials: []CredentialSpec{
				plain("SERVICENOW_BASE_URL", "Instance URL", "Your ServiceNow instance.", "https://acme.service-now.com"),
				plain("SERVICENOW_USER", "Username", "A user with the rest_service role.", ""),
				secret("SERVICENOW_PASSWORD", "Password", "The password for that user."),
			},
		},
		{
			Slug: "pagerduty", Name: "PagerDuty", Category: "Developer Tools", Icon: "zap", Enabled: true,
			Description: "Trigger incidents through the Events API",
			DocsURL:     "https://support.pagerduty.com/docs/services-and-integrations",
			Credentials: []CredentialSpec{
				secret("PAGERDUTY_ROUTING_KEY", "Integration Routing Key", "Service → Integrations → Events API v2."),
			},
		},

		// ── CRM ──────────────────────────────────────────────────────────────
		{
			Slug: "hubspot", Name: "HubSpot", Category: "CRM", Icon: "users", Enabled: true,
			Description: "Create contacts and deals in HubSpot",
			DocsURL:     "https://developers.hubspot.com/docs/api/private-apps",
			Credentials: []CredentialSpec{
				secret("HUBSPOT_TOKEN", "Private App Token", "Settings → Integrations → Private Apps."),
			},
		},
		{
			Slug: "intercom", Name: "Intercom", Category: "CRM", Icon: "users", Enabled: true,
			Description: "Create contacts in Intercom",
			Credentials: []CredentialSpec{
				secret("INTERCOM_TOKEN", "Access Token", "Developer Hub → your app → Authentication."),
			},
		},
		{
			Slug: "close", Name: "Close CRM", Category: "CRM", Icon: "users", Enabled: true,
			Description: "Create leads in Close",
			Credentials: []CredentialSpec{
				secret("CLOSE_API_KEY", "API Key", "Close → Settings → API Keys."),
			},
		},

		// ── Productivity ─────────────────────────────────────────────────────
		{
			Slug: "notion", Name: "Notion", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "Create pages in Notion databases",
			DocsURL:     "https://www.notion.so/my-integrations",
			Credentials: []CredentialSpec{
				secret("NOTION_TOKEN", "Integration Secret", "notion.so/my-integrations. Share the target database with the integration."),
			},
		},
		{
			Slug: "google_sheets", Name: "Google Sheets", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "Append and read rows in Google Sheets",
			DocsURL:     "https://console.cloud.google.com/apis/credentials",
			Credentials: []CredentialSpec{
				secret("GOOGLE_CLIENT_ID", "OAuth Client ID", "Google Cloud Console → APIs & Services → Credentials."),
				secret("GOOGLE_CLIENT_SECRET", "OAuth Client Secret", "Issued alongside the client ID."),
				secret("GOOGLE_REFRESH_TOKEN", "Refresh Token", "Obtained once through the OAuth consent flow. KNOTT exchanges it for access tokens."),
				alt(secret("GOOGLE_ACCESS_TOKEN", "Access Token",
					"A short-lived token. Useful for a quick test; it expires within an hour."), "GOOGLE_REFRESH_TOKEN"),
			},
			CredentialSets: [][]string{{"GOOGLE_ACCESS_TOKEN"}, {"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOOGLE_REFRESH_TOKEN"}},
		},
		{
			Slug: "google_calendar", Name: "Google Calendar", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "Create and read calendar events",
			DocsURL:     "https://console.cloud.google.com/apis/credentials",
			Credentials: []CredentialSpec{
				secret("GOOGLE_CLIENT_ID", "OAuth Client ID", "Google Cloud Console → APIs & Services → Credentials."),
				secret("GOOGLE_CLIENT_SECRET", "OAuth Client Secret", "Issued alongside the client ID."),
				secret("GOOGLE_REFRESH_TOKEN", "Refresh Token", "Obtained once through the OAuth consent flow."),
				alt(secret("GOOGLE_ACCESS_TOKEN", "Access Token", "A short-lived token, useful for a quick test."), "GOOGLE_REFRESH_TOKEN"),
			},
			CredentialSets: [][]string{{"GOOGLE_ACCESS_TOKEN"}, {"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOOGLE_REFRESH_TOKEN"}},
		},
		{
			Slug: "trello", Name: "Trello", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "Create cards on Trello boards",
			DocsURL:     "https://trello.com/power-ups/admin",
			Credentials: []CredentialSpec{
				secret("TRELLO_KEY", "API Key", "trello.com/power-ups/admin → your Power-Up → API key."),
				secret("TRELLO_TOKEN", "Token", "Generated from the API key page."),
			},
		},
		{
			Slug: "asana", Name: "Asana", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "Create tasks in Asana projects",
			Credentials: []CredentialSpec{
				secret("ASANA_TOKEN", "Personal Access Token", "Asana → My Settings → Apps → Developer apps."),
			},
		},
		{
			Slug: "clickup", Name: "ClickUp", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "Create tasks in ClickUp lists",
			Credentials: []CredentialSpec{
				secret("CLICKUP_TOKEN", "API Token", "ClickUp → Settings → Apps → API Token."),
			},
		},
		{
			Slug: "monday", Name: "Monday.com", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "Create items on Monday boards",
			Credentials: []CredentialSpec{
				secret("MONDAY_TOKEN", "API Token", "Monday → Avatar → Developers → My access tokens."),
			},
		},
		{
			Slug: "coda", Name: "Coda", Category: "Productivity", Icon: "database", Enabled: true,
			Description: "Insert rows into Coda tables",
			Credentials: []CredentialSpec{
				secret("CODA_TOKEN", "API Token", "coda.io/account → API settings."),
			},
		},
		{
			Slug: "calendly", Name: "Calendly", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "Read Calendly account and event data",
			Credentials: []CredentialSpec{
				secret("CALENDLY_TOKEN", "Personal Access Token", "Calendly → Integrations → API & Webhooks."),
			},
		},

		// ── Data ─────────────────────────────────────────────────────────────
		{
			Slug: "airtable", Name: "Airtable", Category: "Databases", Icon: "database", Enabled: true,
			Description: "Create, update and list Airtable records",
			DocsURL:     "https://airtable.com/create/tokens",
			Credentials: []CredentialSpec{
				secret("AIRTABLE_TOKEN", "Personal Access Token", "airtable.com/create/tokens. Grant it the bases you need."),
			},
		},
		{
			Slug: "database", Name: "SQL Database", Category: "Databases", Icon: "database", Enabled: true,
			Description: "Run SQL queries against SQLite, PostgreSQL or MySQL",
			Credentials: []CredentialSpec{
				secret("DATABASE_DSN", "Connection String", "e.g. postgres://user:pass@host:5432/db?sslmode=require"),
			},
		},

		// ── Commerce & marketing ─────────────────────────────────────────────
		{
			Slug: "stripe", Name: "Stripe", Category: "Finance", Icon: "credit-card", Enabled: true,
			Description: "Create customers, charges and refunds in Stripe",
			DocsURL:     "https://dashboard.stripe.com/apikeys",
			Credentials: []CredentialSpec{
				secret("STRIPE_SECRET_KEY", "Secret Key", "Stripe Dashboard → Developers → API keys. Starts with sk_."),
			},
		},
		{
			Slug: "shopify", Name: "Shopify", Category: "E-commerce", Icon: "credit-card", Enabled: true,
			Description: "List products and create customers in Shopify",
			Credentials: []CredentialSpec{
				plain("SHOPIFY_STORE_URL", "Store URL", "Your myshopify domain.", "https://acme.myshopify.com"),
				secret("SHOPIFY_ACCESS_TOKEN", "Admin API Access Token", "Shopify admin → Apps → Develop apps → your app → API credentials."),
			},
		},
		{
			Slug: "mailchimp", Name: "Mailchimp", Category: "Marketing", Icon: "mail", Enabled: true,
			Description: "Add and update members in a Mailchimp audience",
			Credentials: []CredentialSpec{
				secret("MAILCHIMP_API_KEY", "API Key", "Mailchimp → Account → Extras → API keys. Ends with the datacentre, e.g. -us21."),
			},
		},

		// ── AI ───────────────────────────────────────────────────────────────
		{
			Slug: "openai", Name: "OpenAI", Category: "AI", Icon: "cpu", Enabled: true,
			Description: "Generate text through OpenAI chat completions",
			DocsURL:     "https://platform.openai.com/api-keys",
			Credentials: []CredentialSpec{
				secret("OPENAI_API_KEY", "API Key", "platform.openai.com/api-keys."),
			},
		},
		{
			Slug: "anthropic", Name: "Anthropic", Category: "AI", Icon: "cpu", Enabled: true,
			Description: "Generate text with Claude", DocsURL: "https://console.anthropic.com/settings/keys",
			Credentials: []CredentialSpec{secret("ANTHROPIC_API_KEY", "API Key", "Anthropic Console → Settings → API Keys.")},
		},
		{
			Slug: "gemini", Name: "Google Gemini", Category: "AI", Icon: "cpu", Enabled: true,
			Description: "Generate text with Gemini", DocsURL: "https://aistudio.google.com/app/apikey",
			Credentials: []CredentialSpec{secret("GEMINI_API_KEY", "API Key", "Google AI Studio → Get API key.")},
		},
		{
			Slug: "groq", Name: "Groq", Category: "AI", Icon: "cpu", Enabled: true,
			Description: "Run low-latency language models through Groq", DocsURL: "https://console.groq.com/keys",
			Credentials: []CredentialSpec{secret("GROQ_API_KEY", "API Key", "Groq Console → API Keys.")},
		},
		{
			Slug: "cohere", Name: "Cohere", Category: "AI", Icon: "cpu", Enabled: true,
			Description: "Generate and classify text with Cohere", DocsURL: "https://dashboard.cohere.com/api-keys",
			Credentials: []CredentialSpec{secret("COHERE_API_KEY", "API Key", "Cohere Dashboard → API Keys.")},
		},

		// ── Storage & files ────────────────────────────────────────────────────
		{
			Slug: "dropbox", Name: "Dropbox", Category: "Files & Storage", Icon: "archive", Enabled: true,
			Description: "List and upload files in Dropbox", DocsURL: "https://www.dropbox.com/developers/apps",
			Credentials: []CredentialSpec{secret("DROPBOX_ACCESS_TOKEN", "Access Token", "Dropbox App Console → OAuth 2 → Generated access token.")},
		},
		{
			Slug: "box", Name: "Box", Category: "Files & Storage", Icon: "archive", Enabled: true,
			Description: "List folders and upload files in Box", DocsURL: "https://developer.box.com/guides/authentication/",
			Credentials: []CredentialSpec{secret("BOX_ACCESS_TOKEN", "Access Token", "A Box OAuth 2 access token for the target enterprise or user.")},
		},
		{
			Slug: "google_drive", Name: "Google Drive", Category: "Files & Storage", Icon: "archive", Enabled: true,
			Description: "List and create files in Google Drive", DocsURL: "https://console.cloud.google.com/apis/credentials",
			Credentials: []CredentialSpec{
				secret("GOOGLE_CLIENT_ID", "OAuth Client ID", "Google Cloud Console → APIs & Services → Credentials."),
				secret("GOOGLE_CLIENT_SECRET", "OAuth Client Secret", "Issued alongside the client ID."),
				secret("GOOGLE_REFRESH_TOKEN", "Refresh Token", "Obtained through the OAuth consent flow with a Drive scope."),
				alt(secret("GOOGLE_ACCESS_TOKEN", "Access Token", "A short-lived OAuth access token."), "GOOGLE_REFRESH_TOKEN"),
			},
			CredentialSets: [][]string{{"GOOGLE_ACCESS_TOKEN"}, {"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOOGLE_REFRESH_TOKEN"}},
		},
		{
			Slug: "onedrive", Name: "Microsoft OneDrive", Category: "Files & Storage", Icon: "archive", Enabled: true,
			Description: "List and create files in OneDrive through Microsoft Graph",
			Credentials: []CredentialSpec{secret("MS_GRAPH_TOKEN", "Microsoft Graph Token", "An OAuth token with Files.ReadWrite permissions.")},
		},

		// ── Infrastructure & observability ─────────────────────────────────────
		{
			Slug: "cloudflare", Name: "Cloudflare", Category: "Developer Tools", Icon: "cloud", Enabled: true,
			Description: "Manage Cloudflare zones and DNS records", DocsURL: "https://dash.cloudflare.com/profile/api-tokens",
			Credentials: []CredentialSpec{secret("CLOUDFLARE_API_TOKEN", "API Token", "Cloudflare → My Profile → API Tokens.")},
		},
		{
			Slug: "digitalocean", Name: "DigitalOcean", Category: "Developer Tools", Icon: "cloud", Enabled: true,
			Description: "List and manage DigitalOcean resources", DocsURL: "https://cloud.digitalocean.com/account/api/tokens",
			Credentials: []CredentialSpec{secret("DIGITALOCEAN_TOKEN", "Personal Access Token", "DigitalOcean → API → Tokens/Keys.")},
		},
		{
			Slug: "datadog", Name: "Datadog", Category: "Developer Tools", Icon: "zap", Enabled: true,
			Description: "Submit events and query Datadog", DocsURL: "https://app.datadoghq.com/organization-settings/api-keys",
			Credentials: []CredentialSpec{
				secret("DATADOG_API_KEY", "API Key", "Datadog → Organization Settings → API Keys."),
				secret("DATADOG_APP_KEY", "Application Key", "Datadog → Organization Settings → Application Keys."),
				optional(plain("DATADOG_SITE", "Datadog Site", "API site for your region.", "datadoghq.com")),
			},
		},
		{
			Slug: "newrelic", Name: "New Relic", Category: "Developer Tools", Icon: "zap", Enabled: true,
			Description: "Query New Relic NerdGraph", DocsURL: "https://one.newrelic.com/api-keys",
			Credentials: []CredentialSpec{secret("NEW_RELIC_API_KEY", "User API Key", "New Relic → API keys. Use a User key.")},
		},
		{
			Slug: "sentry", Name: "Sentry", Category: "Developer Tools", Icon: "zap", Enabled: true,
			Description: "List projects and inspect issues in Sentry", DocsURL: "https://sentry.io/settings/account/api/auth-tokens/",
			Credentials: []CredentialSpec{secret("SENTRY_AUTH_TOKEN", "Auth Token", "Sentry → User settings → Auth tokens.")},
		},
		{
			Slug: "grafana", Name: "Grafana", Category: "Developer Tools", Icon: "zap", Enabled: true,
			Description: "Search dashboards and call the Grafana API",
			Credentials: []CredentialSpec{
				plain("GRAFANA_URL", "Grafana URL", "The root URL of your Grafana instance.", "https://grafana.example.com"),
				secret("GRAFANA_TOKEN", "Service Account Token", "Grafana → Administration → Service accounts."),
			},
		},
		{
			Slug: "elasticsearch", Name: "Elasticsearch", Category: "Databases", Icon: "database", Enabled: true,
			Description: "Search and index Elasticsearch documents",
			Credentials: []CredentialSpec{
				plain("ELASTICSEARCH_URL", "Cluster URL", "Elasticsearch endpoint.", "https://cluster.example.com"),
				secret("ELASTICSEARCH_API_KEY", "API Key", "An Elasticsearch encoded API key."),
			},
		},

		// ── Data platforms ─────────────────────────────────────────────────────
		{
			Slug: "supabase", Name: "Supabase", Category: "Databases", Icon: "database", Enabled: true,
			Description: "Read and write Supabase tables through PostgREST", DocsURL: "https://supabase.com/dashboard/project/_/settings/api",
			Credentials: []CredentialSpec{
				plain("SUPABASE_URL", "Project URL", "Supabase project settings → API.", "https://project.supabase.co"),
				secret("SUPABASE_SERVICE_KEY", "Service Role Key", "Supabase project settings → API. Keep this server-side."),
			},
		},
		{
			Slug: "mongodb_atlas", Name: "MongoDB Atlas Data API", Category: "Databases", Icon: "database", Enabled: true,
			Description: "Find and insert MongoDB Atlas documents through the Data API",
			Credentials: []CredentialSpec{
				plain("MONGODB_DATA_API_URL", "Data API URL", "Atlas App Services Data API endpoint.", "https://data.mongodb-api.com/app/data-xxxxx/endpoint/data/v1"),
				secret("MONGODB_DATA_API_KEY", "API Key", "Atlas App Services → Data API → API Keys."),
			},
		},
		{
			Slug: "rabbitmq", Name: "RabbitMQ", Category: "Communication", Icon: "message-square", Enabled: true,
			Description: "Publish messages through the RabbitMQ Management API",
			Credentials: []CredentialSpec{
				plain("RABBITMQ_URL", "Management URL", "RabbitMQ management endpoint.", "https://rabbitmq.example.com"),
				plain("RABBITMQ_USER", "Username", "RabbitMQ user with management API access.", "knott"),
				secret("RABBITMQ_PASSWORD", "Password", "Password for the RabbitMQ user."),
			},
		},
		{
			Slug: "kafka_rest", Name: "Kafka REST Proxy", Category: "Communication", Icon: "message-square", Enabled: true,
			Description: "Produce records through a Kafka REST Proxy",
			Credentials: []CredentialSpec{
				plain("KAFKA_REST_URL", "REST Proxy URL", "Confluent or self-hosted REST Proxy root URL.", "https://kafka.example.com"),
				optional(secret("KAFKA_REST_TOKEN", "Bearer Token", "Optional bearer token for the REST Proxy.")),
			},
		},

		// ── Collaboration, forms & commerce ────────────────────────────────────
		{
			Slug: "zoom", Name: "Zoom", Category: "Communication", Icon: "users", Enabled: true,
			Description: "List users and create Zoom meetings", DocsURL: "https://developers.zoom.us/docs/internal-apps/",
			Credentials: []CredentialSpec{secret("ZOOM_ACCESS_TOKEN", "Access Token", "A Zoom OAuth access token with meeting scopes.")},
		},
		{
			Slug: "typeform", Name: "Typeform", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "List forms and retrieve Typeform responses", DocsURL: "https://www.typeform.com/developers/get-started/personal-access-token/",
			Credentials: []CredentialSpec{secret("TYPEFORM_TOKEN", "Personal Access Token", "Typeform account → Personal tokens.")},
		},
		{
			Slug: "surveymonkey", Name: "SurveyMonkey", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "List surveys and retrieve responses", DocsURL: "https://developer.surveymonkey.com/api/v3/",
			Credentials: []CredentialSpec{secret("SURVEYMONKEY_TOKEN", "Access Token", "SurveyMonkey developer app credentials.")},
		},
		{
			Slug: "wordpress", Name: "WordPress", Category: "Productivity", Icon: "layers", Enabled: true,
			Description: "Create and list WordPress posts through the REST API",
			Credentials: []CredentialSpec{
				plain("WORDPRESS_URL", "Site URL", "WordPress site root URL.", "https://example.com"),
				plain("WORDPRESS_USER", "Username", "WordPress user for the application password.", "editor"),
				secret("WORDPRESS_APP_PASSWORD", "Application Password", "Users → Profile → Application Passwords."),
			},
		},
		{
			Slug: "woocommerce", Name: "WooCommerce", Category: "E-commerce", Icon: "credit-card", Enabled: true,
			Description: "List products and create WooCommerce orders",
			Credentials: []CredentialSpec{
				plain("WOOCOMMERCE_URL", "Store URL", "WooCommerce store root URL.", "https://store.example.com"),
				secret("WOOCOMMERCE_KEY", "Consumer Key", "WooCommerce → Settings → Advanced → REST API."),
				secret("WOOCOMMERCE_SECRET", "Consumer Secret", "Issued with the consumer key."),
			},
		},
		{
			Slug: "quickbooks", Name: "QuickBooks Online", Category: "Finance", Icon: "credit-card", Enabled: true,
			Description: "Query customers and invoices in QuickBooks Online",
			Credentials: []CredentialSpec{
				secret("QUICKBOOKS_ACCESS_TOKEN", "OAuth Access Token", "Intuit OAuth 2 access token."),
				plain("QUICKBOOKS_REALM_ID", "Company Realm ID", "The QuickBooks company ID returned by OAuth.", "123456789"),
			},
		},
		{
			Slug: "x_twitter", Name: "X / Twitter", Category: "Communication", Icon: "message-square", Enabled: true,
			Description: "Search recent posts and publish through the X API", DocsURL: "https://developer.x.com/en/portal/dashboard",
			Credentials: []CredentialSpec{secret("X_BEARER_TOKEN", "Bearer Token", "X Developer Portal → Project/App → Keys and tokens.")},
		},

		// ── Generic ──────────────────────────────────────────────────────────
		{
			Slug: "webhook", Name: "HTTP / Webhook", Category: "Developer Tools", Icon: "zap", Enabled: true,
			Description: "Call any HTTP endpoint — REST, webhooks, internal services",
		},
		{
			Slug: "graphql", Name: "GraphQL", Category: "Developer Tools", Icon: "zap", Enabled: true,
			Description: "Call any GraphQL API endpoint",
		},
	}
}

// CatalogBySlug indexes the catalog for lookup by the executor and the API.
func CatalogBySlug() map[string]CatalogEntry {
	out := make(map[string]CatalogEntry, 48)
	for _, e := range Catalog() {
		out[e.Slug] = e
	}
	return out
}

// KnownSecretNames returns every credential key the catalog references, in
// catalog order and without duplicates. It is what the API reports as the set
// of secrets an operator may configure.
func KnownSecretNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range Catalog() {
		for _, c := range e.Credentials {
			if !seen[c.Name] {
				seen[c.Name] = true
				out = append(out, c.Name)
			}
		}
	}
	return out
}
