// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

type workRect struct {
	Left, Top, Right, Bottom int32
}

func initialWindowBounds() windowBounds {
	user32 := syscall.NewLazyDLL("user32.dll")
	var area workRect
	ok, _, _ := user32.NewProc("SystemParametersInfoW").Call(
		0x0030, // SPI_GETWORKAREA: excludes the taskbar on the primary display
		0,
		uintptr(unsafe.Pointer(&area)),
		0,
	)
	if ok == 0 {
		return fitWindowToWorkArea(0, 0)
	}
	dpi, _, _ := user32.NewProc("GetDpiForSystem").Call()
	if dpi == 0 {
		dpi = 96
	}
	// Wails accepts logical pixels and scales them by the window DPI before
	// centering. Convert the physical work area to the same units first.
	width := int(int64(area.Right-area.Left) * 96 / int64(dpi))
	height := int(int64(area.Bottom-area.Top) * 96 / int64(dpi))
	return fitWindowToWorkArea(width, height)
}
