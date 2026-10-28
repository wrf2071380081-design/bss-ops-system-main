package api

import (
	"testing"

	"model_bss/internal/infra/supplypg"
)

// 「同一受测配置一次有效测试」的判定。
//
// 关键回归：压测没有 verdict（只有 RPS/TPS），旧实现按 verdict 判「有没有结果」，
// 于是每一次已完成的压测都被当成工具异常 → 每点一次发起测试就重跑一轮。
// 现在判据是「报告是否存在」（2026-10-28 审计：一条单子 4 分钟跑出 5 个压测任务）。
func TestShouldSkipRetest(t *testing.T) {
	jobID := func(value int64) *int64 { return &value }
	const fp = "fp-current"

	baseline := func() *supplypg.TestRow {
		return &supplypg.TestRow{TestJobID: jobID(11), TestJobStatus: "SUCCEEDED", TestJobFingerprint: fp, TestJobHasReport: true}
	}
	stress := func() *supplypg.TestRow {
		return &supplypg.TestRow{StressJobID: jobID(12), StressJobStatus: "SUCCEEDED", StressJobFingerprint: fp, StressJobHasReport: true}
	}

	cases := map[string]struct {
		row      *supplypg.TestRow
		category string
		wantSkip bool
	}{
		"没有任务：要发起": {row: &supplypg.TestRow{}, category: "stress", wantSkip: false},
		"排队中：不重复建单": {
			row:      &supplypg.TestRow{StressJobID: jobID(12), StressJobStatus: "QUEUED", StressJobFingerprint: fp},
			category: "stress", wantSkip: true,
		},
		"运行中：不重复建单": {
			row:      &supplypg.TestRow{StressJobID: jobID(12), StressJobStatus: "RUNNING", StressJobFingerprint: fp},
			category: "stress", wantSkip: true,
		},
		"压测完成且有报告：算有效结果，不重跑": {row: stress(), category: "stress", wantSkip: true},
		"连通完成且有报告：算有效结果":     {row: baseline(), category: "baseline", wantSkip: true},
		"终态但没报告：工具异常，重测": {
			row:      &supplypg.TestRow{StressJobID: jobID(12), StressJobStatus: "SUCCEEDED", StressJobFingerprint: fp, StressJobHasReport: false},
			category: "stress", wantSkip: false,
		},
		"受测目标变了：允许重测": {
			row:      &supplypg.TestRow{StressJobID: jobID(12), StressJobStatus: "SUCCEEDED", StressJobFingerprint: "fp-old", StressJobHasReport: true},
			category: "stress", wantSkip: false,
		},
		"任务侧没有指纹（老数据）：不误判成变更": {
			row:      &supplypg.TestRow{StressJobID: jobID(12), StressJobStatus: "SUCCEEDED", StressJobFingerprint: "", StressJobHasReport: true},
			category: "stress", wantSkip: true,
		},
		"连通任务的报告状态不能串到压测上": {
			row:      &supplypg.TestRow{TestJobID: jobID(11), TestJobStatus: "SUCCEEDED", TestJobHasReport: true},
			category: "stress", wantSkip: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, skip := shouldSkipRetest(tc.row, tc.category, fp)
			if skip != tc.wantSkip {
				t.Fatalf("skip=%v，期望 %v", skip, tc.wantSkip)
			}
		})
	}
}
