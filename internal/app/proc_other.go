// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package app

import "os/exec"

func hideWindow(*exec.Cmd) {}
