// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestFitWindowToWorkArea(t *testing.T) {
	for _, screen := range []struct {
		name          string
		width, height int
	}{
		{"large", 1920, 1040},
		{"laptop", 1366, 720},
		{"small", 1024, 560},
	} {
		t.Run(screen.name, func(t *testing.T) {
			window := fitWindowToWorkArea(screen.width, screen.height)
			if window.Width > screen.width-96 || window.Height > screen.height-96 {
				t.Fatalf("window %dx%d exceeds %dx%d work area", window.Width, window.Height, screen.width, screen.height)
			}
			if window.MinWidth > window.Width || window.MinHeight > window.Height {
				t.Fatalf("minimum size exceeds initial size: %+v", window)
			}
		})
	}
}
