package campaign

import (
	"encoding/json"

	gamev1 "github.com/muse/pkg/gen/game/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// publicView is the widget-facing render config: the campaign's display
// metadata, run window, channels, linked games/quests, and the player-relevant
// settings (auth + lead-capture + play caps). Internal ids (tenant/merchant) and
// the wallet scope are intentionally omitted.
func publicView(c *gamev1.Campaign) map[string]any {
	return map[string]any{
		"campaign_id": c.GetId(),
		"name":        c.GetName(),
		"status":      c.GetStatus(),
		"start_date":  tsString(c.GetStartDate()),
		"end_date":    tsString(c.GetEndDate()),
		"channels":    strSlice(c.GetChannels()),
		"games":       strSlice(c.GetGames()),
		"quests":      strSlice(c.GetQuests()),
		"settings":    rawJSON(c.GetSettings()),
	}
}

func strSlice(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
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
