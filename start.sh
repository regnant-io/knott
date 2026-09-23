#!/usr/bin/env bash

# Copyright 2026 Regnant
# SPDX-License-Identifier: Apache-2.0

# Build and run KNOTT from a checkout: the console, then the single `knott`
# binary that hosts every service. Extra arguments go to `knott serve`
# (for example --port 9000 or --host 0.0.0.0).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

if [ -f .env ]; then
  set -a; . ./.env; set +a
fi

if [ ! -f apps/designer/dist/index.html ]; then
  echo "→ Building the console…"
  npm --prefix apps/designer ci --no-audit --no-fund
  npm --prefix apps/designer run build
fi
make embed >/dev/null

echo "→ Building knott…"
go build -o bin/knott ./cmd/knott
exec bin/knott serve --open "$@"
