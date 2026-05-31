// Package verifiers holds the built-in QuestVerifier implementations (Phase 6),
// one per quest type. A verifier validates the player's completion proof against
// the quest's config and returns nil to accept or a VALIDATION_FAILED gkerr to
// reject. They are pure: completion gating and the reward grant are the hosting
// layer's job. Proofs whose authenticity can only be confirmed out-of-band
// (social shares, external events) are accepted on a well-formed proof here and
// reconciled by the integration hub later — these are the dev-stub seams.
package verifiers

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

func decode(quest *types.Quest, proof types.Payload, cfg, p any) error {
	if len(quest.Config) > 0 && cfg != nil {
		if err := json.Unmarshal(quest.Config, cfg); err != nil {
			return gkerr.Newf(gkerr.ReasonValidationFailed, "invalid %s quest config", quest.Type).Wrap(err)
		}
	}
	if len(proof) > 0 && p != nil {
		if err := json.Unmarshal(proof, p); err != nil {
			return gkerr.New(gkerr.ReasonValidationFailed, "malformed proof").Wrap(err)
		}
	}
	return nil
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

// DailyCheckin needs no proof — showing up is the action. The once-per-day gate
// is enforced by the hosting layer's completion records.
type DailyCheckin struct{}

func (DailyCheckin) Verify(ctx context.Context, quest *types.Quest, proof types.Payload) error {
	return nil
}

// ShareSocial expects {platform, share_id}. If the quest config lists allowed
// platforms, the proof's platform must be one of them. share_id must be present
// (its authenticity is reconciled by the integration hub).
type ShareSocial struct{}

func (ShareSocial) Verify(ctx context.Context, quest *types.Quest, proof types.Payload) error {
	var cfg struct {
		Platforms []string `json:"platforms"`
	}
	var p struct {
		Platform string `json:"platform"`
		ShareID  string `json:"share_id"`
	}
	if err := decode(quest, proof, &cfg, &p); err != nil {
		return err
	}
	if p.ShareID == "" {
		return gkerr.New(gkerr.ReasonValidationFailed, "share_id is required")
	}
	if len(cfg.Platforms) > 0 && !contains(cfg.Platforms, p.Platform) {
		return gkerr.Newf(gkerr.ReasonValidationFailed, "platform %q not allowed for this quest", p.Platform)
	}
	return nil
}

// InviteFriend expects {invitee} — a contact/id of the invited person.
type InviteFriend struct{}

func (InviteFriend) Verify(ctx context.Context, quest *types.Quest, proof types.Payload) error {
	var p struct {
		Invitee string `json:"invitee"`
	}
	if err := decode(quest, proof, nil, &p); err != nil {
		return err
	}
	if strings.TrimSpace(p.Invitee) == "" {
		return gkerr.New(gkerr.ReasonValidationFailed, "invitee is required")
	}
	return nil
}

// ScanQR expects {code}. If the quest config pins an expected code, the proof
// must match it; otherwise any non-empty code is accepted.
type ScanQR struct{}

func (ScanQR) Verify(ctx context.Context, quest *types.Quest, proof types.Payload) error {
	var cfg struct {
		Code string `json:"code"`
	}
	var p struct {
		Code string `json:"code"`
	}
	if err := decode(quest, proof, &cfg, &p); err != nil {
		return err
	}
	if p.Code == "" {
		return gkerr.New(gkerr.ReasonValidationFailed, "code is required")
	}
	if cfg.Code != "" && cfg.Code != p.Code {
		return gkerr.New(gkerr.ReasonValidationFailed, "qr code does not match")
	}
	return nil
}

// ViewPage expects {page}. If the quest config pins an expected page, the proof
// must match; otherwise any non-empty page is accepted (dwell time is trusted).
type ViewPage struct{}

func (ViewPage) Verify(ctx context.Context, quest *types.Quest, proof types.Payload) error {
	var cfg struct {
		Page string `json:"page"`
	}
	var p struct {
		Page string `json:"page"`
	}
	if err := decode(quest, proof, &cfg, &p); err != nil {
		return err
	}
	if p.Page == "" {
		return gkerr.New(gkerr.ReasonValidationFailed, "page is required")
	}
	if cfg.Page != "" && cfg.Page != p.Page {
		return gkerr.New(gkerr.ReasonValidationFailed, "page does not match")
	}
	return nil
}

// AnswerQuestion expects {answers:[...]}. The quest config holds the expected
// answers (order-sensitive, case-insensitive); the proof must match exactly.
type AnswerQuestion struct{}

func (AnswerQuestion) Verify(ctx context.Context, quest *types.Quest, proof types.Payload) error {
	var cfg struct {
		Answers []string `json:"answers"`
	}
	var p struct {
		Answers []string `json:"answers"`
	}
	if err := decode(quest, proof, &cfg, &p); err != nil {
		return err
	}
	if len(cfg.Answers) == 0 {
		return gkerr.New(gkerr.ReasonValidationFailed, "quest has no configured answers")
	}
	if len(p.Answers) != len(cfg.Answers) {
		return gkerr.New(gkerr.ReasonValidationFailed, "incorrect answers")
	}
	for i := range cfg.Answers {
		if !strings.EqualFold(strings.TrimSpace(p.Answers[i]), strings.TrimSpace(cfg.Answers[i])) {
			return gkerr.New(gkerr.ReasonValidationFailed, "incorrect answers")
		}
	}
	return nil
}

// ExternalEvent expects {event_id} — a reference an external system emitted.
// Presence is accepted here; the integration hub reconciles authenticity.
type ExternalEvent struct{}

func (ExternalEvent) Verify(ctx context.Context, quest *types.Quest, proof types.Payload) error {
	var p struct {
		EventID string `json:"event_id"`
	}
	if err := decode(quest, proof, nil, &p); err != nil {
		return err
	}
	if strings.TrimSpace(p.EventID) == "" {
		return gkerr.New(gkerr.ReasonValidationFailed, "event_id is required")
	}
	return nil
}
