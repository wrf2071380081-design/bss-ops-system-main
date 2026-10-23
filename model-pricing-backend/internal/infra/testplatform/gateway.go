package testplatform

import (
	"context"

	tpdomain "model_bss/internal/domain/testplatform"
)

// Gateway 把领域层的端口适配到 open-api 客户端。
// 放在 infra 侧（而不是 domain），保证 domain 不依赖 HTTP 细节。
type Gateway struct{ client *Client }

var _ tpdomain.Gateway = (*Gateway)(nil)

// NewGateway 构造适配器；未配置时返回 tpdomain.ErrNotConfigured。
func NewGateway(cfg Config) (*Gateway, error) {
	client, err := New(cfg)
	if err != nil {
		return nil, tpdomain.ErrNotConfigured
	}
	return &Gateway{client: client}, nil
}

// CreateJob 发起任务。
func (g *Gateway) CreateJob(ctx context.Context, cmd tpdomain.CreateCommand) (tpdomain.Accepted, error) {
	request := CreateJobRequest{
		Category: string(cmd.Category),
		Target: Target{
			Model:    cmd.Target.Model,
			BaseURL:  cmd.Target.BaseURL,
			Protocol: cmd.Target.Protocol,
			APIKey:   cmd.Target.APIKey,
		},
		Params:          paramsOrDefault(cmd.Category, cmd.Params),
		Owner:           cmd.Owner,
		ReviewOf:        cmd.ReviewOf,
		BusinessContact: cmd.ActorID,
	}
	if cmd.BatchLabel != "" {
		request.Labels = map[string]string{"bss_batch": cmd.BatchLabel}
	}
	accepted, err := g.client.CreateJob(ctx, request, cmd.IdempotencyKey)
	if err != nil {
		return tpdomain.Accepted{}, err
	}
	return tpdomain.Accepted{JobID: accepted.JobID, Status: accepted.Status}, nil
}

// GetJob 查询任务状态。
func (g *Gateway) GetJob(ctx context.Context, jobID string) (tpdomain.JobState, error) {
	job, err := g.client.GetJob(ctx, jobID)
	if err != nil {
		return tpdomain.JobState{}, err
	}
	return tpdomain.JobState{
		JobID:      job.JobID,
		Status:     job.Status,
		Verdict:    job.Verdict,
		Summary:    job.Summary,
		FinishedAt: job.FinishedAt,
	}, nil
}

// DownloadReport 下载报告原始字节。
func (g *Gateway) DownloadReport(ctx context.Context, jobID, format string) ([]byte, error) {
	raw, _, err := g.client.Report(ctx, jobID, format)
	return raw, err
}

// paramsOrDefault 在调用方未给参数时提供最小可用参数。
// 这里的默认值属于平台契约的一部分，不放到业务层，便于一处调整。
func paramsOrDefault(category tpdomain.Category, params map[string]any) map[string]any {
	if len(params) > 0 {
		return params
	}
	switch category {
	case tpdomain.CategoryBaseline:
		return map[string]any{"min_layer_score": 0.8, "mode": "gate"}
	case tpdomain.CategoryStress:
		return map[string]any{"concurrency": []int{1}, "duration": "30s"}
	}
	return nil
}
