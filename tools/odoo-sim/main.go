// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Command odoo-sim is a stand-in for an Odoo 19 server, for rehearsing the
// purchase-order approval workflow without access to a real instance.
//
// It speaks the slice of Odoo's External JSON-2 API that the KNOTT Odoo
// connector uses — POST /json/2/<model>/<method> with a bearer API key and an
// X-Odoo-Database header — over an in-memory purchasing dataset in Tanzanian
// shillings. Requests, responses and error bodies follow Odoo's documented
// shapes, so a workflow that runs against the simulator runs against Odoo by
// changing only the connector's address, database and key.
//
// It is not Odoo. Access rights, record rules, taxes and most fields are
// absent; anything the workflow does not touch is not modelled.
//
//	go run ./tools/odoo-sim                       # http://127.0.0.1:8069
//	go run ./tools/odoo-sim -addr :8069 -key demo-key -db demo
//
// Open the address in a browser for a live view of the orders and the notes
// KNOTT posts on them. POST /sim/reset restores the dataset; POST /sim/order
// adds an order awaiting approval (body: {"vendor_id": 7, "amount": 2500000}).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	addr   = flag.String("addr", "127.0.0.1:8069", "listen address")
	apiKey = flag.String("key", "demo-key", "API key accepted as the bearer token")
	dbName = flag.String("db", "demo", "database name; requests naming another database are refused")
)

// ─── Dataset ────────────────────────────────────────────────────────────────

type record = map[string]any

type store struct {
	mu       sync.Mutex
	models   map[string]map[int]record
	nextID   map[string]int
	messages map[string][]record // "model/id" → chatter
	// Two-step approval: orders at or above this total go to "to approve" on
	// confirmation, as with Odoo's Purchase → Order Approval setting.
	approvalMin float64
}

func newStore() *store {
	s := &store{}
	s.reset()
	return s
}

func m2o(id int, name string) []any { return []any{id, name} }

func (s *store) reset() {
	s.models = map[string]map[int]record{
		"res.company": {}, "res.partner": {}, "res.currency": {},
		"purchase.order": {}, "purchase.order.line": {}, "product.product": {},
	}
	s.nextID = map[string]int{}
	s.messages = map[string][]record{}
	s.approvalMin = 0

	s.put("res.company", record{"id": 1, "name": "Demo Company (Dar es Salaam)", "currency_id": m2o(1, "TZS")})
	s.put("res.currency", record{"id": 1, "name": "TZS", "symbol": "TSh"})
	vendors := []struct {
		id   int
		name string
	}{
		{7, "Kilimanjaro Office Supplies"},
		{8, "Msasani IT Solutions"},
		{9, "Dar Fuel & Lubricants"},
		{10, "Arusha Packaging Ltd"},
	}
	for _, v := range vendors {
		s.put("res.partner", record{"id": v.id, "name": v.name, "is_company": true, "supplier_rank": 1, "city": "Dar es Salaam", "country_id": m2o(215, "Tanzania")})
	}
	products := map[int]string{
		21: "A4 Paper (box of 5 reams)", 22: "Toner cartridge HP 85A", 23: "Laptop, 14\", 16 GB RAM",
		24: "Diesel (litre)", 25: "Engine oil 15W-40 (20 L)", 26: "Corrugated carton 40x30x30",
	}
	for id, name := range products {
		s.put("product.product", record{"id": id, "name": name, "display_name": name})
	}

	// Confirmed history, so each vendor has a usual order size.
	day := func(n int) string { return time.Now().AddDate(0, 0, -n).Format(time.DateTime) }
	hist := []struct {
		vendor int
		date   int
		lines  [][3]any // product, qty, unit price
	}{
		{7, 60, [][3]any{{21, 40.0, 38000.0}, {22, 6.0, 95000.0}}},
		{7, 31, [][3]any{{21, 35.0, 38000.0}, {22, 4.0, 95000.0}}},
		{7, 12, [][3]any{{21, 45.0, 39000.0}, {22, 5.0, 96000.0}}},
		{8, 90, [][3]any{{23, 2.0, 2650000.0}}},
		{8, 40, [][3]any{{23, 3.0, 2600000.0}}},
		{9, 45, [][3]any{{24, 600.0, 3150.0}}},
		{9, 25, [][3]any{{24, 650.0, 3180.0}, {25, 2.0, 210000.0}}},
		{9, 6, [][3]any{{24, 580.0, 3200.0}}},
	}
	for _, h := range hist {
		s.order(h.vendor, "purchase", day(h.date), h.lines)
	}

	// Waiting for a decision — one of each kind the workflow handles.
	s.order(7, "to approve", day(1), [][3]any{{21, 42.0, 39000.0}, {22, 2.0, 96000.0}})   // routine
	s.order(8, "to approve", day(1), [][3]any{{23, 5.0, 2900000.0}})                      // above the limit
	s.order(10, "to approve", day(0), [][3]any{{26, 4000.0, 800.0}})                      // new vendor
	s.order(9, "to approve", day(0), [][3]any{{24, 2800.0, 3200.0}, {25, 4.0, 215000.0}}) // unusual size
}

