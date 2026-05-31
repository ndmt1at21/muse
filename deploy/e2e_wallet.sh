#!/usr/bin/env bash
# End-to-end for Phase 8: wallet, points & exchange. A lucky_item game credits a
# wallet currency into the player ledger inside each Play; a cumulative_unlock
# milestone converts the accumulated balance into a real prize via redeem
# (grant-once). Exercises the consumer wallet surface (balances/ledger/
# milestones/redeem) end-to-end over the REST stack with a real player JWT.
# Requires: curl, jq, running Core (AUTH_DEV_MODE=true, a JWT_SECRET) + both BFFs.
set -euo pipefail

ADMIN="${ADMIN_URL:-http://localhost:8081}"
CONSUMER="${CONSUMER_URL:-http://localhost:8080}"
TENANT="${TENANT:-tenant_wallet}"
CAMPAIGN="${CAMPAIGN:-camp_wallet}"
PHONE="+8497${RANDOM:0:3}${$:0:4}"

command -v jq >/dev/null || { echo "jq required"; exit 1; }
say() { printf '\n\033[36m== %s ==\033[0m\n' "$1"; }
ahdr=(-H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Roles: admin")

say "1. Player login (OTP dev code) → JWT"
START=$(curl -sf "${ahdr[@]}" -X POST "${CONSUMER}/api/v1/players/auth/start" \
  -d "{\"identifier\":{\"type\":\"phone\",\"value\":\"${PHONE}\"},\"method\":\"otp\"}")
CHL=$(echo "$START" | jq -r '.data.challenge_id')
CODE=$(echo "$START" | jq -r '.data.dev_code')
[ "$CODE" != "null" ] && [ -n "$CODE" ] || { echo "no dev_code (run Core with AUTH_DEV_MODE=true)"; echo "$START"; exit 1; }
VERIFY=$(curl -sf "${ahdr[@]}" -X POST "${CONSUMER}/api/v1/players/auth/verify" \
  -d "{\"challenge_id\":\"${CHL}\",\"code\":\"${CODE}\"}")
TOKEN=$(echo "$VERIFY" | jq -r '.data.token')
PLAYER=$(echo "$VERIFY" | jq -r '.data.player.player_id')
echo "player_id=${PLAYER}"
phdr=(-H "Content-Type: application/json" -H "Authorization: Bearer ${TOKEN}")

say "2. Create the milestone prize (admin)"
PRIZE=$(curl -sf "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/prizes" \
  -d '{"name":"Lucky Grand Prize","type":"voucher","value":500000,"stock":{"total":100}}' | jq -r '.data.prize_id')
echo "prize=${PRIZE}"

say "3. Create a lucky_item game (currency=lucky_star, cumulative_unlock @2)"
GAME=$(curl -sf "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/games" -d "{
  \"name\":\"Star Collector\",\"type\":\"collection\",\"campaign_id\":\"${CAMPAIGN}\",
  \"seed_generator\":\"none\",\"reward_handler\":\"lucky_item\",\"validator\":\"basic\",\"status\":\"active\",
  \"wallet_scope\":\"campaign\",
  \"rules\":{\"max_plays_per_user\":10},
  \"handler_config\":{\"items\":[{\"item\":\"lucky_star\",\"weight\":1,\"quantity\":1,\"slot_index\":0}]},
  \"milestones\":{\"currency\":\"lucky_star\",\"mode\":\"cumulative_unlock\",\"auto_grant\":false,
    \"milestones\":[{\"milestone_id\":\"m_grand\",\"threshold\":2,\"prize_id\":\"${PRIZE}\"}]}
}")
GAME_ID=$(echo "$GAME" | jq -r '.data.game_id'); echo "game=${GAME_ID}"

play() { # one start→play round; awards 1 lucky_star
  local s
  s=$(curl -sf "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GAME_ID}/start" -d '{}' | jq -r '.data.session_id')
  curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GAME_ID}/play" \
    -d "{\"session_id\":\"${s}\",\"payload\":{}}" | jq -c '{item: .data.metadata.item}'
}

say "4. Play twice → accrue 2 lucky_star"
play; play

say "5. GET /wallet → balance (scope_key = campaign)"
curl -s "${phdr[@]}" "${CONSUMER}/api/v1/wallet?scope_key=${CAMPAIGN}" | jq '.data.balances'

say "6. GET /games/{id}/milestones → progress + status (expect unlocked @2)"
curl -s "${phdr[@]}" "${CONSUMER}/api/v1/games/${GAME_ID}/milestones" \
  | jq '.data | {currency, mode, balance, milestones: [.milestones[] | {milestone_id, threshold, status, progress, remaining}]}'

say "7. Redeem the milestone → grant the prize (grant-once)"
curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GAME_ID}/redeem" \
  -d '{"milestone_id":"m_grand"}' | jq '.data | {redeemed, mode, reward: .reward.name}'

say "8. Redeem again → ALREADY_EXISTS (grant-once enforced)"
curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GAME_ID}/redeem" \
  -d '{"milestone_id":"m_grand"}' | jq '{code, reason: .data.error.reason}'

say "9. GET /wallet/ledger → the two play credits"
curl -s "${phdr[@]}" "${CONSUMER}/api/v1/wallet/ledger?scope_key=${CAMPAIGN}" \
  | jq '.data.items | [.[] | {currency, amount, reason}]'

echo; echo "Phase 8 wallet e2e complete."
