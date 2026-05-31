package campaign

import (
	"encoding/json"

	"github.com/muse/pkg/enumx"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// publicView is the widget-facing render config: the campaign's display
// metadata, run window, channels, linked games/quests, and the player-relevant
// settings (auth + lead-capture + play caps). Internal ids (tenant/merchant) and
// the wallet scope are intentionally omitted.
func publicView(c *gamev1.Campaign) map[string]any {
	return map[string]any{
		"campaign_id": c.GetId(),
		"name":        c.GetName(),
		"status":      enumx.Name(c.GetStatus()),
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

// tsString returns the unix-seconds timestamp, or nil when unset (0).
func tsString(unix int64) any {
	if unix == 0 {
		return nil
	}
	return unix
}
