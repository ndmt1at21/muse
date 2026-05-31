#!/usr/bin/env bash
# End-to-end for Phase 10 (integration hub): register outbound integrations,
# list them, and inject domain events that fan out to the integrations
# subscribed to them. Uses a stub adapter (sms) for a deterministic dispatch
# count (no external receiver needed) and a webhook integration to show config
# round-trips. Requires: curl, jq, both BFFs running (admin under RBAC, so the
# X-Roles header is sent). Real gameplay emits play_completed/prize_won through
# the same hub — see the README.
set -euo pipefail

ADMIN="${ADMIN_URL:-http://localhost:8081}"
TENANT="${TENANT:-tenant_intg}"; MERCHANT="${MERCHANT:-merchant_intg}"
ahdr=(-H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Roles: admin")

command -v jq >/dev/null || { echo "jq required"; exit 1; }
say() { printf '\n\033[36m== %s ==\033[0m\n' "$1"; }

say "1. Register a stub (sms) integration subscribed to prize_won + play_completed"
SMS=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/integrations" \
  -d '{"type":"sms","events":["prize_won","play_completed"],"config":{"sender":"MUSE"}}')
echo "$SMS" | jq '.data | {id, type, events, status}'
SMS_ID=$(echo "$SMS" | jq -r '.data.id')
[ "$SMS_ID" != "null" ] && [ -n "$SMS_ID" ] || { echo "✗ create failed"; echo "$SMS"; exit 1; }

say "2. Register a webhook integration (prize_claimed) — config round-trips"
curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/integrations" \
  -d '{"type":"webhook","events":["prize_claimed"],"config":{"url":"https://example.test/hook","hmac_secret":"shh"}}' \
  | jq '.data | {id, type, events, config}'

say "3. List integrations in scope"
curl -s "${ahdr[@]}" "${ADMIN}/api/v1/admin/integrations" | jq '.data.items | map({id, type, events})'

say "4. RBAC — create WITHOUT a role → 403"
code=$(curl -s -o /dev/null -w "%{http_code}" -H "Content-Type: application/json" \
  -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" \
  -X POST "${ADMIN}/api/v1/admin/integrations" -d '{"type":"sms","events":["prize_won"]}')
echo "HTTP ${code}"; [ "$code" = "403" ] && echo "✓ blocked without a role" || { echo "✗ expected 403"; exit 1; }

say "5. Emit prize_won → fans out to the subscribed sms integration (dispatched=1)"
D=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/integrations/emit" \
  -d '{"type":"prize_won","payload":{"prize_id":"p1","player_id":"pl1"}}' | jq -r '.data.dispatched')
echo "dispatched=${D}"; [ "$D" = "1" ] && echo "✓ delivered to 1 integration" || echo "… expected 1 (got ${D})"

say "6. Emit an unsubscribed event (quest_completed) → dispatched=0"
D0=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/integrations/emit" \
  -d '{"type":"quest_completed","payload":{"quest_id":"q1"}}' | jq -r '.data.dispatched')
echo "dispatched=${D0}"; [ "$D0" = "0" ] && echo "✓ no subscribers, nothing delivered" || echo "… expected 0 (got ${D0})"

say "7. Delete the sms integration → subsequent emit dispatches to 0"
curl -s "${ahdr[@]}" -X DELETE "${ADMIN}/api/v1/admin/integrations/${SMS_ID}" | jq -c '.data'
D2=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/integrations/emit" \
  -d '{"type":"prize_won","payload":{"prize_id":"p1"}}' | jq -r '.data.dispatched')
echo "dispatched after delete=${D2}"; [ "$D2" = "0" ] && echo "✓ deleted integration no longer fires" || echo "… expected 0 (got ${D2})"

echo; echo "Phase 10 integration e2e complete."
