package quest

import (
	"encoding/json"
	"strconv"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
	gamev1 "github.com/muse/pkg/gen/game/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func invalidArg(msg string) error {
	return apierr.FromDomainError(gkerr.New(gkerr.ReasonValidationFailed, msg)).Err()
}

func orEmptyObj(b json.RawMessage) json.RawMessage {
	if len(b) == 0 || string(b) == "null" {
		return json.RawMessage(`{}`)
	}
	return b
}

func questView(q *gamev1.Quest) map[string]any {
	var reward any
	if r := q.GetReward(); r != nil {
		reward = map[string]any{"type": r.GetType(), "quantity": r.GetQuantity()}
	}
	return map[string]any{
		"quest_id":    q.GetId(),
		"tenant_id":   q.GetTenantId(),
		"merchant_id": q.GetMerchantId(),
		"campaign_id": q.GetCampaignId(),
		"type":        q.GetType(),
		"name":        q.GetName(),
		"status":      q.GetStatus(),
		"reward":      reward,
		"config":      rawJSON(q.GetConfig()),
		"created_at":  tsString(q.GetCreatedAt()),
		"updated_at":  tsString(q.GetUpdatedAt()),
	}
}

func paged(items []any, nextCursor string) map[string]any {
	return map[string]any{
		"items": items,
		"pagination": map[string]any{
			"next_cursor": emptyToNil(nextCursor),
			"has_more":    nextCursor != "",
		},
	}
}

func emptyToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func rawJSON(s string) any {
	if s == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	return v
}

func tsString(t *timestamppb.Timestamp) any {
	if t == nil {
		return nil
	}
	return t.AsTime().UTC().Format("2006-01-02T15:04:05Z07:00")
}

func parseLimit(s string) int {
	if s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 20
}
