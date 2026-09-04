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
