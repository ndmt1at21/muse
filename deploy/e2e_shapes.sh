#!/usr/bin/env bash
# End-to-end for the Phase 2 game shapes: egg-catcher (score_to_tier +
# time_and_score_range) and gift-catcher (drop_sequence + collect_items +
# drop_plan), over the REST stack. Requires: curl, jq, running BFFs.
set -euo pipefail

ADMIN="${ADMIN_URL:-http://localhost:8081}"
CONSUMER="${CONSUMER_URL:-http://localhost:8080}"
TENANT="${TENANT:-tenant_demo}"; MERCHANT="${MERCHANT:-merchant_demo}"; PLAYER="${PLAYER:-player_bob}"

hdr=(-H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Roles: admin")
phdr=("${hdr[@]}" -H "X-Player-Id: ${PLAYER}")
say() { printf '\n\033[36m== %s ==\033[0m\n' "$1"; }
mkprize() { # name total -> prize_id
  curl -sf "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/prizes" \
    -d "{\"name\":\"$1\",\"type\":\"voucher\",\"value\":1000,\"stock\":{\"total\":$2}}" | jq -r '.data.prize_id'
}

############################################
say "EGG-CATCHER (score_to_tier + time_and_score_range)"
############################################
SMALL=$(mkprize "Small Voucher" 100)
BIG=$(mkprize "Big Voucher" 100)
echo "prizes: small=$SMALL big=$BIG"

EGG=$(curl -sf "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/games" -d "{
  \"name\":\"Egg Catcher\",\"type\":\"egg_catcher\",\"campaign_id\":\"camp_demo\",
  \"seed_generator\":\"none\",\"reward_handler\":\"score_to_tier\",\"validator\":\"time_and_score_range\",\"status\":\"active\",
  \"rules\":{\"max_plays_per_user\":10},
  \"handler_config\":{
    \"tiers\":[{\"min\":0,\"max\":29,\"prize_group\":\"t0\"},{\"min\":30,\"max\":69,\"prize_group\":\"t1\"},{\"min\":70,\"max\":1000,\"prize_group\":\"t2\"}],
    \"prize_groups\":{\"t1\":[{\"prize_id\":\"${SMALL}\",\"probability\":1.0}],\"t2\":[{\"prize_id\":\"${BIG}\",\"probability\":1.0}]}},
  \"validator_config\":{\"min_duration_ms\":2000,\"max_duration_ms\":120000,\"max_score\":150}
}")
EGG_ID=$(echo "$EGG" | jq -r '.data.game_id'); echo "game: $EGG_ID"

say "score 75 → tier t2 → Big Voucher"
S=$(curl -sf "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${EGG_ID}/start" -d '{}' | jq -r '.data.session_id')
curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${EGG_ID}/play" \
  -d "{\"session_id\":\"${S}\",\"payload\":{\"score\":75,\"duration_ms\":8000}}" | jq '{code, reward: .data.rewards[0].name, tier: .data.metadata.tier}'

say "score 9999 → CHEAT_DETECTED (over ceiling)"
S=$(curl -sf "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${EGG_ID}/start" -d '{}' | jq -r '.data.session_id')
curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${EGG_ID}/play" \
  -d "{\"session_id\":\"${S}\",\"payload\":{\"score\":9999,\"duration_ms\":8000}}" | jq '{code, reason: .data.error.reason}'

############################################
say "GIFT-CATCHER (drop_sequence + collect_items + drop_plan)"
############################################
V50=$(mkprize "Voucher 50K" 100)
COIN=$(mkprize "Coin" 1000)
echo "prizes: v50=$V50 coin=$COIN"

GIFT=$(curl -sf "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/games" -d "{
  \"name\":\"Gift Catcher\",\"type\":\"gift_catcher\",\"campaign_id\":\"camp_demo\",
  \"seed_generator\":\"drop_sequence\",\"reward_handler\":\"collect_items\",\"validator\":\"drop_plan\",\"status\":\"active\",
  \"rules\":{\"max_plays_per_user\":10},
  \"handler_config\":{
    \"drops\":[{\"type\":\"voucher_50k\",\"prize_id\":\"${V50}\",\"frequency\":4,\"max_catchable\":2},
              {\"type\":\"coin\",\"prize_id\":\"${COIN}\",\"frequency\":4,\"max_catchable\":3}],
    \"total_items\":15,\"interval_ms\":500}
}")
GIFT_ID=$(echo "$GIFT" | jq -r '.data.game_id'); echo "game: $GIFT_ID"

say "start → server issues drop sequence"
START=$(curl -sf "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GIFT_ID}/start" -d '{}')
S=$(echo "$START" | jq -r '.data.session_id')
echo "$START" | jq '{session: .data.session_id, drops: (.data.seed_data.drops | length)}'

# Pick 2 voucher_50k (== cap) and 1 coin id from the server's sequence.
CAUGHT=$(echo "$START" | jq -c '[.data.seed_data.drops[] | select(.type=="voucher_50k") | .drop_id][0:2] + [.data.seed_data.drops[] | select(.type=="coin") | .drop_id][0:1]')
echo "catching: $CAUGHT"

say "play → expect 3 reward units (2 vouchers + 1 coin)"
curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GIFT_ID}/play" \
  -d "{\"session_id\":\"${S}\",\"payload\":{\"caught_items\":${CAUGHT}}}" \
  | jq '{code, units: (.data.rewards | length), total_caught: .data.metadata.total_caught, by_type: .data.metadata.by_type}'

say "over-catch 3 vouchers (cap 2) → CHEAT_DETECTED"
START=$(curl -sf "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GIFT_ID}/start" -d '{}')
S=$(echo "$START" | jq -r '.data.session_id')
OVER=$(echo "$START" | jq -c '[.data.seed_data.drops[] | select(.type=="voucher_50k") | .drop_id][0:3]')
curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/${GIFT_ID}/play" \
  -d "{\"session_id\":\"${S}\",\"payload\":{\"caught_items\":${OVER}}}" | jq '{code, reason: .data.error.reason}'

echo; echo "Phase 2 shapes e2e complete."
