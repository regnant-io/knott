// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package humantask

import "testing"

func TestOnlyEngineCallbacksAreAccepted(t *testing.T) {
	t.Setenv("EXECUTION_ENGINE_URL", "http://127.0.0.1:8002")
	cases := map[string]bool{
		"http://127.0.0.1:8002/internal/v1/task-complete/run/node?sig=x": true,
		"http://169.254.169.254/latest/meta-data/":                       false,
		"http://127.0.0.1:8002/api/v1/credentials":                       false,
		"http://attacker.example/internal/v1/task-complete/run/node":     false,
		"file:///etc/passwd":                                             false,
	}
	for raw, want := range cases {
		if got := trustedCallback(raw); got != want {
			t.Errorf("%s: got %v want %v", raw, got, want)
		}
	}
}
