package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/muse/gamekit/defaults"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

// taskIDs mints ids for tasks enqueued by the adapter (claim-time path).
var taskIDs defaults.IDGen

const taskCols = `id, tenant_id, merchant_id, reward_id, prize_id, player_id, game_id, campaign_id,
	channel, channel_config, status, attempts, max_attempts, last_error, receipt,
	next_attempt_at, created_at, updated_at`

type taskRow struct {
	ID            string    `db:"id"`
	TenantID      string    `db:"tenant_id"`
	MerchantID    string    `db:"merchant_id"`
	RewardID      string    `db:"reward_id"`
	PrizeID       string    `db:"prize_id"`
	PlayerID      string    `db:"player_id"`
	GameID        string    `db:"game_id"`
	CampaignID    string    `db:"campaign_id"`
	Channel       string    `db:"channel"`
	ChannelConfig []byte    `db:"channel_config"`
	Status        string    `db:"status"`
	Attempts      int       `db:"attempts"`
	MaxAttempts   int       `db:"max_attempts"`
	LastError     string    `db:"last_error"`
	Receipt       []byte    `db:"receipt"`
	NextAttemptAt time.Time `db:"next_attempt_at"`
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
}

func (r taskRow) toDomain() types.FulfillmentTask {
	return types.FulfillmentTask{
		ID:            r.ID,
		Scope:         types.Scope{TenantID: r.TenantID, MerchantID: r.MerchantID},
		RewardID:      r.RewardID,
		PrizeID:       r.PrizeID,
		PlayerID:      r.PlayerID,
		GameID:        r.GameID,
		CampaignID:    r.CampaignID,
		Channel:       r.Channel,
		ChannelConfig: json.RawMessage(r.ChannelConfig),
		Status:        types.TaskStatus(r.Status),
		Attempts:      r.Attempts,
		MaxAttempts:   r.MaxAttempts,
		LastError:     r.LastError,
		Receipt:       json.RawMessage(r.Receipt),
		NextAttemptAt: r.NextAttemptAt,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

// EnqueueTask implements ports.FulfillmentStore: it inserts one outbox task.
// Called from inside the Play transaction (instant) or the claim transaction
// (on_claim), so the task commits atomically with the reward + stock deduction.
func (db *DB) EnqueueTask(ctx context.Context, t *types.FulfillmentTask) error {
	if t.ID == "" {
		t.ID = taskIDs.NewID("task")
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	if t.NextAttemptAt.IsZero() {
		t.NextAttemptAt = now
	}
	if t.Status == "" {
		t.Status = types.TaskPending
	}
	_, err := db.execContext(ctx,
		`INSERT INTO fulfillment_tasks (`+taskCols+`)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Scope.TenantID, t.Scope.MerchantID, t.RewardID, t.PrizeID, t.PlayerID, t.GameID, t.CampaignID,
		t.Channel, string(nonEmptyJSON(t.ChannelConfig)), string(t.Status), t.Attempts, t.MaxAttempts,
		t.LastError, string(nonEmptyJSON(t.Receipt)), t.NextAttemptAt, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "enqueue fulfillment task").Wrap(err)
	}
	return nil
}

// ClaimDueTasks atomically grabs up to limit runnable tasks and marks them
// processing (incrementing attempts). "Runnable" = pending tasks whose
// next_attempt_at is due, plus processing tasks stuck longer than staleAfter
// (crash recovery). It is the dispatcher's poll primitive and is NOT
// scope-filtered: the worker drains across all tenants. SELECT ... FOR UPDATE
// SKIP LOCKED makes concurrent dispatchers grab disjoint batches on both
// engines.
func (db *DB) ClaimDueTasks(ctx context.Context, limit int, staleAfter time.Duration) ([]types.FulfillmentTask, error) {
	if limit <= 0 {
		limit = 50
	}
	var claimed []types.FulfillmentTask
	err := db.WithTx(ctx, func(ctx context.Context) error {
		now := time.Now().UTC()
		staleBefore := now.Add(-staleAfter)
		var rows []taskRow
		if err := db.selectContext(ctx, &rows,
			`SELECT `+taskCols+` FROM fulfillment_tasks
			  WHERE (status IN ('pending', 'failed') AND next_attempt_at <= ?)
			     OR (status = 'processing' AND updated_at <= ?)
			  ORDER BY next_attempt_at
			  LIMIT ?
			  FOR UPDATE SKIP LOCKED`,
			now, staleBefore, limit); err != nil {
			return gkerr.New(gkerr.ReasonInternal, "select due tasks").Wrap(err)
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]any, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := append([]any{now}, ids...)
		if _, err := db.execContext(ctx,
			`UPDATE fulfillment_tasks SET status='processing', attempts = attempts + 1, updated_at = ?
			  WHERE id IN (`+ph+`)`, args...); err != nil {
			return gkerr.New(gkerr.ReasonInternal, "claim tasks").Wrap(err)
		}
		claimed = make([]types.FulfillmentTask, 0, len(rows))
		for _, r := range rows {
			t := r.toDomain()
			t.Status = types.TaskProcessing
			t.Attempts++
			t.UpdatedAt = now
			claimed = append(claimed, t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// CompleteTask marks a task fulfilled and flips its reward to fulfilled in the
// same transaction (won|claimed -> fulfilled). The receipt (voucher code,
// tracking no., ...) is stored for audit. Idempotent: a re-delivered callback
// for an already-fulfilled task is a no-op success.
func (db *DB) CompleteTask(ctx context.Context, taskID string, receipt json.RawMessage) error {
	return db.WithTx(ctx, func(ctx context.Context) error {
		task, err := db.getTaskByID(ctx, taskID)
		if err != nil {
			return err
		}
		if task.Status == types.TaskFulfilled {
			return nil
		}
		now := time.Now().UTC()
		if _, err := db.execContext(ctx,
			`UPDATE fulfillment_tasks SET status='fulfilled', receipt=?, last_error='', updated_at=? WHERE id=?`,
			string(nonEmptyJSON(receipt)), now, taskID); err != nil {
			return gkerr.New(gkerr.ReasonInternal, "complete task").Wrap(err)
		}
		if task.RewardID != "" {
			if _, err := db.execContext(ctx,
				`UPDATE rewards SET status='fulfilled', fulfilled_at=?
				  WHERE id=? AND tenant_id=? AND merchant_id=? AND status IN ('won','claimed')`,
				now, task.RewardID, task.Scope.TenantID, task.Scope.MerchantID); err != nil {
				return gkerr.New(gkerr.ReasonInternal, "fulfill reward via task").Wrap(err)
			}
		}
		return nil
	})
}

// FailTask records a failed attempt: it either reschedules the task with the
// given backoff (pending, next_attempt_at = now + backoff) or, once attempts
// reach the effective max, moves it to the dead-letter state. permanent forces
// dead immediately (e.g. an unknown channel). Returns the resulting status.
func (db *DB) FailTask(ctx context.Context, taskID, errMsg string, backoff time.Duration, defaultMax int, permanent bool) (types.TaskStatus, error) {
	var result types.TaskStatus
	err := db.WithTx(ctx, func(ctx context.Context) error {
		task, err := db.getTaskByID(ctx, taskID)
		if err != nil {
			return err
		}
		max := task.MaxAttempts
		if max <= 0 {
			max = defaultMax
		}
		now := time.Now().UTC()
		if permanent || task.Attempts >= max {
			result = types.TaskDead
			_, err = db.execContext(ctx,
				`UPDATE fulfillment_tasks SET status='dead', last_error=?, updated_at=? WHERE id=?`,
				truncErr(errMsg), now, taskID)
		} else {
			// Transient failure with budget remaining: rest in 'failed' and let
			// the dispatcher re-pick it once next_attempt_at is due.
			result = types.TaskFailed
			_, err = db.execContext(ctx,
				`UPDATE fulfillment_tasks SET status='failed', last_error=?, next_attempt_at=?, updated_at=? WHERE id=?`,
				truncErr(errMsg), now.Add(backoff), now, taskID)
		}
		if err != nil {
			return gkerr.New(gkerr.ReasonInternal, "fail task").Wrap(err)
		}
		return nil
	})
	return result, err
}

// AwaitCallback leaves an external_workflow task in processing after a
// successful hand-off, recording any interim receipt. The dispatcher will not
// re-pick it until the staleAfter window elapses; normally the n8n callback
// completes it first.
func (db *DB) AwaitCallback(ctx context.Context, taskID string, receipt json.RawMessage) error {
	_, err := db.execContext(ctx,
		`UPDATE fulfillment_tasks SET status='processing', receipt=?, last_error='', updated_at=? WHERE id=?`,
		string(nonEmptyJSON(receipt)), time.Now().UTC(), taskID)
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "await callback").Wrap(err)
	}
	return nil
}

// --- admin queries (scope-filtered) ---

// GetTask fetches a task by id within scope.
func (db *DB) GetTask(ctx context.Context, scope types.Scope, taskID string) (*types.FulfillmentTask, error) {
	var row taskRow
	err := db.getContext(ctx, &row,
		`SELECT `+taskCols+` FROM fulfillment_tasks WHERE id=? AND tenant_id=? AND merchant_id=?`,
		taskID, scope.TenantID, scope.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "fulfillment task not found").WithMeta("task_id", taskID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load task").Wrap(err)
	}
	t := row.toDomain()
	return &t, nil
}

// FindTask loads a task by its globally-unique id without a scope filter. Used
// by the machine callback path (n8n reports a result for a task id it was given;
// the task id is opaque and unguessable, and the BFF verifies the HMAC).
func (db *DB) FindTask(ctx context.Context, taskID string) (*types.FulfillmentTask, error) {
	return db.getTaskByID(ctx, taskID)
}

// getTaskByID loads a task by primary key only (dispatcher path; no scope).
func (db *DB) getTaskByID(ctx context.Context, taskID string) (*types.FulfillmentTask, error) {
	var row taskRow
	err := db.getContext(ctx, &row, `SELECT `+taskCols+` FROM fulfillment_tasks WHERE id=?`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "fulfillment task not found").WithMeta("task_id", taskID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load task").Wrap(err)
	}
	t := row.toDomain()
	return &t, nil
}

// TaskFilter narrows the admin task list.
type TaskFilter struct {
	Status     string
	CampaignID string
	PrizeID    string
}

// ListTasks returns outbox tasks for admin review (newest first, cursor by
// created_at), filtered by status/campaign/prize within scope.
func (db *DB) ListTasks(ctx context.Context, scope types.Scope, f TaskFilter, limit int, cursor string) ([]types.FulfillmentTask, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	where := `tenant_id=? AND merchant_id=?`
	args := []any{scope.TenantID, scope.MerchantID}
	if f.Status != "" {
		where += ` AND status=?`
		args = append(args, f.Status)
	}
	if f.CampaignID != "" {
		where += ` AND campaign_id=?`
		args = append(args, f.CampaignID)
	}
	if f.PrizeID != "" {
		where += ` AND prize_id=?`
		args = append(args, f.PrizeID)
	}
	if cursor != "" {
		if t, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			where += ` AND created_at < ?`
			args = append(args, t)
		}
	}
	args = append(args, limit+1)
	var rows []taskRow
	if err := db.selectContext(ctx, &rows,
		`SELECT `+taskCols+` FROM fulfillment_tasks WHERE `+where+` ORDER BY created_at DESC LIMIT ?`, args...); err != nil {
		return nil, "", gkerr.New(gkerr.ReasonInternal, "list tasks").Wrap(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].CreatedAt.Format(time.RFC3339Nano)
		rows = rows[:limit]
	}
	out := make([]types.FulfillmentTask, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, next, nil
}

// RetryTask re-arms a failed or dead task: status -> pending, attempts reset to
// 0, due immediately. A pending/processing/fulfilled task cannot be retried.
func (db *DB) RetryTask(ctx context.Context, scope types.Scope, taskID string) (*types.FulfillmentTask, error) {
	now := time.Now().UTC()
	res, err := db.execContext(ctx,
		`UPDATE fulfillment_tasks SET status='pending', attempts=0, last_error='', next_attempt_at=?, updated_at=?
		  WHERE id=? AND tenant_id=? AND merchant_id=? AND status IN ('failed','dead')`,
		now, now, taskID, scope.TenantID, scope.MerchantID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "retry task").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, gErr := db.GetTask(ctx, scope, taskID); gErr != nil {
			return nil, gErr
		}
		return nil, gkerr.New(gkerr.ReasonTaskBadState, "only failed or dead tasks can be retried").WithMeta("task_id", taskID)
	}
	return db.GetTask(ctx, scope, taskID)
}

func truncErr(s string) string {
	const max = 1000
	if len(s) > max {
		return s[:max]
	}
	return s
}
