// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCatalogHasSixtyExecutableConnectors(t *testing.T) {
	catalog := Catalog()
	if len(catalog) < 60 {
		t.Fatalf("connector catalog has %d entries; want at least 60", len(catalog))
	}
	seen := map[string]bool{}
	for _, entry := range catalog {
		if entry.Slug == "" || entry.Name == "" {
			t.Fatalf("connector has missing identity: %+v", entry)
		}
		if seen[entry.Slug] {
			t.Fatalf("duplicate connector slug %q", entry.Slug)
		}
		seen[entry.Slug] = true
	}
}

func TestGoogleConnectorsAcceptEitherOAuthRecipe(t *testing.T) {
	for _, slug := range []string{"google_sheets", "google_calendar", "google_drive"} {
		entry := CatalogBySlug()[slug]
		if len(entry.CredentialSets) != 2 {
			t.Fatalf("%s has %d credential recipes; want access-token and refresh-token recipes", slug, len(entry.CredentialSets))
		}
	}
}

func TestLegacyConnectorTableGetsSlugMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE connectors (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, category TEXT,
		description TEXT DEFAULT '', icon TEXT DEFAULT 'zap',
		status TEXT DEFAULT 'available', installed INTEGER DEFAULT 0,
		config TEXT DEFAULT '{}', credential_keys TEXT DEFAULT '[]',
		created_at TEXT DEFAULT (datetime('now'))
	)`)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	db, err := NewDB(path)
	if err != nil {
		t.Fatalf("open upgraded DB: %v", err)
	}
	defer db.Close()
	db.SeedConnectors()
	connectors, err := db.ListConnectors()
	if err != nil {
		t.Fatalf("list connectors after migration: %v", err)
	}
	if len(connectors) < 60 {
		t.Fatalf("got %d migrated connectors", len(connectors))
	}
}
