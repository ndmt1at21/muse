package campaign

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
	"github.com/muse/pkg/enumx"
	gamev1 "github.com/muse/pkg/gen/game/v1"
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
func parseDate(s string) int64 {
	if s == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Unix()
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC().Unix()
	}
	return 0
}

func campaignView(c *gamev1.Campaign) map[string]any {
	return map[string]any{
		"campaign_id": c.GetId(),
		"tenant_id":   c.GetTenantId(),
		"merchant_id": c.GetMerchantId(),
		"name":        c.GetName(),
		"status":      enumx.Name(c.GetStatus()),
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
