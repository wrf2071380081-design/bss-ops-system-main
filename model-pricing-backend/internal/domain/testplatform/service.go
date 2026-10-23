// Package testplatform 负责"发起测试 → 收结果 → 落报告"的编排。
//
// 它不关心 HTTP 细节（那是 infra/testplatform），也不关心 SQL（那是 infra/supplypg），
// 只定义端口与判定口径。口径来源：文档资料/测试平台接入-功能盘点与决策清单-20261023.md
//
//	Q1  提交申报看「完成+数据完整」，登记接入看「通过」 —— 这里只产出事实，不决定门禁
//	Q3  连通性判定用 baseline（L1），耗时取 L1 minimal_chat
//	Q9  一期只做 baseline + stress
package testplatform

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrNotConfigured 表示测试平台未配置，调用方应跳过而不是报错。
var ErrNotConfigured = errors.New("test platform is not configured")

// ErrInvalidCommand 表示发起参数不合法（缺凭据、模型为空等）。
var ErrInvalidCommand = errors.New("test platform: invalid command")

// ErrSignature 表示回调验签未通过（缺失、错误或超出时间窗）。
var ErrSignature = errors.New("test platform: callback signature rejected")

// ErrNotFound 表示任务或报告不存在。
var ErrNotFound = errors.New("test platform: not found")

// Category 是测试门类。一期只有 baseline / stress。
type Category string

const (
	CategoryBaseline Category = "baseline"
	CategoryStress   Category = "stress"
)

// Valid 判断门类是否在本期范围内。
func (c Category) Valid() bool { return c == CategoryBaseline || c == CategoryStress }

// Target 是被测目标（供应商提供的测试接口）。
type Target struct {
	Model    string
	BaseURL  string
	Protocol string
	APIKey   string
}

// CreateCommand 是一次发起测试的输入。
type CreateCommand struct {
	ResourceID     string
	ActorID        string
	Category       Category
	Owner          string
	Target         Target
	Params         map[string]any
	IdempotencyKey string
	ReviewOf       string
	BatchLabel     string
}

// Accepted 是平台接受任务后的回执。
type Accepted struct {
	JobID  string
	Status string
}

// JobState 是平台侧任务状态（只取本系统需要的字段）。
type JobState struct {
	JobID      string
	Status     string
	Verdict    string
	Summary    json.RawMessage
	FinishedAt *time.Time
}

// Terminal 判断是否已结束。
func (j JobState) Terminal() bool {
	switch j.Status {
	case "succeeded", "failed", "aborted", "canceled":
		return true
	}
	return false
}

// Outcome 是从平台结果里提取出的、可直接落库的事实。
type Outcome struct {
	Verdict            string
	ConnectivityPassed bool
	PressurePassed     bool
	ConnectionMS       *string
	RPS                *string
	TPS                *string
	CacheHitTokens     *int64
	CacheInputTokens   *int64
	// CacheRatioPercent 是平台直接上报的命中率（0-100）；CacheRatioProvided 区分
	// 「上报了 0%」与「根本没上报」——页面三态靠它（Q4）。
	CacheRatioPercent  *string
	CacheRatioProvided bool
	Scene              string
	// DataComplete 表示"该有的数据都有"，供 Q1 的提交门禁使用。
	DataComplete bool
}

// Gateway 是测试平台端口（infra/testplatform 实现）。
type Gateway interface {
	CreateJob(context.Context, CreateCommand) (Accepted, error)
	GetJob(context.Context, string) (JobState, error)
	DownloadReport(context.Context, string, string) ([]byte, error)
}

// NewJobRecord 是新建任务时要落库的字段。
type NewJobRecord struct {
	ResourceID     string
	Category       Category
	IdempotencyKey string
	RequestPayload []byte
	ReviewOf       string
	ActorID        string
	ExternalJobID  string
	Status         string
}

// PendingJob 是等待收口的一项任务。
type PendingJob struct {
	ID            string
	ResourceID    string
	ExternalJobID string
	Category      Category
	Status        string
	CreatedAt     time.Time
}

