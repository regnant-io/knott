// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/regnant/knott/internal/registry/store"
)

type Handler struct {
	DB *store.DB
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workflows, err := h.DB.GetAll()
	if err != nil {
		writeError(w, 500, "INTERNAL_ERROR", err.Error())
		return
	}
	if workflows == nil {
		workflows = []*store.Workflow{}
	}
	writeJSON(w, 200, map[string]any{"data": workflows, "total": len(workflows)})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, err := h.DB.GetByID(id)
	if err != nil {
		writeError(w, 404, "WORKFLOW_NOT_FOUND", "Workflow not found: "+id)
		return
	}
	writeJSON(w, 200, wf)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Status      string          `json:"status"`
		Definition  json.RawMessage `json:"definition"`
		Tags        []string        `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID_REQUEST", "Invalid JSON: "+err.Error())
		return
	}
	if body.Name == "" {
		writeError(w, 400, "VALIDATION_ERROR", "name is required")
		return
	}
	if len(body.Definition) == 0 {
		body.Definition = json.RawMessage(`{"trigger":{"type":"api"},"steps":[]}`)
	}
	if body.Status == "" {
		body.Status = "draft"
	}
	wf, err := h.DB.Create(body.Name, body.Description, body.Status, body.Definition, body.Tags)
	if err != nil {
		writeError(w, 500, "CREATE_FAILED", err.Error())
		return
	}
	writeJSON(w, 201, wf)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Status      string          `json:"status"`
		Definition  json.RawMessage `json:"definition"`
		Tags        []string        `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	wf, err := h.DB.Update(id, body.Name, body.Description, body.Status, body.Definition, body.Tags)
	if err != nil {
		writeError(w, 404, "WORKFLOW_NOT_FOUND", err.Error())
		return
	}
	writeJSON(w, 200, wf)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.DB.Delete(id); err != nil {
		writeError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	versions, err := h.DB.GetVersions(id)
	if err != nil {
		writeError(w, 500, "INTERNAL_ERROR", err.Error())
		return
	}
	if versions == nil {
		versions = []*store.WorkflowVersion{}
	}
	writeJSON(w, 200, map[string]any{"data": versions, "total": len(versions)})
}

func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Definition json.RawMessage `json:"definition"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}

	var def map[string]any
	if err := json.Unmarshal(body.Definition, &def); err != nil {
		writeJSON(w, 200, map[string]any{"valid": false, "errors": []string{"Invalid JSON: " + err.Error()}})
		return
	}

	writeJSON(w, 200, Validate(def))
}
