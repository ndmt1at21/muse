package restgw

import (
	"bytes"
	"encoding/json"
	"testing"

	gamev1 "github.com/muse/pkg/gen/game/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// marshal mirrors the gateway's JSONPb options (UseProtoNames + EmitUnpopulated).
func marshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// decode parses JSON keeping numbers as json.Number so we can assert on type.
func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestNumberizeTimestamps_TimestampsBecomeNumbers(t *testing.T) {
	g := &gamev1.Game{
		Id:        "game_1",                             // string id stays a string
		TenantId:  "tenant_1",                           // string stays a string
		Status:    gamev1.GameStatus_GAME_STATUS_ACTIVE, // enum stays a name
		CreatedAt: 1717200000,                           // timestamp -> number
		UpdatedAt: 0,                                    // unset timestamp -> 0 (number)
		Rules:     &gamev1.Rules{StartDate: 1700000000, EndDate: 0, MaxPlaysPerUser: 3},
	}

	out := decode(t, numberizeTimestamps(g, marshal(t, g)))

	if _, ok := out["created_at"].(json.Number); !ok {
		t.Errorf("created_at: want json.Number, got %T (%v)", out["created_at"], out["created_at"])
	}
	if n, _ := out["created_at"].(json.Number); n.String() != "1717200000" {
		t.Errorf("created_at value = %v, want 1717200000", out["created_at"])
	}
	if _, ok := out["updated_at"].(json.Number); !ok {
		t.Errorf("updated_at (unset): want json.Number, got %T", out["updated_at"])
	}
	if out["id"] != "game_1" {
		t.Errorf("id: want string %q, got %v", "game_1", out["id"])
	}
	if out["status"] != "GAME_STATUS_ACTIVE" {
		t.Errorf("status: want enum name string, got %v", out["status"])
	}
	// Nested message: rules.start_date must also be a number.
	rules, ok := out["rules"].(map[string]any)
	if !ok {
		t.Fatalf("rules: want object, got %T", out["rules"])
	}
	if _, ok := rules["start_date"].(json.Number); !ok {
		t.Errorf("rules.start_date: want json.Number, got %T", rules["start_date"])
	}
}

func TestNumberizeTimestamps_NonTimestampInt64StaysString(t *testing.T) {
	// Prize.value/total are int64 quantities, not timestamps: they stay strings.
	p := &gamev1.Prize{Id: "p1", Value: 100, Total: 50}
	out := decode(t, numberizeTimestamps(p, marshal(t, p)))
	if _, ok := out["value"].(string); !ok {
		t.Errorf("value: want string (untouched int64), got %T (%v)", out["value"], out["value"])
	}

	// map<string,int64> balances are quantities too: values stay strings.
	resp := &gamev1.RedeemResponse{Redeemed: true, Balances: map[string]int64{"points": 150}}
	rout := decode(t, numberizeTimestamps(resp, marshal(t, resp)))
	balances, ok := rout["balances"].(map[string]any)
	if !ok {
		t.Fatalf("balances: want object, got %T", rout["balances"])
	}
	if _, ok := balances["points"].(string); !ok {
		t.Errorf("balances.points: want string, got %T (%v)", balances["points"], balances["points"])
	}

	// google.protobuf.Struct payloads carry arbitrary JSON and must be untouched.
	st, _ := structpb.NewStruct(map[string]any{"label": "hello", "n": float64(5)})
	play := &gamev1.PlayResponse{PlayId: "p1", Metadata: st}
	pout := decode(t, numberizeTimestamps(play, marshal(t, play)))
	meta, ok := pout["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata: want object, got %T", pout["metadata"])
	}
	if meta["label"] != "hello" {
		t.Errorf("metadata.label = %v, want hello", meta["label"])
	}
}
