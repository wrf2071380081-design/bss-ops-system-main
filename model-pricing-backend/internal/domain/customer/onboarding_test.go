package customer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 客户开通流程的领域测试（G4-B2）。
//
// 覆盖「不连库能判定的部分」：入参校验矩阵、节点完成的分派与幂等守卫、合同授信就绪端口。
// 绑定写入原子性、部分唯一索引、v_work_item 视图修复这些只有真库能证明的，
// 放在 internal/infra/supplypg/customer_onboarding_store_integration_test.go。

type fakeOnboardingStore struct {
	region    string
	regionErr error

	started   []StartFlowRecord
	startErr  error
	startFlow *OnboardingFlow

	nodeCtx *OnboardingNodeContext
	nodeErr error

	configCalls   []CompleteConfigRecord
	contractCalls []CompleteContractRecord
	completeErr   error

	flow    *OnboardingFlow
	flowErr error
}

func (f *fakeOnboardingStore) CustomerRegion(_ context.Context, _ int64) (string, error) {
	return f.region, f.regionErr
}

func (f *fakeOnboardingStore) StartFlow(_ context.Context, in StartFlowRecord) (*OnboardingFlow, error) {
	f.started = append(f.started, in)
	if f.startErr != nil {
		return nil, f.startErr
	}
	if f.startFlow != nil {
		return f.startFlow, nil
	}
	return &OnboardingFlow{ID: 1, Status: OnboardingFlowProcessing}, nil
}

func (f *fakeOnboardingStore) LoadNodeContext(_ context.Context, _ int64) (*OnboardingNodeContext, error) {
	if f.nodeErr != nil {
		return nil, f.nodeErr
	}
	if f.nodeCtx == nil {
		return nil, ErrOnboardingNodeNotFound
	}
	return f.nodeCtx, nil
}

func (f *fakeOnboardingStore) CompleteConfig(_ context.Context, in CompleteConfigRecord) (*OnboardingFlow, error) {
	f.configCalls = append(f.configCalls, in)
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	return &OnboardingFlow{ID: in.FlowID, Status: OnboardingFlowCompleted}, nil
}

func (f *fakeOnboardingStore) CompleteContract(_ context.Context, in CompleteContractRecord) (*OnboardingFlow, error) {
	f.contractCalls = append(f.contractCalls, in)
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	return &OnboardingFlow{ID: in.FlowID, Status: OnboardingFlowProcessing, CurrentNode: OnboardingNodeConfig}, nil
}

func (f *fakeOnboardingStore) GetFlow(_ context.Context, _ int64) (*OnboardingFlow, error) {
	if f.flowErr != nil {
		return nil, f.flowErr
	}
	return f.flow, nil
}

// fakeResourceFacts 只实现 LoadResourceFacts，其余方法不会被调用。
type fakeResourceFacts struct {
	facts map[int64]*ResourceFacts
	err   error
}

func (f *fakeResourceFacts) CreateLink(context.Context, CreateLinkRecord) (*ResourceLink, error) {
	return nil, errors.New("not used")
}
func (f *fakeResourceFacts) GetLink(context.Context, int64) (*ResourceLink, error) {
	return nil, errors.New("not used")
}
func (f *fakeResourceFacts) ApplyTransition(context.Context, TransitionRecord) (*ResourceLink, error) {
	return nil, errors.New("not used")
}
func (f *fakeResourceFacts) RebindLink(context.Context, RebindRecord) (*ResourceLink, error) {
	return nil, errors.New("not used")
}
func (f *fakeResourceFacts) ListCustomerModelServices(context.Context, int64) ([]CustomerModelServiceRow, error) {
	return nil, errors.New("not used")
}
func (f *fakeResourceFacts) LoadResourceFacts(_ context.Context, id int64) (*ResourceFacts, error) {
	if f.err != nil {
		return nil, f.err
	}
	if facts, ok := f.facts[id]; ok {
		return facts, nil
	}
	return nil, nil
}

func admittedFacts(id, modelID int64) *ResourceFacts {
	return &ResourceFacts{ID: id, StandardModelID: modelID, Region: "CN", State: "ADMITTED"}
}

func newOnboardingServiceWith(store OnboardingStore, facts map[int64]*ResourceFacts) *OnboardingService {
	return NewOnboardingService(store, &fakeResourceFacts{facts: facts})
}