// Completion 是任务收口时要写回的完整结果。
type Completion struct {
	JobID              string
	ResourceID         string
	ExternalJobID      string
	Category           Category
	FinalStatus        string
	Verdict            string
	Outcome            Outcome
	ResponseSummary    []byte
	ReportJSON         []byte
	ReportMD           []byte
	TestedBy   string
	FinishedAt time.Time
}

// Store 是持久化端口（infra/supplypg 实现）。
type Store interface {
	CreateJob(context.Context, NewJobRecord) (string, error)
	AttachExternalJob(ctx context.Context, localID, externalID, status string) error
	PendingJobs(context.Context, int) ([]PendingJob, error)
	CompleteJob(context.Context, Completion) (string, error)
	// ClaimEvent 登记回调事件；返回 false 表示该 event_id 已处理过（幂等命中）。
	ClaimEvent(ctx context.Context, eventID, externalJobID string, payload []byte) (bool, error)
	// JobByExternalID 按平台任务号找本地任务。
	JobByExternalID(ctx context.Context, externalJobID string) (*PendingJob, error)
}

// Service 编排发起与收口。
type Service struct {
	gateway Gateway
	store   Store
	clock   func() time.Time
}

// NewService 构造编排服务；gateway 或 store 为空时返回 ErrNotConfigured。
func NewService(gateway Gateway, store Store, clock func() time.Time) (*Service, error) {
	if gateway == nil || store == nil {
		return nil, ErrNotConfigured
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{gateway: gateway, store: store, clock: clock}, nil
}

// StartResult 是发起测试的返回。
type StartResult struct {
	LocalJobID string
	ExternalID string
	Status     string
}

// Start 发起一次测试：先落本地任务（拿到本地 ID 用于幂等），再调用平台。
// 平台调用失败时任务保留在 QUEUED，由轮询任务重试，不会丢单。
func (s *Service) Start(ctx context.Context, cmd CreateCommand) (*StartResult, error) {
	if !cmd.Category.Valid() {
		return nil, ErrInvalidCommand
	}
	if strings.TrimSpace(cmd.ResourceID) == "" || strings.TrimSpace(cmd.ActorID) == "" {
		return nil, ErrInvalidCommand
	}
	if strings.TrimSpace(cmd.Target.Model) == "" || strings.TrimSpace(cmd.Target.BaseURL) == "" {
		return nil, ErrInvalidCommand
	}
	payload, err := json.Marshal(map[string]any{
		"category": cmd.Category, "resource_id": cmd.ResourceID,
		"model": cmd.Target.Model, "base_url": cmd.Target.BaseURL,
	})
	if err != nil {
		return nil, err
	}
	localID, err := s.store.CreateJob(ctx, NewJobRecord{
		ResourceID: cmd.ResourceID, Category: cmd.Category,
		IdempotencyKey: cmd.IdempotencyKey, RequestPayload: payload,
		ReviewOf: cmd.ReviewOf, ActorID: cmd.ActorID, Status: "QUEUED",
	})
	if err != nil {
		return nil, err
	}
	accepted, err := s.gateway.CreateJob(ctx, cmd)
	if err != nil {
		return &StartResult{LocalJobID: localID, Status: "QUEUED"}, err
	}
	if err := s.store.AttachExternalJob(ctx, localID, accepted.JobID, mapStatus(accepted.Status)); err != nil {
		return &StartResult{LocalJobID: localID, ExternalID: accepted.JobID}, err
	}
	return &StartResult{LocalJobID: localID, ExternalID: accepted.JobID, Status: accepted.Status}, nil
}

// PollResult 汇总一次轮询。
type PollResult struct {
	Scanned   int
	Completed int
	Failed    int
	Skipped   int
}

// Poll 拉取未终态任务并收口。
func (s *Service) Poll(ctx context.Context, limit int) (PollResult, error) {
	if limit <= 0 {
		limit = 50
	}
	jobs, err := s.store.PendingJobs(ctx, limit)
	if err != nil {
		return PollResult{}, err
	}
	result := PollResult{Scanned: len(jobs)}
	var firstErr error
	for _, job := range jobs {
		if job.ExternalJobID == "" {
			result.Skipped++ // 尚未拿到平台任务号，等下一轮
			continue
		}
		done, err := s.collect(ctx, job)
		switch {
		case err != nil:
			result.Failed++
			if firstErr == nil {
				firstErr = err
			}
		case done:
			result.Completed++
		default:
			result.Skipped++ // 还在跑
		}
	}
	return result, firstErr
}

// collect 处理单个任务：查状态、终态时下报告并落库。done 表示本轮已收口。
func (s *Service) collect(ctx context.Context, job PendingJob) (bool, error) {
	state, err := s.gateway.GetJob(ctx, job.ExternalJobID)
	if err != nil {
		return false, err
	}
	if !state.Terminal() {
		return false, nil
	}
	var reportJSON, reportMD []byte
	if state.Status == "succeeded" {
		reportJSON, err = s.gateway.DownloadReport(ctx, job.ExternalJobID, "json")
		if err != nil {
			return false, err
		}
		reportMD, _ = s.gateway.DownloadReport(ctx, job.ExternalJobID, "md")
	}
	outcome := Interpret(job.Category, state, reportJSON)
	summary, err := json.Marshal(map[string]any{
		"verdict": state.Verdict, "status": state.Status,
		"connectivity_passed": outcome.ConnectivityPassed,
		"pressure_passed":     outcome.PressurePassed,
		"data_complete":       outcome.DataComplete,
	})
	if err != nil {
		return false, err
	}
	finished := s.clock().UTC()
	if state.FinishedAt != nil {
		finished = state.FinishedAt.UTC()
	}
	_, err = s.store.CompleteJob(ctx, Completion{
		JobID: job.ID, ResourceID: job.ResourceID, ExternalJobID: job.ExternalJobID,
		Category: job.Category, FinalStatus: mapStatus(state.Status), Verdict: state.Verdict,
		Outcome: outcome, ResponseSummary: summary, ReportJSON: reportJSON, ReportMD: reportMD,
		TestedBy: "", FinishedAt: finished,
	})
	return err == nil, err
}

// ApplyCallback 处理平台终态回调。
//
// 幂等：event_id 是平台派生的确定性键（同一任务重复投递同 ID），先登记再处理；
// 已处理过的直接返回 duplicate=true，调用方照常回 2xx（平台 at-least-once）。
//
// 不信任回调内容：登记事件后仍以 GetJob 的平台真值为准再收口，
// 避免伪造/过期载荷直接写库。
func (s *Service) ApplyCallback(ctx context.Context, eventID, externalJobID string, payload []byte) (bool, error) {
	if strings.TrimSpace(eventID) == "" || strings.TrimSpace(externalJobID) == "" {
		return false, ErrInvalidCommand
	}
	fresh, err := s.store.ClaimEvent(ctx, eventID, externalJobID, payload)
	if err != nil {
		return false, err
	}
	if !fresh {
		return true, nil
	}
	job, err := s.store.JobByExternalID(ctx, externalJobID)
	if err != nil {
		return false, err
	}
	if _, err := s.collect(ctx, *job); err != nil {
		return false, err
	}
	return false, nil
}

// mapStatus 把平台状态映射为本地 test_job.status。
func mapStatus(status string) string {
	switch status {
	case "succeeded":
		return "SUCCEEDED"
	case "failed":
		return "FAILED"
	case "aborted":
		return "ABORTED"
	case "canceled":
		return "CANCELED"
	case "running":
		return "RUNNING"
	default:
		return "QUEUED"
	}
}

var decimalPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// FormatDecimal 把浮点转成契约要求的十进制字符串；负数与 NaN 视为缺失。
func FormatDecimal(v float64) *string {
	if v < 0 || v != v {
		return nil
	}
	formatted := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 4, 64), "0"), ".")
	if formatted == "" || !decimalPattern.MatchString(formatted) {
		return nil
	}
	return &formatted
}
