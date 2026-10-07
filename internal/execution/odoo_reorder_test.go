// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// reorderOdoo answers the calls the spare-part reorder workflow makes. A part
// it knows is BRG-6308-2Z (product 31, template 31) sold by vendor 11.
type reorderOdoo struct {
	mu        sync.Mutex
	calls     []string
	created   map[string]any
	notes     []string
	noVendors bool
}

func (f *reorderOdoo) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		call := strings.TrimPrefix(r.URL.Path, "/json/2/")
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, call)
		switch call {
		case "product.product/search_read":
			domain, _ := json.Marshal(body["domain"])
			if strings.Contains(string(domain), `"BRG-6308-2Z"`) {
				json.NewEncoder(w).Encode([]any{map[string]any{"id": 31, "name": "Bearing SKF 6308-2Z",
					"default_code": "BRG-6308-2Z", "product_tmpl_id": []any{31, "Bearing SKF 6308-2Z"}}})
				return
			}
			json.NewEncoder(w).Encode([]any{})
		case "product.supplierinfo/search_read":
			if f.noVendors {
				json.NewEncoder(w).Encode([]any{})
				return
			}
			json.NewEncoder(w).Encode([]any{map[string]any{"partner_id": []any{11, "Tanga Industrial Bearings"}, "price": 118000}})
		case "purchase.order/create":
			f.created = body
			json.NewEncoder(w).Encode([]any{77})
		case "purchase.order/message_post":
			f.notes = append(f.notes, body["body"].(string))
			json.NewEncoder(w).Encode([]any{9002})
		case "purchase.order/button_confirm":
			json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func reorderHarness(t *testing.T, noVendors bool) *reorderOdoo {
	t.Helper()
	harness(t, map[string]map[string]any{"reorder": odooSparePartReorder().Definition})
	odoo := &reorderOdoo{noVendors: noVendors}
	srv := odoo.server(t)
	for k, v := range map[string]string{"ODOO_URL": srv.URL, "ODOO_DATABASE": "testdb", "ODOO_API_KEY": "test-key"} {
		if err := db.SetCredential(k, v); err != nil {
			t.Fatalf("store credential: %v", err)
		}
	}
	executor.SecretLookup = db.GetCredential
	return odoo
}

func reorderInput(part string) map[string]any {
	return map[string]any{"part_number": part, "part_name": "Bearing SKF 6308-2Z", "quantity": float64(8),
		"source": "IIN", "action_id": "4f2c9e1a", "urgency": "high",
		"reason": "Bearing failure predicted on P-101. Approved in IIN by Asha (maintenance manager)."}
}

// An approved IIN reorder becomes a confirmed RFQ with the part's preferred
// vendor, an origin pointing back at the IIN action, and a note saying why.
func TestSparePartReorderRaisesAnRFQ(t *testing.T) {
	odoo := reorderHarness(t, false)
	run := startRun(t, "reorder", reorderInput("BRG-6308-2Z"))
	if run.Status != "COMPLETED" || run.Outcome != "RFQ_SUBMITTED" {
		t.Fatalf("status %s / %s at %s; context %s", run.Status, run.Outcome, run.CurrentNode, run.Context)
	}
	vals := odoo.created["vals_list"].([]any)[0].(map[string]any)
	line := vals["order_line"].([]any)[0].([]any)
	lv := line[2].(map[string]any)
	if vals["partner_id"] != float64(11) || lv["product_id"] != float64(31) || lv["product_qty"] != float64(8) ||
		vals["origin"] != "IIN 4f2c9e1a" || line[0] != float64(0) {
		t.Fatalf("RFQ values = %v", odoo.created)
	}
	if _, priced := lv["price_unit"]; priced {
		t.Fatalf("price_unit should come from Odoo's vendor list, got %v", lv["price_unit"])
	}
	if len(odoo.notes) != 1 || !strings.Contains(odoo.notes[0], "Approved in IIN by Asha") || !strings.Contains(odoo.notes[0], run.ID) {
		t.Fatalf("note = %q", odoo.notes)
	}
	if odoo.calls[len(odoo.calls)-1] != "purchase.order/button_confirm" {
		t.Fatalf("RFQ was not confirmed; calls = %v", odoo.calls)
	}
}

// A part Odoo does not know goes to a buyer, and nothing is created.
func TestSparePartReorderUnknownPartGoesToABuyer(t *testing.T) {
	odoo := reorderHarness(t, false)
	run := runUntilWaiting(t, "reorder", reorderInput("NOPE-1"))
	if run.CurrentNode != "unknown_part" || odoo.created != nil {
		t.Fatalf("paused at %q, created %v", run.CurrentNode, odoo.created)
	}
}

// A part with no vendor also goes to a buyer.
func TestSparePartReorderWithoutVendorGoesToABuyer(t *testing.T) {
	odoo := reorderHarness(t, true)
	run := runUntilWaiting(t, "reorder", reorderInput("BRG-6308-2Z"))
	if run.CurrentNode != "no_vendor" || odoo.created != nil {
		t.Fatalf("paused at %q, created %v", run.CurrentNode, odoo.created)
	}
}

// A caller that retries with the same Idempotency-Key gets the first run
// back instead of starting a second one.
func TestCreateRunIsIdempotent(t *testing.T) {
	reorderHarness(t, false)
	post := func(key string) map[string]any {
		body, _ := json.Marshal(map[string]any{"workflow_id": "reorder", "input_data": reorderInput("BRG-6308-2Z")})
		req := httptest.NewRequest("POST", "/api/v1/runs", bytes.NewReader(body))
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		rec := httptest.NewRecorder()
		createRun(rec, req)
		if rec.Code != 200 && rec.Code != 201 {
			t.Fatalf("HTTP %d: %s", rec.Code, rec.Body)
		}
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	first := post("iin-action-4f2c9e1a")
	again := post("iin-action-4f2c9e1a")
	other := post("iin-action-other")
	if first["id"] == nil || first["id"] != again["id"] {
		t.Fatalf("retry started a new run: %v vs %v", first["id"], again["id"])
	}
	if other["id"] == first["id"] {
		t.Fatal("a different key returned the same run")
	}
}
