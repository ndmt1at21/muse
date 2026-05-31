package game

import (
	"encoding/json"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
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

// gameView shapes a proto Game into the admin response (admin sees full config).
func gameView(g *gamev1.Game) map[string]any {
	return map[string]any{
		"game_id":          g.GetId(),
		"name":             g.GetName(),
		"type":             g.GetType(),
		"campaign_id":      g.GetCampaignId(),
		"seed_generator":   g.GetSeedGenerator(),
		"reward_handler":   g.GetRewardHandler(),
		"validator":        g.GetValidator(),
		"status":           g.GetStatus(),
		"handler_config":   rawJSON(g.GetHandlerConfig()),
		"validator_config": rawJSON(g.GetValidatorConfig()),
		"ui":               rawJSON(g.GetUi()),
		"rules": map[string]any{
			"max_plays_per_user": g.GetRules().GetMaxPlaysPerUser(),
			"max_plays_per_day":  g.GetRules().GetMaxPlaysPerDay(),
			"require_login":      g.GetRules().GetRequireLogin(),
		},
	}
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
