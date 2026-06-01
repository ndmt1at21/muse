package grpcsvc

import (
	"context"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// --- QuestService: admin CRUD (merchant-scoped) + player list/complete ---

func (s *Server) CreateQuest(ctx context.Context, req *gamev1.CreateQuestRequest) (*gamev1.CreateQuestResponse, error) {
	scope := scopeFromProto(req)
	if scope.TenantID == "" || scope.MerchantID == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "tenant_id and merchant_id are required"))
	}
	q := questFromProto(scope, req.GetQuest())
	if _, err := s.reg.Verifier(q.Type); err != nil {
		return nil, s.fail(gkerr.Newf(gkerr.ReasonValidationFailed, "unknown quest type %q", q.Type))
	}
	saved, err := s.store.CreateQuest(ctx, q)
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.CreateQuestResponse{Quest: questToProto(saved)}, nil
}

func (s *Server) GetQuest(ctx context.Context, req *gamev1.GetQuestRequest) (*gamev1.GetQuestResponse, error) {
	scope := scopeFromProto(req)
	q, err := s.store.GetQuest(ctx, scope.TenantID, scope.MerchantID, req.GetQuestId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetQuestResponse{Quest: questToProto(q)}, nil
}

func (s *Server) UpdateQuest(ctx context.Context, req *gamev1.UpdateQuestRequest) (*gamev1.UpdateQuestResponse, error) {
	scope := scopeFromProto(req)
	if scope.TenantID == "" || scope.MerchantID == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "tenant_id and merchant_id are required"))
	}
	q := questFromProto(scope, req.GetQuest())
	if _, err := s.reg.Verifier(q.Type); err != nil {
		return nil, s.fail(gkerr.Newf(gkerr.ReasonValidationFailed, "unknown quest type %q", q.Type))
	}
	saved, err := s.store.UpdateQuest(ctx, q)
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.UpdateQuestResponse{Quest: questToProto(saved)}, nil
}

func (s *Server) DeleteQuest(ctx context.Context, req *gamev1.DeleteQuestRequest) (*gamev1.DeleteQuestResponse, error) {
	scope := scopeFromProto(req)
	if err := s.store.DeleteQuest(ctx, scope.TenantID, scope.MerchantID, req.GetQuestId()); err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.DeleteQuestResponse{}, nil
}

func (s *Server) ListQuests(ctx context.Context, req *gamev1.ListQuestsRequest) (*gamev1.ListQuestsResponse, error) {
	scope := scopeFromProto(req)
	qs, next, err := s.store.ListQuests(ctx, scope.TenantID, scope.MerchantID, req.GetCampaignId(), int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.Quest, 0, len(qs))
	for i := range qs {
		out = append(out, questToProto(&qs[i]))
	}
	return &gamev1.ListQuestsResponse{Quests: out, NextCursor: next}, nil
}

// ListPlayerQuests returns a campaign's quests with the caller's completion
// state (lifetime count, last completion, and whether they may complete now).
func (s *Server) ListPlayerQuests(ctx context.Context, req *gamev1.ListPlayerQuestsRequest) (*gamev1.ListPlayerQuestsResponse, error) {
	scope := scopeFromProto(req)
	playerID := req.GetPlayerId()
	qs, _, err := s.store.ListQuests(ctx, scope.TenantID, scope.MerchantID, req.GetCampaignId(), 100, "")
	if err != nil {
		return nil, s.fail(err)
	}
	now := s.clock.Now()
	out := make([]*gamev1.QuestState, 0, len(qs))
	for i := range qs {
		q := &qs[i]
		st := &gamev1.QuestState{Quest: questToProto(q)}
		if playerID != "" {
			total, today, last, cErr := s.store.CountCompletions(ctx, scope.TenantID, q.ID, playerID, startOfUTCDay(now))
			if cErr != nil {
				return nil, s.fail(cErr)
			}
			st.Completions = int32(total)
			st.LastCompleted = tsPtr(last)
			done := total > 0
			if q.IsRepeatable() {
				done = today > 0 // daily: "completed" means done today
			}
			st.Completed = done
		}
		out = append(out, st)
	}
	return &gamev1.ListPlayerQuestsResponse{Quests: out}, nil
}

