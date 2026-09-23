// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"os/exec"
	"syscall"
)

// hideWindow stops a child process flashing a console window when the parent
// is the windowless desktop build.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
