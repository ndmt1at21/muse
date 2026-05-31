package game

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
	gamev1 "github.com/muse/pkg/gen/game/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// invalidArg builds an INVALID_ARGUMENT gRPC status error (rendered by the
// envelope with reason VALIDATION_FAILED), so BFF-side validation errors share
// the exact same shape as Core errors.
func invalidArg(msg string) error {
	return apierr.FromDomainError(gkerr.New(gkerr.ReasonValidationFailed, msg)).Err()
}

// rewardsJSON shapes proto rewards into the public reward_result items.
func rewardsJSON(rs []*gamev1.Reward) []any {
	out := make([]any, 0, len(rs))
	for _, r := range rs {
		out = append(out, map[string]any{
			"prize_id":  r.GetPrizeId(),
			"name":      r.GetName(),
			"image":     r.GetImage(),
			"type":      r.GetType(),
			"quantity":  r.GetQuantity(),
			"value":     r.GetValue(),
			"reward_id": emptyToNil(r.GetRewardId()),
			"code":      emptyToNil(r.GetCode()),
		})
	}
	return out
}

// rawJSON parses a raw JSON string into a generic value for embedding.
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

func emptyToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseLimit(r *http.Request) int {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 20
}
