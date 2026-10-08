// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPollSourceHTTPItemsPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"records":[{"id":1},{"id":2},{"id":3}]}`))
	}))
	defer srv.Close()
	e := NewExecutor(Services{})
	items, err := e.PollSource(map[string]any{
		"source": "http", "url": srv.URL, "items_path": "records",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items want 3", len(items))
	}
}

func TestPollSourceHTTPTopLevelArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":1},{"id":2}]`))
	}))
	defer srv.Close()
	e := NewExecutor(Services{})
	items, err := e.PollSource(map[string]any{"source": "http", "url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items want 2", len(items))
	}
}

func TestPollSourceConnector(t *testing.T) {
	// Airtable list_records via the connector path, redirected to a test server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"records":[{"id":"rec1"},{"id":"rec2"}]}`))
	}))
	defer srv.Close()
	e := newTestExecutor(map[string]string{"AIRTABLE_TOKEN": "t"})
	items, err := e.PollSource(map[string]any{
		"source": "connector", "connector_id": "airtable", "action": "list_records",
		"base_url": srv.URL, "base_id": "appX", "table": "T",
		"items_path": "records",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("connector poll got %d items want 2", len(items))
	}
}

func TestPollSourceError(t *testing.T) {
	e := NewExecutor(Services{})
	if _, err := e.PollSource(map[string]any{"source": "http"}); err == nil {
		t.Fatal("expected error when no url provided")
	}
}

func TestPollSourceOdooPurchaseOrders(t *testing.T) {
	// Mock Odoo server: returns a list of purchase orders in response to search_read.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Odoo External API calls are POST /json/2/<model>/<method>
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/json/2/purchase.order/search_read" {
			t.Errorf("expected path /json/2/purchase.order/search_read, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Mock response: a list of purchase order records.
		w.Write([]byte(`[{"id": 1, "name": "P00001", "state": "to approve"}, {"id": 2, "name": "P00002", "state": "to approve"}]`))
	}))
	defer srv.Close()

	// Setup executor with Odoo credentials.
	e := newTestExecutor(map[string]string{
		"ODOO_URL":      srv.URL,
		"ODOO_DATABASE": "demo",
		"ODOO_API_KEY":  "demo-key",
	})

	// Poll for purchase orders.
	items, err := e.PollSource(map[string]any{
		"source":       "connector",
		"connector_id": "odoo",
		"action":       "search_read",
		"model":         "purchase.order",
		"domain":       []any{[]any{"state", "=", "to approve"}},
		"fields":       []any{"name", "state"},
		"items_path":   "items",
	})

	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	// DEBUG: print what we actually got
	t.Logf("Poll results: %v", items)

	if len(items) != 2 {
		t.Fatalf("expected 2 purchase orders, got %d", len(items))
	}

	po0, ok := items[0].(map[string]any)
	if !ok || po0["name"] != "P00001" {
		t.Errorf("expected PO P00001, got %v", items[0])
	}
}
