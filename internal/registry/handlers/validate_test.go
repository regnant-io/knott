// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"strings"
	"testing"
)

// def parses a JSON literal into a definition, so these tests read like the
// thing the console actually posts.
func def(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("bad test fixture: %v", err)
	}
	return m
}

func hasMatch(list []string, substr string) bool {
	for _, s := range list {
		if strings.Contains(strings.ToLower(s), strings.ToLower(substr)) {
			return true
		}
	}
	return false
}

func TestValidateAcceptsAWorkingWorkflow(t *testing.T) {
	f := Validate(def(t, `{"steps":[
	  {"id":"start","type":"trigger","name":"Trigger","next":"gate"},
	  {"id":"gate","type":"condition","name":"Gate",
	   "cases":[{"condition":"input.amount > 100","next":"call"}],"default":"stop"},
	  {"id":"call","type":"tool_call","name":"Notify","next":"stop",
	   "config":{"connector_id":"slack"}},
	  {"id":"stop","type":"end","name":"Done"}
	]}`))
	if !f.Valid {
		t.Fatalf("expected valid, got errors: %v", f.Errors)
	}
	if len(f.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", f.Warnings)
	}
}

func TestValidateCatchesDanglingReferences(t *testing.T) {
	// Deleting a step leaves edges behind in fields the canvas does not draw.
	// Each of these used to pass validation and fail at run time.
	cases := []struct{ name, defn, want string }{
		{"next", `{"steps":[
		   {"id":"a","type":"trigger","name":"Start","next":"ghost"},
		   {"id":"z","type":"end"}]}`, "next step"},
		{"error output", `{"steps":[
		   {"id":"a","type":"trigger","next":"b"},
		   {"id":"b","type":"tool_call","name":"Call","next":"z","config":{"connector_id":"slack","on_error":"ghost"}},
		   {"id":"z","type":"end"}]}`, "error output"},
		{"branch", `{"steps":[
		   {"id":"a","type":"trigger","next":"g"},
		   {"id":"g","type":"condition","name":"Gate","cases":[{"condition":"x","next":"ghost"}],"default":"z"},
		   {"id":"z","type":"end"}]}`, "branch 1"},
		{"default", `{"steps":[
		   {"id":"a","type":"trigger","next":"g"},
		   {"id":"g","type":"condition","cases":[{"condition":"x","next":"z"}],"default":"ghost"},
		   {"id":"z","type":"end"}]}`, "default branch"},
		{"fallback", `{"steps":[
		   {"id":"a","type":"trigger","next":"b"},
		   {"id":"b","type":"ai_decision","next":"z","config":{"task":"invoice_approval","fallback":"ghost"}},
		   {"id":"z","type":"end"}]}`, "fallback"},
		{"merge source", `{"steps":[
		   {"id":"a","type":"trigger","next":"m"},
		   {"id":"m","type":"merge","next":"z","config":{"sources":["ghost"]}},
		   {"id":"z","type":"end"}]}`, "merge source"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := Validate(def(t, c.defn))
			if f.Valid {
				t.Fatalf("expected an error for a dangling %s", c.name)
			}
			if !hasMatch(f.Errors, c.want) {
				t.Errorf("error should name the %s; got %v", c.want, f.Errors)
			}
			if !hasMatch(f.Errors, "ghost") {
				t.Errorf("error should name the missing step; got %v", f.Errors)
			}
		})
	}
}

func TestValidateNamesStepsTheWayTheirAuthorDid(t *testing.T) {
	// "Step node_1041 is broken" is not something anyone can act on.
	f := Validate(def(t, `{"steps":[
	  {"id":"node_1041","type":"trigger","name":"Nightly sync","next":"ghost"},
	  {"id":"z","type":"end"}]}`))
	if !hasMatch(f.Errors, "Nightly sync") {
		t.Errorf("error should use the step's name; got %v", f.Errors)
	}
}

func TestValidateRequiresATriggerAndRejectsTwo(t *testing.T) {
	none := Validate(def(t, `{"steps":[{"id":"z","type":"end"}]}`))
	if none.Valid || !hasMatch(none.Errors, "no trigger") {
		t.Errorf("a workflow with no trigger should be rejected; got %v", none.Errors)
	}
	two := Validate(def(t, `{"steps":[
	  {"id":"a","type":"trigger","next":"z"},
	  {"id":"b","type":"trigger","next":"z"},
	  {"id":"z","type":"end"}]}`))
	if two.Valid || !hasMatch(two.Errors, "more than one trigger") {
		t.Errorf("two triggers should be rejected; got %v", two.Errors)
	}
}

func TestValidateRejectsDuplicateIDs(t *testing.T) {
	f := Validate(def(t, `{"steps":[
	  {"id":"a","type":"trigger","next":"dup"},
	  {"id":"dup","type":"set","next":"z"},
	  {"id":"dup","type":"set","next":"z"},
	  {"id":"z","type":"end"}]}`))
	if f.Valid || !hasMatch(f.Errors, "share the id") {
		t.Errorf("duplicate ids make routing ambiguous; got %v", f.Errors)
	}
}

