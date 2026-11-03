package customer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 客户开通流程（G4-B2）。
//
// 为什么客户侧单独实现、不复用供应商流程（路线 B）：
//   - 供应商流程的每一步都挂在 `submission` / `submission_row` 上（审批 → 资源配置 →
//     资源准入 → 合同授信），而客户没有 submission 表；硬套共用函数会把 supplier 语义
//     漏进 customer 分支（`native_handling_approval.go` 里那堆 `f.kind='SUPPLIER'` 断言就是证据）。
//   - 所以客户流程只复用**表结构**（onboarding_flow / onboarding_node）、唯一索引与审计写法，
//     建流程与推进逻辑另写一份，供应商链路一行不动（零回归面）。
//
// 节点顺序（已定口径）：
//   首单：CONTRACT（合同与授信）→ CONFIG（资源配置）
//   非首单：CONFIG
// CONFIG 完成 = 真正给客户开通服务：按流程里保存的模型写 customer_resource_link（ACTIVE）。
//
// 与 F12 的关系：CONTRACT 完成必须「F12 合同与授信资料齐备」，通过
// CustomerContractReadiness 端口读取就绪快照——**不伪造数据**；
// 快照不支持客户主体时该端口会返回不可就绪并给出可读原因。

// 流程与节点词表（与 000033 DDL / 现有代码保持一致）。
const (
	OnboardingKindCustomer = "CUSTOMER"

	OnboardingNodeContract = "CONTRACT"
	OnboardingNodeConfig   = "CONFIG"

	OnboardingNodeWaiting    = "WAITING_PREDECESSOR"
	OnboardingNodeInProgress = "IN_PROGRESS"
	OnboardingNodeCompleted  = "COMPLETED"

	OnboardingFlowProcessing = "PROCESSING"
	OnboardingFlowCompleted  = "COMPLETED"
)

var (
	ErrOnboardingUnavailable         = errors.New("客户开通能力不可用")
	ErrOnboardingModelsRequired      = errors.New("请至少选择一个模型与供应资源")
	ErrOnboardingModelInvalid        = errors.New("开通模型参数非法")
	ErrOnboardingModelDuplicated     = errors.New("同一个供应资源不能在本次开通里重复出现")
	ErrOnboardingResourceNotAdmitted = errors.New("供应资源不是已准入状态，不能开通给客户")
	ErrOnboardingRegionMismatch      = errors.New("供应资源的部署节点与客户主体区域不一致")
	ErrOnboardingAssigneeRequired    = errors.New("请选择资源配置办理人")
	ErrOnboardingContractAssignee    = errors.New("首单开通必须选择合同与授信办理人")
	ErrOnboardingInProgress          = errors.New("该客户已有进行中的开通流程，请先完成后再发起")
	ErrOnboardingFlowNotFound        = errors.New("该客户还没有开通流程")
	ErrOnboardingNodeNotFound        = errors.New("开通节点不存在")
	ErrOnboardingNodeCompleted       = errors.New("该开通节点已完成，不能重复提交")
	ErrOnboardingNodeNotReady        = errors.New("前序节点尚未完成，暂不能办理该节点")
	ErrOnboardingContractNotReady    = errors.New("合同与授信资料尚未齐备，不能完成该节点")
	ErrOnboardingReasonRequired      = errors.New("请填写本次操作说明")
)

// OnboardingModelInput 是本次开通指定的「标准模型 × 供应资源」。
type OnboardingModelInput struct {
	StandardModelID int64 `json:"standard_model_id"`
	ResourceID      int64 `json:"resource_id"`
}

// StartOnboardingInput 是发起开通的入参。
type StartOnboardingInput struct {
	CustomerProfileID  int64                  `json:"-"`
	FirstOrder         bool                   `json:"first_order"`
	ContractAssigneeID int64                  `json:"contract_assignee_id"`
	ConfigAssigneeID   int64                  `json:"config_assignee_id"`
	Models             []OnboardingModelInput `json:"models"`
	Reason             string                 `json:"reason"`
}

// CompleteNodeInput 是节点完成的入参。
type CompleteNodeInput struct {
	NodeID   int64  `json:"-"`
	Reason   string `json:"reason"`
	Operator int64  `json:"-"`
	Request  string `json:"-"`
}

