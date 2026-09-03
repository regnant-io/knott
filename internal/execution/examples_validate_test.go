// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"encoding/json"
	"testing"

	"github.com/regnant/knott/internal/registry/handlers"
)

// The example workflows are the first thing a new operator opens, and a template
// that fails validation is a bad first impression that also teaches the wrong
// shape. This runs the real validator over every one of them.
func TestShippedExamplesPassValidation(t *testing.T) {
	for _, ex := range exampleWorkflows() {
		raw, _ := json.Marshal(ex.Definition)
		var def map[string]any
		json.Unmarshal(raw, &def)
		f := handlers.Validate(def)
		if !f.Valid {
			t.Errorf("%s: %v", ex.Name, f.Errors)
		}
		if len(f.Warnings) > 0 {
			// Warnings are advisory for a user's own draft, but a shipped
			// template should not be modelling something questionable.
			t.Errorf("%s has warnings: %v", ex.Name, f.Warnings)
		}
	}
}
