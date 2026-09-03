#!/usr/bin/env bash

# Copyright 2026 Regnant
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# ─── KNOTT — Platform Launcher ───────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Load .env if present
if [ -f .env ]; then
  set -a && source .env && set +a
  echo "✓ Loaded .env"
else
  echo "⚠  No .env file found — copying from .env.example"
  cp .env.example .env
  echo "  → Edit .env and set ANTHROPIC_API_KEY, then re-run ./start.sh"
fi

# Directories
mkdir -p data logs bin

# ─── Check / build binaries ───────────────────────────────────────────────────
build_if_missing() {
  local name=$1 dir=$2
  if [ ! -f "bin/$name" ]; then
    echo "→ Building $name..."
    (cd "$dir" && go build -o "../../bin/$name" . )
    echo "  ✓ $name compiled"
  fi
}

if command -v go &>/dev/null; then
  export GONOSUMDB='*' GOINSECURE='*' GOFLAGS='-mod=mod' GOPROXY=direct
  build_if_missing workflow-registry  services/workflow-registry
  build_if_missing execution-engine   services/execution-engine
  build_if_missing human-task-service services/human-task-service
  build_if_missing agent-integration  services/agent-integration
else
  echo "⚠  Go not found — assuming pre-built binaries exist in ./bin/"
fi

# ─── Check / build frontend ───────────────────────────────────────────────────
if [ ! -d "apps/designer/dist" ]; then
  echo "→ Building frontend..."
  (cd apps/designer && npm install --legacy-peer-deps --silent && npm run build)
  echo "  ✓ Frontend built"
fi

# ─── Check Python (AI engine uses stdlib only — no pip install needed) ─────────
if ! command -v python3 &>/dev/null; then
  echo "⚠  Python3 not found — AI Decision Engine will not start"
fi

# ─── Kill any existing processes ──────────────────────────────────────────────
echo ""
echo "→ Stopping any running services..."
pkill -f "workflow-registry"  2>/dev/null || true
pkill -f "execution-engine"   2>/dev/null || true
pkill -f "human-task-service" 2>/dev/null || true
pkill -f "agent-integration"  2>/dev/null || true
pkill -f "ai-decision-engine/main.py" 2>/dev/null || true
sleep 1

# ─── Port preflight ───────────────────────────────────────────────────────────
# Detect ports already held (commonly a leftover Docker stack) and warn before
# starting, so a silent collision doesn't send services into a crash loop.
check_port_free() {
  local port=$1
  if command -v lsof &>/dev/null; then
    lsof -iTCP:"$port" -sTCP:LISTEN -t >/dev/null 2>&1 && return 1 || return 0
  elif command -v ss &>/dev/null; then
    ss -ltn 2>/dev/null | grep -q ":$port " && return 1 || return 0
  fi
  return 0
}

PORT_CONFLICT=0
for p in "${REGISTRY_PORT:-8001}" "${ENGINE_PORT:-8002}" "${AI_PORT:-8003}" "${TASK_PORT:-8004}" "${AGENT_PORT:-8005}"; do
  if ! check_port_free "$p"; then
    echo "  ⚠ Port $p is already in use."
    PORT_CONFLICT=1
  fi
done
if [ "$PORT_CONFLICT" = "1" ]; then
  echo ""
  echo "  ⚠ One or more KNOTT ports are occupied. Common cause: a leftover Docker"
  echo "    stack. Check with 'docker ps' and stop it with 'docker compose down',"
  echo "    or change the *_PORT values in .env."
  read -r -p "  Continue anyway? [y/N] " ans
  case "$ans" in
    [yY]*) : ;;
    *) echo "  Aborted. Free the ports and re-run ./start.sh"; exit 1 ;;
  esac
fi

# ─── Start services ───────────────────────────────────────────────────────────
echo ""
echo "⊗  Starting KNOTT platform..."
echo "────────────────────────────────────────────────────────"

# Workflow Registry
PORT="${REGISTRY_PORT:-8001}" \
BIND_HOST="${BIND_HOST:-}" \
DB_PATH="${REGISTRY_DB:-./data/workflows.db}" \
  ./bin/workflow-registry >> logs/workflow-registry.log 2>&1 &
echo "  ● Workflow Registry     → http://localhost:${REGISTRY_PORT:-8001}"

