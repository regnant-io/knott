// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"
)

func TestConnectionDoesNotExecuteWebhookBusinessAction(t *testing.T) {
	e := newTestExecutor(map[string]string{"SLACK_WEBHOOK_URL": "hooks.slack.com/services/T/B/X"})
	out, err := e.TestConnection("slack")
	if err != nil {
		t.Fatalf("configuration probe failed: %v", err)
	}
	if out["validated"] != "configuration" {
		t.Fatalf("webhook probe should be non-mutating, got %#v", out)
	}
}

func TestConnectionReportsMissingCredentialByName(t *testing.T) {
	e := newTestExecutor(nil)
	_, err := e.TestConnection("zoom")
	if err == nil || !strings.Contains(err.Error(), "ZOOM_ACCESS_TOKEN") {
		t.Fatalf("expected actionable missing credential error, got %v", err)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	if got := normalizeBaseURL("acme.myshopify.com/"); got != "https://acme.myshopify.com" {
		t.Fatalf("normalizeBaseURL = %q", got)
	}
	if got := normalizeBaseURL("http://localhost:9999/"); got != "http://localhost:9999" {
		t.Fatalf("normalizeBaseURL should preserve explicit scheme, got %q", got)
	}
}