func (s *store) put(model string, r record) int {
	id, _ := r["id"].(int)
	if id == 0 {
		id = s.nextID[model] + 1
		r["id"] = id
	}
	if id > s.nextID[model] {
		s.nextID[model] = id
	}
	if _, ok := r["write_date"]; !ok {
		r["write_date"] = time.Now().UTC().Format(time.DateTime)
	}
	s.models[model][id] = r
	return id
}

func (s *store) order(vendor int, state, date string, lines [][3]any) int {
	partner := s.models["res.partner"][vendor]
	id := s.nextID["purchase.order"] + 1
	name := fmt.Sprintf("P%05d", id)
	var untaxed float64
	var lineIDs []any
	for _, l := range lines {
		pid := l[0].(int)
		qty, price := l[1].(float64), l[2].(float64)
		sub := qty * price
		untaxed += sub
		prod := s.models["product.product"][pid]
		lid := s.put("purchase.order.line", record{
			"order_id": m2o(id, name), "product_id": m2o(pid, prod["name"].(string)), "name": prod["name"],
			"product_qty": qty, "product_uom_id": m2o(1, "Units"), "price_unit": price,
			"price_subtotal": sub, "taxes_id": []any{}, "date_planned": date,
		})
		lineIDs = append(lineIDs, lid)
	}
	return s.put("purchase.order", record{
		"id": id, "name": name, "partner_id": m2o(vendor, partner["name"].(string)),
		"amount_untaxed": untaxed, "amount_tax": 0.0, "amount_total": untaxed,
		"currency_id": m2o(1, "TZS"), "company_id": m2o(1, "Demo Company (Dar es Salaam)"),
		"date_order": date, "date_planned": date, "date_approve": false,
		"user_id": m2o(2, "Purchasing Officer"), "origin": false, "state": state,
		"order_line": lineIDs, "priority": "0",
	})
}

// ─── JSON-2 ─────────────────────────────────────────────────────────────────

type odooError struct {
	status int
	name   string
	msg    string
}

func (e *odooError) Error() string { return e.msg }

func userError(msg string) error {
	return &odooError{422, "odoo.exceptions.UserError", msg}
}

func writeErr(w http.ResponseWriter, err error) {
	var oe *odooError
	if !errors.As(err, &oe) {
		oe = &odooError{500, "builtins.Exception", err.Error()}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(oe.status)
	json.NewEncoder(w).Encode(map[string]any{
		"name": oe.name, "message": oe.msg, "arguments": []any{oe.msg, oe.status},
		"context": map[string]any{}, "debug": "odoo-sim: " + oe.name + ": " + oe.msg,
	})
}

func (s *store) json2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, &odooError{405, "werkzeug.exceptions.MethodNotAllowed", "use POST"})
		return
	}
	auth := r.Header.Get("Authorization")
	if len(auth) < 7 || !strings.EqualFold(auth[:7], "bearer ") || strings.TrimSpace(auth[7:]) != *apiKey {
		writeErr(w, &odooError{401, "werkzeug.exceptions.Unauthorized", "Invalid apikey"})
		return
	}
	if db := r.Header.Get("X-Odoo-Database"); db != "" && db != *dbName {
		writeErr(w, &odooError{404, "werkzeug.exceptions.NotFound", fmt.Sprintf("database %q not found", db)})
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/json/2/"), "/"), "/")
	if len(parts) != 2 {
		writeErr(w, &odooError{404, "werkzeug.exceptions.NotFound", "expected /json/2/<model>/<method>"})
		return
	}
	model, method := parts[0], parts[1]
	var args map[string]any
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil && err.Error() != "EOF" {
		writeErr(w, &odooError{400, "werkzeug.exceptions.BadRequest", "invalid JSON body: " + err.Error()})
		return
	}
	if args == nil {
		args = map[string]any{}
	}

	s.mu.Lock()
	result, err := s.call(model, method, args)
	s.mu.Unlock()
	log.Printf("%s.%s ids=%v → err=%v", model, method, args["ids"], err)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(result)
}

