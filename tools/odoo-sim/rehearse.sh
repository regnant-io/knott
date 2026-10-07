#!/usr/bin/env bash
# Copyright 2026 Regnant
# SPDX-License-Identifier: Apache-2.0
#
# Wire a running KNOTT to an Odoo (real or odoo-sim) for the purchase-order
# approval workflow: save the connector credentials, test the connection,
# seed the example workflows and activate "Purchase Order Approval (Odoo)".
#
#   go run ./tools/odoo-sim &                  # or a real Odoo 19 test database
#   knott serve &
#   tools/odoo-sim/rehearse.sh
#
# Environment (defaults match odoo-sim):
#   KNOTT     http://127.0.0.1:8002          KNOTT_API_KEY  (when API_KEYS is set)
#   ODOO_URL  http://127.0.0.1:8069          ODOO_DATABASE  demo
#   ODOO_API_KEY demo-key
set -euo pipefail

KNOTT="${KNOTT:-http://127.0.0.1:8002}"
API="$KNOTT/api/v1"
ODOO_URL="${ODOO_URL:-http://127.0.0.1:8069}"
ODOO_DATABASE="${ODOO_DATABASE:-demo}"
ODOO_API_KEY="${ODOO_API_KEY:-demo-key}"
AUTH=()
[ -n "${KNOTT_API_KEY:-}" ] && AUTH=(-H "X-API-Key: $KNOTT_API_KEY")

post() { curl -fsS "${AUTH[@]}" -X "$1" "$API$2" -H 'Content-Type: application/json' -d "$3"; }

echo "→ Saving Odoo credentials"
for kv in "ODOO_URL=$ODOO_URL" "ODOO_DATABASE=$ODOO_DATABASE" "ODOO_API_KEY=$ODOO_API_KEY"; do
  post POST /credentials "{\"name\":\"${kv%%=*}\",\"value\":\"${kv#*=}\"}" >/dev/null
done

echo "→ Testing the connection"
post POST /connectors/test '{"connector":"odoo"}' | grep -q '"ok":true' \
  || { echo "  Odoo did not answer. Check the address, database and API key." >&2; exit 1; }
echo "  Odoo answered."

echo "→ Seeding example workflows"
post POST /examples/seed '{}' >/dev/null

WF=$(curl -fsS "${AUTH[@]}" "$API/workflows" | python -c \
  "import sys,json; print(next(w['id'] for w in json.load(sys.stdin)['data'] if w['name']=='Purchase Order Approval (Odoo)'))")
echo "→ Activating Purchase Order Approval (Odoo): $WF"
post PUT "/workflows/$WF" '{"status":"active"}' >/dev/null

echo
echo "Done. Within about a minute KNOTT polls Odoo's To Approve queue and starts one run per order."
echo "  Runs:        $KNOTT/runs"
echo "  Task Inbox:  $KNOTT/tasks"
echo "  Odoo view:   $ODOO_URL (odoo-sim only)"
