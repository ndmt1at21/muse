package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
)

func TestWebhookProviderPostsSignedEvent(t *testing.T) {
	var gotBody []byte
	var gotSig, gotEvent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Muse-Signature")
		gotEvent = r.Header.Get("X-Muse-Event")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(map[string]string{"url": srv.URL, "hmac_secret": "s3cret"})
	integ := types.Integration{ID: "i1", Type: types.IntegrationWebhook, Config: cfg}
	evt := ports.Event{
		Type:    types.EventPrizeWon,
		Scope:   types.Scope{TenantID: "t1", MerchantID: "m1"},
		Payload: map[string]any{"prize_id": "p1", "player_id": "pl1"},
	}

	prov := NewWebhookProvider(srv.Client(), nil)
	if err := prov.Deliver(context.Background(), integ, evt); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if gotEvent != types.EventPrizeWon {
		t.Fatalf("X-Muse-Event = %q", gotEvent)
	}
	if gotSig != signHMAC("s3cret", gotBody) {
		t.Fatalf("signature mismatch")
	}
	var payload eventPayload
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if payload.Event != types.EventPrizeWon || payload.TenantID != "t1" || payload.Payload["prize_id"] != "p1" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestWebhookProviderNoURLErrors(t *testing.T) {
	prov := NewWebhookProvider(http.DefaultClient, nil)
	err := prov.Deliver(context.Background(), types.Integration{Type: types.IntegrationWebhook}, ports.Event{Type: "x"})
	if err == nil {
		t.Fatal("expected error when no url configured")
	}
}

func TestWebhookProviderNon2xxErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg, _ := json.Marshal(map[string]string{"url": srv.URL})
	prov := NewWebhookProvider(srv.Client(), nil)
	if err := prov.Deliver(context.Background(), types.Integration{Type: types.IntegrationWebhook, Config: cfg}, ports.Event{Type: "x"}); err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestRegisterBuiltins(t *testing.T) {
	reg := NewRegistry()
	RegisterBuiltins(reg, http.DefaultClient, nil)
	for _, typ := range []string{types.IntegrationWebhook, types.IntegrationN8N, types.IntegrationGSheet, types.IntegrationSMS, types.IntegrationZNS, types.IntegrationCRM} {
		if _, ok := reg.Get(typ); !ok {
			t.Fatalf("provider not registered for %q", typ)
		}
	}
	// Stub providers succeed (no-op).
	stub, _ := reg.Get(types.IntegrationSMS)
	if err := stub.Deliver(context.Background(), types.Integration{Type: types.IntegrationSMS}, ports.Event{Type: "x"}); err != nil {
		t.Fatalf("stub deliver: %v", err)
	}
}
