package leaderboard

import (
	"encoding/json"
	"strconv"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
	"github.com/muse/pkg/enumx"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

func invalidArg(msg string) error {
	return apierr.FromDomainError(gkerr.New(gkerr.ReasonValidationFailed, msg)).Err()
}

func orEmptyObj(b json.RawMessage) string {
	if len(b) == 0 || string(b) == "null" {
		return "{}"
	}
	return string(b)
}

func orEmptyArr(b json.RawMessage) string {
	if len(b) == 0 || string(b) == "null" {
		return "[]"
	}
	return string(b)
}

func lbView(lb *gamev1.Leaderboard) map[string]any {
	return map[string]any{
		"leaderboard_id": lb.GetId(),
		"tenant_id":      lb.GetTenantId(),
		"merchant_id":    lb.GetMerchantId(),
		"campaign_id":    lb.GetCampaignId(),
		"name":           lb.GetName(),
		"metric":         enumx.Name(lb.GetMetric()),
		"time_window":    rawJSON(lb.GetTimeWindow()),
		"prize_tiers":    rawJSON(lb.GetPrizeTiers()),
		"anti_cheat":     rawJSON(lb.GetAntiCheat()),
		"status":         enumx.Name(lb.GetStatus()),
		"created_at":     tsString(lb.GetCreatedAt()),
		"updated_at":     tsString(lb.GetUpdatedAt()),
	}
}

func entryView(e *gamev1.RankedEntry) map[string]any {
	return map[string]any{
		"player_id": e.GetPlayerId(),
		"score":     e.GetScore(),
		"plays":     e.GetPlays(),
		"rank":      e.GetRank(),
		"state":     enumx.Name(e.GetState()),
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

// tsString returns the unix-seconds timestamp, or nil when unset (0).
func tsString(unix int64) any {
	if unix == 0 {
		return nil
	}
	return unix
}

func parseLimit(s string) int {
	if s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 20
}
