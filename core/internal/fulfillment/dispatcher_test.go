package fulfillment

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/muse/gamekit/types"
)

// fakeQueue is an in-memory Queue that records dispatcher decisions.
type fakeQueue struct {
	due       []types.FulfillmentTask
	completed map[string]json.RawMessage
	awaited   map[string]json.RawMessage
	failed    map[string]failRec
}

type failRec struct {
	msg       string
	permanent bool
}

func newFakeQueue(due ...types.FulfillmentTask) *fakeQueue {
	return &fakeQueue{
		due:       due,
		completed: map[string]json.RawMessage{},
		awaited:   map[string]json.RawMessage{},
		failed:    map[string]failRec{},
	}
}

func (q *fakeQueue) ClaimDueTasks(_ context.Context, limit int, _ time.Duration) ([]types.FulfillmentTask, error) {
	out := q.due
	q.due = nil // one-shot, like a drained queue
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (q *fakeQueue) CompleteTask(_ context.Context, taskID string, receipt json.RawMessage) error {
	q.completed[taskID] = receipt
	return nil
}

func (q *fakeQueue) AwaitCallback(_ context.Context, taskID string, receipt json.RawMessage) error {
	q.awaited[taskID] = receipt
	return nil
}

func (q *fakeQueue) FailTask(_ context.Context, taskID, msg string, _ time.Duration, _ int, permanent bool) (types.TaskStatus, error) {
	q.failed[taskID] = failRec{msg: msg, permanent: permanent}
	if permanent {
		return types.TaskDead, nil
	}
	return types.TaskFailed, nil
}

type fakeProvider struct{ res Result }

func (p fakeProvider) Deliver(_ context.Context, _ *types.FulfillmentTask) Result { return p.res }

func task(id, channel string) types.FulfillmentTask {
	return types.FulfillmentTask{ID: id, Channel: channel, Attempts: 1, Status: types.TaskProcessing}
}

func TestDispatcherDelivers(t *testing.T) {
	q := newFakeQueue(task("task_1", "x"))
	reg := NewRegistry()
	reg.Register("x", fakeProvider{res: DeliveredResult(mustJSON(map[string]any{"ok": true}))})
	d := NewDispatcher(q, reg, Config{}, nil)

	n, err := d.Drain(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("Drain = (%d,%v), want (1,nil)", n, err)
	}
	if _, ok := q.completed["task_1"]; !ok {
		t.Fatalf("expected task_1 completed, got %+v", q)
	}
}

func TestDispatcherUnknownChannelDeadLetters(t *testing.T) {
	q := newFakeQueue(task("task_1", "ghost"))
	d := NewDispatcher(q, NewRegistry(), Config{}, nil)

	if _, err := d.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec, ok := q.failed["task_1"]
	if !ok || !rec.permanent {
		t.Fatalf("unknown channel should permanently fail, got %+v ok=%v", rec, ok)
	}
}

func TestDispatcherRetryableVsPermanent(t *testing.T) {
	q := newFakeQueue(task("retry", "r"), task("perm", "p"))
	reg := NewRegistry()
	reg.Register("r", fakeProvider{res: Retry(errPoolEmpty)})
	reg.Register("p", fakeProvider{res: Permanent(errPoolEmpty)})
	d := NewDispatcher(q, reg, Config{}, nil)

	if _, err := d.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.failed["retry"].permanent {
		t.Error("retryable provider should not permanently fail")
	}
	if !q.failed["perm"].permanent {
		t.Error("permanent provider should permanently fail")
	}
}

func TestDispatcherAwaitsCallback(t *testing.T) {
	q := newFakeQueue(task("task_1", "ext"))
	reg := NewRegistry()
	reg.Register("ext", fakeProvider{res: Awaiting(mustJSON(map[string]any{"dispatched": true}))})
	d := NewDispatcher(q, reg, Config{}, nil)

	if _, err := d.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := q.awaited["task_1"]; !ok {
		t.Fatalf("expected task_1 awaiting callback, got %+v", q)
	}
	if _, ok := q.completed["task_1"]; ok {
		t.Error("awaiting task must not be completed")
	}
}

type fakePool struct {
	code string
	ok   bool
}

func (p fakePool) PopVoucherCode(_ context.Context, _ types.Scope, _ string) (string, bool, error) {
	return p.code, p.ok, nil
}

func TestVoucherCodeProvider(t *testing.T) {
	p := NewVoucherCodeProvider(fakePool{code: "VC-XYZ", ok: true})
	res := p.Deliver(context.Background(), &types.FulfillmentTask{PrizeID: "prize_1"})
	if res.Outcome != Delivered {
		t.Fatalf("outcome = %v, want Delivered", res.Outcome)
	}
	if got := receiptString(res.Receipt, "code"); got != "VC-XYZ" {
		t.Errorf("receipt code = %q, want VC-XYZ", got)
	}

	empty := NewVoucherCodeProvider(fakePool{ok: false})
	if r := empty.Deliver(context.Background(), &types.FulfillmentTask{PrizeID: "p"}); r.Outcome != PermanentError {
		t.Errorf("empty pool outcome = %v, want PermanentError", r.Outcome)
	}
}

func TestExternalWorkflowProviderSignsAndAwaits(t *testing.T) {
	const secret = "s3cr3t"
	var gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Muse-Signature")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewExternalWorkflowProvider(srv.Client(), "", "", "https://bff.test", nil)
	cfg := mustJSON(map[string]any{"webhook_url": srv.URL, "hmac_secret": secret})
	res := p.Deliver(context.Background(), &types.FulfillmentTask{ID: "task_1", Channel: types.ChannelExternalWorkflow, ChannelConfig: cfg})

	if res.Outcome != AwaitingCallback {
		t.Fatalf("outcome = %v, want AwaitingCallback", res.Outcome)
	}
	if want := SignHMAC(secret, gotBody); gotSig != want {
		t.Errorf("signature = %q, want %q", gotSig, want)
	}
}

func TestExternalWorkflowProviderStatusMapping(t *testing.T) {
	cases := []struct {
		code int
		want Outcome
	}{
		{http.StatusOK, AwaitingCallback},
		{http.StatusInternalServerError, RetryableError},
		{http.StatusTooManyRequests, RetryableError},
		{http.StatusBadRequest, PermanentError},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.code)
		}))
		p := NewExternalWorkflowProvider(srv.Client(), srv.URL, "", "", nil)
		res := p.Deliver(context.Background(), &types.FulfillmentTask{ID: "t", Channel: types.ChannelExternalWorkflow})
		if res.Outcome != c.want {
			t.Errorf("status %d -> outcome %v, want %v", c.code, res.Outcome, c.want)
		}
		srv.Close()
	}
}

func TestExternalWorkflowNoWebhookIsPermanent(t *testing.T) {
	p := NewExternalWorkflowProvider(http.DefaultClient, "", "", "", nil)
	res := p.Deliver(context.Background(), &types.FulfillmentTask{ID: "t", Channel: types.ChannelExternalWorkflow})
	if res.Outcome != PermanentError {
		t.Errorf("missing webhook -> %v, want PermanentError", res.Outcome)
	}
}
