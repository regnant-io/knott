#!/bin/sh

# Copyright 2026 Regnant
# SPDX-License-Identifier: Apache-2.0

# Stop the service before the binary goes away. State in /var/lib/knott is left
# alone: removing a package should not destroy an operator's workflow history.
set -e
if command -v systemctl >/dev/null 2>&1; then
    systemctl stop knott >/dev/null 2>&1 || true
    systemctl disable knott >/dev/null 2>&1 || true
fi
