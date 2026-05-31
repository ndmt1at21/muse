package quest

import (
	"encoding/json"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
	"github.com/muse/pkg/enumx"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

func invalidArg(msg string) error {
	return apierr.FromDomainError(gkerr.New(gkerr.ReasonValidationFailed, msg)).Err()
}

// questStateView is the player-facing quest + completion status.
func questStateView(st *gamev1.QuestState) map[string]any {
	q := st.GetQuest()
	var reward any
	if r := q.GetReward(); r != nil {
		reward = map[string]any{"type": enumx.Name(r.GetType()), "quantity": r.GetQuantity()}
	}
	return map[string]any{
		"quest_id":    q.GetId(),
		"campaign_id": q.GetCampaignId(),
		"type":        enumx.Name(q.GetType()),
		"name":        q.GetName(),
		"reward":      reward,
		"config":      rawJSON(q.GetConfig()),
		"completed":   st.GetCompleted(),
		// can_complete is a UI affordance derived here (not a Core field):
		// the quest is live and the caller hasn't completed it yet.
		"can_complete":   q.GetStatus() == gamev1.QuestStatus_QUEST_STATUS_ACTIVE && !st.GetCompleted(),
		"completions":    st.GetCompletions(),
		"last_completed": tsString(st.GetLastCompleted()),
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

// tsString returns the unix-seconds timestamp, or nil when unset (0).
func tsString(unix int64) any {
	if unix == 0 {
		return nil
	}
	return unix
}
