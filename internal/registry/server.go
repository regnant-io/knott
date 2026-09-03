// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/regnant/knott/internal/registry/handlers"
	"github.com/regnant/knott/internal/registry/store"
)

// Run starts the Workflow Registry service and blocks until it stops.
// It is called both by the standalone cmd/knott-registry binary and by the
// all-in-one knott binary, which runs every service in a single process.
func Run() error {
	port := getEnv2("REGISTRY_PORT", "PORT", "8001")
	dbPath := getEnv2("REGISTRY_DB", "DB_PATH", filepath.Join("..", "..", "data", "workflows.db"))

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("registry: create data dir: %w", err)
	}

	db, err := store.NewDB(dbPath)
	if err != nil {
		return fmt.Errorf("registry: open database: %w", err)
	}
	defer db.Close()

	db.Seed()

	h := &handlers.Handler{DB: db}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", healthHandler("workflow-registry", port))

		r.Get("/workflows", h.List)
		r.Post("/workflows", h.Create)
		r.Get("/workflows/{id}", h.Get)
		r.Put("/workflows/{id}", h.Update)
		r.Delete("/workflows/{id}", h.Delete)
		r.Get("/workflows/{id}/versions", h.ListVersions)
		r.Post("/workflows/{id}/validate", h.Validate)
	})

	log.Printf("╔══════════════════════════════════════╗")
	log.Printf("║   KNOTT — Workflow Registry          ║")
	log.Printf("║   Port: %-5s                        ║", port)
	log.Printf("║   DB:   %-28s ║", dbPath)
	log.Printf("╚══════════════════════════════════════╝")

	bindHost := getEnv("REGISTRY_BIND_HOST", getEnv("BIND_HOST", "127.0.0.1"))
	return http.ListenAndServe(bindHost+":"+port, r)
}

func healthHandler(service, port string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": service,
			"port":    port,
			"version": "1.0.0",
		})
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnv2 prefers a service-specific env var (e.g. REGISTRY_PORT) over the
// generic one (PORT), so a shared launcher that sets PORT for one service does
// not accidentally bleed into another.
func getEnv2(primary, secondary, fallback string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	if v := os.Getenv(secondary); v != "" {
		return v
	}
	return fallback
}