func (s *store) call(model, method string, args map[string]any) (any, error) {
	table, ok := s.models[model]
	if !ok {
		return nil, &odooError{404, "werkzeug.exceptions.NotFound", fmt.Sprintf("model %q not found", model)}
	}
	ids, err := intList(args["ids"])
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, ok := table[id]; !ok {
			return nil, &odooError{422, "odoo.exceptions.MissingError",
				fmt.Sprintf("Record does not exist or has been deleted.\n(Record: %s(%d,), User: 2)", model, id)}
		}
	}

	switch method {
	case "search_read", "search", "search_count":
		domain, _ := args["domain"].([]any)
		var matched []record
		for _, rec := range table {
			ok, err := matches(rec, domain)
			if err != nil {
				return nil, err
			}
			if ok {
				matched = append(matched, rec)
			}
		}
		sortRecords(matched, str(args["order"]))
		if method == "search_count" {
			return len(matched), nil
		}
		if off := int(num(args["offset"])); off > 0 && off < len(matched) {
			matched = matched[off:]
		} else if off >= len(matched) && off > 0 {
			matched = nil
		}
		if lim := int(num(args["limit"])); lim > 0 && lim < len(matched) {
			matched = matched[:lim]
		}
		if method == "search" {
			out := make([]any, 0, len(matched))
			for _, rec := range matched {
				out = append(out, rec["id"])
			}
			return out, nil
		}
		return project(matched, args["fields"]), nil

	case "read":
		recs := make([]record, 0, len(ids))
		for _, id := range ids {
			recs = append(recs, table[id])
		}
		return project(recs, args["fields"]), nil

	case "write":
		vals, _ := args["vals"].(map[string]any)
		for _, id := range ids {
			for k, v := range vals {
				if k == "id" || k == "state" {
					return nil, userError("odoo-sim: use the order's buttons to change its state")
				}
				table[id][k] = v
			}
			touch(table[id])
		}
		return true, nil

	case "context_get":
		return map[string]any{"lang": "en_US", "tz": "Africa/Dar_es_Salaam", "uid": 2}, nil

	case "message_post":
		if len(ids) != 1 {
			return nil, userError("message_post expects exactly one record")
		}
		body := str(args["body"])
		if strings.TrimSpace(body) == "" {
			return nil, userError("odoo-sim: an empty note was posted")
		}
		key := fmt.Sprintf("%s/%d", model, ids[0])
		msgID := len(s.messages[key]) + 1000*ids[0]
		s.messages[key] = append(s.messages[key], record{
			"id": msgID, "body": body, "date": time.Now().Format(time.DateTime),
			"subtype": firstNonEmpty(str(args["subtype_xmlid"]), "mail.mt_comment"),
		})
		return []any{msgID}, nil
	}

	if model == "purchase.order" {
		for _, id := range ids {
			if err := s.button(table[id], method); err != nil {
				return nil, err
			}
		}
		if len(ids) == 0 {
			return nil, userError("Expected singleton: purchase.order()")
		}
		if method == "button_confirm" || method == "button_approve" || method == "button_cancel" || method == "button_draft" {
			return map[string]any{}, nil
		}
	}
	return nil, &odooError{404, "werkzeug.exceptions.NotFound", fmt.Sprintf("method %q not found on %s (odoo-sim models only what the workflow uses)", method, model)}
}

