#!/usr/bin/env bash

# Copyright 2026 Regnant
# SPDX-License-Identifier: Apache-2.0

# KNOTT — Stop all services
echo "⊗  Stopping KNOTT..."
pkill -f "bin/workflow-registry"       2>/dev/null && echo "  ● Workflow Registry   stopped" || true
pkill -f "bin/execution-engine"        2>/dev/null && echo "  ● Execution Engine    stopped" || true
pkill -f "bin/human-task-service"      2>/dev/null && echo "  ● Human Task Service  stopped" || true
pkill -f "bin/agent-integration"       2>/dev/null && echo "  ● Agent Integration   stopped" || true
pkill -f "ai-decision-engine/main.py"  2>/dev/null && echo "  ● AI Decision Engine  stopped" || true
echo "Done."