// TestOnboardingStartValidatesModels 模型校验矩阵：每条一个用例。
func TestOnboardingStartValidatesModels(t *testing.T) {
	validFacts := map[int64]*ResourceFacts{21: admittedFacts(21, 11), 22: admittedFacts(22, 11)}

	cases := []struct {
		name   string
		mutate func(in *StartOnboardingInput)
		facts  map[int64]*ResourceFacts
		region string
		want   error
	}{
		{"没有模型", func(in *StartOnboardingInput) { in.Models = nil }, validFacts, "CN", ErrOnboardingModelsRequired},
		{"模型 ID 非法", func(in *StartOnboardingInput) {
			in.Models = []OnboardingModelInput{{StandardModelID: 0, ResourceID: 21}}
		}, validFacts, "CN", ErrOnboardingModelInvalid},
		{"同一资源重复", func(in *StartOnboardingInput) {
			in.Models = []OnboardingModelInput{{StandardModelID: 11, ResourceID: 21}, {StandardModelID: 11, ResourceID: 21}}
		}, validFacts, "CN", ErrOnboardingModelDuplicated},
		{"资源不存在", func(in *StartOnboardingInput) {
			in.Models = []OnboardingModelInput{{StandardModelID: 11, ResourceID: 99}}
		}, validFacts, "CN", ErrLinkResourceNotFound},
		{"模型不匹配", func(in *StartOnboardingInput) {
			in.Models = []OnboardingModelInput{{StandardModelID: 12, ResourceID: 21}}
		}, validFacts, "CN", ErrLinkModelMismatch},
		{"跨部署节点", func(in *StartOnboardingInput) {
			in.Models = []OnboardingModelInput{{StandardModelID: 11, ResourceID: 21}}
		}, validFacts, "OVERSEAS", ErrOnboardingRegionMismatch},
		{"资源未准入", func(in *StartOnboardingInput) {
			in.Models = []OnboardingModelInput{{StandardModelID: 11, ResourceID: 21}}
		},
			map[int64]*ResourceFacts{21: {ID: 21, StandardModelID: 11, Region: "CN", State: "OFFLINE"}}, "CN", ErrOnboardingResourceNotAdmitted},
		{"缺资源配置办理人", func(in *StartOnboardingInput) { in.ConfigAssigneeID = 0 }, validFacts, "CN", ErrOnboardingAssigneeRequired},
		{"首单缺合同办理人", func(in *StartOnboardingInput) { in.FirstOrder = true; in.ContractAssigneeID = 0 }, validFacts, "CN", ErrOnboardingContractAssignee},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := &fakeOnboardingStore{region: c.region}
			svc := newOnboardingServiceWith(store, c.facts)
			in := StartOnboardingInput{
				CustomerProfileID: 5, ConfigAssigneeID: 9, ContractAssigneeID: 8,
				Models: []OnboardingModelInput{{StandardModelID: 11, ResourceID: 21}},
			}
			c.mutate(&in)
			if _, err := svc.Start(context.Background(), in, 42, "req"); !errors.Is(err, c.want) {
				t.Fatalf("期望 %v，got %v", c.want, err)
			}
			if len(store.started) != 0 {
				t.Fatal("校验失败不得建流程")
			}
		})
	}
}

// TestOnboardingStartPassesNormalizedRecord 正常发起：记录透传（首单/办理人/模型/原因去空格）。
func TestOnboardingStartPassesNormalizedRecord(t *testing.T) {
	store := &fakeOnboardingStore{region: "CN"}
	svc := newOnboardingServiceWith(store, map[int64]*ResourceFacts{21: admittedFacts(21, 11)})
	flow, err := svc.Start(context.Background(), StartOnboardingInput{
		CustomerProfileID: 5, FirstOrder: true, ContractAssigneeID: 8, ConfigAssigneeID: 9,
		Models: []OnboardingModelInput{{StandardModelID: 11, ResourceID: 21}},
		Reason: "  首单开通  ",
	}, 42, "req-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if flow == nil {
		t.Fatal("应返回流程")
	}
	if len(store.started) != 1 {
		t.Fatalf("期望建 1 次，got %d", len(store.started))
	}
	record := store.started[0]
	if !record.FirstOrder || record.ContractAssigneeID != 8 || record.ConfigAssigneeID != 9 {
		t.Fatalf("办理人/首单标记未透传：%+v", record)
	}
	if record.Reason != "首单开通" {
		t.Fatalf("原因应去首尾空格，got %q", record.Reason)
	}
	if record.OperatorID != 42 || record.RequestID != "req-1" {
		t.Fatalf("操作员/请求号未透传：%+v", record)
	}
}

