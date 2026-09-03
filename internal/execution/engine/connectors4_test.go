// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTeamsConnector(t *testing.T) {
	srv, cap := connectorServer(t, 200, `1`)
	e := newTestExecutor(map[string]string{"TEAMS_WEBHOOK_URL": srv.URL})
	if _, err := e.callConnector("teams", "", map[string]any{"text": "build green", "title": "CI"}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	if body["@type"] != "MessageCard" || body["text"] != "build green" || body["title"] != "CI" {
		t.Fatalf("teams card wrong: %v", body)
	}
}

func TestStripeCreateCustomer(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"id":"cus_123"}`)
	e := newTestExecutor(map[string]string{"STRIPE_SECRET_KEY": "sk_test"})
	out, err := e.callConnector("stripe", "create_customer", map[string]any{
		"base_url": srv.URL, "email": "a@b.co", "name": "Acme",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.contentType, "x-www-form-urlencoded") {
		t.Fatalf("stripe must use form encoding, got %s", cap.contentType)
	}
	if !strings.Contains(cap.body, "email=a%40b.co") && !strings.Contains(cap.body, "email=a@b.co") {
		t.Fatalf("stripe form body=%s", cap.body)
	}
	if out["customer_id"] != "cus_123" {
		t.Fatalf("customer_id=%v", out["customer_id"])
	}
}

func TestStripeCharge(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"id":"ch_1"}`)
	e := newTestExecutor(map[string]string{"STRIPE_SECRET_KEY": "sk_test"})
	out, err := e.callConnector("stripe", "create_charge", map[string]any{
		"base_url": srv.URL, "amount": "2000", "currency": "usd", "customer": "cus_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.body, "amount=2000") {
		t.Fatalf("charge body=%s", cap.body)
	}
	if out["charge_id"] != "ch_1" {
		t.Fatalf("charge_id=%v", out["charge_id"])
	}
}

func TestGoogleCalendarCreateEvent(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"id":"evt_1","htmlLink":"https://cal/evt_1"}`)
	e := newTestExecutor(map[string]string{"GOOGLE_ACCESS_TOKEN": "tok"})
	out, err := e.callConnector("google_calendar", "create_event", map[string]any{
		"base_url": srv.URL, "summary": "Sync", "start": "2026-07-01T10:00:00Z", "end": "2026-07-01T10:30:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.path, "/calendars/primary/events") {
		t.Fatalf("calendar path=%s", cap.path)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	st, _ := body["start"].(map[string]any)
	if st["dateTime"] != "2026-07-01T10:00:00Z" {
		t.Fatalf("start wrong: %v", body["start"])
	}
	if out["event_id"] != "evt_1" {
		t.Fatalf("event_id=%v", out["event_id"])
	}
}

func TestTestToolCallHelper(t *testing.T) {
	srv, _ := connectorServer(t, 200, `{}`)
	e := newTestExecutor(map[string]string{"TEAMS_WEBHOOK_URL": srv.URL})
	node := &WorkflowStep{
		Type: "tool_call",
		Config: map[string]any{
			"connector_id": "teams",
			"text":         "hi {{ input.who }}",
		},
	}
	out, err := e.TestToolCall(node, map[string]any{"input": map[string]any{"who": "ops"}})
	if err != nil {
		t.Fatalf("TestToolCall failed: %v", err)
	}
	if out["sent"] != true {
		t.Fatalf("expected sent=true, got %v", out)
	}
}
