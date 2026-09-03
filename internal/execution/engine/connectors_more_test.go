// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLinearCreateIssue(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"data":{"issueCreate":{"success":true,"issue":{"id":"abc","identifier":"ENG-1","url":"https://linear.app/x"}}}}`)
	e := newTestExecutor(map[string]string{"LINEAR_API_KEY": "lin_key"})
	out, err := e.callConnector("linear", "create_issue", map[string]any{
		"base_url": srv.URL, "team_id": "team1", "title": "Bug", "description": "details",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["identifier"] != "ENG-1" {
		t.Fatalf("linear out=%v", out)
	}
	if cap.authHeader != "lin_key" {
		t.Fatalf("linear auth header=%q", cap.authHeader)
	}
}

func TestPagerDutyTrigger(t *testing.T) {
	srv, cap := connectorServer(t, 202, `{"status":"success","dedup_key":"dk1"}`)
	e := newTestExecutor(map[string]string{"PAGERDUTY_ROUTING_KEY": "rk"})
	out, err := e.callConnector("pagerduty", "trigger", map[string]any{
		"base_url": srv.URL, "summary": "disk full", "severity": "critical",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["dedup_key"] != "dk1" {
		t.Fatalf("pagerduty out=%v", out)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	if body["routing_key"] != "rk" || body["event_action"] != "trigger" {
		t.Fatalf("pagerduty body=%v", body)
	}
}

func TestOpenAIChat(t *testing.T) {
	srv, _ := connectorServer(t, 200, `{"choices":[{"message":{"role":"assistant","content":"hello there"}}]}`)
	e := newTestExecutor(map[string]string{"OPENAI_API_KEY": "sk-test"})
	out, err := e.callConnector("openai", "chat", map[string]any{
		"base_url": srv.URL, "prompt": "hi", "model": "gpt-4o-mini",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["text"] != "hello there" {
		t.Fatalf("openai out=%v", out)
	}
}

func TestMattermostSend(t *testing.T) {
	srv, cap := connectorServer(t, 200, `ok`)
	e := newTestExecutor(map[string]string{"MATTERMOST_WEBHOOK_URL": srv.URL})
	if _, err := e.callConnector("mattermost", "", map[string]any{"text": "deploy done"}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	if body["text"] != "deploy done" {
		t.Fatalf("mattermost body=%v", body)
	}
}

func TestZendeskCreateTicket(t *testing.T) {
	srv, _ := connectorServer(t, 201, `{"ticket":{"id":42}}`)
	e := newTestExecutor(map[string]string{"ZENDESK_EMAIL": "me@acme.co", "ZENDESK_API_TOKEN": "tok"})
	out, err := e.callConnector("zendesk", "create_ticket", map[string]any{
		"base_url": srv.URL, "subject": "Help", "comment": "It broke",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["ticket_id"] != float64(42) {
		t.Fatalf("zendesk out=%v", out)
	}
}

func TestGraphQLQuery(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"data":{"viewer":{"login":"octocat"}}}`)
	e := newTestExecutor(nil)
	out, err := e.callConnector("graphql", "", map[string]any{
		"url": srv.URL, "query": "{viewer{login}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["status"] != 200 {
		t.Fatalf("graphql out=%v", out)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	if body["query"] != "{viewer{login}}" {
		t.Fatalf("graphql body=%v", body)
	}
}

func TestNewConnectorMissingCreds(t *testing.T) {
	e := newTestExecutor(nil)
	for _, c := range []string{"linear", "trello", "asana", "clickup", "pagerduty", "zendesk", "shopify", "mailchimp", "openai", "pushover"} {
		if _, err := e.callConnector(c, "", map[string]any{}); err == nil {
			t.Fatalf("%s should error without credentials", c)
		}
	}
}

func TestGitLabCreateIssue(t *testing.T) {
	srv, _ := connectorServer(t, 201, `{"iid":7,"web_url":"https://gitlab.com/x/-/issues/7"}`)
	e := newTestExecutor(map[string]string{"GITLAB_TOKEN": "glpat"})
	out, err := e.callConnector("gitlab", "create_issue", map[string]any{
		"base_url": srv.URL, "project_id": "42", "title": "Bug",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["issue_iid"] != float64(7) {
		t.Fatalf("gitlab out=%v", out)
	}
}

func TestMSGraphSendMail(t *testing.T) {
	srv, cap := connectorServer(t, 202, ``)
	e := newTestExecutor(map[string]string{"MS_GRAPH_TOKEN": "tok"})
	if _, err := e.callConnector("ms_graph", "send_mail", map[string]any{
		"base_url": srv.URL, "to": "a@b.co", "subject": "Hi", "body": "Hello",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.body, "sendMail") && !strings.Contains(cap.body, "toRecipients") {
		t.Fatalf("ms_graph body=%s", cap.body)
	}
}

func TestWhatsAppSend(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"messages":[{"id":"wamid.1"}]}`)
	e := newTestExecutor(map[string]string{"WHATSAPP_TOKEN": "tok", "WHATSAPP_PHONE_ID": "123"})
	if _, err := e.callConnector("whatsapp", "", map[string]any{
		"base_url": srv.URL, "to": "15551234567", "text": "hi there",
	}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	if body["messaging_product"] != "whatsapp" {
		t.Fatalf("whatsapp body=%v", body)
	}
}

func TestWave2MissingCreds(t *testing.T) {
	e := newTestExecutor(nil)
	for _, c := range []string{"gitlab", "monday", "freshdesk", "intercom", "ms_graph", "whatsapp", "coda", "close", "calendly", "servicenow"} {
		if _, err := e.callConnector(c, "", map[string]any{}); err == nil {
			t.Fatalf("%s should error without credentials", c)
		}
	}
}
