// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package main

func initialWindowBounds() windowBounds {
	return fitWindowToWorkArea(0, 0)
}
