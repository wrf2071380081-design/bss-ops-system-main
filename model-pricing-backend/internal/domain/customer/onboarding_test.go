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

	// 默认「资料已齐」，否则既有 CONTRACT 完成用例都会被新前置校验挡住；
	// 要测「资料未齐」的用例显式置 docsMissing=true。
	docsMissing bool
	docsErr     error

	addedModels []OnboardingModelInput
	addedOp     *OperationRecord
	addErr      error
	// autoReady 模拟「本次申请的批准模型已全部配上来源」。
	autoReady bool

	modelsAssignee       int64
	contractTaskAssignee int64

	flow    *OnboardingFlow
	flowErr error
}

func (f *fakeOnboardingStore) CustomerRegion(_ context.Context, _ int64) (string, error) {
	return f.region, f.regionErr
}

func (f *fakeOnboardingStore) AddFlowModels(_ context.Context, flowID int64, models []OnboardingModelInput, op *OperationRecord, _ int64, _ string) error {
	f.addedModels = append(f.addedModels, models...)
	f.addedOp = op
	if f.addErr != nil {
		return f.addErr
	}
	return nil
}

func (f *fakeOnboardingStore) OpenCustomerTaskAssignees(context.Context, int64) (int64, int64, error) {
	return f.modelsAssignee, f.contractTaskAssignee, nil
}

func (f *fakeOnboardingStore) AutoConfigReady(context.Context, int64, int64) (bool, error) {
	return f.autoReady, nil
}