# Execution Engine
PORT="${ENGINE_PORT:-8002}" \
ENGINE_BIND_HOST="${ENGINE_BIND_HOST:-}" \
DB_PATH="${ENGINE_DB:-./data/runs.db}" \
REGISTRY_URL="${REGISTRY_URL:-http://localhost:8001}" \
AI_DECISION_URL="${AI_DECISION_URL:-http://localhost:8003}" \
HUMAN_TASK_URL="${HUMAN_TASK_URL:-http://localhost:8004}" \
AGENT_URL="${AGENT_URL:-http://localhost:8005}" \
API_TOKEN="${API_TOKEN:-}" \
WEBHOOK_SECRET="${WEBHOOK_SECRET:-}" \
KNOTT_SECRET_KEY="${KNOTT_SECRET_KEY:-}" \
FRONTEND_PATH="${FRONTEND_PATH:-./apps/designer/dist}" \
  ./bin/execution-engine >> logs/execution-engine.log 2>&1 &
echo "  ● Execution Engine      → http://localhost:${ENGINE_PORT:-8002}"

# AI Decision Engine (Python — standard library only, no dependencies)
if command -v python3 &>/dev/null; then
  PORT="${AI_PORT:-8003}" \
  BIND_HOST="${BIND_HOST:-}" \
  AI_PROVIDER="${AI_PROVIDER:-auto}" \
  ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-}" \
  OLLAMA_BASE_URL="${OLLAMA_BASE_URL:-http://localhost:11434}" \
  OLLAMA_MODEL="${OLLAMA_MODEL:-llama3.1:latest}" \
  AI_CONFIG_PATH="${AI_CONFIG_PATH:-./data/ai-config.json}" \
    python3 services/ai-decision-engine/main.py >> logs/ai-decision-engine.log 2>&1 &
  echo "  ● AI Decision Engine    → http://localhost:${AI_PORT:-8003}"
else
  echo "  ⚠ AI Decision Engine    → SKIPPED (Python not available)"
fi

# Human Task Service
PORT="${TASK_PORT:-8004}" \
BIND_HOST="${BIND_HOST:-}" \
DB_PATH="${TASK_DB:-./data/tasks.db}" \
  ./bin/human-task-service >> logs/human-task-service.log 2>&1 &
echo "  ● Human Task Service    → http://localhost:${TASK_PORT:-8004}"

# Agent Integration
PORT="${AGENT_PORT:-8005}" \
BIND_HOST="${BIND_HOST:-}" \
DB_PATH="${AGENT_DB:-./data/agents.db}" \
  ./bin/agent-integration >> logs/agent-integration.log 2>&1 &
echo "  ● Agent Integration     → http://localhost:${AGENT_PORT:-8005}"

echo "────────────────────────────────────────────────────────"
echo ""

# ─── Wait for services to be ready ────────────────────────────────────────────
echo "→ Waiting for services to be ready..."
sleep 2

check_health() {
  local url=$1 name=$2
  for i in 1 2 3 4 5; do
    if curl -sf "$url" >/dev/null 2>&1; then
      echo "  ✓ $name"
      return 0
    fi
    sleep 1
  done
  echo "  ✗ $name (may still be starting — check logs/)"
}

check_health "http://localhost:${REGISTRY_PORT:-8001}/api/v1/health" "Workflow Registry"
check_health "http://localhost:${ENGINE_PORT:-8002}/api/v1/health"   "Execution Engine"
check_health "http://localhost:${AI_PORT:-8003}/internal/v1/health"  "AI Decision Engine"
check_health "http://localhost:${TASK_PORT:-8004}/api/v1/health"     "Human Task Service"
check_health "http://localhost:${AGENT_PORT:-8005}/api/v1/health"    "Agent Integration"

echo ""
echo "────────────────────────────────────────────────────────"
echo ""
echo "  ⊗  KNOTT is ready"
echo ""
echo "  Open in browser → http://localhost:${ENGINE_PORT:-8002}"
echo ""
echo "  Logs:    ./logs/"
echo "  Data:    ./data/"
echo "  Stop:    ./stop.sh"
echo ""
echo "────────────────────────────────────────────────────────"

# Wait — keep script alive so Ctrl+C stops everything
wait
