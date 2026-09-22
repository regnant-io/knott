// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"testing"

	"github.com/regnant/knott/internal/execution/store"
)

func TestConnectorReadySupportsAuthenticationRecipes(t *testing.T) {
	catalog := store.CatalogBySlug()

	t.Run("slack bot token alternative", func(t *testing.T) {
		t.Setenv("SLACK_WEBHOOK_URL", "")
		t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
		if !connectorReady(catalog["slack"], nil) {
			t.Fatal("Slack should be ready with its bot-token alternative")
		}
	})

	t.Run("google direct access token", func(t *testing.T) {
		t.Setenv("GOOGLE_CLIENT_ID", "")
		t.Setenv("GOOGLE_CLIENT_SECRET", "")
		t.Setenv("GOOGLE_REFRESH_TOKEN", "")
		t.Setenv("GOOGLE_ACCESS_TOKEN", "short-lived-token")
		if !connectorReady(catalog["google_sheets"], nil) {
			t.Fatal("Google Sheets should be ready with a direct access token")
		}
		if missing := missingCredentialNames(catalog["google_sheets"], nil); len(missing) != 0 {
			t.Fatalf("unexpected missing credentials: %v", missing)
		}
	})
}
