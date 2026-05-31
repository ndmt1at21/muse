package grpcsvc

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// --- IntegrationService: outbound adapter CRUD + manual event injection ---

func (s *Server) intgReady() error {
	if s.intg == nil {
		return s.fail(gkerr.New(gkerr.ReasonInternal, "integration hub not configured"))
	}
	return nil
}

func (s *Server) CreateIntegration(ctx context.Context, req *gamev1.CreateIntegrationRequest) (*gamev1.CreateIntegrationResponse, error) {
	if err := s.intgReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	if scope.TenantID == "" || scope.MerchantID == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "tenant_id and merchant_id are required"))
	}
	i := integrationFromProto(scope, req.GetIntegration())
	saved, err := s.intg.Create(ctx, i)
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.CreateIntegrationResponse{Integration: integrationToProto(saved)}, nil
}

func (s *Server) ListIntegrations(ctx context.Context, req *gamev1.ListIntegrationsRequest) (*gamev1.ListIntegrationsResponse, error) {
	if err := s.intgReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	integs, next, err := s.intg.List(ctx, scope.TenantID, scope.MerchantID, req.GetCampaignId(), int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.Integration, 0, len(integs))
	for i := range integs {
		out = append(out, integrationToProto(&integs[i]))
	}
	return &gamev1.ListIntegrationsResponse{Integrations: out, NextCursor: next}, nil
}

func (s *Server) DeleteIntegration(ctx context.Context, req *gamev1.DeleteIntegrationRequest) (*gamev1.DeleteIntegrationResponse, error) {
	if err := s.intgReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	if err := s.intg.Delete(ctx, scope.TenantID, scope.MerchantID, req.GetId()); err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.DeleteIntegrationResponse{}, nil
}

// EmitEvent injects a domain event into the hub (manual/testing entry point);
// it dispatches synchronously and reports how many integrations were delivered to.
func (s *Server) EmitEvent(ctx context.Context, req *gamev1.EmitEventRequest) (*gamev1.EmitEventResponse, error) {
	if err := s.intgReady(); err != nil {
		return nil, err
	}
	if req.GetType() == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "event type is required"))
	}
	var payload map[string]any
	if p := req.GetPayload(); p != "" {
		if err := json.Unmarshal([]byte(p), &payload); err != nil {
			return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "payload must be a JSON object"))
		}
	}
	n := s.intg.Dispatch(ctx, ports.Event{Type: req.GetType(), Scope: scopeFromProto(req.GetScope()), Payload: payload})
	return &gamev1.EmitEventResponse{Dispatched: int32(n)}, nil
}

// emit publishes a domain event through the hub, best-effort. A nil hub (hub not
// wired) is a no-op; failures are swallowed inside the hub so a domain operation
// is never affected.
func (s *Server) emit(ctx context.Context, evtType string, scope types.Scope, payload map[string]any) {
	if s.intg == nil {
		return
	}
	s.intg.Emit(ctx, ports.Event{Type: evtType, Scope: scope, Payload: payload})
}

func integrationFromProto(scope types.Scope, p *gamev1.Integration) *types.Integration {
	if p == nil {
		return &types.Integration{TenantID: scope.TenantID, MerchantID: scope.MerchantID}
	}
	i := &types.Integration{
		ID:         p.GetId(),
		TenantID:   scope.TenantID,
		MerchantID: scope.MerchantID,
		CampaignID: p.GetCampaignId(),
		Type:       p.GetType(),
		Events:     p.GetEvents(),
		Status:     p.GetStatus(),
	}
	if c := p.GetConfig(); c != "" {
		i.Config = json.RawMessage(c)
	}
	return i
}

func integrationToProto(i *types.Integration) *gamev1.Integration {
	return &gamev1.Integration{
		Id:         i.ID,
		Scope:      &gamev1.Scope{TenantId: i.TenantID, MerchantId: i.MerchantID},
		CampaignId: i.CampaignID,
		Type:       i.Type,
		Events:     i.Events,
		Config:     string(i.Config),
		Status:     i.Status,
		CreatedAt:  ts(i.CreatedAt),
	}
}
