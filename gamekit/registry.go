package gamekit

import (
	"context"
	"fmt"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
)

// HandlerDeps gives a handler the ports it needs (prizes, randomness, clock)
// without coupling it to a concrete store. Handlers stay pure-ish: they decide
// outcomes; the engine performs persistence (stock deduction) by routing on
// the returned reward types.
type HandlerDeps struct {
	Prizes ports.PrizeStore
	Rand   ports.RandSource
	Clock  ports.Clock
}

// RewardHandler decides the rewards for a play given the game config, the
// session seed, and the client payload. It is the per-shape extension point —
// adding a game shape = registering one handler. It MUST NOT mutate stock; it
// returns candidate rewards and the engine deducts atomically.
type RewardHandler interface {
	Evaluate(ctx context.Context, deps HandlerDeps, game *types.Game, seed types.SeedData, payload types.Payload) (types.RewardResult, error)
}

// SeedGenerator produces the server-authoritative render seed at Start
// (e.g. a drop sequence the client cannot invent).
type SeedGenerator interface {
	Generate(ctx context.Context, deps HandlerDeps, game *types.Game, session *types.Session) (types.SeedData, error)
}

// Validator runs anti-cheat checks at Play before the handler decides rewards.
type Validator interface {
	Validate(ctx context.Context, game *types.Game, session *types.Session, payload types.Payload) error
}

// QuestVerifier checks a player's type-specific completion proof for a quest
// (Phase 6). It is the per-quest-type extension point — adding a quest type =
// registering one verifier. It is pure: it validates the proof against the
// quest's config and returns nil to accept or a VALIDATION_FAILED gkerr to
// reject. Completion gating (once / once-per-day) and the reward grant are the
// hosting layer's job, not the verifier's.
type QuestVerifier interface {
	Verify(ctx context.Context, quest *types.Quest, proof types.Payload) error
}

// Registry is a string-keyed, pluggable registry of the extension points.
// Adding a game shape or quest type never touches the engine — it registers
// impls here.
type Registry struct {
	handlers   map[string]RewardHandler
	seeds      map[string]SeedGenerator
	validators map[string]Validator
	verifiers  map[string]QuestVerifier
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers:   map[string]RewardHandler{},
		seeds:      map[string]SeedGenerator{},
		validators: map[string]Validator{},
		verifiers:  map[string]QuestVerifier{},
	}
}

// RegisterHandler registers a reward handler under a key. Panics on duplicate
// (registration is startup-time wiring; a dup is a programming error).
func (r *Registry) RegisterHandler(key string, h RewardHandler) {
	if _, ok := r.handlers[key]; ok {
		panic(fmt.Sprintf("gamekit: duplicate reward handler %q", key))
	}
	r.handlers[key] = h
}

// RegisterSeed registers a seed generator under a key.
func (r *Registry) RegisterSeed(key string, s SeedGenerator) {
	if _, ok := r.seeds[key]; ok {
		panic(fmt.Sprintf("gamekit: duplicate seed generator %q", key))
	}
	r.seeds[key] = s
}

// RegisterValidator registers a validator under a key.
func (r *Registry) RegisterValidator(key string, v Validator) {
	if _, ok := r.validators[key]; ok {
		panic(fmt.Sprintf("gamekit: duplicate validator %q", key))
	}
	r.validators[key] = v
}

// RegisterVerifier registers a quest verifier under a quest-type key.
func (r *Registry) RegisterVerifier(key string, v QuestVerifier) {
	if _, ok := r.verifiers[key]; ok {
		panic(fmt.Sprintf("gamekit: duplicate quest verifier %q", key))
	}
	r.verifiers[key] = v
}

// Handler resolves a reward handler by key.
func (r *Registry) Handler(key string) (RewardHandler, error) {
	h, ok := r.handlers[key]
	if !ok {
		return nil, gkerr.Newf(gkerr.ReasonHandlerNotFound, "unknown reward handler %q", key)
	}
	return h, nil
}

// Seed resolves a seed generator by key. An empty key resolves to "none".
func (r *Registry) Seed(key string) (SeedGenerator, error) {
	if key == "" {
		key = "none"
	}
	s, ok := r.seeds[key]
	if !ok {
		return nil, gkerr.Newf(gkerr.ReasonHandlerNotFound, "unknown seed generator %q", key)
	}
	return s, nil
}

// Validator resolves a validator by key. An empty key resolves to "basic".
func (r *Registry) Validator(key string) (Validator, error) {
	if key == "" {
		key = "basic"
	}
	v, ok := r.validators[key]
	if !ok {
		return nil, gkerr.Newf(gkerr.ReasonHandlerNotFound, "unknown validator %q", key)
	}
	return v, nil
}

// Verifier resolves a quest verifier by quest type.
func (r *Registry) Verifier(questType string) (QuestVerifier, error) {
	v, ok := r.verifiers[questType]
	if !ok {
		return nil, gkerr.Newf(gkerr.ReasonHandlerNotFound, "unknown quest type %q", questType)
	}
	return v, nil
}
