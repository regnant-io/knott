package main

import (
	"encoding/json"
	"testing"
)

// TestExampleWorkflowsValid checks every seeded template is structurally sound:
// a trigger-first graph whose every edge (next / cases / next_map / default)
// points at a real node, and that has at least one terminal end node. This is a
// cheap guard so a broken template can never ship to a client install.
func TestExampleWorkflowsValid(t *testing.T) {
	exs := exampleWorkflows()
	if len(exs) != 10 {
		t.Fatalf("expected 10 templates, got %d", len(exs))
	}

	names := map[string]bool{}
	for _, ex := range exs {
		if names[ex.Name] {
			t.Fatalf("duplicate template name: %s", ex.Name)
		}
		names[ex.Name] = true

		// Round-trip the definition through JSON like the registry would store it.
		raw, err := json.Marshal(ex.Definition)
		if err != nil {
			t.Fatalf("%s: marshal: %v", ex.Name, err)
		}
		var def struct {
			Steps []struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Next    string `json:"next"`
				Default string `json:"default"`
				Cases   []struct {
					Next string `json:"next"`
				} `json:"cases"`
				NextMap map[string]string `json:"next_map"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &def); err != nil {
			t.Fatalf("%s: unmarshal: %v", ex.Name, err)
		}
		if len(def.Steps) == 0 {
			t.Fatalf("%s: no steps", ex.Name)
		}
		if def.Steps[0].Type != "trigger" {
			t.Fatalf("%s: first node is %q, want trigger", ex.Name, def.Steps[0].Type)
		}

		ids := map[string]bool{}
		for _, s := range def.Steps {
			if s.ID == "" || s.Type == "" {
				t.Fatalf("%s: step missing id/type", ex.Name)
			}
			ids[s.ID] = true
		}

		check := func(target, where string) {
			if target != "" && !ids[target] {
				t.Fatalf("%s: %s references unknown node %q", ex.Name, where, target)
			}
		}
		hasEnd := false
		for _, s := range def.Steps {
			if s.Type == "end" {
				hasEnd = true
			}
			check(s.Next, "next")
			check(s.Default, "default")
			for _, c := range s.Cases {
				check(c.Next, "case.next")
			}
			for _, v := range s.NextMap {
				check(v, "next_map")
			}
		}
		if !hasEnd {
			t.Fatalf("%s: no end node", ex.Name)
		}
	}
}
