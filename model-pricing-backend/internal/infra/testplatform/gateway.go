package testplatform

import (
	"context"
	"time"

	tpdomain "model_bss/internal/domain/testplatform"
)

// probeTimeout 是发起前可达性探测的时间上限。
//
// 故意比出站请求超时（默认 30s）短得多：探测的意义是「快速告诉用户连不上内网」，
// 没必要让页面陪着干等半分钟。
const probeTimeout = 5 * time.Second

// Gateway 把领域层的端口适配到 open-api 客户端。
// 放在 infra 侧（而不是 domain），保证 domain 不依赖 HTTP 细节。
type Gateway struct{ client *Client }

var _ tpdomain.Gateway = (*Gateway)(nil)
var _ tpdomain.Prober = (*Gateway)(nil)

// NewGateway 构造适配器；未配置时返回 tpdomain.ErrNotConfigured。
func NewGateway(cfg Config) (*Gateway, error) {
	client, err := New(cfg)
	if err != nil {
		return nil, tpdomain.ErrNotConfigured
	}
	return &Gateway{client: client}, nil
}

// platformCategory 把 BSS 内部门类翻译成平台门类。
//
// 2026-11-06 实测：平台已把 baseline 门类下线，建任务直接 400
// 「category: 门类已下线，请改用综合测试（comprehensive）」。
// comprehensive 的 report/summary 结构与 baseline 完全一致
// （summary.checks 里仍有 layer=L1、name=minimal_chat、duration_ms），
// 所以 BSS 侧语义与解读保持不变，只在出站这一层换名字。
// 平台若恢复 baseline，删掉这层映射即可。
func platformCategory(category tpdomain.Category) string {
	if category == tpdomain.CategoryBaseline {
		return "comprehensive"
	}
	return string(category)
}

// Healthy 用一次最小代价的列表请求确认平台可达（含鉴权与网关）。
// 只判断「能不能拿到正常信封」，不解析业务字段。
func (g *Gateway) Healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return g.client.Ping(ctx)
}

// CreateJob 发起任务。
func (g *Gateway) CreateJob(ctx context.Context, cmd tpdomain.CreateCommand) (tpdomain.Accepted, error) {
	request := CreateJobRequest{
		Category: platformCategory(cmd.Category),
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
		// 平台侧已由 baseline 切到 comprehensive（见 platformCategory）。
		// comprehensive 拒绝 mode=gate（只有 baseline 认），2026-11-06 实测：
		//   params.mode: "gate 仅支持 baseline"
		// 只传 min_layer_score 即可，平台按 reference-evaluation-v1 跑 L1→L6。
		return map[string]any{"min_layer_score": 0.8}
	case tpdomain.CategoryStress:
		// engine 是平台 stress 门类的必填参数，且当前只有这一个合法值；
		// 不传会被平台判 400「参数校验失败」，表现为「测试平台调用失败，请稍后重试」。
		return map[string]any{"engine": "reference-performance-v1", "concurrency": []int{1}, "duration": "30s"}
	}
	return nil
}