// TestOnboardingStartSurfacesInProgress 已有进行中流程时，仓储的冲突原样上抛。
func TestOnboardingStartSurfacesInProgress(t *testing.T) {
	store := &fakeOnboardingStore{region: "CN", startErr: ErrOnboardingInProgress}
	svc := newOnboardingServiceWith(store, map[int64]*ResourceFacts{21: admittedFacts(21, 11)})
	_, err := svc.Start(context.Background(), StartOnboardingInput{
		CustomerProfileID: 5, ConfigAssigneeID: 9,
		Models: []OnboardingModelInput{{StandardModelID: 11, ResourceID: 21}},
	}, 42, "req")
	if !errors.Is(err, ErrOnboardingInProgress) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "进行中") {
		t.Fatalf("文案要能直接给用户看：%s", err.Error())
	}
}

// TestOnboardingCompleteNodeGuards 节点完成的守卫：已完成/流程已结束/缺原因/状态不对。
func TestOnboardingCompleteNodeGuards(t *testing.T) {
	baseFlow := func() *OnboardingNodeContext {
		return &OnboardingNodeContext{
			NodeID: 31, NodeKey: OnboardingNodeConfig, NodeStatus: OnboardingNodeInProgress,
			FlowID: 7, FlowStatus: OnboardingFlowProcessing, CustomerProfileID: 5,
		}
	}
	cases := []struct {
		name   string
		mutate func(node *OnboardingNodeContext, in *CompleteNodeInput)
		want   error
	}{
		{"缺说明", func(_ *OnboardingNodeContext, in *CompleteNodeInput) { in.Reason = "   " }, ErrOnboardingReasonRequired},
		{"节点已完成", func(node *OnboardingNodeContext, _ *CompleteNodeInput) { node.NodeStatus = OnboardingNodeCompleted }, ErrOnboardingNodeCompleted},
		{"流程已结束", func(node *OnboardingNodeContext, _ *CompleteNodeInput) { node.FlowStatus = OnboardingFlowCompleted }, ErrOnboardingNodeCompleted},
		{"CONFIG 还在等待前序", func(node *OnboardingNodeContext, _ *CompleteNodeInput) { node.NodeStatus = OnboardingNodeWaiting }, ErrOnboardingNodeNotReady},
		{"CONTRACT 还在等待前序", func(node *OnboardingNodeContext, _ *CompleteNodeInput) {
			node.NodeKey = OnboardingNodeContract
			node.NodeStatus = OnboardingNodeWaiting
		}, ErrOnboardingNodeNotReady},
		{"未知节点类型", func(node *OnboardingNodeContext, _ *CompleteNodeInput) { node.NodeKey = "ACCOUNT" }, ErrOnboardingNodeNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node := baseFlow()
			store := &fakeOnboardingStore{nodeCtx: node}
			svc := newOnboardingServiceWith(store, nil)
			in := CompleteNodeInput{NodeID: 31, Reason: "完成", Operator: 42, Request: "req"}
			c.mutate(node, &in)
			_, err := svc.CompleteNode(context.Background(), in)
			if !errors.Is(err, c.want) {
				t.Fatalf("期望 %v，got %v", c.want, err)
			}
			if len(store.configCalls)+len(store.contractCalls) != 0 {
				t.Fatal("守卫拦下时不得写库")
			}
		})
	}
}

