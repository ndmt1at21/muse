#!/usr/bin/env bash
# Seed demo data from deploy/games.json: a campaign + prizes + every game defined
# in the config (one per built-in shape) + a webhook integration — enough to play
# every game in the embedded widget and exercise the metrics/dashboards end to
# end. Tune the games (probabilities, tiers, drop frequencies, stock, plays) by
# editing games.json and re-running — no code changes. Idempotent-ish: re-running
# creates fresh ids. Requires: curl, jq, both BFFs running. Admin calls carry
# X-Roles: admin. Prints a /play URL that prefills the spin/egg/gift games.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CFG="${GAMES_CONFIG:-$SCRIPT_DIR/games.json}"
ADMIN="${ADMIN_URL:-http://localhost:8081}"
CONSUMER="${CONSUMER_URL:-http://localhost:8080}"
TENANT="${TENANT:-tenant_demo}"; MERCHANT="${MERCHANT:-merchant_demo}"
ahdr=(-H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Roles: admin")

command -v jq >/dev/null || { echo "jq required"; exit 1; }
[ -f "$CFG" ] || { echo "config not found: $CFG"; exit 1; }
say() { printf '\n\033[36m== %s ==\033[0m\n' "$1"; }

say "Campaign"
CAMP_NAME=$(jq -r '.campaign_name' "$CFG")
CAMP=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/campaigns" \
  -d "$(jq -n --arg n "$CAMP_NAME" '{name:$n, status:"CAMPAIGN_STATUS_ACTIVE"}')")
CAMP_ID=$(echo "$CAMP" | jq -r '.data.campaign_id // empty')
echo "campaign=${CAMP_ID:-<none>}"

# Create every prize in the config; build IDS = { "<key>": "<prize_id>", ... }
# so games can reference prizes by key. An empty "prize":"" stays a no-win.
say "Prizes"
IDS=$(jq -n '{}')
for key in $(jq -r '.prizes | keys_unsorted[]' "$CFG"); do
  body=$(jq -c --arg k "$key" '.prizes[$k] | {name, type, value, stock:{total:.stock}}' "$CFG")
  pid=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/prizes" -d "$body" | jq -r '.data.prize_id')
  IDS=$(jq -c --arg k "$key" --arg id "$pid" '. + {($k):$id}' <<<"$IDS")
  echo "prize ${key}=${pid}"
done

# Create every game. Each game's handler_config references prizes by key under a
# "prize" field; we rewrite those to the real "prize_id" before posting.
declare -A GID
for key in $(jq -r '.games | keys_unsorted[]' "$CFG"); do
  gdef=$(jq -c --arg k "$key" '.games[$k]' "$CFG")
  gresolved=$(jq -c --argjson ids "$IDS" '
    .handler_config |= walk(
      if type=="object" and has("prize")
      then .prize_id = ($ids[.prize] // "") | del(.prize)
      else . end)' <<<"$gdef")
  body=$(jq -n --arg camp "$CAMP_ID" --argjson g "$gresolved" '
    {
      name:$g.name, type:$g.type, campaign_id:$camp,
      seed_generator:$g.seed_generator, reward_handler:$g.reward_handler, validator:$g.validator,
      status:"GAME_STATUS_ACTIVE",
      rules:{max_plays_per_user:($g.max_plays_per_user // 100)},
      handler_config:$g.handler_config
    } + (if $g.validator_config then {validator_config:$g.validator_config} else {} end)
      + (if $g.ui then {ui:$g.ui} else {} end)')
  gid=$(curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/games" -d "$body" | jq -r '.data.game_id')
  GID[$key]=$gid
  say "Game: ${key} ($(jq -r '.type' <<<"$gdef"))"
  echo "${key}=${gid}"
done

say "Integration (webhook on prize_won + play_completed)"
curl -s "${ahdr[@]}" -X POST "${ADMIN}/api/v1/admin/integrations" \
  -d '{"type":"webhook","events":["prize_won","play_completed"],"config":{"url":"https://example.test/hook"}}' \
  | jq -c '.data | {id, type, events}'

if [ -n "${GID[spin]:-}" ]; then
  say "Smoke: spin once"
  S=$(curl -s -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Player-Id: demo_player" \
    -X POST "${CONSUMER}/api/v1/games/${GID[spin]}/start" -d '{}' | jq -r '.data.session_id')
  curl -s -H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Player-Id: demo_player" \
    -X POST "${CONSUMER}/api/v1/games/${GID[spin]}/play" -d "{\"session_id\":\"${S}\",\"payload\":{}}" \
    | jq -c '{code, reward: (.data.rewards[0].name // "no win")}'
fi

PLAY_URL="${CONSUMER}/play/?tenant=${TENANT}&merchant=${MERCHANT}&player=demo_player&spin=${GID[spin]:-}&egg=${GID[egg]:-}&gift=${GID[gift]:-}"
echo
echo "Seed complete. Games: spin=${GID[spin]:-} egg=${GID[egg]:-} gift=${GID[gift]:-}"
printf '\n\033[32mPlay the demos:\033[0m %s\n' "$PLAY_URL"
echo "Edit deploy/games.json (e.g. spin prize probabilities) and re-run to retune."
echo "Grafana: http://localhost:3000 (anonymous Admin)   Prometheus: http://localhost:9092"
