package testplatform

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// 「同一受测目标只能有一个排队/运行中的任务」是防重复发起的核心，
// 下面盯住 Service.Start 的三条分支：
//  1. 抢到本地任务 → 才调平台
//  2. 没抢到（已有活跃任务）→ 复用，**绝不调平台**
//  3. 平台拒单 → 立刻把本地任务收成 FAILED，不留僵尸占槽位
type fakeStartStore struct {
	nextID  int
	created bool
	failed  []string
	calls   int
}

func (f *fakeStartStore) CreateJob(context.Context, NewJobRecord) (string, bool, error) {
	f.calls++
	f.nextID++
	return fmt.Sprintf("job-%d", f.nextID), f.created, nil
}
func (f *fakeStartStore) FailJob(_ context.Context, localID, _ string) error {
	f.failed = append(f.failed, localID)
	return nil
}
func (f *fakeStartStore) AttachExternalJob(context.Context, string, string, string) error { return nil }
func (f *fakeStartStore) PendingJobs(context.Context, int) ([]PendingJob, error)          { return nil, nil }
func (f *fakeStartStore) CompleteJob(context.Context, Completion) (string, error)         { return "1", nil }
func (f *fakeStartStore) ClaimEvent(context.Context, string, string, []byte) (bool, error) {
	return true, nil
}
func (f *fakeStartStore) JobByExternalID(context.Context, string) (*PendingJob, error) {
	return nil, nil
}

type fakeStartGateway struct {
	calls int
	err   error
}

func (f *fakeStartGateway) CreateJob(context.Context, CreateCommand) (Accepted, error) {
	f.calls++
	if f.err != nil {
		return Accepted{}, f.err
	}
	return Accepted{JobID: "ext-1", Status: "queued"}, nil
}
func (f *fakeStartGateway) GetJob(context.Context, string) (JobState, error) { return JobState{}, nil }
func (f *fakeStartGateway) DownloadReport(context.Context, string, string) ([]byte, error) {
	return nil, nil
}

func startCmd() CreateCommand {
	return CreateCommand{
		ResourceID: "3", ActorID: "9", Category: CategoryStress,
		Target: Target{Model: "m", BaseURL: "https://example.test/v1"},
	}
}

func TestStartCreatesWhenNoActiveJob(t *testing.T) {
	store := &fakeStartStore{created: true}
	gateway := &fakeStartGateway{}
	svc, err := NewService(gateway, store, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	result, err := svc.Start(context.Background(), startCmd())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if gateway.calls != 1 {
		t.Fatalf("抢到任务后应调用平台一次，实际 %d", gateway.calls)
	}
	if result.Deduplicated || result.ExternalID != "ext-1" {
		t.Fatalf("应返回新任务，实际 %+v", result)
	}
}

// 已有同目标活跃任务时，必须直接复用、一次都不能打平台。
func TestStartDeduplicatesWithoutCallingPlatform(t *testing.T) {
	store := &fakeStartStore{created: false}
	gateway := &fakeStartGateway{}
	svc, _ := NewService(gateway, store, nil)
	result, err := svc.Start(context.Background(), startCmd())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if gateway.calls != 0 {
		t.Fatalf("重复发起不能调平台，实际调用 %d 次", gateway.calls)
	}
	if !result.Deduplicated {
		t.Fatalf("应标记为复用，实际 %+v", result)
	}
	if result.LocalJobID == "" {
		t.Fatal("复用也要把已有任务号带回去")
	}
}

// 平台拒单必须收口，不能留 QUEUED 僵尸（它会一直占着去重槽位）。
func TestStartFailsJobWhenPlatformRejects(t *testing.T) {
	store := &fakeStartStore{created: true}
	gateway := &fakeStartGateway{err: errors.New("platform down")}
	svc, _ := NewService(gateway, store, nil)
	if _, err := svc.Start(context.Background(), startCmd()); err == nil {
		t.Fatal("平台失败时应返回错误")
	}
	if len(store.failed) != 1 {
		t.Fatalf("应把本地任务收成 FAILED，实际 %v", store.failed)
	}
}
