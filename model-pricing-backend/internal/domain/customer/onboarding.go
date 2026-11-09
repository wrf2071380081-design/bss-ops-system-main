package customer

import (
	"context"
	"errors"
	"fmt"
	"log"
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
// 与 F12 的关系（重要，别当成漏了）：
// **F12 就绪快照目前只覆盖供应商**。类型层允许 subject_type=CUSTOMER
// （internal/domain/supply/f12_read.go:105），但唯一实现
// internal/infra/supplypg/f12_native_flow.go:54 对非 SUPPLIER 直接返回
// ErrDependencyUnavailable——它的 SQL 要 join submission/operation_log 的供应商审批留痕，
// 而客户没有 submission。所以客户侧的 CONTRACT 完成**不走就绪快照**，改成
// 「办理人确认合同与授信已归档 + 必填说明」，也不去伪造 F12 数据、不建
// customer_profile→customer 的映射（那会把废弃的 000033 表拖回主链路）。
// 等以后真的做客户侧 F12 就绪（需要先有那层映射），再把这一段换成就绪校验。

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
	ErrOnboardingReasonRequired      = errors.New("请填写本次操作说明")
	// ErrOnboardingContractDocs 对齐原型：商务完成不是"点一下确认"，而是两份资料真的在档。
	ErrOnboardingContractDocs = errors.New("客户合同与授信资料未齐：请先在「档案与办理」归档合同并登记授信额度")
	// ErrOnboardingConfigDone 对齐原型：资源配置完成后，本批就不能再往里加模型/换来源了。
	ErrOnboardingConfigDone = errors.New("本次开通的资源配置已完成，不能再追加模型或更换来源")
	// ErrOnboardingOperationInvalid 是「操作登记」超长（只做长度校验，不做语义解析）。
	ErrOnboardingOperationInvalid = errors.New("操作登记内容过长：操作时间 ≤32 字、操作人员 ≤64 字、完成依据 ≤500 字")
)

// OnboardingModelInput 是本次开通指定的「标准模型 × 供应资源」。
type OnboardingModelInput struct {
	StandardModelID int64 `json:"standard_model_id"`
	ResourceID      int64 `json:"resource_id"`
}

// OperationRecord 是「配置资源」这一步的人工操作登记。
//
// 对齐原型：技术先去生产平台（网关）把来源配好，再回平台登记「什么时候、谁、凭什么」。
// 我们这边 BSS 自己写绑定，时间和人本来就有（operation_log 记的是点按钮的人），
// 缺的是**生产侧**的凭证——可能是在别的机器上操作的另一个人、另一个时间。
// 所以三项都选填：填了就以登记为准写进节点说明与审计，不填就用系统口径，不挡流程。
type OperationRecord struct {
	ExecutedAt string `json:"executed_at"` // 实际操作时间（北京时间，原样留痕，不解析）
	ExecutedBy string `json:"executed_by"` // 实际操作人员（可与登录人不同）
	Evidence   string `json:"evidence"`    // 完成依据：工单号 / 变更单 / 操作截图链接
}

// normalize 去空白并做长度校验（这些都只进审计与备注，不做语义解析）。
func (r *OperationRecord) normalize() (*OperationRecord, error) {
	if r == nil {
		return nil, nil
	}
	out := &OperationRecord{
		ExecutedAt: strings.TrimSpace(r.ExecutedAt),
		ExecutedBy: strings.TrimSpace(r.ExecutedBy),
		Evidence:   strings.TrimSpace(r.Evidence),
	}
	if len(out.ExecutedAt) > 32 || len(out.ExecutedBy) > 64 || len(out.Evidence) > 500 {
		return nil, ErrOnboardingOperationInvalid
	}
	if out.ExecutedAt == "" && out.ExecutedBy == "" && out.Evidence == "" {
		return nil, nil
	}
	return out, nil
}

