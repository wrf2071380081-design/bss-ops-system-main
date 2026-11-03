package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.uber.org/zap"

	"model_bss/internal/domain/customer"
)

// 供应域事件 → 客户绑定联动的单元测试（G3-0）。
//
// 真库链路（事件 → job → 状态机 → 审计）在 internal/api 的集成测试里；
// 这里钉的是**分支判定**：哪些该迁、哪些该跳过、单条失败是否影响其余。

type fakeTransitioner struct {
	links   map[int64][]customer.ResourceLink
	calls   []fakeTransitionCall
	failFor map[int64]error
	listErr error
}

type fakeTransitionCall struct {
	kind     string // offline / restored
	linkID   int64
	revision int
	request  string
}

func (f *fakeTransitioner) ListLinksByResource(_ context.Context, resourceID int64) ([]customer.ResourceLink, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.links[resourceID], nil
}

func (f *fakeTransitioner) MarkSupplyOffline(_ context.Context, id int64, expectedRevision int, _ string, requestID string) (*customer.ResourceLink, error) {
	f.calls = append(f.calls, fakeTransitionCall{kind: "offline", linkID: id, revision: expectedRevision, request: requestID})
	if err := f.failFor[id]; err != nil {
		return nil, err
	}
	return &customer.ResourceLink{ID: id, State: customer.LinkStateSupplyOfflinePending}, nil
}

func (f *fakeTransitioner) MarkSupplyRestored(_ context.Context, id int64, expectedRevision int, requestID string) (*customer.ResourceLink, error) {
	f.calls = append(f.calls, fakeTransitionCall{kind: "restored", linkID: id, revision: expectedRevision, request: requestID})
	if err := f.failFor[id]; err != nil {
		return nil, err
	}
	return &customer.ResourceLink{ID: id, State: customer.LinkStateActive}, nil
}

func offlinePayload(t *testing.T, resourceID int64) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"resource_id": resourceID, "region": "CN", "state": "OFFLINE"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

// TestSyncResourceLinksOfflineMovesAllActiveLinks 一条资源被 N 个客户绑定 → 全部迁到待处理。
func TestSyncResourceLinksOfflineMovesAllActiveLinks(t *testing.T) {
	fake := &fakeTransitioner{links: map[int64][]customer.ResourceLink{
		77: {
			{ID: 1, ResourceID: 77, Revision: 3, State: customer.LinkStateActive},
			{ID: 2, ResourceID: 77, Revision: 5, State: customer.LinkStateDisabled},
		},
	}}
	if err := SyncResourceLinks(context.Background(), fake, 900, "RESOURCE_OFFLINED", offlinePayload(t, 77), zap.NewNop()); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("应逐条迁移 2 条，got %d", len(fake.calls))
	}
	for _, call := range fake.calls {
		if call.kind != "offline" {
			t.Fatalf("应为 offline，got %s", call.kind)
		}
		if call.request != "event:900" {
			t.Fatalf("request_id 必须是 event:<id>，got %q", call.request)
		}
	}
	if fake.calls[0].revision != 3 || fake.calls[1].revision != 5 {
		t.Fatalf("必须带各自的 revision 做乐观锁：%+v", fake.calls)
	}
}

// TestSyncResourceLinksRestoreOnlyTouchesPending 上架只恢复「待处理」的，其余跳过（不误伤 ACTIVE/DISABLED）。
func TestSyncResourceLinksRestoreOnlyTouchesPending(t *testing.T) {
	fake := &fakeTransitioner{links: map[int64][]customer.ResourceLink{
		77: {
			{ID: 1, Revision: 4, State: customer.LinkStateSupplyOfflinePending},
			{ID: 2, Revision: 2, State: customer.LinkStateActive},
			{ID: 3, Revision: 9, State: customer.LinkStateDisabled},
		},
	}}
	if err := SyncResourceLinks(context.Background(), fake, 901, "RESOURCE_ONLINED", offlinePayload(t, 77), zap.NewNop()); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0].linkID != 1 || fake.calls[0].kind != "restored" {
		t.Fatalf("只应恢复待处理那条，got %+v", fake.calls)
	}
}

// TestSyncResourceLinksSkipsAlreadyPendingAndArchived 幂等与「已归档不被复活」。
func TestSyncResourceLinksSkipsAlreadyPendingAndArchived(t *testing.T) {
	fake := &fakeTransitioner{
		links: map[int64][]customer.ResourceLink{
			77: {
				{ID: 1, Revision: 4, State: customer.LinkStateSupplyOfflinePending}, // 已处理 → 跳过，不调服务
				{ID: 2, Revision: 2, State: customer.LinkStateActive},               // 正常迁移
			},
		},
		failFor: map[int64]error{2: customer.ErrLinkArchived}, // 并发场景下被归档 → 跳过并记日志
	}
	if err := SyncResourceLinks(context.Background(), fake, 902, "RESOURCE_OFFLINED", offlinePayload(t, 77), zap.NewNop()); err != nil {
		t.Fatalf("单条失败不应返回错误（由调用方决定事件是否重试）：%v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0].linkID != 2 {
		t.Fatalf("已在待处理态的那条不应再调服务，got %+v", fake.calls)
	}
}

// TestSyncResourceLinksRevisionConflictDoesNotStopOthers 并发 409 只跳过该条。
func TestSyncResourceLinksRevisionConflictDoesNotStopOthers(t *testing.T) {
	fake := &fakeTransitioner{
		links: map[int64][]customer.ResourceLink{
			77: {
				{ID: 1, Revision: 1, State: customer.LinkStateActive},
				{ID: 2, Revision: 1, State: customer.LinkStateActive},
			},
		},
		failFor: map[int64]error{1: customer.ErrLinkRevisionConflict},
	}
	if err := SyncResourceLinks(context.Background(), fake, 903, "RESOURCE_OFFLINED", offlinePayload(t, 77), zap.NewNop()); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("冲突那条跳过、其余继续，应调用 2 次，got %d", len(fake.calls))
	}
}

