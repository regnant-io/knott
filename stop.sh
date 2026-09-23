#!/usr/bin/env bash

# Copyright 2026 Regnant
# SPDX-License-Identifier: Apache-2.0

# Stop a KNOTT started with start.sh. In-flight runs are checkpointed and
# resume on the next start.
pkill -INT -x knott 2>/dev/null && echo "KNOTT stopping" || echo "KNOTT is not running"
