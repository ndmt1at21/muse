// Package events is the internal event bus over Redis pub/sub (Phase 10). The
// integration Hub publishes every domain event here so any process — another
// Core replica, the realtime gateway (Phase 9.5), an integration consumer — can
// react without coupling to the emitter. It is best-effort fan-out, never the
// durable record (SQL stays the source of truth); a publish failure must not
// fail the originating operation.
package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/muse/gamekit/ports"
	"github.com/redis/go-redis/v9"
)

// Bus publishes/subscribes domain events on a single Redis channel. Pub/sub is
// lossy by design (no delivery guarantee, no replay) — appropriate for live
// fan-out, not for money/stock.
type Bus struct {
	rdb     *redis.Client
	channel string
}

// Open dials Redis at addr and binds the bus to "<prefix>:events".
func Open(ctx context.Context, addr, prefix string) (*Bus, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("events: ping %s: %w", addr, err)
	}
	return &Bus{rdb: rdb, channel: prefix + ":events"}, nil
}

// Close closes the underlying client.
func (b *Bus) Close() error { return b.rdb.Close() }

// Publish broadcasts an event as JSON. Best-effort: returns an error for the
// caller to log, but callers must not propagate it into the domain operation.
func (b *Bus) Publish(ctx context.Context, evt ports.Event) error {
	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("events: marshal: %w", err)
	}
	if err := b.rdb.Publish(ctx, b.channel, payload).Err(); err != nil {
		return fmt.Errorf("events: publish: %w", err)
	}
	return nil
}

// Subscribe streams events until ctx is cancelled. Each received message is
// decoded and sent on the returned channel; decode failures are skipped. The
// returned channel is closed when ctx ends or the subscription drops. (Used by
// the realtime gateway and out-of-process integration consumers.)
func (b *Bus) Subscribe(ctx context.Context) (<-chan ports.Event, error) {
	sub := b.rdb.Subscribe(ctx, b.channel)
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, fmt.Errorf("events: subscribe: %w", err)
	}
	out := make(chan ports.Event)
	go func() {
		defer close(out)
		defer sub.Close()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var evt ports.Event
				if json.Unmarshal([]byte(msg.Payload), &evt) != nil {
					continue
				}
				select {
				case out <- evt:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}
