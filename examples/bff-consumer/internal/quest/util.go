package quest

import (
	"encoding/json"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
	gamev1 "github.com/muse/pkg/gen/game/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func invalidArg(msg string) error {
	return apierr.FromDomainError(gkerr.New(gkerr.ReasonValidationFailed, msg)).Err()
}

// questStateView is the player-facing quest + completion status.
func questStateView(st *gamev1.QuestState) map[string]any {
	q := st.GetQuest()
	var reward any
	if r := q.GetReward(); r != nil {
		reward = map[string]any{"type": r.GetType(), "quantity": r.GetQuantity()}
	}
	return map[string]any{
		"quest_id":       q.GetId(),
		"campaign_id":    q.GetCampaignId(),
		"type":           q.GetType(),
		"name":           q.GetName(),
		"reward":         reward,
		"config":         rawJSON(q.GetConfig()),
		"completed":      st.GetCompleted(),
		"can_complete":   st.GetCanComplete(),
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

func tsString(t *timestamppb.Timestamp) any {
	if t == nil {
		return nil
	}
	return t.AsTime().UTC().Format("2006-01-02T15:04:05Z07:00")
}