// CompleteQuest verifies the proof, enforces the re-completion gate, and — on
// success, inside one transaction — records the completion and grants the
// reward turns to the player's campaign turn balance.
func (s *Server) CompleteQuest(ctx context.Context, req *gamev1.CompleteQuestRequest) (*gamev1.CompleteQuestResponse, error) {
	scope := scopeFromProto(req)
	playerID := req.GetPlayerId()
	if playerID == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonUnauthenticated, "player_id is required"))
	}
	q, err := s.store.GetQuest(ctx, scope.TenantID, scope.MerchantID, req.GetQuestId())
	if err != nil {
		return nil, s.fail(err)
	}
	if q.Status != types.QuestActive {
		return nil, s.fail(gkerr.New(gkerr.ReasonGameNotActive, "quest is not active"))
	}

	now := s.clock.Now()
	total, today, _, err := s.store.CountCompletions(ctx, scope.TenantID, q.ID, playerID, startOfUTCDay(now))
	if err != nil {
		return nil, s.fail(err)
	}
	if q.IsRepeatable() {
		if today > 0 {
			return nil, s.fail(gkerr.New(gkerr.ReasonAlreadyExists, "quest already completed today"))
		}
	} else if total > 0 {
		return nil, s.fail(gkerr.New(gkerr.ReasonAlreadyExists, "quest already completed"))
	}

	verifier, err := s.reg.Verifier(q.Type)
	if err != nil {
		return nil, s.fail(err)
	}
	if err := verifier.Verify(ctx, q, types.Payload(req.GetProof())); err != nil {
		return nil, s.fail(err)
	}

	grant := int64(0)
	if q.Reward.Type == types.QuestRewardPlayTurn {
		grant = q.Reward.Quantity
	}
	scopeKey := questTurnKey(scope, q)

	var balance int64
	txErr := s.store.WithTx(ctx, func(ctx context.Context) error {
		if rErr := s.store.RecordCompletion(ctx, &types.QuestCompletion{
			TenantID: scope.TenantID, QuestID: q.ID, PlayerID: playerID, CreatedAt: now,
		}); rErr != nil {
			return rErr
		}
		if grant > 0 {
			bal, gErr := s.store.GrantTurns(ctx, scope.TenantID, playerID, scopeKey, grant)
			if gErr != nil {
				return gErr
			}
			balance = bal
		}
		return nil
	})
	if txErr != nil {
		return nil, s.fail(txErr)
	}
	if grant == 0 {
		// No turns granted (or non-turn reward): read the current balance for the response.
		balance, _ = s.store.GetTurnBalance(ctx, scope.TenantID, playerID, scopeKey)
	}

	// Best-effort domain event → integration hub (Phase 10).
	s.emit(ctx, types.EventQuestCompleted, scope, map[string]any{
		"quest_id": q.ID, "campaign_id": q.CampaignID, "player_id": playerID, "turns_granted": grant,
	})
	return &gamev1.CompleteQuestResponse{Completed: true, TurnsGranted: grant, TurnBalance: balance}, nil
}

// questTurnKey is the turn-balance scope key for a quest grant: the campaign
// (default wallet scope), falling back to merchant then tenant — mirroring the
// engine's engineTurnKey so a turn-gated game spends what the quest granted.
func questTurnKey(scope types.Scope, q *types.Quest) string {
	if q.CampaignID != "" {
		return q.CampaignID
	}
	if scope.MerchantID != "" {
		return scope.MerchantID
	}
	return scope.TenantID
}

func startOfUTCDay(t time.Time) time.Time {
	u := t.UTC()
	y, m, d := u.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// --- converters ---

func questFromProto(scope types.Scope, p *gamev1.Quest) *types.Quest {
	if p == nil {
		return &types.Quest{TenantID: scope.TenantID, MerchantID: scope.MerchantID}
	}
	q := &types.Quest{
		ID:         p.GetId(),
		TenantID:   scope.TenantID,
		MerchantID: scope.MerchantID,
		CampaignID: p.GetCampaignId(),
		Type:       domQuestType(p.GetType()),
		Name:       p.GetName(),
		Status:     domQuestStatus(p.GetStatus()),
		Config:     []byte(p.GetConfig()),
	}
	if r := p.GetReward(); r != nil {
		q.Reward = types.QuestReward{Type: domQuestRewardType(r.GetType()), Quantity: r.GetQuantity()}
	}
	return q
}

func questToProto(q *types.Quest) *gamev1.Quest {
	return &gamev1.Quest{
		Id:         q.ID,
		TenantId:   q.TenantID,
		MerchantId: q.MerchantID,
		CampaignId: q.CampaignID,
		Type:       pbQuestType(q.Type),
		Name:       q.Name,
		Status:     pbQuestStatus(q.Status),
		Reward:     &gamev1.QuestReward{Type: pbQuestRewardType(q.Reward.Type), Quantity: q.Reward.Quantity},
		Config:     string(q.Config),
		CreatedAt:  ts(q.CreatedAt),
		UpdatedAt:  ts(q.UpdatedAt),
	}
}
