package sqlstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

type historyRow struct {
	ID         string    `db:"id"`
	TenantID   string    `db:"tenant_id"`
	MerchantID string    `db:"merchant_id"`
	GameID     string    `db:"game_id"`
	PlayerID   string    `db:"player_id"`
	SessionID  string    `db:"session_id"`
	Payload    []byte    `db:"payload"`
	Rewards    []byte    `db:"rewards"`
	Metadata   []byte    `db:"metadata"`
	TraceID    string    `db:"trace_id"`
	CreatedAt  time.Time `db:"created_at"`
}

// InsertHistory implements ports.HistoryStore. Runs inside the Play txn.
func (db *DB) InsertHistory(ctx context.Context, h *types.PlayHistory) error {
	_, err := db.execContext(ctx,
		`INSERT INTO play_history (id, tenant_id, merchant_id, game_id, player_id, session_id, payload, rewards, metadata, trace_id, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		h.ID, h.Scope.TenantID, h.Scope.MerchantID, h.GameID, h.PlayerID, h.SessionID,
		jsonOrNull(h.Payload), jsonOrNull(h.Rewards), jsonOrNull(h.Metadata), h.TraceID, h.CreatedAt)
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "insert history").Wrap(err)
	}
	return nil
}

// ListHistory implements ports.HistoryStore (simple time-desc pagination using
// created_at as an opaque cursor).
func (db *DB) ListHistory(ctx context.Context, scope types.Scope, gameID, playerID string, limit int, cursor string) ([]types.PlayHistory, string, error) {
	args := []any{scope.TenantID, scope.MerchantID, gameID, playerID}
	where := `tenant_id = ? AND merchant_id = ? AND game_id = ? AND player_id = ?`
	if cursor != "" {
		if t, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			where += ` AND created_at < ?`
			args = append(args, t)
		}
	}
	args = append(args, limit+1)
	var rows []historyRow
	err := db.selectContext(ctx, &rows,
		`SELECT id, tenant_id, merchant_id, game_id, player_id, session_id, payload, rewards, metadata, trace_id, created_at
		   FROM play_history WHERE `+where+` ORDER BY created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, "", gkerr.New(gkerr.ReasonInternal, "list history").Wrap(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].CreatedAt.Format(time.RFC3339Nano)
		rows = rows[:limit]
	}
	out := make([]types.PlayHistory, 0, len(rows))
	for _, r := range rows {
		out = append(out, types.PlayHistory{
			ID:        r.ID,
			Scope:     types.Scope{TenantID: r.TenantID, MerchantID: r.MerchantID},
			GameID:    r.GameID,
			PlayerID:  r.PlayerID,
			SessionID: r.SessionID,
			Payload:   json.RawMessage(r.Payload),
			Rewards:   json.RawMessage(r.Rewards),
			Metadata:  json.RawMessage(r.Metadata),
			TraceID:   r.TraceID,
			CreatedAt: r.CreatedAt,
		})
	}
	return out, next, nil
}

// CountPlays implements ports.HistoryStore: total all-time plays and plays
// since `since` (start of today) for the per-user / per-day eligibility caps.
func (db *DB) CountPlays(ctx context.Context, scope types.Scope, gameID, playerID string, since time.Time) (int, int, error) {
	var counts struct {
		Total int `db:"total"`
		Today int `db:"today"`
	}
	// COUNT with a conditional SUM works identically on both engines.
	err := db.getContext(ctx, &counts,
		`SELECT COUNT(*) AS total,
		        COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0) AS today
		   FROM play_history
		  WHERE tenant_id = ? AND merchant_id = ? AND game_id = ? AND player_id = ?`,
		since, scope.TenantID, scope.MerchantID, gameID, playerID)
	if err != nil {
		return 0, 0, gkerr.New(gkerr.ReasonInternal, "count plays").Wrap(err)
	}
	return counts.Total, counts.Today, nil
}

func jsonOrNull(b json.RawMessage) any {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return string(b)
}