func (f *fakeOnboardingStore) CustomerBusinessDocsReady(_ context.Context, _ int64) (bool, error) {
	if f.docsErr != nil {
		return false, f.docsErr
	}
	return !f.docsMissing, nil
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
func (f *fakeResourceFacts) ListCustomerLinkDetails(context.Context, int64) ([]CustomerLinkDetail, error) {
	return nil, errors.New("not used")
}
func (f *fakeResourceFacts) ListLinksByResource(context.Context, int64) ([]ResourceLink, error) {
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

	if _, err := svc.CompleteNode(context.Background(), CompleteNodeInput{
		NodeID: 30, Reason: "合同与授信已归档", F12Reference: " HT-2026-001 ", Operator: 42, Request: "req",
	}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(store.contractCalls) != 1 || len(store.configCalls) != 0 {
		t.Fatalf("应只调用 CompleteContract，got contract=%d config=%d", len(store.contractCalls), len(store.configCalls))
	}
	call := store.contractCalls[0]
	if call.Reason != "合同与授信已归档" || call.OperatorID != 42 || call.RequestID != "req" {
		t.Fatalf("确认说明/操作员/请求号未透传：%+v", call)
	}
	// 可选凭据（合同号 / F12 记录号）应去空格后原样带给仓储（写审计用）。
	if call.F12Reference != "HT-2026-001" {
		t.Fatalf("F12 凭据未透传：%+v", call)
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

// TestOnboardingCompleteContractRequiresDocs 商务完成必须先有合同与授信资料（对齐原型）。
//
// 原型里商务模块的完成不是"点一下确认"，而是合同与授信两份资料真的在档。
// 只填一句说明就能把 CONTRACT 标完成，会让客户在没有合同依据的情况下被配资源开通。
func TestOnboardingCompleteContractRequiresDocs(t *testing.T) {
	node := &OnboardingNodeContext{
		NodeID: 31, NodeKey: OnboardingNodeContract, NodeStatus: OnboardingNodeInProgress,
		FlowID: 7, FlowStatus: OnboardingFlowProcessing, CustomerProfileID: 5,
	}
	in := CompleteNodeInput{NodeID: 31, Reason: "合同与授信已归档", Operator: 42, Request: "req"}

	t.Run("资料未齐时拦下", func(t *testing.T) {
		store := &fakeOnboardingStore{nodeCtx: node, docsMissing: true}
		svc := newOnboardingServiceWith(store, nil)
		if _, err := svc.CompleteNode(context.Background(), in); !errors.Is(err, ErrOnboardingContractDocs) {
			t.Fatalf("want ErrOnboardingContractDocs, got %v", err)
		}
		if len(store.contractCalls) != 0 {
			t.Fatal("资料未齐时不得写库")
		}
	})

	t.Run("资料齐了才放行", func(t *testing.T) {
		store := &fakeOnboardingStore{nodeCtx: node}
		svc := newOnboardingServiceWith(store, nil)
		if _, err := svc.CompleteNode(context.Background(), in); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if len(store.contractCalls) != 1 {
			t.Fatalf("应调用一次 CompleteContract，got %d", len(store.contractCalls))
		}
	})
}

// 追加待配置模型：CONFIG 完成后不能加；来源必须已准入且同节点。
func TestOnboardingAddModels(t *testing.T) {
	baseFlow := func() *OnboardingFlow {
		return &OnboardingFlow{
			ID: 7, CustomerProfileID: 5, Status: OnboardingFlowProcessing,
			Nodes: []OnboardingNode{{Key: OnboardingNodeConfig, Status: OnboardingNodeInProgress}},
		}
	}
	facts := map[int64]*ResourceFacts{
		100: {ID: 100, StandardModelID: 13, Region: "CN", State: resourceStateAdmitted},
		101: {ID: 101, StandardModelID: 14, Region: "OVERSEAS", State: resourceStateAdmitted},
	}

	t.Run("正常追加", func(t *testing.T) {
		store := &fakeOnboardingStore{flow: baseFlow(), region: "CN"}
		svc := newOnboardingServiceWith(store, facts)
		if _, err := svc.AddModels(context.Background(), 5, []OnboardingModelInput{{StandardModelID: 13, ResourceID: 100}}, nil, 42, "req"); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if len(store.addedModels) != 1 {
			t.Fatalf("应追加 1 条，got %+v", store.addedModels)
		}
	})

	t.Run("资源配置已完成不能再加", func(t *testing.T) {
		flow := baseFlow()
		flow.Nodes[0].Status = OnboardingNodeCompleted
		store := &fakeOnboardingStore{flow: flow, region: "CN"}
		svc := newOnboardingServiceWith(store, facts)
		_, err := svc.AddModels(context.Background(), 5, []OnboardingModelInput{{StandardModelID: 13, ResourceID: 100}}, nil, 42, "req")
		if !errors.Is(err, ErrOnboardingConfigDone) {
			t.Fatalf("want ErrOnboardingConfigDone, got %v", err)
		}
	})

	t.Run("跨部署节点的来源要拒绝", func(t *testing.T) {
		store := &fakeOnboardingStore{flow: baseFlow(), region: "CN"}
		svc := newOnboardingServiceWith(store, facts)
		_, err := svc.AddModels(context.Background(), 5, []OnboardingModelInput{{StandardModelID: 14, ResourceID: 101}}, nil, 42, "req")
		if !errors.Is(err, ErrOnboardingRegionMismatch) {
			t.Fatalf("want ErrOnboardingRegionMismatch, got %v", err)
		}
	})
}

// 配齐即自动完成 CONFIG：对齐原型「绑完即生效」，技术不用再跑去另一个页签点完成节点。
func TestOnboardingAddModelsAutoCompletesConfig(t *testing.T) {
	baseFlow := func(status string) *OnboardingFlow {
		return &OnboardingFlow{
			ID: 7, CustomerProfileID: 5, Status: OnboardingFlowProcessing,
			Nodes: []OnboardingNode{{Key: OnboardingNodeConfig, Status: status}},
		}
	}
	facts := map[int64]*ResourceFacts{
		100: {ID: 100, StandardModelID: 13, Region: "CN", State: resourceStateAdmitted},
	}
	models := []OnboardingModelInput{{StandardModelID: 13, ResourceID: 100}}

	t.Run("配齐即自动完成", func(t *testing.T) {
		store := &fakeOnboardingStore{flow: baseFlow(OnboardingNodeInProgress), region: "CN", autoReady: true}
		svc := newOnboardingServiceWith(store, facts)
		out, err := svc.AddModels(context.Background(), 5, models, nil, 42, "req")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if len(store.configCalls) != 1 {
			t.Fatalf("配齐后应自动完成 CONFIG，got %d 次", len(store.configCalls))
		}
		if store.configCalls[0].Reason != autoConfigReason {
			t.Fatalf("自动完成的说明应为系统口径，got %q", store.configCalls[0].Reason)
		}
		if out == nil || out.Status != OnboardingFlowCompleted {
			t.Fatalf("应返回完成态流程，got %+v", out)
		}
	})

	t.Run("没配齐不动节点", func(t *testing.T) {
		store := &fakeOnboardingStore{flow: baseFlow(OnboardingNodeInProgress), region: "CN"}
		svc := newOnboardingServiceWith(store, facts)
		if _, err := svc.AddModels(context.Background(), 5, models, nil, 42, "req"); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if len(store.configCalls) != 0 {
			t.Fatal("没配齐就不该自动完成")
		}
	})

	t.Run("CONFIG 还在等待前序时不自动完成", func(t *testing.T) {
		store := &fakeOnboardingStore{flow: baseFlow(OnboardingNodeWaiting), region: "CN", autoReady: true}
		svc := newOnboardingServiceWith(store, facts)
		if _, err := svc.AddModels(context.Background(), 5, models, nil, 42, "req"); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if len(store.configCalls) != 0 {
			t.Fatal("前序节点没完成时不该收尾流程")
		}
	})

	t.Run("自动完成失败不挡用户", func(t *testing.T) {
		store := &fakeOnboardingStore{
			flow: baseFlow(OnboardingNodeInProgress), region: "CN",
			autoReady: true, completeErr: ErrLinkModelAlreadyActive,
		}
		svc := newOnboardingServiceWith(store, facts)
		out, err := svc.AddModels(context.Background(), 5, models, nil, 42, "req")
		if err != nil {
			t.Fatalf("自动完成失败应降级为手动，不能回传错误: %v", err)
		}
		if len(store.configCalls) != 1 {
			t.Fatalf("应尝试过一次自动完成，got %d", len(store.configCalls))
		}
		if out == nil || out.Status != OnboardingFlowProcessing {
			t.Fatalf("失败时应返回未完成的流程，got %+v", out)
		}
	})
}

// 操作登记（对齐原型「先去生产平台操作、再回来登记」）：
// 填了就进节点说明与审计，三项全空视为没登记，不挡流程。
func TestOnboardingAddModelsOperationRecord(t *testing.T) {
	baseFlow := func() *OnboardingFlow {
		return &OnboardingFlow{
			ID: 7, CustomerProfileID: 5, Status: OnboardingFlowProcessing,
			Nodes: []OnboardingNode{{Key: OnboardingNodeConfig, Status: OnboardingNodeInProgress}},
		}
	}
	facts := map[int64]*ResourceFacts{
		100: {ID: 100, StandardModelID: 13, Region: "CN", State: resourceStateAdmitted},
	}
	models := []OnboardingModelInput{{StandardModelID: 13, ResourceID: 100}}

	t.Run("登记信息进节点说明", func(t *testing.T) {
		store := &fakeOnboardingStore{flow: baseFlow(), region: "CN", autoReady: true}
		svc := newOnboardingServiceWith(store, facts)
		op := &OperationRecord{ExecutedAt: "2026-11-08 11:30", ExecutedBy: "张三", Evidence: "WO-0912"}
		if _, err := svc.AddModels(context.Background(), 5, models, op, 42, "req"); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if store.addedOp == nil || store.addedOp.ExecutedBy != "张三" {
			t.Fatalf("登记应原样传给仓储，got %+v", store.addedOp)
		}
		if len(store.configCalls) != 1 {
			t.Fatalf("应自动完成一次，got %d", len(store.configCalls))
		}
		reason := store.configCalls[0].Reason
		for _, want := range []string{autoConfigReason, "2026-11-08 11:30", "张三", "WO-0912"} {
			if !strings.Contains(reason, want) {
				t.Fatalf("节点说明应含 %q，got %q", want, reason)
			}
		}
	})

	t.Run("三项全空等于没登记", func(t *testing.T) {
		store := &fakeOnboardingStore{flow: baseFlow(), region: "CN", autoReady: true}
		svc := newOnboardingServiceWith(store, facts)
		if _, err := svc.AddModels(context.Background(), 5, models, &OperationRecord{ExecutedAt: "   "}, 42, "req"); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if store.addedOp != nil {
			t.Fatalf("空白登记应归一为 nil，got %+v", store.addedOp)
		}
		if len(store.configCalls) != 1 || store.configCalls[0].Reason != autoConfigReason {
			t.Fatalf("没有登记时节点说明应只有系统口径，got %+v", store.configCalls)
		}
	})

	t.Run("超长登记要拒绝", func(t *testing.T) {
		store := &fakeOnboardingStore{flow: baseFlow(), region: "CN"}
		svc := newOnboardingServiceWith(store, facts)
		_, err := svc.AddModels(context.Background(), 5, models,
			&OperationRecord{Evidence: strings.Repeat("证", 501)}, 42, "req")
		if !errors.Is(err, ErrOnboardingOperationInvalid) {
			t.Fatalf("want ErrOnboardingOperationInvalid, got %v", err)
		}
		if len(store.addedModels) != 0 {
			t.Fatal("校验失败不得写库")
		}
	})
}
