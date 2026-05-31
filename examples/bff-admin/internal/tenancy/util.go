package tenancy

import (
	"encoding/json"
	"strconv"

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

func tenantView(t *gamev1.Tenant) map[string]any {
	return map[string]any{
		"tenant_id":  t.GetId(),
		"name":       t.GetName(),
		"plan":       t.GetPlan(),
		"settings":   rawJSON(t.GetSettings()),
		"created_at": tsString(t.GetCreatedAt()),
		"updated_at": tsString(t.GetUpdatedAt()),
	}
}

func merchantView(m *gamev1.Merchant) map[string]any {
	return map[string]any{
		"merchant_id": m.GetId(),
		"tenant_id":   m.GetTenantId(),
		"name":        m.GetName(),
		"logo":        m.GetLogo(),
		"settings":    rawJSON(m.GetSettings()),
		"created_at":  tsString(m.GetCreatedAt()),
		"updated_at":  tsString(m.GetUpdatedAt()),
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