// TestOnboardingCompleteConfigDispatches CONFIG 完成走 CompleteConfig，不需要就绪校验。
func TestOnboardingCompleteConfigDispatches(t *testing.T) {
	store := &fakeOnboardingStore{nodeCtx: &OnboardingNodeContext{
		NodeID: 31, NodeKey: OnboardingNodeConfig, NodeStatus: OnboardingNodeInProgress,
		FlowID: 7, FlowStatus: OnboardingFlowProcessing, CustomerProfileID: 5,
	}}
	svc := newOnboardingServiceWith(store, nil)
	flow, err := svc.CompleteNode(context.Background(), CompleteNodeInput{NodeID: 31, Reason: "资源已配好", Operator: 42, Request: "req-9"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if flow.Status != OnboardingFlowCompleted {
		t.Fatalf("CONFIG 完成后流程应 COMPLETED，got %s", flow.Status)
	}
	if len(store.configCalls) != 1 || len(store.contractCalls) != 0 {
		t.Fatalf("应只调用 CompleteConfig，got config=%d contract=%d", len(store.configCalls), len(store.contractCalls))
	}
	if store.configCalls[0].RequestID != "req-9" || store.configCalls[0].OperatorID != 42 {
		t.Fatalf("操作员/请求号未透传：%+v", store.configCalls[0])
	}
}

// TestOnboardingCompleteContractUsesOperatorConfirmation CONTRACT 完成走「办理人确认」而不是 F12 就绪。
//
// 背景：F12 就绪快照目前只覆盖供应商（唯一实现要求 supplier + submission 审批留痕），
// 客户侧走不通。所以这里的口径是：只要节点是 IN_PROGRESS 且填了说明就允许完成，
// 完成的依据就是办理人的确认（写进节点 note 与审计 reason）。
func TestOnboardingCompleteContractUsesOperatorConfirmation(t *testing.T) {
	store := &fakeOnboardingStore{nodeCtx: &OnboardingNodeContext{
		NodeID: 30, NodeKey: OnboardingNodeContract, NodeStatus: OnboardingNodeInProgress,
		FlowID: 7, FlowStatus: OnboardingFlowProcessing, CustomerProfileID: 5,
		ConfigNodeStatus: OnboardingNodeWaiting,
	}}
	svc := newOnboardingServiceWith(store, nil)

	if _, err := svc.CompleteNode(context.Background(), CompleteNodeInput{NodeID: 30, Reason: "合同与授信已归档", Operator: 42, Request: "req"}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(store.contractCalls) != 1 || len(store.configCalls) != 0 {
		t.Fatalf("应只调用 CompleteContract，got contract=%d config=%d", len(store.contractCalls), len(store.configCalls))
	}
	call := store.contractCalls[0]
	if call.Reason != "合同与授信已归档" || call.OperatorID != 42 || call.RequestID != "req" {
		t.Fatalf("确认说明/操作员/请求号未透传：%+v", call)
	}

	// 没写说明就不许确认（这是唯一的"资料已归档"依据）。
	store2 := &fakeOnboardingStore{nodeCtx: store.nodeCtx}
	svc2 := newOnboardingServiceWith(store2, nil)
	if _, err := svc2.CompleteNode(context.Background(), CompleteNodeInput{NodeID: 30, Reason: "  ", Operator: 42}); !errors.Is(err, ErrOnboardingReasonRequired) {
		t.Fatalf("缺说明应被拒，got %v", err)
	}
	if len(store2.contractCalls) != 0 {
		t.Fatal("缺说明不得完成")
	}
}

// TestOnboardingGetBuildsDeeplink 读流程时后端拼好 deeplink；没有流程 → 明确 404 语义。
func TestOnboardingGetBuildsDeeplink(t *testing.T) {
	store := &fakeOnboardingStore{flow: &OnboardingFlow{ID: 3, Status: OnboardingFlowProcessing, CurrentNode: OnboardingNodeConfig}}
	svc := newOnboardingServiceWith(store, nil)
	flow, err := svc.Get(context.Background(), 5)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if flow.Deeplink != "/internal/customers/5?tab=onboarding" {
		t.Fatalf("deeplink 不符：%q", flow.Deeplink)
	}

	empty := newOnboardingServiceWith(&fakeOnboardingStore{}, nil)
	if _, err := empty.Get(context.Background(), 5); !errors.Is(err, ErrOnboardingFlowNotFound) {
		t.Fatalf("没有流程应返回 ErrOnboardingFlowNotFound，got %v", err)
	}
}

// TestOnboardingWithoutStoreFailsLoudly 未装配时显式报错。
func TestOnboardingWithoutStoreFailsLoudly(t *testing.T) {
	var svc *OnboardingService
	if _, err := svc.Get(context.Background(), 1); !errors.Is(err, ErrOnboardingUnavailable) {
		t.Fatalf("got %v", err)
	}
	if _, err := NewOnboardingService(nil, nil).CompleteNode(context.Background(), CompleteNodeInput{NodeID: 1, Reason: "x"}); !errors.Is(err, ErrOnboardingUnavailable) {
		t.Fatalf("got %v", err)
	}
}
