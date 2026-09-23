// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package main

// Wails' file dialogs use UTType, which lives in its own framework on macOS 11
// and later. The Wails CLI adds this flag itself; declaring it here makes a
// plain `go build` link too.

// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
