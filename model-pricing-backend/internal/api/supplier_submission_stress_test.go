package api

import (
	"testing"

	"model_bss/internal/infra/supplypg"
)

// 「发起测试」的压测前置门禁（2026-10-28 用户报障）：
//
// 以前 categories 带 STRESS、但一行都没勾时，后端把每行都记成 NOT_SELECTED
// 然后照常回 200，前端按成功提示；供应商以为测过了，提交时才被门禁挡住。
// 现在 isStressRequested + anyStressSelected 一起决定是否直接 400。
func TestAnyStressSelected(t *testing.T) {
	cases := map[string]struct {
		rows []supplypg.SubmissionRowRecord
		want bool
	}{
		"空单":       {rows: nil, want: false},
		"一行没勾":     {rows: []supplypg.SubmissionRowRecord{{}, {}}, want: false},
		"勾了第二行":    {rows: []supplypg.SubmissionRowRecord{{}, {ParticipateTest: true}}, want: true},
		"全部勾选":     {rows: []supplypg.SubmissionRowRecord{{ParticipateTest: true}}, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := anyStressSelected(tc.rows); got != tc.want {
				t.Fatalf("anyStressSelected=%v，期望 %v", got, tc.want)
			}
		})
	}
}

// 类别解析要能识别出「这次请求里到底要不要压测」，门禁按它判断。
func TestStressRequestedByCategories(t *testing.T) {
	cases := map[string]struct {
		raw  []string
		want bool
	}{
		"只有连通":   {raw: []string{"CONNECTIVITY"}, want: false},
		"连通+压力":  {raw: []string{"CONNECTIVITY", "STRESS"}, want: true},
		"小写带空格":  {raw: []string{" stress "}, want: true},
		"未知类别":   {raw: []string{"BASELINE"}, want: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := false
			for _, item := range tc.raw {
				if _, platformCategory, ok := resolveTestCategory(item); ok && platformCategory == "stress" {
					got = true
				}
			}
			if got != tc.want {
				t.Fatalf("wantStress=%v，期望 %v", got, tc.want)
			}
		})
	}
}