// button applies purchase.order's state machine the way Odoo does.
func (s *store) button(po record, method string) error {
	state := po["state"].(string)
	switch method {
	case "button_confirm":
		if state != "draft" && state != "sent" {
			return nil // Odoo ignores orders not in draft/sent
		}
		if num(po["amount_total"]) >= s.approvalMin && s.approvalMin > 0 {
			po["state"] = "to approve"
		} else {
			po["state"], po["date_approve"] = "purchase", time.Now().Format(time.DateTime)
		}
	case "button_approve":
		if state != "to approve" {
			return userError(fmt.Sprintf("%s cannot be approved: it is %q, not waiting for approval.", po["name"], state))
		}
		po["state"], po["date_approve"] = "purchase", time.Now().Format(time.DateTime)
	case "button_cancel":
		if state == "cancel" {
			return nil
		}
		po["state"] = "cancel"
	case "button_draft":
		po["state"] = "draft"
	default:
		return &odooError{404, "werkzeug.exceptions.NotFound", fmt.Sprintf("method %q not found on purchase.order", method)}
	}
	touch(po)
	return nil
}

func touch(r record) { r["write_date"] = time.Now().UTC().Format(time.DateTime) }

// ─── Domains, fields, ordering ───────────────────────────────────────────────

func matches(rec record, domain []any) (bool, error) {
	for _, term := range domain {
		switch t := term.(type) {
		case string:
			if t == "&" {
				continue
			}
			return false, userError(fmt.Sprintf("odoo-sim supports only implicit AND domains, not %q", t))
		case []any:
			if len(t) != 3 {
				return false, userError(fmt.Sprintf("Invalid leaf %v", t))
			}
			ok, err := leaf(rec, str(t[0]), str(t[1]), t[2])
			if err != nil || !ok {
				return false, err
			}
		default:
			return false, userError(fmt.Sprintf("Invalid domain term %v", term))
		}
	}
	return true, nil
}

func leaf(rec record, field, op string, want any) (bool, error) {
	v := rec[field]
	if pair, ok := v.([]any); ok && len(pair) == 2 {
		v = pair[0] // many2one compares by id
	}
	switch op {
	case "=", "==":
		return equal(v, want), nil
	case "!=", "<>":
		return !equal(v, want), nil
	case "in", "not in":
		list, _ := want.([]any)
		found := false
		for _, w := range list {
			if equal(v, w) {
				found = true
			}
		}
		return found == (op == "in"), nil
	case ">", ">=", "<", "<=":
		a, b := num(v), num(want)
		if sa, ok := v.(string); ok {
			sb := str(want)
			return map[string]bool{">": sa > sb, ">=": sa >= sb, "<": sa < sb, "<=": sa <= sb}[op], nil
		}
		return map[string]bool{">": a > b, ">=": a >= b, "<": a < b, "<=": a <= b}[op], nil
	case "ilike", "like":
		pat := strings.Trim(strings.ToLower(str(want)), "%")
		return strings.Contains(strings.ToLower(str(v)), pat), nil
	}
	return false, userError(fmt.Sprintf("odoo-sim does not support the %q operator", op))
}

