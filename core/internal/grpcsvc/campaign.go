package grpcsvc

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// --- CampaignService (merchant-admin; tenant_id + merchant_id from the scope) ---

func (s *Server) CreateCampaign(ctx context.Context, req *gamev1.CreateCampaignRequest) (*gamev1.CreateCampaignResponse, error) {
	scope := scopeFromProto(req)
	if scope.TenantID == "" || scope.MerchantID == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "tenant_id and merchant_id are required"))
	}
	c, err := s.store.CreateCampaign(ctx, campaignFromProto(scope, req.GetCampaign()))
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.CreateCampaignResponse{Campaign: campaignToProto(c)}, nil
}

func (s *Server) GetCampaign(ctx context.Context, req *gamev1.GetCampaignRequest) (*gamev1.GetCampaignResponse, error) {
	scope := scopeFromProto(req)
	c, err := s.store.GetCampaign(ctx, scope.TenantID, scope.MerchantID, req.GetCampaignId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetCampaignResponse{Campaign: campaignToProto(c)}, nil
}

func (s *Server) UpdateCampaign(ctx context.Context, req *gamev1.UpdateCampaignRequest) (*gamev1.UpdateCampaignResponse, error) {
	scope := scopeFromProto(req)
	if scope.TenantID == "" || scope.MerchantID == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "tenant_id and merchant_id are required"))
	}
	saved, err := s.store.UpdateCampaign(ctx, campaignFromProto(scope, req.GetCampaign()))
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.UpdateCampaignResponse{Campaign: campaignToProto(saved)}, nil
}

func (s *Server) ListCampaigns(ctx context.Context, req *gamev1.ListCampaignsRequest) (*gamev1.ListCampaignsResponse, error) {
	scope := scopeFromProto(req)
	cs, next, err := s.store.ListCampaigns(ctx, scope.TenantID, scope.MerchantID, int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.Campaign, 0, len(cs))
	for i := range cs {
		out = append(out, campaignToProto(&cs[i]))
	}
	return &gamev1.ListCampaignsResponse{Campaigns: out, NextCursor: next}, nil
}

func (s *Server) DuplicateCampaign(ctx context.Context, req *gamev1.DuplicateCampaignRequest) (*gamev1.DuplicateCampaignResponse, error) {
	scope := scopeFromProto(req)
	c, err := s.store.DuplicateCampaign(ctx, scope.TenantID, scope.MerchantID, req.GetCampaignId(), req.GetName())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.DuplicateCampaignResponse{Campaign: campaignToProto(c)}, nil
}

func (s *Server) GetCampaignAnalytics(ctx context.Context, req *gamev1.GetCampaignAnalyticsRequest) (*gamev1.GetCampaignAnalyticsResponse, error) {
	scope := scopeFromProto(req)
	a, err := s.store.CampaignAnalytics(ctx, scope.TenantID, scope.MerchantID, req.GetCampaignId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetCampaignAnalyticsResponse{Analytics: &gamev1.CampaignAnalytics{
		CampaignId:    a.CampaignID,
		TotalPlays:    a.TotalPlays,
		TotalWins:     a.TotalWins,
		UniquePlayers: a.UniquePlayers,
		Conversion:    a.Conversion,
	}}, nil
}

// GetPublicConfig resolves a campaign by id alone (no scope) for the FE widget.
func (s *Server) GetPublicConfig(ctx context.Context, req *gamev1.GetPublicConfigRequest) (*gamev1.GetPublicConfigResponse, error) {
	c, err := s.store.GetCampaignByID(ctx, req.GetCampaignId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetPublicConfigResponse{Campaign: campaignToProto(c)}, nil
}

// --- converters ---

func campaignFromProto(scope types.Scope, p *gamev1.Campaign) *types.Campaign {
	if p == nil {
		return &types.Campaign{TenantID: scope.TenantID, MerchantID: scope.MerchantID}
	}
	c := &types.Campaign{
		ID:         p.GetId(),
		TenantID:   scope.TenantID,
		MerchantID: scope.MerchantID,
		Name:       p.GetName(),
		Status:     domCampaignStatus(p.GetStatus()),
		StartDate:  ptime(p.GetStartDate()),
		EndDate:    ptime(p.GetEndDate()),
		Channels:   p.GetChannels(),
		Games:      p.GetGames(),
		Quests:     p.GetQuests(),
	}
	if str := p.GetSettings(); str != "" {
		_ = json.Unmarshal([]byte(str), &c.Settings)
	}
	return c
}

func campaignToProto(c *types.Campaign) *gamev1.Campaign {
	settings, _ := json.Marshal(c.Settings)
	return &gamev1.Campaign{
		Id:         c.ID,
		TenantId:   c.TenantID,
		MerchantId: c.MerchantID,
		Name:       c.Name,
		Status:     pbCampaignStatus(c.Status),
		StartDate:  tsPtr(c.StartDate),
		EndDate:    tsPtr(c.EndDate),
		Channels:   c.Channels,
		Games:      c.Games,
		Quests:     c.Quests,
		Settings:   string(settings),
		CreatedAt:  ts(c.CreatedAt),
		UpdatedAt:  ts(c.UpdatedAt),
	}
}
