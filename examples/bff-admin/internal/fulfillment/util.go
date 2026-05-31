package fulfillment

import (
	"encoding/json"
	"strconv"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
)

func invalidArg(msg string) error {
	return apierr.FromDomainError(gkerr.New(gkerr.ReasonValidationFailed, msg)).Err()
}

func unauthenticated(msg string) error {
	return apierr.FromDomainError(gkerr.New(gkerr.ReasonUnauthenticated, msg)).Err()
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
