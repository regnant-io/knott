// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Package httpx holds the HTTP serving defaults every KNOTT service shares.
package httpx

import (
	"encoding/json"
	"net/http"
	"time"
)

// Listen serves h on addr with header and idle timeouts. http.ListenAndServe
// has none, so a client that opens connections and trickles headers holds them
// forever. There is deliberately no write timeout: long AI calls and run
// polling are legitimate slow responses.
func Listen(addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	return srv.ListenAndServe()
}

// InternalOnly refuses requests that a browser made directly.
//
// The registry, task and agent services listen on loopback ports that only the
// engine calls. A web page can still reach a loopback port — and scan for it —
// so any request carrying browser provenance is rejected here. The engine
// strips those headers after applying its own origin checks, so proxied
// console requests pass.
func InternalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
				"code": "INTERNAL_SERVICE", "message": "This service is internal; use the KNOTT API port",
			}})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// StripBrowserHeaders removes request provenance headers before a request is
// forwarded to an internal service.
func StripBrowserHeaders(r *http.Request) {
	for _, h := range []string{"Origin", "Referer", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "Cookie"} {
		r.Header.Del(h)
	}
}
