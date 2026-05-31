#!/usr/bin/env bash
# End-to-end for Phase 3 reward system: per-user cap, voucher-code assignment,
# instant vs on_claim redemption, and the claim/fulfill/revoke lifecycle.
# Requires: curl, jq, running BFFs.
set -euo pipefail

ADMIN="${ADMIN_URL:-http://localhost:8081}"
CONSUMER="${CONSUMER_URL:-http://localhost:8080}"
TENANT="${TENANT:-tenant_rw}"; MERCHANT="${MERCHANT:-merchant_rw}"; PLAYER="${PLAYER:-player_carol}"

hdr=(-H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Roles: admin")
phdr=("${hdr[@]}" -H "X-Player-Id: ${PLAYER}")
say() { printf '\n\033[36m== %s ==\033[0m\n' "$1"; }
play() { # game_id -> play response (fresh session each call)
  local sid; sid=$(curl -sf "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/$1/start" -d '{}' | jq -r '.data.session_id')
  curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/$1/play" -d "{\"session_id\":\"${sid}\",\"payload\":{}}"
}

say "1. Create voucher prize: on_claim + code pool, max_per_user=1, stock=5"
PRIZE=$(curl -sf "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/prizes" -d '{
  "name":"Voucher 100K","type":"voucher","value":100000,"stock":{"total":5},
  "constraints":{"max_per_user":1},
  "fulfillment":{"redemption_mode":"on_claim","method":"code"}}')
echo "$PRIZE" | jq '{prize_id: .data.prize_id, fulfillment: .data.fulfillment, constraints: .data.award_constraints}' 2>/dev/null || echo "$PRIZE" | jq .
PRIZE_ID=$(echo "$PRIZE" | jq -r '.data.prize_id')

say "2. Import 3 voucher codes"
curl -s "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/prizes/${PRIZE_ID}/codes" \
  -d '{"codes":["VC-AAA","VC-BBB","VC-CCC"]}' | jq '.data'

say "3. Always-win game referencing the prize"
GAME=$(curl -sf "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/games" -d "{
  \"name\":\"Reward Spin\",\"type\":\"spin_wheel\",\"seed_generator\":\"none\",
  \"reward_handler\":\"probability\",\"validator\":\"basic\",\"status\":\"active\",
  \"handler_config\":{\"prizes\":[{\"prize_id\":\"${PRIZE_ID}\",\"probability\":1.0,\"slot_index\":0}]}}")
GAME_ID=$(echo "$GAME" | jq -r '.data.game_id')

say "4. First play → win, reward_id + assigned code, status won"
R1=$(play "$GAME_ID")
echo "$R1" | jq '{code, reward: .data.rewards[0] | {reward_id, code}}' 2>/dev/null || echo "$R1" | jq '.data.rewards[0]'
REWARD_ID=$(echo "$R1" | jq -r '.data.rewards[0].reward_id')

say "5. Second play (same player) → per-user cap drops the reward (empty)"
play "$GAME_ID" | jq '{code, units: (.data.rewards | length)}'

say "6. List my rewards → one WON reward with a code"
curl -s "${phdr[@]}" "${CONSUMER}/api/v1/players/me/rewards" | jq '.data.items[0] | {reward_id, status, code}'

say "7. Claim the reward → status claimed"
curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/players/me/rewards/${REWARD_ID}/claim" -d '{}' | jq '{code, status: .data.status}'

say "8. Admin fulfill → status fulfilled"
curl -s "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/rewards/${REWARD_ID}/fulfill" -d '{}' | jq '{code, status: .data.status}'

say "9. Claim again → REWARD_INVALID_STATE (already past won)"
curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/players/me/rewards/${REWARD_ID}/claim" -d '{}' | jq '{code, reason: .data.error.reason}'

say "10. Admin revoke → status revoked"
curl -s "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/rewards/${REWARD_ID}/revoke" -d '{}' | jq '{code, status: .data.status}'

say "11. Prize summary → awarded=1, codes_available=2"
curl -s "${hdr[@]}" "${ADMIN}/api/v1/admin/prizes/summary" | jq '.data.items[] | {name, total, remaining, awarded, codes_available}'

echo; echo "Phase 3 reward e2e complete."
