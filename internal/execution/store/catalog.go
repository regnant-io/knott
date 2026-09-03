package store

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

// Catalog returns every connector KNOTT ships with.
func Catalog() []CatalogEntry {
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
			Slug: "github", Name: "GitHub", Category: "Developer", Icon: "layers", Enabled: true,
			Description: "Create, comment on and close GitHub issues",
			DocsURL:     "https://github.com/settings/tokens",
			Credentials: []CredentialSpec{
				secret("GITHUB_TOKEN", "Personal Access Token", "Settings → Developer settings → Tokens. Needs the repo scope."),
			},
		},
		{
			Slug: "gitlab", Name: "GitLab", Category: "Developer", Icon: "layers", Enabled: true,
			Description: "Create issues in GitLab projects",
			DocsURL:     "https://gitlab.com/-/user_settings/personal_access_tokens",
			Credentials: []CredentialSpec{
				secret("GITLAB_TOKEN", "Personal Access Token", "Needs the api scope."),
			},
		},
		{
			Slug: "linear", Name: "Linear", Category: "Developer", Icon: "layers", Enabled: true,
			Description: "Create issues in Linear",
			DocsURL:     "https://linear.app/settings/api",
			Credentials: []CredentialSpec{
				secret("LINEAR_API_KEY", "API Key", "Linear → Settings → API → Personal API keys."),
			},
		},

		// ── Ticketing & support ──────────────────────────────────────────────
		{
			Slug: "jira", Name: "Jira", Category: "Ticketing", Icon: "layers", Enabled: true,
			Description: "Create and comment on Jira issues",
			DocsURL:     "https://id.atlassian.com/manage-profile/security/api-tokens",
			Credentials: []CredentialSpec{
				plain("JIRA_BASE_URL", "Site URL", "Your Atlassian site.", "https://acme.atlassian.net"),
				plain("JIRA_EMAIL", "Account Email", "The Atlassian account the token belongs to.", "you@acme.com"),
				secret("JIRA_API_TOKEN", "API Token", "id.atlassian.com → Security → API tokens."),
			},
		},
		{
			Slug: "zendesk", Name: "Zendesk", Category: "Ticketing", Icon: "layers", Enabled: true,
			Description: "Create support tickets in Zendesk",
			Credentials: []CredentialSpec{
				plain("ZENDESK_BASE_URL", "Site URL", "Your Zendesk subdomain.", "https://acme.zendesk.com"),
				plain("ZENDESK_EMAIL", "Account Email", "The agent account the token belongs to.", "you@acme.com"),
				secret("ZENDESK_API_TOKEN", "API Token", "Admin Center → Apps and integrations → APIs → Zendesk API."),
			},
		},
		{
			Slug: "freshdesk", Name: "Freshdesk", Category: "Ticketing", Icon: "layers", Enabled: true,
			Description: "Create support tickets in Freshdesk",
			Credentials: []CredentialSpec{
				plain("FRESHDESK_BASE_URL", "Site URL", "Your Freshdesk domain.", "https://acme.freshdesk.com"),
				secret("FRESHDESK_API_KEY", "API Key", "Profile settings → Your API Key."),
			},
		},
		{
			Slug: "servicenow", Name: "ServiceNow", Category: "Operations", Icon: "zap", Enabled: true,
			Description: "Create incidents in ServiceNow",
			Credentials: []CredentialSpec{
				plain("SERVICENOW_BASE_URL", "Instance URL", "Your ServiceNow instance.", "https://acme.service-now.com"),
				plain("SERVICENOW_USER", "Username", "A user with the rest_service role.", ""),
				secret("SERVICENOW_PASSWORD", "Password", "The password for that user."),
			},
		},
		{
			Slug: "pagerduty", Name: "PagerDuty", Category: "Operations", Icon: "zap", Enabled: true,
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
			Slug: "airtable", Name: "Airtable", Category: "Database", Icon: "database", Enabled: true,
			Description: "Create, update and list Airtable records",
			DocsURL:     "https://airtable.com/create/tokens",
			Credentials: []CredentialSpec{
				secret("AIRTABLE_TOKEN", "Personal Access Token", "airtable.com/create/tokens. Grant it the bases you need."),
			},
		},
		{
			Slug: "database", Name: "SQL Database", Category: "Database", Icon: "database", Enabled: true,
			Description: "Run SQL queries against SQLite, PostgreSQL or MySQL",
			Credentials: []CredentialSpec{
				secret("DATABASE_DSN", "Connection String", "e.g. postgres://user:pass@host:5432/db?sslmode=require"),
			},
		},

		// ── Commerce & marketing ─────────────────────────────────────────────
		{
			Slug: "stripe", Name: "Stripe", Category: "Payments", Icon: "credit-card", Enabled: true,
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
				plain("SHOPIFY_STORE_URL", "Store URL", "Your myshopify domain.", "acme.myshopify.com"),
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

		// ── Generic ──────────────────────────────────────────────────────────
		{
			Slug: "webhook", Name: "HTTP / Webhook", Category: "Custom", Icon: "zap", Enabled: true,
			Description: "Call any HTTP endpoint — REST, webhooks, internal services",
		},
		{
			Slug: "graphql", Name: "GraphQL", Category: "Custom", Icon: "zap", Enabled: true,
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
