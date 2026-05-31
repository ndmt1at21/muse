package player

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

func orEmptyObj(b json.RawMessage) json.RawMessage {
	if len(b) == 0 || string(b) == "null" {
		return json.RawMessage(`{}`)
	}
	return b
}

func tsString(t *timestamppb.Timestamp) any {
	if t == nil {
		return nil
	}
	return t.AsTime().UTC().Format("2006-01-02T15:04:05Z07:00")
}

// playerView shapes a player (+ optional identity) into the player-facing
// response. Contacts come from the identity; profile is the tenant-scoped blob.
func playerView(p *gamev1.Player, idn *gamev1.Identity) map[string]any {
	out := map[string]any{
		"player_id":   p.GetId(),
		"identity_id": p.GetIdentityId(),
		"profile":     rawJSON(p.GetProfile()),
	}
	if idn != nil {
		out["contacts"] = contactsView(idn)
	}
	return out
}

func identityView(idn *gamev1.Identity) map[string]any {
	return map[string]any{
		"identity_id": idn.GetId(),
		"contacts":    contactsView(idn),
	}
}

func contactsView(idn *gamev1.Identity) []any {
	contacts := make([]any, 0, len(idn.GetContacts()))
	for _, c := range idn.GetContacts() {
		contacts = append(contacts, map[string]any{
			"type": c.GetType(), "value": c.GetValue(), "verified": c.GetVerified(),
		})
	}
	return contacts
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
