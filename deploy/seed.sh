#!/usr/bin/env bash
# Seed demo data (Phase 11): a campaign + a ready-to-play spin-wheel game with
# prizes, plus a webhook integration — enough to exercise the widget, gameplay,
# and the metrics/dashboards end to end. Idempotent-ish: re-running creates fresh
# ids. Requires: curl, jq, both BFFs running. Admin calls carry X-Roles: admin.
set -euo pipefail

ADMIN="${ADMIN_URL:-http://localhost:8081}"
CONSUMER="${CONSUMER_URL:-http://localhost:8080}"
TENANT="${TENANT:-tenant_demo}"; MERCHANT="${MERCHANT:-merchant_demo}"
ahdr=(-H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Roles: admin")

command -v jq >/dev/null || { echo "jq required"; exit 1; }
say() { printf '\n\033[36m== %s ==\033[0m\n' "$1"; }

say "Campaign"
CAMP=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/campaigns" \
  -d '{"name":"Demo Lì Xì","status":"active"}')
CAMP_ID=$(echo "$CAMP" | jq -r '.data.campaign_id // empty')
echo "campaign=${CAMP_ID:-<none>}"

say "Prizes"
mkprize() { curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/prizes" \
  -d "{\"name\":\"$1\",\"type\":\"$2\",\"value\":$3,\"stock\":{\"total\":$4}}" | jq -r '.data.prize_id'; }
JACKPOT=$(mkprize "Voucher 100K" voucher 100000 50)
SMALL=$(mkprize "Voucher 10K"  voucher 10000 500)
MISS=$(mkprize   "Chúc may mắn" points 0 1000000)
echo "prizes: jackpot=$JACKPOT small=$SMALL miss=$MISS"

say "Spin-wheel game (probability handler)"
GAME=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/games" -d "{
  \"name\":\"Vòng Quay May Mắn\",\"type\":\"spin_wheel\",\"campaign_id\":\"${CAMP_ID}\",
  \"seed_generator\":\"none\",\"reward_handler\":\"probability\",\"validator\":\"basic\",\"status\":\"active\",
  \"rules\":{\"max_plays_per_user\":100},
  \"handler_config\":{\"prizes\":[
    {\"prize_id\":\"${JACKPOT}\",\"probability\":0.01},
    {\"prize_id\":\"${SMALL}\",\"probability\":0.20},
    {\"prize_id\":\"${MISS}\",\"probability\":0.79}]}
}")
GAME_ID=$(echo "$GAME" | jq -r '.data.game_id'); echo "game=${GAME_ID}"

say "Integration (webhook on prize_won + play_completed)"
curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/integrations" \
  -d '{"type":"webhook","events":["prize_won","play_completed"],"config":{"url":"https://example.test/hook"}}' \
  | jq -c '.data | {id, type, events}'

say "Smoke: start + play once"
S=$(curl -s -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Player-Id: demo_player" \
  -X POST "${CONSUMER}/api/v1/games/${GAME_ID}/start" -d '{}' | jq -r '.data.session_id')
curl -s -H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Player-Id: demo_player" \
  -X POST "${CONSUMER}/api/v1/games/${GAME_ID}/play" -d "{\"session_id\":\"${S}\",\"payload\":{}}" \
  | jq -c '{code, reward: .data.rewards[0].name}'

echo
echo "Seed complete. Game id: ${GAME_ID}"
echo "Grafana: http://localhost:3000 (anonymous Admin)   Prometheus: http://localhost:9092"
