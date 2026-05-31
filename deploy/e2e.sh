#!/usr/bin/env bash
# End-to-end spin-wheel flow against the running BFFs (admin :8081, consumer :8080).
# Creates a prize + game, starts a session, plays, and reads eligibility/history —
# exercising the uniform envelope and atomic stock deduction. Requires: curl, jq.
set -euo pipefail

ADMIN="${ADMIN_URL:-http://localhost:8081}"
CONSUMER="${CONSUMER_URL:-http://localhost:8080}"
TENANT="${TENANT:-tenant_demo}"
MERCHANT="${MERCHANT:-merchant_demo}"
PLAYER="${PLAYER:-player_alice}"

hdr=(-H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Roles: admin")
phdr=("${hdr[@]}" -H "X-Player-Id: ${PLAYER}")
# Unique idempotency key per run so a retried Play is safe but distinct runs
# don't collide on a cached result.
IDEM="e2e-$(date +%s)-${RANDOM}"

say() { printf '\n\033[36m== %s ==\033[0m\n' "$1"; }

say "1. Create prize (stock=3) — capture its id"
PRIZE=$(curl -sf "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/prizes" -d '{
  "name":"Voucher 100K","type":"voucher","value":100000,"stock":{"total":3}}')
echo "$PRIZE" | jq .
PRIZE_ID=$(echo "$PRIZE" | jq -r '.data.prize_id')

say "2. Create spin-wheel game referencing that prize id (always-win)"
GAME=$(curl -sf "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/games" -d "{
  \"name\":\"Lucky Spin\",\"type\":\"spin_wheel\",\"campaign_id\":\"camp_demo\",
  \"seed_generator\":\"none\",\"reward_handler\":\"probability\",\"validator\":\"basic\",\"status\":\"active\",
  \"rules\":{\"max_plays_per_user\":5},
  \"handler_config\":{\"prizes\":[{\"prize_id\":\"${PRIZE_ID}\",\"probability\":1.0,\"slot_index\":0}]}
}")
echo "$GAME" | jq .
GAME_ID=$(echo "$GAME" | jq -r '.data.game_id')

say "3. Start a session (consumer)"
START=$(curl -sf "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GAME_ID}/start" -d "{\"user_id\":\"${PLAYER}\"}")
echo "$START" | jq .
SESSION_ID=$(echo "$START" | jq -r '.data.session_id')

say "4. Play (consumer) — expect the voucher reward"
curl -s "${phdr[@]}" -H "Idempotency-Key: ${IDEM}" -X POST "${CONSUMER}/api/v1/games/${GAME_ID}/play" \
  -d "{\"session_id\":\"${SESSION_ID}\",\"payload\":{}}" | jq .

say "5. Replay same session — expect SESSION_CONSUMED error envelope"
curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GAME_ID}/play" \
  -d "{\"session_id\":\"${SESSION_ID}\",\"payload\":{}}" | jq .

say "6. Eligibility (consumer)"
curl -s "${phdr[@]}" "${CONSUMER}/api/v1/games/${GAME_ID}/eligibility" | jq .

say "7. History (consumer)"
curl -s "${phdr[@]}" "${CONSUMER}/api/v1/games/${GAME_ID}/history/me" | jq .

echo
echo "e2e flow complete."