// OnboardingNode 是节点投影。
type OnboardingNode struct {
	ID           int64      `json:"id"`
	Key          string     `json:"key"`
	Status       string     `json:"status"`
	AssigneeID   int64      `json:"assignee_id"`
	AssigneeName string     `json:"assignee_name"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	Note         string     `json:"note,omitempty"`
}

// OnboardingModelRow 是流程里保存的模型明细（含展示用的名字）。
type OnboardingModelRow struct {
	StandardModelID int64  `json:"standard_model_id"`
	ModelName       string `json:"model_name"`
	ResourceID      int64  `json:"resource_id"`
	SupplierName    string `json:"supplier_name"`
	Region          string `json:"region"`
}

// OnboardingFlow 是客户开通流程的读模型。
type OnboardingFlow struct {
	ID                int64                `json:"id"`
	CustomerProfileID int64                `json:"customer_profile_id"`
	Status            string               `json:"status"`
	CurrentNode       string               `json:"current_node"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
	Nodes             []OnboardingNode     `json:"nodes"`
	Models            []OnboardingModelRow `json:"models"`
	// Links 是本次模型对应的客户来源绑定（含其它来源的历史绑定，前端按 model+resource 对齐）。
	Links []ResourceLink `json:"links"`
	// Deeplink 由后端拼好，前端直接用，避免各端各拼一套。
	Deeplink string `json:"deeplink"`
}

// StartFlowRecord 是建流程的落库参数（领域层产出，仓储不做业务判断）。
type StartFlowRecord struct {
	CustomerProfileID  int64
	FirstOrder         bool
	ContractAssigneeID int64
	ConfigAssigneeID   int64
	Models             []OnboardingModelInput
	OperatorID         int64
	RequestID          string
	Reason             string
}

// CompleteConfigRecord 是 CONFIG 节点完成的落库参数。
type CompleteConfigRecord struct {
	FlowID     int64
	OperatorID int64
	RequestID  string
	Reason     string
}

// CompleteContractRecord 是 CONTRACT 节点完成的落库参数。
type CompleteContractRecord struct {
	FlowID       int64
	NodeID       int64
	OperatorID   int64
	RequestID    string
	Reason       string
	ReadinessSum string // 就绪摘要（写审计 after 用，便于追溯当时依据）
}

// OnboardingNodeContext 是完成节点前需要读到的上下文。
type OnboardingNodeContext struct {
	NodeID            int64
	NodeKey           string
	NodeStatus        string
	NodeAssigneeID    int64
	FlowID            int64
	FlowStatus        string
	CustomerProfileID int64
	ConfigNodeStatus  string // CONFIG 节点当前状态（诊断/审计用）
	ContractCompleted bool   // CONTRACT 是否已完成（诊断用）
}

// OnboardingStore 是客户开通流程的持久化端口。
type OnboardingStore interface {
	// CustomerRegion 读客户主体区域（校验开通资源必须同节点）。
	CustomerRegion(ctx context.Context, customerProfileID int64) (string, error)
	// StartFlow 单事务建流程 + 节点 + 模型明细 + 审计。
	StartFlow(ctx context.Context, in StartFlowRecord) (*OnboardingFlow, error)
	// LoadNodeContext 读取节点上下文（完成节点前校验）。
	LoadNodeContext(ctx context.Context, nodeID int64) (*OnboardingNodeContext, error)
	// CompleteConfig 单事务写绑定 + 完成 CONFIG + 推进/收尾流程 + 审计。
	CompleteConfig(ctx context.Context, in CompleteConfigRecord) (*OnboardingFlow, error)
	// CompleteContract 单事务完成 CONTRACT + 把 CONFIG 推到 IN_PROGRESS + 审计。
	CompleteContract(ctx context.Context, in CompleteContractRecord) (*OnboardingFlow, error)
	// GetFlow 读最近一条流程（含节点、模型、绑定）。
	GetFlow(ctx context.Context, customerProfileID int64) (*OnboardingFlow, error)
}

// CustomerContractReadiness 判断「该客户该流程的合同与授信资料是否齐备」。
//
// 由 api 层用 F12 就绪快照实现（subject_type=CUSTOMER）。刻意做成端口而不是直接依赖
// supply 包：领域层不该知道 F12 的 SQL 细节，也便于单测打桩。
type CustomerContractReadiness interface {
	Ready(ctx context.Context, customerProfileID, flowID int64) (ready bool, summary string, err error)
}

// OnboardingService 客户开通流程服务。
type OnboardingService struct {
	store     OnboardingStore
	resources ResourceLinkStore
	readiness CustomerContractReadiness
}

// NewOnboardingService 构造。resources 复用绑定仓储的资源事实读取，避免第二份口径。
func NewOnboardingService(store OnboardingStore, resources ResourceLinkStore, readiness CustomerContractReadiness) *OnboardingService {
	return &OnboardingService{store: store, resources: resources, readiness: readiness}
}

func (s *OnboardingService) available() error {
	if s == nil || s.store == nil || s.resources == nil {
		return ErrOnboardingUnavailable
	}
	return nil
}

