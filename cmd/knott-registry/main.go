// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Command knott-registry runs the Workflow Registry as a standalone service.
// Use it for horizontally scaled or containerised deployments; for a single
// node, the all-in-one `knott` binary runs this service in-process.
package main

import (
	"log"

	"github.com/regnant/knott/internal/registry"
)

func main() {
	if err := registry.Run(); err != nil {
		log.Fatal(err)
	}
}