func TestValidateRejectsAnUnrunnableStepType(t *testing.T) {
	f := Validate(def(t, `{"steps":[
	  {"id":"a","type":"trigger","next":"b"},
	  {"id":"b","type":"quantum_flux","name":"Flux","next":"z"},
	  {"id":"z","type":"end"}]}`))
	if f.Valid || !hasMatch(f.Errors, "cannot run") {
		t.Errorf("an unknown type fails on its first visit; catch it here. got %v", f.Errors)
	}
}

func TestValidateWarnsAboutUnreachableSteps(t *testing.T) {
	// Not an error: a half-finished draft is a normal thing to save.
	f := Validate(def(t, `{"steps":[
	  {"id":"a","type":"trigger","name":"Start","next":"z"},
	  {"id":"lost","type":"set","name":"Orphan"},
	  {"id":"z","type":"end"}]}`))
	if !f.Valid {
		t.Errorf("an unreachable step should not block saving; got %v", f.Errors)
	}
	if !hasMatch(f.Warnings, "Orphan") {
		t.Errorf("expected a warning naming the orphan; got %v", f.Warnings)
	}
}

func TestValidateWarnsAboutDeadEnds(t *testing.T) {
	f := Validate(def(t, `{"steps":[
	  {"id":"a","type":"trigger","next":"b"},
	  {"id":"b","type":"set","name":"Stash"},
	  {"id":"z","type":"end"}]}`))
	if !hasMatch(f.Warnings, "Stash") || !hasMatch(f.Warnings, "nothing after it") {
		t.Errorf("expected a dead-end warning; got %v", f.Warnings)
	}
}

func TestValidateChecksPerStepConfiguration(t *testing.T) {
	cases := []struct{ name, defn, want string }{
		{"connector not chosen",
			`{"id":"b","type":"tool_call","name":"Call","next":"z"}`, "no connector"},
		{"task spec not chosen",
			`{"id":"b","type":"ai_decision","name":"Decide","next":"z"}`, "no task spec"},
		{"threshold out of range",
			`{"id":"b","type":"ai_decision","name":"Decide","next":"z","config":{"task":"invoice_approval","confidence_threshold":4}}`,
			"between 0 and 1"},
		{"agent not chosen",
			`{"id":"b","type":"agent_call","name":"Agent","next":"z"}`, "no agent"},
		{"sub-workflow not chosen",
			`{"id":"b","type":"sub_workflow","name":"Child","next":"z"}`, "no workflow"},
		{"filter without a condition",
			`{"id":"b","type":"filter","name":"Gate","next":"z"}`, "no condition"},
		{"loop without a list",
			`{"id":"b","type":"loop","name":"Each","next":"z"}`, "no list"},
		{"wait without a duration",
			`{"id":"b","type":"wait","name":"Pause","next":"z"}`, "no duration"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := Validate(def(t, `{"steps":[
			  {"id":"a","type":"trigger","next":"b"},`+c.defn+`,
			  {"id":"z","type":"end"}]}`))
			if f.Valid {
				t.Fatalf("expected an error")
			}
			if !hasMatch(f.Errors, c.want) {
				t.Errorf("error should say %q; got %v", c.want, f.Errors)
			}
		})
	}
}

func TestValidateChecksConditionBranches(t *testing.T) {
	f := Validate(def(t, `{"steps":[
	  {"id":"a","type":"trigger","next":"g"},
	  {"id":"g","type":"condition","name":"Gate",
	   "cases":[{"condition":"","next":"z"},{"condition":"x > 1","next":""}]},
	  {"id":"z","type":"end"}]}`))
	if f.Valid {
		t.Fatal("a branch with no expression can never match — that is an error")
	}
	if !hasMatch(f.Errors, "no expression") {
		t.Errorf("expected the empty-expression error; got %v", f.Errors)
	}
	if !hasMatch(f.Warnings, "not connected") {
		t.Errorf("expected a warning for the unconnected branch; got %v", f.Warnings)
	}
	if !hasMatch(f.Warnings, "no default branch") {
		t.Errorf("expected a warning about the missing default; got %v", f.Warnings)
	}
}

func TestValidateRejectsAnEmptyWorkflowKindly(t *testing.T) {
	f := Validate(def(t, `{"steps":[]}`))
	if f.Valid {
		t.Fatal("an empty workflow is not valid")
	}
	if !hasMatch(f.Errors, "add a trigger") {
		t.Errorf("the message should say what to do next; got %v", f.Errors)
	}
}

func TestValidateAlwaysReturnsListsNotNulls(t *testing.T) {
	// The console iterates these; a null would need a guard at every call site.
	f := Validate(def(t, `{"steps":[
	  {"id":"a","type":"trigger","next":"z"},{"id":"z","type":"end"}]}`))
	b, _ := json.Marshal(f)
	if strings.Contains(string(b), "null") {
		t.Errorf("errors and warnings must serialise as arrays: %s", b)
	}
}
