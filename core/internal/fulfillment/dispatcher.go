package fulfillment

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/muse/gamekit/types"
)

// Queue is the outbox surface the dispatcher drives. sqlstore.DB implements it.
type Queue interface {
	// ClaimDueTasks atomically grabs up to limit runnable tasks, marks them
	// processing, and returns them (also reclaiming processing tasks stuck
	// longer than staleAfter).
	ClaimDueTasks(ctx context.Context, limit int, staleAfter time.Duration) ([]types.FulfillmentTask, error)
	// CompleteTask marks a task fulfilled and flips its reward to fulfilled.
	CompleteTask(ctx context.Context, taskID string, receipt json.RawMessage) error
	// FailTask reschedules with backoff or dead-letters once the budget is spent.
	FailTask(ctx context.Context, taskID, errMsg string, backoff time.Duration, defaultMax int, permanent bool) (types.TaskStatus, error)
	// AwaitCallback parks an external_workflow task in processing post hand-off.
	AwaitCallback(ctx context.Context, taskID string, receipt json.RawMessage) error
}

// Config tunes the dispatcher loop.
type Config struct {
	Interval           time.Duration        // poll period (default 2s)
	BatchSize          int                  // tasks claimed per tick (default 50)
	DefaultMaxAttempts int                  // used when a task has no per-prize max (default 5)
	BaseBackoff        time.Duration        // first retry delay (default 30s)
	MaxBackoff         time.Duration        // backoff ceiling (default 1h)
	ProcessingStale    time.Duration        // reclaim stuck processing tasks after this (default 15m)
	OnOutcome          func(outcome string) // optional metrics hook: delivered|awaiting|retry|dead
}

func (c *Config) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 2 * time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 50
	}
	if c.DefaultMaxAttempts <= 0 {
		c.DefaultMaxAttempts = 5
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 30 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = time.Hour
	}
	if c.ProcessingStale <= 0 {
		c.ProcessingStale = 15 * time.Minute
	}
}

// Dispatcher drains the outbox and delivers tasks via their channel provider.
type Dispatcher struct {
	q   Queue
	reg *Registry
	log *slog.Logger
	cfg Config
}

// NewDispatcher builds a dispatcher. log may be nil.
func NewDispatcher(q Queue, reg *Registry, cfg Config, log *slog.Logger) *Dispatcher {
	cfg.applyDefaults()
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{q: q, reg: reg, log: log, cfg: cfg}
}

// Run polls until ctx is cancelled, draining due tasks each tick.
func (d *Dispatcher) Run(ctx context.Context) {
	t := time.NewTicker(d.cfg.Interval)
	defer t.Stop()
	d.log.Info("fulfillment dispatcher started", "interval", d.cfg.Interval, "batch", d.cfg.BatchSize)
	for {
		select {
		case <-ctx.Done():
			d.log.Info("fulfillment dispatcher stopped")
			return
		case <-t.C:
			if _, err := d.Drain(ctx); err != nil {
				d.log.Error("dispatcher drain failed", "err", err)
			}
		}
	}
}

// Drain processes one batch of due tasks and returns how many it handled. Useful
// directly in tests; Run calls it on each tick.
func (d *Dispatcher) Drain(ctx context.Context) (int, error) {
	tasks, err := d.q.ClaimDueTasks(ctx, d.cfg.BatchSize, d.cfg.ProcessingStale)
	if err != nil {
		return 0, err
	}
	for i := range tasks {
		d.process(ctx, &tasks[i])
	}
	return len(tasks), nil
}

func (d *Dispatcher) process(ctx context.Context, task *types.FulfillmentTask) {
	provider, ok := d.reg.Get(task.Channel)
	if !ok {
		d.finishFail(ctx, task, "no provider registered for channel "+task.Channel, true)
		return
	}
	res := provider.Deliver(ctx, task)
	switch res.Outcome {
	case Delivered:
		if err := d.q.CompleteTask(ctx, task.ID, res.Receipt); err != nil {
			d.log.Error("complete task failed", "task_id", task.ID, "err", err)
			return
		}
		d.log.Info("task fulfilled", "task_id", task.ID, "channel", task.Channel)
		d.outcome("delivered")
	case AwaitingCallback:
		if err := d.q.AwaitCallback(ctx, task.ID, res.Receipt); err != nil {
			d.log.Error("await callback failed", "task_id", task.ID, "err", err)
		}
		d.outcome("awaiting")
	case PermanentError:
		d.finishFail(ctx, task, errString(res.Err), true)
		d.outcome("dead")
	default: // RetryableError
		d.finishFail(ctx, task, errString(res.Err), false)
		d.outcome("retry")
	}
}

// outcome reports a delivery outcome to the optional metrics hook.
func (d *Dispatcher) outcome(o string) {
	if d.cfg.OnOutcome != nil {
		d.cfg.OnOutcome(o)
	}
}

func (d *Dispatcher) finishFail(ctx context.Context, task *types.FulfillmentTask, msg string, permanent bool) {
	status, err := d.q.FailTask(ctx, task.ID, msg, d.backoff(task.Attempts), d.cfg.DefaultMaxAttempts, permanent)
	if err != nil {
		d.log.Error("fail task failed", "task_id", task.ID, "err", err)
		return
	}
	if status == types.TaskDead {
		d.log.Warn("task dead-lettered", "task_id", task.ID, "channel", task.Channel, "attempts", task.Attempts, "reason", msg)
	} else {
		d.log.Info("task scheduled for retry", "task_id", task.ID, "attempts", task.Attempts, "reason", msg)
	}
}

// backoff returns the delay before the next attempt: exponential on the attempt
// count (which already includes the just-finished try), capped at MaxBackoff.
func (d *Dispatcher) backoff(attempts int) time.Duration {
	b := d.cfg.BaseBackoff
	for i := 1; i < attempts && b < d.cfg.MaxBackoff; i++ {
		b *= 2
	}
	if b > d.cfg.MaxBackoff || b <= 0 {
		b = d.cfg.MaxBackoff
	}
	return b
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
