package integration

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
)

// fakeStore returns a fixed integration set and records ListForEvent args.
type fakeStore struct {
	integs      []types.Integration
	forEventErr error
	gotEvent    string
	gotCamp     string
}

func (f *fakeStore) CreateIntegration(_ context.Context, i *types.Integration) (*types.Integration, error) {
	f.integs = append(f.integs, *i)
	return i, nil
}
func (f *fakeStore) DeleteIntegration(context.Context, string, string, string) error { return nil }
func (f *fakeStore) ListIntegrations(context.Context, string, string, string, int, string) ([]types.Integration, string, error) {
	return f.integs, "", nil
}
func (f *fakeStore) ListIntegrationsForEvent(_ context.Context, _ types.Scope, campaignID, eventType string) ([]types.Integration, error) {
	f.gotEvent, f.gotCamp = eventType, campaignID
	if f.forEventErr != nil {
		return nil, f.forEventErr
	}
	var out []types.Integration
	for _, i := range f.integs {
		if i.Subscribes(eventType) && (i.CampaignID == "" || i.CampaignID == campaignID) {
			out = append(out, i)
		}
	}
	return out, nil
}

type fakeProvider struct {
	mu         sync.Mutex
	calls      int
	lastEvt    string
	deliverErr error
}

func (p *fakeProvider) Deliver(_ context.Context, _ types.Integration, evt ports.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.lastEvt = evt.Type
	return p.deliverErr
}

type captureBus struct{ published []ports.Event }

func (b *captureBus) Publish(_ context.Context, evt ports.Event) error {
	b.published = append(b.published, evt)
	return nil
}

func scope() types.Scope { return types.Scope{TenantID: "t1", MerchantID: "m1"} }

func TestDispatchToSubscribers(t *testing.T) {
	store := &fakeStore{integs: []types.Integration{
		{ID: "i1", Type: types.IntegrationWebhook, Status: types.IntegrationActive, Events: []string{types.EventPrizeWon}},
		{ID: "i2", Type: types.IntegrationWebhook, Status: types.IntegrationActive, Events: []string{types.EventPlayCompleted}}, // not subscribed
	}}
	prov := &fakeProvider{}
	reg := NewRegistry()
	reg.Register(types.IntegrationWebhook, prov)
	hub := NewHub(store, reg, nil, nil)

	n := hub.Dispatch(context.Background(), ports.Event{Type: types.EventPrizeWon, Scope: scope(), Payload: map[string]any{"prize_id": "p1"}})
	if n != 1 {
		t.Fatalf("dispatched = %d, want 1 (only i1 subscribes to prize_won)", n)
	}
	if prov.calls != 1 || prov.lastEvt != types.EventPrizeWon {
		t.Fatalf("provider calls=%d lastEvt=%s", prov.calls, prov.lastEvt)
	}
}

func TestDispatchHonorsCampaignNarrowing(t *testing.T) {
	store := &fakeStore{integs: []types.Integration{
		{ID: "wide", Type: types.IntegrationWebhook, Status: types.IntegrationActive, Events: []string{types.EventPrizeWon}},
		{ID: "campA", Type: types.IntegrationWebhook, Status: types.IntegrationActive, CampaignID: "campA", Events: []string{types.EventPrizeWon}},
		{ID: "campB", Type: types.IntegrationWebhook, Status: types.IntegrationActive, CampaignID: "campB", Events: []string{types.EventPrizeWon}},
	}}
	prov := &fakeProvider{}
	reg := NewRegistry()
	reg.Register(types.IntegrationWebhook, prov)
	hub := NewHub(store, reg, nil, nil)

	n := hub.Dispatch(context.Background(), ports.Event{Type: types.EventPrizeWon, Scope: scope(), Payload: map[string]any{"campaign_id": "campA"}})
	if store.gotCamp != "campA" {
		t.Fatalf("ListForEvent campaign = %q, want campA", store.gotCamp)
	}
	if n != 2 { // wide + campA, not campB
		t.Fatalf("dispatched = %d, want 2 (wide + campA)", n)
	}
}

func TestEmitPublishesToBusAndDispatches(t *testing.T) {
	store := &fakeStore{integs: []types.Integration{
		{ID: "i1", Type: types.IntegrationWebhook, Status: types.IntegrationActive, Events: []string{types.EventPlayCompleted}},
	}}
	prov := &fakeProvider{}
	reg := NewRegistry()
	reg.Register(types.IntegrationWebhook, prov)
	bus := &captureBus{}
	hub := NewHub(store, reg, bus, nil)

	hub.Emit(context.Background(), ports.Event{Type: types.EventPlayCompleted, Scope: scope()})
	if len(bus.published) != 1 || bus.published[0].Type != types.EventPlayCompleted {
		t.Fatalf("bus published = %+v", bus.published)
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.calls)
	}
}

func TestPausedIntegrationSkipped(t *testing.T) {
	store := &fakeStore{integs: []types.Integration{
		{ID: "i1", Type: types.IntegrationWebhook, Status: types.IntegrationPaused, Events: []string{types.EventPrizeWon}},
	}}
	prov := &fakeProvider{}
	reg := NewRegistry()
	reg.Register(types.IntegrationWebhook, prov)
	hub := NewHub(store, reg, nil, nil)

	n := hub.Dispatch(context.Background(), ports.Event{Type: types.EventPrizeWon, Scope: scope()})
	if n != 0 || prov.calls != 0 {
		t.Fatalf("paused integration should be skipped: n=%d calls=%d", n, prov.calls)
	}
}

func TestDeliveryErrorDoesNotCountOrPanic(t *testing.T) {
	store := &fakeStore{integs: []types.Integration{
		{ID: "i1", Type: types.IntegrationWebhook, Status: types.IntegrationActive, Events: []string{types.EventPrizeWon}},
	}}
	prov := &fakeProvider{deliverErr: errors.New("endpoint down")}
	reg := NewRegistry()
	reg.Register(types.IntegrationWebhook, prov)
	hub := NewHub(store, reg, nil, nil)

	n := hub.Dispatch(context.Background(), ports.Event{Type: types.EventPrizeWon, Scope: scope()})
	if n != 0 {
		t.Fatalf("failed delivery should not count: n=%d", n)
	}
}

func TestUnknownTypeSkipped(t *testing.T) {
	store := &fakeStore{integs: []types.Integration{
		{ID: "i1", Type: "mystery", Status: types.IntegrationActive, Events: []string{types.EventPrizeWon}},
	}}
	hub := NewHub(store, NewRegistry(), nil, nil)
	if n := hub.Dispatch(context.Background(), ports.Event{Type: types.EventPrizeWon, Scope: scope()}); n != 0 {
		t.Fatalf("unknown provider type should be skipped: n=%d", n)
	}
}
