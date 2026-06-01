package grpcsvc

import (
	"encoding/json"
	"time"

	gamev1 "github.com/muse/pkg/gen/game/v1"

	"github.com/muse/gamekit/types"
	"google.golang.org/protobuf/types/known/structpb"
)

// scoped is satisfied by every request/entity proto that carries the flat
// tenant_id + merchant_id isolation keys.
type scoped interface {
	GetTenantId() string
	GetMerchantId() string
}

// scopeFromProto reads the flat tenant/merchant isolation keys off any proto
// that carries them.
func scopeFromProto(s scoped) types.Scope {
	if s == nil {
		return types.Scope{}
	}
	return types.Scope{TenantID: s.GetTenantId(), MerchantID: s.GetMerchantId()}
}

// ts converts a domain time to a unix-seconds timestamp (0 for the zero time).
func ts(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// ptime converts a unix-seconds timestamp to a domain time (nil when 0/unset).
func ptime(unix int64) *time.Time {
	if unix == 0 {
		return nil
	}
	v := time.Unix(unix, 0).UTC()
	return &v
}

// gameFromProto converts an inbound proto Game (admin create) to the domain.
func gameFromProto(scope types.Scope, g *gamev1.Game) *types.Game {
	if g == nil {
		return &types.Game{Scope: scope}
	}
	out := &types.Game{
		ID:              g.GetId(),
		Scope:           scope,
		CampaignID:      g.GetCampaignId(),
		Name:            g.GetName(),
		Type:            g.GetType(),
		SeedGenerator:   g.GetSeedGenerator(),
		RewardHandler:   g.GetRewardHandler(),
		Validator:       g.GetValidator(),
		Status:          domGameStatus(g.GetStatus()),
		HandlerConfig:   json.RawMessage(g.GetHandlerConfig()),
		ValidatorConfig: json.RawMessage(g.GetValidatorConfig()),
		UI:              json.RawMessage(g.GetUi()),
		WalletScope:     domWalletScope(g.GetWalletScope()),
		Milestones:      json.RawMessage(g.GetMilestones()),
	}
	if r := g.GetRules(); r != nil {
		out.Rules = types.Rules{
			MaxPlaysPerUser: int(r.GetMaxPlaysPerUser()),
			MaxPlaysPerDay:  int(r.GetMaxPlaysPerDay()),
			RequireLogin:    r.GetRequireLogin(),
			StartDate:       ptime(r.GetStartDate()),
			EndDate:         ptime(r.GetEndDate()),
			UseTurns:        r.GetUseTurns(),
		}
	}
	return out
}

// gameToProto converts a domain Game to proto.
func gameToProto(g *types.Game) *gamev1.Game {
	return &gamev1.Game{
		Id:            g.ID,
		TenantId:      g.Scope.TenantID,
		MerchantId:    g.Scope.MerchantID,
		CampaignId:    g.CampaignID,
		Name:          g.Name,
		Type:          g.Type,
		SeedGenerator: g.SeedGenerator,
		RewardHandler: g.RewardHandler,
		Validator:     g.Validator,
		Status:        pbGameStatus(g.Status),
		WalletScope:   pbWalletScope(g.WalletScope),
		Rules: &gamev1.Rules{
			MaxPlaysPerUser: int32(g.Rules.MaxPlaysPerUser),
			MaxPlaysPerDay:  int32(g.Rules.MaxPlaysPerDay),
			RequireLogin:    g.Rules.RequireLogin,
			StartDate:       tsPtr(g.Rules.StartDate),
			EndDate:         tsPtr(g.Rules.EndDate),
			UseTurns:        g.Rules.UseTurns,
		},
		HandlerConfig:   string(g.HandlerConfig),
		ValidatorConfig: string(g.ValidatorConfig),
		Ui:              string(g.UI),
		Milestones:      string(g.Milestones),
		CreatedAt:       ts(g.CreatedAt),
		UpdatedAt:       ts(g.UpdatedAt),
	}
}

// tsPtr converts an optional domain time to a unix-seconds timestamp (0 when nil).
func tsPtr(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.Unix()
}

func prizeFromProto(scope types.Scope, p *gamev1.Prize) *types.Prize {
	out := &types.Prize{
		ID:        p.GetId(),
		Scope:     scope,
		Name:      p.GetName(),
		Type:      p.GetType(),
		Image:     p.GetImage(),
		Value:     p.GetValue(),
		Total:     p.GetTotal(),
		Remaining: p.GetRemaining(),
		Metadata:  json.RawMessage(p.GetMetadata()),
	}
	if s := p.GetAwardConstraints(); s != "" {
		_ = json.Unmarshal([]byte(s), &out.Constraints)
	}
	if s := p.GetFulfillment(); s != "" {
		_ = json.Unmarshal([]byte(s), &out.Fulfillment)
	}
	return out
}

func prizeToProto(p *types.Prize) *gamev1.Prize {
	cstr, _ := json.Marshal(p.Constraints)
	ful, _ := json.Marshal(p.Fulfillment)
	return &gamev1.Prize{
		Id:               p.ID,
		TenantId:         p.Scope.TenantID,
		MerchantId:       p.Scope.MerchantID,
		Name:             p.Name,
		Type:             p.Type,
		Image:            p.Image,
		Value:            p.Value,
		Total:            p.Total,
		Remaining:        p.Remaining,
		Metadata:         string(p.Metadata),
		AwardConstraints: string(cstr),
		Fulfillment:      string(ful),
	}
}

func rewardToProto(r *types.RewardRecord) *gamev1.RewardRecord {
	return &gamev1.RewardRecord{
		Id:          r.ID,
		TenantId:    r.Scope.TenantID,
		MerchantId:  r.Scope.MerchantID,
		GameId:      r.GameID,
		PlayerId:    r.PlayerID,
		PrizeId:     r.PrizeID,
		PlayId:      r.PlayID,
		Name:        r.Name,
		Type:        r.Type,
		Value:       r.Value,
		Code:        r.Code,
		Status:      pbRewardStatus(r.Status),
		CreatedAt:   ts(r.CreatedAt),
		ClaimedAt:   tsPtr(r.ClaimedAt),
		FulfilledAt: tsPtr(r.FulfilledAt),
		RevokedAt:   tsPtr(r.RevokedAt),
	}
}

func taskToProto(t *types.FulfillmentTask) *gamev1.FulfillmentTask {
	return &gamev1.FulfillmentTask{
		Id:            t.ID,
		TenantId:      t.Scope.TenantID,
		MerchantId:    t.Scope.MerchantID,
		RewardId:      t.RewardID,
		PrizeId:       t.PrizeID,
		PlayerId:      t.PlayerID,
		GameId:        t.GameID,
		CampaignId:    t.CampaignID,
		Channel:       t.Channel,
		ChannelConfig: string(t.ChannelConfig),
		Status:        pbTaskStatus(t.Status),
		Attempts:      int32(t.Attempts),
		MaxAttempts:   int32(t.MaxAttempts),
		LastError:     t.LastError,
		Receipt:       string(t.Receipt),
		NextAttemptAt: ts(t.NextAttemptAt),
		CreatedAt:     ts(t.CreatedAt),
		UpdatedAt:     ts(t.UpdatedAt),
	}
}

func rewardsToProto(rs []types.Reward) []*gamev1.Reward {
	out := make([]*gamev1.Reward, 0, len(rs))
	for _, r := range rs {
		out = append(out, &gamev1.Reward{
			PrizeId:  r.PrizeID,
			Name:     r.Name,
			Image:    r.Image,
			Type:     r.Type,
			Quantity: r.Quantity,
			Value:    r.Value,
			RewardId: r.RewardID,
			Code:     r.Code,
		})
	}
	return out
}

// payloadJSON extracts raw JSON bytes from a proto Struct payload.
func payloadJSON(s *structpb.Struct) json.RawMessage {
	if s == nil {
		return json.RawMessage(`{}`)
	}
	b, err := s.MarshalJSON()
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

// metadataToStruct converts handler metadata (map[string]any) to a proto Struct.
func metadataToStruct(m map[string]any) *structpb.Struct {
	if len(m) == 0 {
		return nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		// Fall back to a JSON round-trip for values NewStruct rejects (e.g. ints).
		b, _ := json.Marshal(m)
		var generic map[string]any
		_ = json.Unmarshal(b, &generic)
		s, _ = structpb.NewStruct(generic)
	}
	return s
}
