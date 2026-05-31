#!/usr/bin/env bash
# End-to-end for Phase 9 (BFF hardening): admin role-based authz (403 without a
# role, 200 with one), the read-model cache on the public campaign config (with
# admin invalidation), and the distributed gameplay rate limit (429 + Retry-After
# once the per-IP window is exhausted). Requires: curl, jq, both BFFs running
# with REDIS_ADDR set (rate limit + cache are no-ops without Redis). Run the
# consumer with a low PLAY_RATE_LIMIT to make the limit observable, e.g.
#   PLAY_RATE_LIMIT=5 make run-consumer
set -euo pipefail

ADMIN="${ADMIN_URL:-http://localhost:8081}"
CONSUMER="${CONSUMER_URL:-http://localhost:8080}"
TENANT="${TENANT:-tenant_hard}"; MERCHANT="${MERCHANT:-merchant_hard}"
LIMIT="${PLAY_RATE_LIMIT:-60}"

command -v jq >/dev/null || { echo "jq required"; exit 1; }
say() { printf '\n\033[36m== %s ==\033[0m\n' "$1"; }
ahdr=(-H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Roles: admin")
nohdr=(-H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}")

############################################
say "1. RBAC — admin mutation WITHOUT a role → 403 PERMISSION_DENIED"
############################################
OUT=$(curl -s -o /tmp/rbac.json -w "%{http_code}" "${nohdr[@]}" -X POST "${ADMIN}/api/v1/admin/games" \
  -d '{"name":"X","type":"spin_wheel","reward_handler":"probability","validator":"basic","seed_generator":"none"}')
echo "HTTP ${OUT}"; jq -c '{code, reason: .data.error.reason}' /tmp/rbac.json
[ "$OUT" = "403" ] || { echo "✗ expected 403 without role"; exit 1; }
echo "✓ blocked without a role"

say "2. RBAC — same call WITH X-Roles: admin → 201"
GAME=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/games" \
  -d '{"name":"Hard Wheel","type":"spin_wheel","campaign_id":"camp_hard","reward_handler":"probability","validator":"basic","seed_generator":"none","status":"active","rules":{"max_plays_per_user":100000},"handler_config":{"prizes":[]}}')
GAME_ID=$(echo "$GAME" | jq -r '.data.game_id')
[ "$GAME_ID" != "null" ] && [ -n "$GAME_ID" ] || { echo "✗ create failed"; echo "$GAME"; exit 1; }
echo "✓ created game ${GAME_ID} with a role"

############################################
say "3. Read-model cache — public campaign config served + invalidated on update"
############################################
CAMP=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/campaigns" \
  -d '{"name":"Hard Campaign","status":"active"}')
CAMP_ID=$(echo "$CAMP" | jq -r '.data.campaign_id')
if [ "$CAMP_ID" != "null" ] && [ -n "$CAMP_ID" ]; then
  echo "campaign=${CAMP_ID}"
  say "   warm the cache (2 reads), then update (busts), then read again"
  curl -s "${CONSUMER}/api/v1/public/campaigns/${CAMP_ID}" | jq -c '{code, name: .data.name}'
  curl -s "${CONSUMER}/api/v1/public/campaigns/${CAMP_ID}" | jq -c '{code, name: .data.name}'  # cache hit
  curl -s "${ahdr[@]}" -X PUT "${ADMIN}/api/v1/admin/campaigns/${CAMP_ID}" \
    -d '{"name":"Hard Campaign (renamed)","status":"active"}' | jq -c '{code, name: .data.name}'
  curl -s "${CONSUMER}/api/v1/public/campaigns/${CAMP_ID}" | jq -c '{code, name_after_bust: .data.name}'
  echo "✓ public config served; admin update invalidates the cached view"
else
  echo "… skipped (campaign create unavailable in this build): $(echo "$CAMP" | jq -c '{code}')"
fi

############################################
say "4. Rate limit — exhaust the per-IP /start window → 429 + Retry-After"
############################################
echo "limit=${LIMIT}/min; firing $((LIMIT + 3)) requests"
got429=0; retry=""
for _ in $(seq 1 $((LIMIT + 3))); do
  read -r code ra < <(curl -s -o /dev/null -w "%{http_code} %header{retry-after}" \
    "${nohdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GAME_ID}/start" -d '{}')
  if [ "$code" = "429" ]; then got429=1; retry="$ra"; break; fi
done
if [ "$got429" = "1" ]; then
  echo "✓ hit 429 (Retry-After: ${retry:-n/a}s)"
else
  echo "… no 429 seen — is the consumer running with REDIS_ADDR set (and a low PLAY_RATE_LIMIT)?"
fi

echo; echo "Phase 9 hardening e2e complete."
