// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package main

type windowBounds struct {
	Width, Height       int
	MinWidth, MinHeight int
}

// Wails centers the initial outer window in the monitor's work area. A window
// larger than that area is centered partly off-screen, hiding its title bar.
func fitWindowToWorkArea(workWidth, workHeight int) windowBounds {
	if workWidth <= 0 || workHeight <= 0 {
		return windowBounds{Width: 1200, Height: 680, MinWidth: 900, MinHeight: 560}
	}
	// Leave room for the non-client frame and Wails' Windows size adjustment.
	const margin = 96
	width := max(1, min(1440, workWidth-margin))
	height := max(1, min(900, workHeight-margin))
	return windowBounds{
		Width:     width,
		Height:    height,
		MinWidth:  min(960, width),
		MinHeight: min(600, height),
	}
}
