package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/muse/gamekit/types"
)

// errPoolEmpty is the permanent failure when a voucher_code task finds no codes
// left in the prize's pool (an operator must import more).
var errPoolEmpty = errors.New("voucher code pool is empty")

// CodePool is the slice of the reward store the voucher provider needs: pop one
// available code for a prize, atomically. sqlstore.DB satisfies it via
// PopVoucherCode (which wraps the pooled SELECT ... FOR UPDATE in a txn).
type CodePool interface {
	PopVoucherCode(ctx context.Context, scope types.Scope, prizeID string) (code string, ok bool, err error)
}

// VoucherCodeProvider delivers by popping a code from the prize's imported pool
// and returning it as the receipt. On the normal path voucher codes are handed
// out in-app at win, so this provider mainly serves outbox tasks that were
// created with the voucher_code channel explicitly (or admin-retried). If the
// task already carries a code in its receipt, it is reused (idempotent).
type VoucherCodeProvider struct {
	pool CodePool
}

// NewVoucherCodeProvider builds the provider over a code pool.
func NewVoucherCodeProvider(pool CodePool) *VoucherCodeProvider {
	return &VoucherCodeProvider{pool: pool}
}

func (p *VoucherCodeProvider) Deliver(ctx context.Context, task *types.FulfillmentTask) Result {
	// Idempotency: if a code was already assigned (receipt has one), reuse it.
	if code := receiptString(task.Receipt, "code"); code != "" {
		return DeliveredResult(task.Receipt)
	}
	code, ok, err := p.pool.PopVoucherCode(ctx, task.Scope, task.PrizeID)
	if err != nil {
		return Retry(err)
	}
	if !ok {
		// Pool empty: not transient, an operator must import more codes.
		return Permanent(errPoolEmpty)
	}
	return DeliveredResult(mustJSON(map[string]any{"code": code}))
}

// StubProvider is a logging no-op delivery for channels whose real integration
// is out of scope for this build (sms/zns/email/points_credit/physical_shipping/
// crm_sync/ecommerce). It records a synthetic receipt so the lifecycle and the
// outbox dashboards work end to end; swap in a real Provider to go live.
type StubProvider struct {
	channel string
	log     *slog.Logger
}

// NewStubProvider builds a stub for the named channel.
func NewStubProvider(channel string, log *slog.Logger) *StubProvider {
	return &StubProvider{channel: channel, log: log}
}

func (p *StubProvider) Deliver(ctx context.Context, task *types.FulfillmentTask) Result {
	if p.log != nil {
		p.log.Info("fulfillment stub delivery",
			"channel", p.channel, "task_id", task.ID, "reward_id", task.RewardID, "player_id", task.PlayerID)
	}
	return DeliveredResult(mustJSON(map[string]any{"channel": p.channel, "delivered": true, "stub": true}))
}

// RegisterBuiltins wires the batteries-included providers into the registry:
// voucher_code (real, code-pool backed) plus logging stubs for the remaining
// built-in channels. external_workflow is registered separately by the caller
// (it needs an HTTP client + signing config).
func RegisterBuiltins(reg *Registry, pool CodePool, log *slog.Logger) {
	reg.Register(types.ChannelVoucherCode, NewVoucherCodeProvider(pool))
	for _, ch := range []string{
		types.ChannelSMS, types.ChannelZNS, types.ChannelEmail, types.ChannelPointsCredit,
		types.ChannelPhysicalShipping, types.ChannelCRMSync, types.ChannelEcommerce,
	} {
		reg.Register(ch, NewStubProvider(ch, log))
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func receiptString(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}
