#!/usr/bin/env bash
# End-to-end for Phase 3.5 fulfillment & delivery: the transactional outbox,
# the dispatcher worker (auto-delivery via a built-in provider), the dead-letter
# + admin retry path, and the HMAC-signed external_workflow (n8n) callback.
# Requires: curl, jq, openssl, running BFFs + Core (with the dispatcher enabled).
#
# Optional: export CALLBACK_SECRET to the same value Core/bff-admin were started
# with (FULFILLMENT_CALLBACK_SECRET) to exercise real signature verification;
# unset, the callback runs in dev mode (accepted unsigned).
set -euo pipefail

ADMIN="${ADMIN_URL:-http://localhost:8081}"
CONSUMER="${CONSUMER_URL:-http://localhost:8080}"
TENANT="${TENANT:-tenant_ful}"; MERCHANT="${MERCHANT:-merchant_ful}"; PLAYER="${PLAYER:-player_dave}"
CALLBACK_SECRET="${CALLBACK_SECRET:-}"
POLL="${POLL:-3}" # seconds to wait for the dispatcher between steps

hdr=(-H "Content-Type: application/json" -H "X-Tenant-Id: ${TENANT}" -H "X-Merchant-Id: ${MERCHANT}" -H "X-Roles: admin")
phdr=("${hdr[@]}" -H "X-Player-Id: ${PLAYER}")
say() { printf '\n\033[36m== %s ==\033[0m\n' "$1"; }

mkgame() { # prize_id -> game_id (always-win spin referencing the prize)
  curl -sf "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/games" -d "{
    \"name\":\"Fulfillment Spin\",\"type\":\"spin_wheel\",\"seed_generator\":\"none\",
    \"reward_handler\":\"probability\",\"validator\":\"basic\",\"status\":\"active\",
    \"handler_config\":{\"prizes\":[{\"prize_id\":\"$1\",\"probability\":1.0,\"slot_index\":0}]}}" \
    | jq -r '.data.game_id'
}
play() { # game_id -> play response (fresh session each call)
  local sid; sid=$(curl -sf "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/$1/start" -d '{}' | jq -r '.data.session_id')
  curl -s "${phdr[@]}" -X POST "${CONSUMER}/api/v1/games/$1/play" -d "{\"session_id\":\"${sid}\",\"payload\":{}}"
}
task_for_reward() { # reward_id -> task_id (search the admin outbox)
  curl -s "${hdr[@]}" "${ADMIN}/api/v1/admin/fulfillment/tasks?limit=100" \
    | jq -r --arg r "$1" '.data.items[] | select(.reward_id==$r) | .task_id' | head -1
}
task_status() { # task_id -> status
  curl -s "${hdr[@]}" "${ADMIN}/api/v1/admin/fulfillment/tasks?limit=100" \
    | jq -r --arg t "$1" '.data.items[] | select(.task_id==$t) | .status' | head -1
}

# ---------------------------------------------------------------------------
say "A. Auto-delivery via a built-in channel (sms stub), instant redemption"
PRIZE_A=$(curl -sf "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/prizes" -d '{
  "name":"SMS Reward","type":"physical","value":50000,"stock":{"total":5},
  "fulfillment":{"redemption_mode":"instant","channel":"sms"}}' | jq -r '.data.prize_id')
GAME_A=$(mkgame "$PRIZE_A")
R_A=$(play "$GAME_A")
REWARD_A=$(echo "$R_A" | jq -r '.data.rewards[0].reward_id')
echo "won reward ${REWARD_A}; reward status right after win:"
curl -s "${phdr[@]}" "${CONSUMER}/api/v1/players/me/rewards" | jq -r --arg r "$REWARD_A" \
  '.data.items[] | select(.reward_id==$r) | {reward_id, status}'
echo "waiting ${POLL}s for the dispatcher to deliver…"; sleep "$POLL"
echo "outbox task (should be fulfilled, sms receipt):"
curl -s "${hdr[@]}" "${ADMIN}/api/v1/admin/fulfillment/tasks?prize_id=${PRIZE_A}" \
  | jq '.data.items[0] | {task_id, channel, status, attempts, receipt}'
echo "reward now (should be fulfilled by the dispatcher):"
curl -s "${phdr[@]}" "${CONSUMER}/api/v1/players/me/rewards" | jq -r --arg r "$REWARD_A" \
  '.data.items[] | select(.reward_id==$r) | {reward_id, status}'

# ---------------------------------------------------------------------------
say "B. external_workflow (n8n) → dead-letter, admin retry, then signed callback"
PRIZE_B=$(curl -sf "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/prizes" -d '{
  "name":"Workflow Reward","type":"physical","value":200000,"stock":{"total":5},
  "fulfillment":{"redemption_mode":"instant","channel":"external_workflow",
    "channel_config":{"webhook_url":"http://127.0.0.1:1/never"},
    "retry":{"max_attempts":1}}}' | jq -r '.data.prize_id')
GAME_B=$(mkgame "$PRIZE_B")
R_B=$(play "$GAME_B")
REWARD_B=$(echo "$R_B" | jq -r '.data.rewards[0].reward_id')
echo "won reward ${REWARD_B} (delivery pending via external_workflow)"
echo "waiting ${POLL}s for the dispatcher to attempt + dead-letter (max_attempts=1)…"; sleep "$POLL"
TASK_B=$(task_for_reward "$REWARD_B")
echo "task ${TASK_B} status: $(task_status "$TASK_B") (expect dead — unreachable webhook)"

say "B.2 Admin retry re-arms the dead task"
curl -s "${hdr[@]}" -X POST "${ADMIN}/api/v1/admin/fulfillment/tasks/${TASK_B}/retry" -d '{}' \
  | jq '.data | {task_id, status, attempts}'
sleep "$POLL"
echo "task status after retry+attempt: $(task_status "$TASK_B") (dead again — still unreachable)"

say "B.3 Orchestrator (n8n) reports success via the signed callback"
BODY='{"status":"fulfilled","receipt":{"voucher":"WF-987","provider":"n8n"}}'
sig_hdr=()
if [ -n "$CALLBACK_SECRET" ]; then
  SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$CALLBACK_SECRET" | awk '{print $NF}')
  sig_hdr=(-H "X-Muse-Signature: ${SIG}")
  echo "signing callback with HMAC-SHA256"
else
  echo "no CALLBACK_SECRET set — callback runs in dev mode (unsigned)"
fi
curl -s -H "Content-Type: application/json" "${sig_hdr[@]}" \
  -X POST "${ADMIN}/api/v1/fulfillment/tasks/${TASK_B}/callback" -d "$BODY" \
  | jq '.data | {task_id, status, receipt}'

echo "reward ${REWARD_B} after callback (should be fulfilled):"
curl -s "${phdr[@]}" "${CONSUMER}/api/v1/players/me/rewards" | jq -r --arg r "$REWARD_B" \
  '.data.items[] | select(.reward_id==$r) | {reward_id, status}'

# ---------------------------------------------------------------------------
say "C. Admin outbox overview (by status)"
for st in fulfilled dead processing failed pending; do
  n=$(curl -s "${hdr[@]}" "${ADMIN}/api/v1/admin/fulfillment/tasks?status=${st}&limit=100" | jq '.data.items | length')
  printf '  %-11s %s\n' "$st" "$n"
done

echo; echo "Phase 3.5 fulfillment e2e complete."
