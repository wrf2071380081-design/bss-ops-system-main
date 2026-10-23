package testplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

// 门类常量。一期只用 baseline 与 stress（见 Q9 决策）。
const (
	CategoryBaseline = "baseline"
	CategoryStress   = "stress"
)

// 任务状态。终态见 IsTerminal。
const (
	StatusQueued    = "queued"
	StatusScheduled = "scheduled"
	StatusRunning   = "running"
	StatusPaused    = "paused"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusAborted   = "aborted"
	StatusCanceled  = "canceled"
)

// IsTerminal 判断任务是否已结束（不会再变）。回调/轮询只在该状态收口。
func IsTerminal(status string) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusAborted, StatusCanceled:
		return true
	}
	return false
}

// Target 是被测目标。
type Target struct {
	Model      string `json:"model"`
	BaseURL    string `json:"base_url"`
	Protocol   string `json:"protocol,omitempty"`
	Path       string `json:"path,omitempty"`
	APIVersion string `json:"api_version,omitempty"`
	APIKey     string `json:"api_key,omitempty"`
	TokenGroup string `json:"token_group,omitempty"`
}

// CreateJobRequest 是 POST /test-jobs 的请求体。
//
// 幂等纪律：同一次业务重试必须逐字节重发同一份 body（平台按 SHA-256 比对），
// 因此调用方应复用同一个请求值，不要每次重建 Params。
type CreateJobRequest struct {
	Category        string            `json:"category"`
	Target          Target            `json:"target"`
	Params          map[string]any    `json:"params,omitempty"`
	Owner           string            `json:"owner"`
	CallbackURL     string            `json:"callback_url,omitempty"`
	ScheduleAt      string            `json:"schedule_at,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	BusinessContact string            `json:"business_contact,omitempty"`
	ReviewOf        string            `json:"review_of,omitempty"`
}

// Marshal 生成请求体字节。调用方缓存它可保证重试逐字节一致。
func (r CreateJobRequest) Marshal() ([]byte, error) { return json.Marshal(r) }

// JobAccepted 是创建任务的响应。
type JobAccepted struct {
	JobID            string `json:"job_id"`
	Status           string `json:"status"`
	Position         int    `json:"position"`
	EstimatedSeconds int    `json:"estimated_seconds"`
}

// Job 是任务表示（轮询、列表、回调里的 job 字段同构），只取本系统用到的字段。
type Job struct {
	JobID        string          `json:"job_id"`
	Status       string          `json:"status"`
	Category     string          `json:"category"`
	Verdict      string          `json:"verdict"`
	Owner        string          `json:"owner"`
	Summary      json.RawMessage `json:"summary"`
	ReportDir    string          `json:"report_dir"`
	CurrentNode  string          `json:"current_node"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    time.Time       `json:"created_at"`
	FinishedAt   *time.Time      `json:"finished_at"`
	ErrorMessage string          `json:"error,omitempty"`
}

// jobIDPattern 限制任务号形态，避免把用户输入直接拼进 URL 路径。
var jobIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// CreateJob 发起任务。idempotencyKey 为空时不带幂等头（每次都会新建）。
func (c *Client) CreateJob(ctx context.Context, req CreateJobRequest, idempotencyKey string) (*JobAccepted, error) {
	body, err := req.Marshal()
	if err != nil {
		return nil, err
	}
	var out JobAccepted
	if err := c.do(ctx, http.MethodPost, "/test-jobs", body, idempotencyKey, &out); err != nil {
		return nil, err
	}
	if out.JobID == "" {
		return nil, fmt.Errorf("test platform returned an empty job_id")
	}
	return &out, nil
}

// GetJob 查询任务进度。
func (c *Client) GetJob(ctx context.Context, jobID string) (*Job, error) {
	if !jobIDPattern.MatchString(jobID) {
		return nil, fmt.Errorf("invalid job id")
	}
	var out Job
	if err := c.do(ctx, http.MethodGet, "/test-jobs/"+jobID, nil, "", &out); err != nil {
		return nil, err
	}
	if out.JobID == "" {
		out.JobID = jobID
	}
	return &out, nil
}

// Report 下载报告产物（非信封响应）。format 取 json/md/csv/xlsx，空值按 json。
func (c *Client) Report(ctx context.Context, jobID, format string) ([]byte, int, error) {
	if !jobIDPattern.MatchString(jobID) {
		return nil, 0, fmt.Errorf("invalid job id")
	}
	if format == "" {
		format = "json"
	}
	query := url.Values{"format": []string{format}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.endpoint("/test-jobs/"+jobID+"/report")+"?"+query.Encode(), bytes.NewReader(nil))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var env envelope
		_ = json.Unmarshal(raw, &env)
		return nil, resp.StatusCode, &APIError{HTTPStatus: resp.StatusCode, Code: env.Code, Message: env.Message, Fields: env.Errors}
	}
	return raw, resp.StatusCode, nil
}
