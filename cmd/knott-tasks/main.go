// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Command knott-tasks runs the Human Task service as a standalone service.
package main

import (
	"log"

	"github.com/regnant/knott/internal/humantask"
)

func main() {
	if err := humantask.Run(); err != nil {
		log.Fatal(err)
	}
}
