package testplatform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	tpdomain "model_bss/internal/domain/testplatform"
)

type fakeGateway struct {
	state  tpdomain.JobState
	report []byte
	calls  int
}

func (f *fakeGateway) CreateJob(context.Context, tpdomain.CreateCommand) (tpdomain.Accepted, error) {
	return tpdomain.Accepted{JobID: "j_1", Status: "queued"}, nil
}
func (f *fakeGateway) GetJob(context.Context, string) (tpdomain.JobState, error) {
	f.calls++
	return f.state, nil
}
func (f *fakeGateway) DownloadReport(context.Context, string, string) ([]byte, error) {
	return f.report, nil
}

type fakeStore struct {
	seen       map[string]bool
	completions []tpdomain.Completion
}

func (f *fakeStore) CreateJob(context.Context, tpdomain.NewJobRecord) (string, bool, error) {
	return "1", true, nil
}
func (f *fakeStore) FailJob(context.Context, string, string) error           { return nil }
func (f *fakeStore) AttachExternalJob(context.Context, string, string, string) error { return nil }
func (f *fakeStore) PendingJobs(context.Context, int) ([]tpdomain.PendingJob, error) {
	return nil, nil
}
func (f *fakeStore) CompleteJob(_ context.Context, done tpdomain.Completion) (string, error) {
	f.completions = append(f.completions, done)
	return "9", nil
}
func (f *fakeStore) ClaimEvent(_ context.Context, eventID, _ string, _ []byte) (bool, error) {
	if f.seen[eventID] {
		return false, nil
	}
	f.seen[eventID] = true
	return true, nil
}
func (f *fakeStore) JobByExternalID(context.Context, string) (*tpdomain.PendingJob, error) {
	return &tpdomain.PendingJob{ID: "1", ResourceID: "2", ExternalJobID: "j_1", Category: tpdomain.CategoryBaseline}, nil
}

func sign(secret, stamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "."))
	mac.Write(body)
	return "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func newReceiver(t *testing.T) (*Receiver, *fakeStore, *fakeGateway) {
	t.Helper()
	gateway := &fakeGateway{
		state:  tpdomain.JobState{JobID: "j_1", Status: "succeeded", Verdict: "pass", Summary: json.RawMessage(`{"ttft_ms":42}`)},
		report: []byte(`{"ok":true}`),
	}
	store := &fakeStore{seen: map[string]bool{}}
	service, err := tpdomain.NewService(gateway, store, func() time.Time { return time.Unix(1_700_000_000, 0) })
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	receiver, err := NewReceiver("s3cret", service, func() time.Time { return time.Unix(1_700_000_000, 0) })
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	return receiver, store, gateway
}

func TestReceiveAcceptsSignedCallbackAndCompletes(t *testing.T) {
	receiver, store, gateway := newReceiver(t)
	body := []byte(`{"event":"job.completed","event_id":"evt_j_1_succeeded","job":{"job_id":"j_1","status":"succeeded"}}`)
	duplicate, err := receiver.Receive(context.Background(), body, sign("s3cret", "1700000000", body))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if duplicate {
		t.Fatal("first delivery must not be treated as duplicate")
	}
	if len(store.completions) != 1 {
		t.Fatalf("completion count = %d", len(store.completions))
	}
	done := store.completions[0]
	if done.FinalStatus != "SUCCEEDED" || !done.Outcome.ConnectivityPassed {
		t.Fatalf("unexpected completion %+v", done)
	}
	if gateway.calls != 1 {
		t.Fatalf("should re-read platform truth once, got %d", gateway.calls)
	}
}

func TestReceiveIsIdempotentByEventID(t *testing.T) {
	receiver, store, _ := newReceiver(t)
	body := []byte(`{"event":"job.completed","event_id":"evt_j_1_succeeded","job":{"job_id":"j_1","status":"succeeded"}}`)
	sig := sign("s3cret", "1700000000", body)
	if _, err := receiver.Receive(context.Background(), body, sig); err != nil {
		t.Fatalf("first: %v", err)
	}
	duplicate, err := receiver.Receive(context.Background(), body, sig)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !duplicate {
		t.Fatal("repeat delivery should report duplicate")
	}
	if len(store.completions) != 1 {
		t.Fatalf("must not complete twice, got %d", len(store.completions))
	}
}

func TestReceiveRejectsMissingSignature(t *testing.T) {
	receiver, store, _ := newReceiver(t)
	body := []byte(`{"job":{"job_id":"j_1"}}`)
	if _, err := receiver.Receive(context.Background(), body, ""); !errors.Is(err, ErrUnsignedCallback) {
		t.Fatalf("want ErrUnsignedCallback, got %v", err)
	}
	if len(store.completions) != 0 {
		t.Fatal("unsigned delivery must not touch the store")
	}
}

func TestReceiveRejectsWrongSecretAndStaleTimestamp(t *testing.T) {
	receiver, _, _ := newReceiver(t)
	body := []byte(`{"job":{"job_id":"j_1"}}`)
	if _, err := receiver.Receive(context.Background(), body, sign("wrong", "1700000000", body)); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("wrong secret should fail, got %v", err)
	}
	stale := strconv.FormatInt(1_700_000_000-301, 10)
	if _, err := receiver.Receive(context.Background(), body, sign("s3cret", stale, body)); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("stale timestamp should fail, got %v", err)
	}
	// 时间窗边界内应通过（-300s）；用带 event_id 的合法体，验证的只是时间窗。
	edgeBody := []byte(`{"event_id":"evt_edge","job":{"job_id":"j_1","status":"succeeded"}}`)
	edge := strconv.FormatInt(1_700_000_000-300, 10)
	if _, err := receiver.Receive(context.Background(), edgeBody, sign("s3cret", edge, edgeBody)); err != nil {
		t.Fatalf("edge timestamp should pass: %v", err)
	}
}

func TestReceiveRejectsMissingJobID(t *testing.T) {
	receiver, _, _ := newReceiver(t)
	body := []byte(`{"event_id":"evt_1","job":{}}`)
	if _, err := receiver.Receive(context.Background(), body, sign("s3cret", "1700000000", body)); !errors.Is(err, tpdomain.ErrInvalidCommand) {
		t.Fatalf("missing job id should be invalid, got %v", err)
	}
}