// TestSyncResourceLinksIgnoresUnrelatedEvents 与本联动无关的事件直接放行（不查库）。
func TestSyncResourceLinksIgnoresUnrelatedEvents(t *testing.T) {
	fake := &fakeTransitioner{listErr: errors.New("should not be called")}
	if err := SyncResourceLinks(context.Background(), fake, 904, "RESOURCE_COEFFICIENT_CHANGED", offlinePayload(t, 77), zap.NewNop()); err != nil {
		t.Fatalf("无关事件应放行，got %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("无关事件不应触发迁移，got %+v", fake.calls)
	}
}

// TestSyncResourceLinksPropagatesListFailure 系统性失败要上报（让事件重试），不能静默吞。
func TestSyncResourceLinksPropagatesListFailure(t *testing.T) {
	fake := &fakeTransitioner{listErr: errors.New("db down")}
	if err := SyncResourceLinks(context.Background(), fake, 905, "RESOURCE_OFFLINED", offlinePayload(t, 77), zap.NewNop()); err == nil {
		t.Fatal("读取绑定列表失败必须返回错误，否则事件会被标记完成而联动永久丢失")
	}
}

// ---- payload 形态兼容（G3-0 回归） ----

// TestResourceStatePayloadAcceptsProducerShape 钉住「生产方发字符串 ID 也要能解」。
//
// 背景：生产方（supplypg/plan_execution.go 的 payloadValues）里 ID 全是
// `strconv.FormatUint(...)` / 领域 string，落进 event_outbox 是 JSON 字符串；
// 消费端曾经声明 int64 → 线上事件 100% FAILED（`cannot unmarshal string into ... int64`）。
// 注意 json.Number 也救不了：它只吃裸数字字面量。
func TestResourceStatePayloadAcceptsProducerShape(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int64
	}{
		{"字符串 ID（生产方真实形态）", `{"resource_id":"1550","region":"CN","state":"OFFLINE","previous_state":"ADMITTED"}`, 1550},
		{"数字 ID（历史/外部调用方形态）", `{"resource_id":1550,"region":"CN","state":"OFFLINE","previous_state":"ADMITTED"}`, 1550},
		{"字符串 ID 带空白", `{"resource_id":" 42 "}`, 42},
	}
	for _, c := range cases {
		var payload resourceStatePayload
		if err := json.Unmarshal([]byte(c.raw), &payload); err != nil {
			t.Fatalf("%s：解析失败 %v", c.name, err)
		}
		if got := int64(payload.ResourceID); got != c.want {
			t.Fatalf("%s：got %d want %d", c.name, got, c.want)
		}
	}
}

// TestResourceStatePayloadRejectsGarbage 非法 ID 必须报错，不能静默当 0/跳过事件。
func TestResourceStatePayloadRejectsGarbage(t *testing.T) {
	for _, raw := range []string{
		`{"resource_id":null}`,
		`{"resource_id":""}`,
		`{"resource_id":"abc"}`,
		`{"resource_id":{}}`,
	} {
		var payload resourceStatePayload
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			t.Fatalf("%s：应当解析失败，实际通过（got %d）", raw, int64(payload.ResourceID))
		}
	}
}

// TestSyncResourceLinksUsesProducerPayload 用真实形态的 payload 跑一遍联动入口，
// 确保「解析成功 → 真的去列绑定 → 真的调迁移」这条链没断。
func TestSyncResourceLinksUsesProducerPayload(t *testing.T) {
	linker := &fakeTransitioner{links: map[int64][]customer.ResourceLink{
		1550: {{ID: 723, CustomerProfileID: 370, StandardModelID: 13, ResourceID: 1550,
			State: customer.LinkStateActive, Revision: 1}},
	}}
	raw := []byte(`{"contract":"supply.resource_state_changed","schema_version":1,"plan_id":"1",
		"plan_revision":"1","resource_id":"1550","resource_revision":"2","region":"CN",
		"state":"OFFLINE","previous_state":"ADMITTED","actor_id":"3","affected_status":"PENDING"}`)
	if err := SyncResourceLinks(context.Background(), linker, 52, "RESOURCE_OFFLINED", raw, zap.NewNop()); err != nil {
		t.Fatalf("联动入口报错：%v", err)
	}
	if len(linker.calls) != 1 || linker.calls[0].kind != "offline" || linker.calls[0].linkID != 723 {
		t.Fatalf("未按事件触发迁移：%+v", linker.calls)
	}
	if linker.calls[0].request != "event:52" {
		t.Fatalf("request_id 应为 event:<id>，got %q", linker.calls[0].request)
	}
}
