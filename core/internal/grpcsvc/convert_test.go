package grpcsvc

import (
	"testing"

	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// The opaque `ui` render block must survive the proto↔domain conversion
// unchanged (Core stores and returns it verbatim, never interpreting it).
func TestGameConvertUIRoundTrip(t *testing.T) {
	ui := `{"theme":{"primary":"#1f6feb"},"wheel":{"segments":[{"emoji":"💎"}]},"items":{"gift_big":{"emoji":"💎"}}}`
	scope := types.Scope{TenantID: "t1", MerchantID: "m1"}

	dom := gameFromProto(scope, &gamev1.Game{
		Id:            "game_1",
		Type:          "spin_wheel",
		HandlerConfig: `{"prizes":[]}`,
		Ui:            ui,
	})
	if string(dom.UI) != ui {
		t.Fatalf("ui lost in gameFromProto: got %q", dom.UI)
	}

	out := gameToProto(dom)
	if out.GetUi() != ui {
		t.Fatalf("ui lost in gameToProto: got %q", out.GetUi())
	}
	// handler_config must still round-trip too (regression guard for the convert edit).
	if out.GetHandlerConfig() != `{"prizes":[]}` {
		t.Fatalf("handler_config lost: got %q", out.GetHandlerConfig())
	}
}
