// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"strings"
	"testing"
)

func TestEveryDefinitionIsValid(t *testing.T) {
	for _, err := range Validate() {
		t.Error(err)
	}
}

func TestTheCatalogCoversManyIndustries(t *testing.T) {
	categories := map[string]int{}
	for _, d := range All() {
		if !d.Native {
			categories[d.Category]++
		}
	}
	if len(Declarative()) < 60 {
		t.Errorf("expected a broad declarative library, got %d connectors", len(Declarative()))
	}
	for _, want := range []string{"CRM", "Marketing", "E-commerce", "Finance", "Developer Tools", "Communication", "AI", "HR & Recruiting", "Productivity"} {
		if categories[want] == 0 {
			t.Errorf("no declarative connectors in %q", want)
		}
	}
}

func TestCredentialNamesAreUniqueAcrossConnectors(t *testing.T) {
	owner := map[string]string{}
	for _, d := range Declarative() {
		for _, c := range d.Credentials {
			if prev, ok := owner[c.Name]; ok && prev != d.Slug {
				// Sharing is allowed only for deliberately shared secrets.
				if !strings.HasPrefix(c.Name, "GOOGLE_") && !strings.HasPrefix(c.Name, "MS_GRAPH") {
					t.Errorf("credential %s is declared by both %s and %s", c.Name, prev, d.Slug)
				}
			}
			owner[c.Name] = d.Slug
		}
	}
}

func TestActionLookupDefaultsToTheFirst(t *testing.T) {
	d, ok := Get("pipedrive")
	if !ok {
		t.Fatal("pipedrive missing")
	}
	if a, _ := d.Action(""); a.ID != d.Actions[0].ID {
		t.Errorf("got %s", a.ID)
	}
	if _, ok := d.Action("nope"); ok {
		t.Error("unknown action should not resolve")
	}
}
