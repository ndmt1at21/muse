#!/usr/bin/env bash
# Smoke-test Core's REST gateway DIRECTLY — no BFF in front. This proves Core is
# a self-sufficient product surface: the same game.v1 contract, served as
# JSON/HTTP under /api/v1, wrapped in the uniform {code, message, trace_id, data}
# envelope. Core is auth-agnostic, so the tenant/merchant Scope travels as
# ordinary request fields (here in the JSON body / query string) instead of the
# X-Tenant-Id headers a BFF would translate. A real deployment fronts Core with
# a BFF (see examples/) that authenticates callers and fills the Scope.
# Requires: curl, jq, Core running (make run-core). Targets CORE_REST_URL.
set -euo pipefail

CORE="${CORE_REST_URL:-http://localhost:8090}"
TENANT="${TENANT:-tenant_demo}"
MERCHANT="${MERCHANT:-merchant_demo}"
PLAYER="${PLAYER:-player_smoke}"
SCOPE="{\"tenant_id\":\"${TENANT}\",\"merchant_id\":\"${MERCHANT}\"}"

command -v jq >/dev/null || { echo "jq required"; exit 1; }
say() { printf '\n\033[36m== %s ==\033[0m\n' "$1"; }

say "1. Create prize (raw proto JSON; scope in body)"
PRIZE=$(curl -sf -X POST "${CORE}/api/v1/admin/prizes" -H 'Content-Type: application/json' \
  -d "{\"scope\":${SCOPE},\"prize\":{\"name\":\"Voucher 100K\",\"type\":\"voucher\",\"value\":100000,\"total\":3}}")
echo "$PRIZE" | jq '{code, prize_id: .data.prize.id, remaining: .data.prize.remaining}'
PRIZE_ID=$(echo "$PRIZE" | jq -r '.data.prize.id')

say "2. Create an always-win spin-wheel game referencing it"
GAME=$(curl -sf -X POST "${CORE}/api/v1/admin/games" -H 'Content-Type: application/json' -d "{
  \"scope\":${SCOPE},
  \"game\":{
    \"name\":\"Lucky Spin (REST smoke)\",\"type\":\"spin_wheel\",\"campaign_id\":\"camp_smoke\",
    \"seed_generator\":\"none\",\"reward_handler\":\"probability\",\"validator\":\"basic\",\"status\":\"active\",
    \"rules\":{\"max_plays_per_user\":5},
    \"handler_config\":\"{\\\"prizes\\\":[{\\\"prize_id\\\":\\\"${PRIZE_ID}\\\",\\\"probability\\\":1.0,\\\"slot_index\\\":0}]}\"
  }
}")
echo "$GAME" | jq '{code, game_id: .data.game.id, status: .data.game.status}'
GAME_ID=$(echo "$GAME" | jq -r '.data.game.id')

say "3. Start a session"
START=$(curl -sf -X POST "${CORE}/api/v1/games/${GAME_ID}/start" -H 'Content-Type: application/json' \
  -d "{\"scope\":${SCOPE},\"player_id\":\"${PLAYER}\"}")
echo "$START" | jq '{code, session_id: .data.session_id}'
SESSION_ID=$(echo "$START" | jq -r '.data.session_id')

say "4. Play — expect the voucher reward"
curl -sf -X POST "${CORE}/api/v1/games/${GAME_ID}/play" -H 'Content-Type: application/json' \
  -d "{\"scope\":${SCOPE},\"session_id\":\"${SESSION_ID}\",\"player_id\":\"${PLAYER}\",\"payload\":{}}" \
  | jq '{code, reward: .data.rewards[0].name}'

say "5. Replay same session — expect a SESSION_CONSUMED error envelope"
curl -s -X POST "${CORE}/api/v1/games/${GAME_ID}/play" -H 'Content-Type: application/json' \
  -d "{\"scope\":${SCOPE},\"session_id\":\"${SESSION_ID}\",\"player_id\":\"${PLAYER}\",\"payload\":{}}" \
  | jq '{code, message, reason: .data.error.reason}'

say "6. Eligibility — scope travels as query params on a GET"
curl -sf "${CORE}/api/v1/games/${GAME_ID}/eligibility?scope.tenant_id=${TENANT}&scope.merchant_id=${MERCHANT}&player_id=${PLAYER}" \
  | jq '{code, can_play: .data.can_play, remaining: .data.remaining_plays}'

echo
echo "REST smoke complete. Core served the full Start → Play flow over JSON/HTTP."