// Summary 拼成一行写进节点说明，例：
// 实际操作 2026-11-08 12:00 · 操作人 张三 · 依据 WO-2026-0912
func (r *OperationRecord) Summary() string {
	if r == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if r.ExecutedAt != "" {
		parts = append(parts, "实际操作 "+r.ExecutedAt)
	}
	if r.ExecutedBy != "" {
		parts = append(parts, "操作人 "+r.ExecutedBy)
	}
	if r.Evidence != "" {
		parts = append(parts, "依据 "+r.Evidence)
	}
	return strings.Join(parts, " · ")
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
//
// F12Reference 是可选的外部凭据（合同号 / F12 记录号）：客户侧不走 F12 就绪校验
// （该能力目前只有供应商 provider），所以把办理人手里的凭据原样留痕到审计，
// 便于日后审计追溯「凭什么说合同与授信已归档」。
type CompleteNodeInput struct {
	NodeID       int64  `json:"-"`
	Reason       string `json:"reason"`
	F12Reference string `json:"f12_reference"`
	Operator     int64  `json:"-"`
	Request      string `json:"-"`
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
// 没有就绪摘要：客户侧不走 F12 就绪（只覆盖供应商），办理人说明即确认依据；
// F12Reference 是可选的外部凭据，原样写进审计 after。
type CompleteContractRecord struct {
	FlowID       int64
	NodeID       int64
	OperatorID   int64
	RequestID    string
	Reason       string
	F12Reference string
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
	// AddFlowModels 把「模型 × 资源」追加到进行中的流程（不改节点状态）。
	// op 是可选的操作登记（生产侧时间/人员/依据），只进审计，不参与业务判断。
	AddFlowModels(ctx context.Context, flowID int64, models []OnboardingModelInput, op *OperationRecord, operatorID int64, requestID string) error
	// AutoConfigReady 判断本次申请的批准模型是否已全部配上来源。
	// 配齐即自动完成 CONFIG（对齐原型「绑完即生效」），所以判据必须和「接入模型」页的
	// 待配置行同口径，否则会出现「界面已经没有待配置行、节点却还挂着」。
	AutoConfigReady(ctx context.Context, customerProfileID, flowID int64) (bool, error)
	// OpenCustomerTaskAssignees 读审批派发但还没办完的两类待办责任人
	// （CUSTOMER_MODELS / CUSTOMER_CONTRACT）。用于「审批已派活但历史上没建流程」的补偿。
	OpenCustomerTaskAssignees(ctx context.Context, customerProfileID int64) (modelsAssignee, contractAssignee int64, err error)
	// CustomerBusinessDocsReady 判断客户的合同与授信资料是否已齐：
	// 至少一份已归档合同 + 至少一条授信额度 > 0 的授信记录。
	// 对齐原型「商务办理 = 两份资料传齐」，用来在完成 CONTRACT 节点前把关。
	CustomerBusinessDocsReady(ctx context.Context, customerProfileID int64) (bool, error)
}

// OnboardingService 客户开通流程服务。
type OnboardingService struct {
	store     OnboardingStore
	resources ResourceLinkStore
}

// NewOnboardingService 构造。resources 复用绑定仓储的资源事实读取，避免第二份口径。
func NewOnboardingService(store OnboardingStore, resources ResourceLinkStore) *OnboardingService {
	return &OnboardingService{store: store, resources: resources}
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

// AddModels 给进行中的开通流程追加「模型 × 资源」（原型行内「配置资源」的落点）。
//
// 为什么要单独一个动作：原型的配源是逐模型的，而我们最早的实现要求"发起开通时一次性选死"，
// 导致流程跑起来之后再想给某个模型补一条来源只能作废重来。这里补上增量路径：
// 只要 CONFIG 还没完成，就可以继续往里加（同一次开通里同一资源不可重复，靠唯一索引兜底）。
func (s *OnboardingService) AddModels(ctx context.Context, customerProfileID int64, models []OnboardingModelInput, op *OperationRecord, operatorID int64, requestID string) (*OnboardingFlow, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if customerProfileID <= 0 {
		return nil, ErrCustomerProfileNotFound
	}
	if len(models) == 0 {
		return nil, ErrOnboardingModelsRequired
	}
	record, err := op.normalize()
	if err != nil {
		return nil, err
	}
	flow, err := s.store.GetFlow(ctx, customerProfileID)
	if err != nil {
		return nil, err
	}
	if flow == nil {
		return nil, ErrOnboardingFlowNotFound
	}
	if flow.Status != OnboardingFlowProcessing {
		return nil, ErrOnboardingConfigDone
	}
	for _, node := range flow.Nodes {
		if node.Key == OnboardingNodeConfig && node.Status == OnboardingNodeCompleted {
			return nil, ErrOnboardingConfigDone
		}
	}
	customerRegion, err := s.store.CustomerRegion(ctx, customerProfileID)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(models))
	for _, model := range models {
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
	if err := s.store.AddFlowModels(ctx, flow.ID, models, record, operatorID, requestID); err != nil {
		return nil, err
	}
	next, err := s.store.GetFlow(ctx, customerProfileID)
	if err != nil {
		return nil, err
	}
	return s.autoCompleteConfigIfReady(ctx, next, record, operatorID, requestID)
}

// autoConfigReason 是自动完成 CONFIG 写进节点说明/审计的文案。
const autoConfigReason = "本次申请的模型已全部配置供应来源，系统自动完成资源配置"

// autoCompleteConfigIfReady 配齐后自动完成 CONFIG（对齐原型：绑完即生效）。
//
// 原型里技术线没有"确认完成"这个动作——每个模型绑上来源就算办完，界面只显示
// 「已绑定资源数 0/2 → 2/2」。我们因为借用了供应商的流程节点表，"完成"必须落成
// 一个显式的节点动作，于是变成了「接入模型配来源 + 档案与办理点完成节点」两步，
// 技术配完通常不知道还差一步，模型就永远不生效。这里把第二步收进配齐这个瞬间。
//
// 只在这两个前提下动手：CONFIG 处于「办理中」、且本次申请的批准模型已全部有来源。
// 自动完成失败**不向用户抛错**——典型失败是某个模型已有生效来源（撞唯一索引），
// 此时流程留在原地，用户仍可手工点「完成节点」拿到明确报错。
func (s *OnboardingService) autoCompleteConfigIfReady(ctx context.Context, flow *OnboardingFlow, op *OperationRecord, operatorID int64, requestID string) (*OnboardingFlow, error) {
	if flow == nil || flow.Status != OnboardingFlowProcessing {
		return flow, nil
	}
	var config *OnboardingNode
	for i := range flow.Nodes {
		if flow.Nodes[i].Key == OnboardingNodeConfig {
			config = &flow.Nodes[i]
			break
		}
	}
	if config == nil || config.Status != OnboardingNodeInProgress {
		return flow, nil
	}
	ready, err := s.store.AutoConfigReady(ctx, flow.CustomerProfileID, flow.ID)
	if err != nil {
		log.Printf("[auto-config] flow %d readiness check failed: %v", flow.ID, err)
		return flow, nil
	}
	if !ready {
		return flow, nil
	}
	// 节点说明 = 系统口径 + 办理人登记的生产侧凭据（有则附上，方便审计对上生产日志）。
	reason := autoConfigReason
	if summary := op.Summary(); summary != "" {
		reason += "（" + summary + "）"
	}
	completed, err := s.store.CompleteConfig(ctx, CompleteConfigRecord{
		FlowID:     flow.ID,
		OperatorID: operatorID,
		RequestID:  requestID,
		Reason:     reason,
	})
	if err != nil {
		// 自动完成是隐式行为，失败必须留痕：否则用户只看到"配了却没生效"，
		// 排查时无从判断是判据没命中还是写绑定被挡。
		log.Printf("[auto-config] flow %d complete failed: %v", flow.ID, err)

		return flow, nil
	}
	return completed, nil
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
		// 对齐原型：商务完成 = 合同与授信两份资料真的在档，不是"点一下确认"。
		// 资料没齐就不放行，避免客户按「已归档」去配资源、实际没有合同依据。
		ready, err := s.store.CustomerBusinessDocsReady(ctx, node.CustomerProfileID)
		if err != nil {
			return nil, err
		}
		if !ready {
			return nil, ErrOnboardingContractDocs
		}
		// 不走 F12 就绪（只覆盖供应商，见文件头说明）：办理人填写的说明就是确认依据。
		return s.store.CompleteContract(ctx, CompleteContractRecord{
			FlowID: node.FlowID, NodeID: node.NodeID, OperatorID: in.Operator,
			RequestID: in.Request, Reason: reason,
			F12Reference: strings.TrimSpace(in.F12Reference),
		})
	default:
		return nil, ErrOnboardingNodeNotFound
	}
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

// EnsureFlowFromTasks 兜底：审批已经派了办理待办、但客户名下还没有开通流程时，把流程补出来。
//
// 为什么会缺：000063 之前审批只改状态不建流程，靠人工点「发起开通」；
// 那个入口要求先选供应来源（技术的活），商务点进去只能卡住。
// 这里按待办的责任人直接把流程补齐：有「合同与授信」待办 → 首单（CONTRACT 先办），
// 没有 → 只建 CONFIG。已有进行中流程时幂等返回，不重复建。
func (s *OnboardingService) EnsureFlowFromTasks(ctx context.Context, customerProfileID int64, operatorID int64, requestID string) (*OnboardingFlow, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if customerProfileID <= 0 {
		return nil, ErrCustomerProfileNotFound
	}
	current, err := s.store.GetFlow(ctx, customerProfileID)
	if err != nil {
		return nil, err
	}
	if current != nil && current.Status == OnboardingFlowProcessing {
		return current, nil
	}
	modelsAssignee, contractAssignee, err := s.store.OpenCustomerTaskAssignees(ctx, customerProfileID)
	if err != nil {
		return nil, err
	}
	// 兜底：待办都被关掉时用当前操作人，至少让流程可以被推进（办理人之后能改派）。
	if modelsAssignee <= 0 {
		modelsAssignee = operatorID
	}
	// 有「合同与授信」待办 → 首单：CONTRACT 先办、CONFIG 等合同完成后再办。
	firstOrder := contractAssignee > 0
	if !firstOrder {
		contractAssignee = 0
	}
	return s.store.StartFlow(ctx, StartFlowRecord{
		CustomerProfileID:  customerProfileID,
		FirstOrder:         firstOrder,
		ContractAssigneeID: contractAssignee,
		ConfigAssigneeID:   modelsAssignee,
		OperatorID:         operatorID,
		RequestID:          requestID,
		Reason:             "审批已派单但缺少接入流程，按待办责任人补建",
	})
}