// Start 发起开通：校验模型与办理人后建流程。
//
// 模型校验逐条对照资源事实（存在 / 模型一致 / **与客户同部署节点** / 已准入）：
// 跨节点开通会让客户买到另一地的库存，是明确的业务错误，不能只靠前端筛选拦。
func (s *OnboardingService) Start(ctx context.Context, in StartOnboardingInput, operatorID int64, requestID string) (*OnboardingFlow, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if in.CustomerProfileID <= 0 {
		return nil, ErrCustomerProfileNotFound
	}
	if in.ConfigAssigneeID <= 0 {
		return nil, ErrOnboardingAssigneeRequired
	}
	if in.FirstOrder && in.ContractAssigneeID <= 0 {
		return nil, ErrOnboardingContractAssignee
	}
	if len(in.Models) == 0 {
		return nil, ErrOnboardingModelsRequired
	}
	customerRegion, err := s.store.CustomerRegion(ctx, in.CustomerProfileID)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(in.Models))
	for _, model := range in.Models {
		if model.StandardModelID <= 0 || model.ResourceID <= 0 {
			return nil, ErrOnboardingModelInvalid
		}
		if seen[model.ResourceID] {
			return nil, ErrOnboardingModelDuplicated
		}
		seen[model.ResourceID] = true
		facts, err := s.resources.LoadResourceFacts(ctx, model.ResourceID)
		if err != nil {
			return nil, err
		}
		if facts == nil {
			return nil, ErrLinkResourceNotFound
		}
		if facts.StandardModelID != model.StandardModelID {
			return nil, ErrLinkModelMismatch
		}
		if customerRegion != "" && facts.Region != customerRegion {
			return nil, ErrOnboardingRegionMismatch
		}
		if facts.State != resourceStateAdmitted {
			return nil, ErrOnboardingResourceNotAdmitted
		}
	}
	return s.store.StartFlow(ctx, StartFlowRecord{
		CustomerProfileID:  in.CustomerProfileID,
		FirstOrder:         in.FirstOrder,
		ContractAssigneeID: in.ContractAssigneeID,
		ConfigAssigneeID:   in.ConfigAssigneeID,
		Models:             in.Models,
		OperatorID:         operatorID,
		RequestID:          requestID,
		Reason:             strings.TrimSpace(in.Reason),
	})
}

// CompleteNode 完成节点：CONFIG 写绑定并收尾，CONTRACT 校验就绪后解锁 CONFIG。
func (s *OnboardingService) CompleteNode(ctx context.Context, in CompleteNodeInput) (*OnboardingFlow, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if in.NodeID <= 0 {
		return nil, ErrOnboardingNodeNotFound
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return nil, ErrOnboardingReasonRequired
	}
	node, err := s.store.LoadNodeContext(ctx, in.NodeID)
	if err != nil {
		return nil, err
	}
	if node.NodeStatus == OnboardingNodeCompleted {
		return nil, ErrOnboardingNodeCompleted
	}
	if node.FlowStatus != OnboardingFlowProcessing {
		return nil, ErrOnboardingNodeCompleted
	}

	switch node.NodeKey {
	case OnboardingNodeConfig:
		if node.NodeStatus != OnboardingNodeInProgress {
			return nil, ErrOnboardingNodeNotReady
		}
		return s.store.CompleteConfig(ctx, CompleteConfigRecord{
			FlowID: node.FlowID, OperatorID: in.Operator, RequestID: in.Request, Reason: reason,
		})
	case OnboardingNodeContract:
		if node.NodeStatus != OnboardingNodeInProgress {
			// 还在 WAITING_PREDECESSOR：说明前序（理论上无）没完成，或节点未启用。
			return nil, ErrOnboardingNodeNotReady
		}
		ready, summary, err := s.contractReadiness(ctx, node.CustomerProfileID, node.FlowID)
		if err != nil {
			return nil, err
		}
		if !ready {
			return nil, fmt.Errorf("%w：%s", ErrOnboardingContractNotReady, summary)
		}
		return s.store.CompleteContract(ctx, CompleteContractRecord{
			FlowID: node.FlowID, NodeID: node.NodeID, OperatorID: in.Operator,
			RequestID: in.Request, Reason: reason, ReadinessSum: summary,
		})
	default:
		return nil, ErrOnboardingNodeNotFound
	}
}

// contractReadiness 读 F12 就绪；未装配时明确不可完成（不伪造数据）。
func (s *OnboardingService) contractReadiness(ctx context.Context, customerProfileID, flowID int64) (bool, string, error) {
	if s.readiness == nil {
		return false, "合同与授信就绪校验能力未装配，请联系技术人员", nil
	}
	ready, summary, err := s.readiness.Ready(ctx, customerProfileID, flowID)
	if err != nil {
		return false, "", err
	}
	return ready, summary, nil
}

// Get 读该客户最近一条开通流程。
func (s *OnboardingService) Get(ctx context.Context, customerProfileID int64) (*OnboardingFlow, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if customerProfileID <= 0 {
		return nil, ErrCustomerProfileNotFound
	}
	flow, err := s.store.GetFlow(ctx, customerProfileID)
	if err != nil {
		return nil, err
	}
	if flow == nil {
		return nil, ErrOnboardingFlowNotFound
	}
	flow.Deeplink = fmt.Sprintf("/internal/customers/%d?tab=onboarding", customerProfileID)
	return flow, nil
}
