package customer

import (
	"errors"
	"testing"
)

// NormalizeDecisions 是「逐模型审批」的唯一校验点：漏一个模型、重复提交、
// 驳回不写原因，都必须在这里被挡住——否则会落一张半截的审批单。
func TestNormalizeDecisions(t *testing.T) {
	rows := []QuoteApprovalRow{
		{ID: 11, Result: ApprovalRowPending},
		{ID: 12, Result: ApprovalRowPending},
		{ID: 13, Result: ApprovalRowApproved}, // 已决行不参与本次
	}

	t.Run("全部覆盖且批准", func(t *testing.T) {
		out, err := NormalizeDecisions([]ApprovalDecision{
			{RowID: 11, Result: "APPROVED"},
			{RowID: 12, Result: "approved"},
		}, rows)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 2 || out[1].Result != ApprovalRowApproved {
			t.Fatalf("结果未归一化为大写: %+v", out)
		}
	})

	t.Run("漏一个模型要报错", func(t *testing.T) {
		_, err := NormalizeDecisions([]ApprovalDecision{{RowID: 11, Result: "APPROVED"}}, rows)
		if !errors.Is(err, ErrQuoteApprovalRows) {
			t.Fatalf("want ErrQuoteApprovalRows, got %v", err)
		}
	})

	t.Run("驳回必须写原因", func(t *testing.T) {
		_, err := NormalizeDecisions([]ApprovalDecision{
			{RowID: 11, Result: "REJECTED"},
			{RowID: 12, Result: "APPROVED"},
		}, rows)
		if !errors.Is(err, ErrQuoteApprovalReason) {
			t.Fatalf("want ErrQuoteApprovalReason, got %v", err)
		}
	})

	t.Run("重复行与已决行都不接受", func(t *testing.T) {
		if _, err := NormalizeDecisions([]ApprovalDecision{
			{RowID: 11, Result: "APPROVED"},
			{RowID: 11, Result: "APPROVED"},
		}, rows); !errors.Is(err, ErrQuoteApprovalRows) {
			t.Fatalf("重复行应报错, got %v", err)
		}
		if _, err := NormalizeDecisions([]ApprovalDecision{
			{RowID: 11, Result: "APPROVED"},
			{RowID: 12, Result: "APPROVED"},
			{RowID: 13, Result: "APPROVED"},
		}, rows); !errors.Is(err, ErrQuoteApprovalRows) {
			t.Fatalf("已决行不应再被决定, got %v", err)
		}
	})
}