func equal(a, b any) bool {
	if fa, ok := numeric(a); ok {
		if fb, ok := numeric(b); ok {
			return fa == fb
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func project(recs []record, fields any) []any {
	list, _ := fields.([]any)
	out := make([]any, 0, len(recs))
	for _, r := range recs {
		if len(list) == 0 {
			out = append(out, r)
			continue
		}
		p := record{"id": r["id"]}
		for _, f := range list {
			name := str(f)
			if v, ok := r[name]; ok {
				p[name] = v
			} else {
				p[name] = false // Odoo's empty value
			}
		}
		out = append(out, p)
	}
	return out
}

func sortRecords(recs []record, order string) {
	field, desc := "id", false
	if f := strings.Fields(order); len(f) > 0 {
		field = f[0]
		desc = len(f) > 1 && strings.EqualFold(f[1], "desc")
	}
	sort.SliceStable(recs, func(i, j int) bool {
		a, b := recs[i][field], recs[j][field]
		var less bool
		if fa, ok := numeric(a); ok {
			fb, _ := numeric(b)
			less = fa < fb
		} else {
			less = fmt.Sprint(a) < fmt.Sprint(b)
		}
		if desc {
			return !less && fmt.Sprint(a) != fmt.Sprint(b)
		}
		return less
	})
}

func intList(v any) ([]int, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case float64:
		return []int{int(t)}, nil
	case []any:
		out := make([]int, 0, len(t))
		for _, x := range t {
			f, ok := numeric(x)
			if !ok {
				return nil, &odooError{422, "builtins.TypeError", fmt.Sprintf("ids must be integers, got %v", x)}
			}
			out = append(out, int(f))
		}
		return out, nil
	}
	return nil, &odooError{422, "builtins.TypeError", fmt.Sprintf("ids must be a list, got %T", v)}
}

func numeric(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

func num(v any) float64 { f, _ := numeric(v); return f }

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ─── Simulator controls and the live view ───────────────────────────────────

func (s *store) simReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", 405)
		return
	}
	s.mu.Lock()
	s.reset()
	s.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *store) simOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", 405)
		return
	}
	var req struct {
		VendorID int     `json:"vendor_id"`
		Amount   float64 `json:"amount"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.VendorID == 0 {
		req.VendorID = 7
	}
	if req.Amount <= 0 {
		req.Amount = 1500000
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.models["res.partner"][req.VendorID]; !ok {
		http.Error(w, "unknown vendor_id (7-10)", 400)
		return
	}
	product := map[int]int{7: 21, 8: 23, 9: 24, 10: 26}[req.VendorID]
	id := s.order(req.VendorID, "to approve", time.Now().Format(time.DateTime), [][3]any{{product, 1.0, req.Amount}})
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": id, "name": s.models["purchase.order"][id]["name"]})
}

var page = template.Must(template.New("p").Funcs(template.FuncMap{
	"money": func(v any) string {
		n := int64(num(v))
		s := fmt.Sprint(n)
		for i := len(s) - 3; i > 0; i -= 3 {
			s = s[:i] + "," + s[i:]
		}
		return "TSh " + s
	},
	"name": func(v any) any {
		if p, ok := v.([]any); ok && len(p) == 2 {
			return p[1]
		}
		return v
	},
}).Parse(`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="5">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Odoo simulator</title>
<style>body{font:14px system-ui,sans-serif;margin:24px;color:#222;background:#fafafa}h1{font-size:18px}
table{border-collapse:collapse;width:100%;background:#fff}td,th{border-bottom:1px solid #eee;padding:8px;text-align:left;vertical-align:top}
.s{padding:2px 8px;border-radius:10px;font-size:12px}.s-to{background:#fff3cd}.s-purchase{background:#d1e7dd}.s-cancel{background:#f8d7da}
.note{font-size:12px;color:#555;margin:4px 0;max-width:680px}footer{margin-top:24px;font-size:11px;color:#999}</style></head><body>
<h1>Odoo simulator · database <code>{{.DB}}</code></h1>
<p>Purchase orders, newest first. Refreshes every five seconds. Notes are what KNOTT posted to each order's chatter.</p>
<table><tr><th>Order</th><th>Vendor</th><th>Total</th><th>Status</th><th>Notes</th></tr>
{{range .Orders}}<tr><td>{{.name}}</td><td>{{name .partner_id}}</td><td>{{money .amount_total}}</td>
<td><span class="s s-{{if eq .state "to approve"}}to{{else}}{{.state}}{{end}}">{{.state}}</span></td>
<td>{{range .notes}}<div class="note">{{.date}} — {{.body}}</div>{{end}}</td></tr>{{end}}
</table><footer>odoo-sim, part of KNOTT · by Regnant. Not Odoo; a rehearsal stand-in for the JSON-2 API.</footer></body></html>`))

func (s *store) view(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	var orders []record
	for _, po := range s.models["purchase.order"] {
		row := record{}
		for k, v := range po {
			row[k] = v
		}
		row["notes"] = s.messages[fmt.Sprintf("purchase.order/%d", po["id"])]
		orders = append(orders, row)
	}
	s.mu.Unlock()
	sortRecords(orders, "id desc")
	page.Execute(w, map[string]any{"DB": *dbName, "Orders": orders})
}

func main() {
	flag.Parse()
	s := newStore()
	mux := http.NewServeMux()
	mux.HandleFunc("/json/2/", s.json2)
	mux.HandleFunc("/sim/reset", s.simReset)
	mux.HandleFunc("/sim/order", s.simOrder)
	mux.HandleFunc("/web/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"server_version": "19.0-sim", "server_serie": "19.0"})
	})
	mux.HandleFunc("/", s.view)
	log.Printf("odoo-sim: JSON-2 at http://%s/json/2 · database %q · API key %q", *addr, *dbName, *apiKey)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
