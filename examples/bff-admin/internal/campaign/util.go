package campaign

import (
	"encoding/json"
	"strconv"
	"time"

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

// parseDate accepts an RFC3339 timestamp or a date-only "2006-01-02" string and
// returns a proto Timestamp (nil for empty/unparseable input — the field is
// optional).
func parseDate(s string) *timestamppb.Timestamp {
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return timestamppb.New(t.UTC())
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return timestamppb.New(t.UTC())
	}
	return nil
}

func campaignView(c *gamev1.Campaign) map[string]any {
	return map[string]any{
		"campaign_id": c.GetId(),
		"tenant_id":   c.GetTenantId(),
		"merchant_id": c.GetMerchantId(),
		"name":        c.GetName(),
		"status":      c.GetStatus(),
		"start_date":  tsString(c.GetStartDate()),
		"end_date":    tsString(c.GetEndDate()),
		"channels":    strSlice(c.GetChannels()),
		"games":       strSlice(c.GetGames()),
		"quests":      strSlice(c.GetQuests()),
		"settings":    rawJSON(c.GetSettings()),
		"created_at":  tsString(c.GetCreatedAt()),
		"updated_at":  tsString(c.GetUpdatedAt()),
	}
}

func analyticsView(a *gamev1.CampaignAnalytics) map[string]any {
	return map[string]any{
		"campaign_id":    a.GetCampaignId(),
		"total_plays":    a.GetTotalPlays(),
		"total_wins":     a.GetTotalWins(),
		"unique_players": a.GetUniquePlayers(),
		"conversion":     a.GetConversion(),
	}
}

func strSlice(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
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
