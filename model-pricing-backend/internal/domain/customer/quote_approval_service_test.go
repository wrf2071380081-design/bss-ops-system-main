package customer

import (
	"context"
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

// ConclusionOf 是原型三分法的唯一实现：全部批准 → 已分派、全部驳回 → 已驳回、其余 → 部分批准。
func TestConclusionOf(t *testing.T) {
	cases := []struct {
		name               string
		approved, rejected int
		want               string
	}{
		{"全部批准", 3, 0, QuoteApprovalApproved},
		{"全部驳回", 0, 3, QuoteApprovalRejected},
		{"部分批准", 2, 1, QuoteApprovalPartial},
		{"一个都没决定", 0, 0, QuoteApprovalPending},
	}
	for _, c := range cases {
		if got := ConclusionOf(c.approved, c.rejected); got != c.want {
			t.Fatalf("%s: ConclusionOf(%d,%d)=%q want %q", c.name, c.approved, c.rejected, got, c.want)
		}
	}
}

// fakeApprovalStore 只实现 Decide 需要的部分，其余方法 panic——测试里出现就是用例写错了。
type fakeApprovalStore struct {
	current       *QuoteApproval
	needsBusiness bool
	decided       *DecideRecord
	withdrawnNote string
	savedDraft    *ApprovalDraft
	// notApprover 为 true 时模拟「有 M9:A 但没有超管标记位」的销售账号。
	notApprover bool
}

func (f *fakeApprovalStore) CheckQuoteOwnerScope(context.Context, int64, OwnerScope) (bool, bool, error) {
	return true, true, nil
}
func (f *fakeApprovalStore) SubmitQuoteApprovalTx(context.Context, int64, int64, []int64, int64, string) (*QuoteApproval, error) {
	panic("unused")
}
func (f *fakeApprovalStore) DecideQuoteApprovalTx(_ context.Context, _ int64, in DecideRecord, _ int64, _ string) (*QuoteApproval, error) {
	f.decided = &in
	return f.current, nil
}
func (f *fakeApprovalStore) WithdrawQuoteApprovalTx(_ context.Context, _ int64, reason string, _ int64, _ string) (*QuoteApproval, error) {
	f.withdrawnNote = reason
	return f.current, nil
}
func (f *fakeApprovalStore) LoadQuoteApproval(context.Context, int64) (*QuoteApproval, error) {
	return f.current, nil
}
func (f *fakeApprovalStore) ListQuoteApprovals(context.Context, QuoteApprovalQuery) ([]QuoteApprovalSummary, error) {
	panic("unused")
}
func (f *fakeApprovalStore) SaveQuoteApprovalDraft(_ context.Context, _ int64, draft *ApprovalDraft, _ int64, _ string) error {
	f.savedDraft = draft
	return nil
}
func (f *fakeApprovalStore) StaffCanApproveCustomerQuote(context.Context, int64) (bool, error) {
	return !f.notApprover, nil
}
func (f *fakeApprovalStore) CustomerNeedsBusiness(context.Context, int64) (bool, error) {
	return f.needsBusiness, nil
}

func newPendingApproval() *QuoteApproval {
	return &QuoteApproval{
		ID: 7, QuoteID: 19, Status: QuoteApprovalPending, SubmittedBy: 1,
		Rows: []QuoteApprovalRow{
			{ID: 11, Result: ApprovalRowPending},
			{ID: 12, Result: ApprovalRowPending},
		},
	}
}

func approveAll() []ApprovalDecision {
	return []ApprovalDecision{
		{RowID: 11, Result: ApprovalRowApproved},
		{RowID: 12, Result: ApprovalRowApproved},
	}
}

// Decide 的四条必填对齐原型弹窗：处理意见、技术办理人、必要商务办理人。
func TestDecideValidation(t *testing.T) {
	ctx := context.Background()
	scope := OwnerScope{}

	t.Run("处理意见必填", func(t *testing.T) {
		store := &fakeApprovalStore{current: newPendingApproval()}
		svc := NewQuoteApprovalService(store)
		_, err := svc.Decide(ctx, 7, DecideInput{Rows: approveAll(), OpsAssigneeID: 9}, scope, 2, "req")
		if !errors.Is(err, ErrQuoteApprovalNoteRequired) {
			t.Fatalf("want ErrQuoteApprovalNoteRequired, got %v", err)
		}
	})

	t.Run("有批准模型必须选技术办理人", func(t *testing.T) {
		store := &fakeApprovalStore{current: newPendingApproval()}
		svc := NewQuoteApprovalService(store)
		_, err := svc.Decide(ctx, 7, DecideInput{Rows: approveAll(), Note: "同意"}, scope, 2, "req")
		if !errors.Is(err, ErrQuoteApprovalOpsAssignee) {
			t.Fatalf("want ErrQuoteApprovalOpsAssignee, got %v", err)
		}
	})

	t.Run("客户合同未归档必须选商务办理人", func(t *testing.T) {
		store := &fakeApprovalStore{current: newPendingApproval(), needsBusiness: true}
		svc := NewQuoteApprovalService(store)
		_, err := svc.Decide(ctx, 7, DecideInput{Rows: approveAll(), Note: "同意", OpsAssigneeID: 9}, scope, 2, "req")
		if !errors.Is(err, ErrQuoteApprovalBizAssignee) {
			t.Fatalf("want ErrQuoteApprovalBizAssignee, got %v", err)
		}
	})

	t.Run("合同已归档时商务可不填", func(t *testing.T) {
		store := &fakeApprovalStore{current: newPendingApproval()}
		svc := NewQuoteApprovalService(store)
		if _, err := svc.Decide(ctx, 7, DecideInput{Rows: approveAll(), Note: "同意", OpsAssigneeID: 9}, scope, 2, "req"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if store.decided == nil || store.decided.Note != "同意" || store.decided.OpsAssigneeID != 9 {
			t.Fatalf("落库参数不对: %+v", store.decided)
		}
	})

	t.Run("全部驳回不必选办理人", func(t *testing.T) {
		store := &fakeApprovalStore{current: newPendingApproval(), needsBusiness: true}
		svc := NewQuoteApprovalService(store)
		rows := []ApprovalDecision{
			{RowID: 11, Result: ApprovalRowRejected, Reason: "价格过低"},
			{RowID: 12, Result: ApprovalRowRejected, Reason: "来源不可用"},
		}
		if _, err := svc.Decide(ctx, 7, DecideInput{Rows: rows, Note: "全部驳回"}, scope, 2, "req"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("提交人不能自审", func(t *testing.T) {
		store := &fakeApprovalStore{current: newPendingApproval()}
		svc := NewQuoteApprovalService(store)
		_, err := svc.Decide(ctx, 7, DecideInput{Rows: approveAll(), Note: "同意", OpsAssigneeID: 9}, scope, 1, "req")
		if !errors.Is(err, ErrQuoteApprovalSelfReview) {
			t.Fatalf("want ErrQuoteApprovalSelfReview, got %v", err)
		}
	})
}

// 撤回原因必填（原型撤回弹窗），且只能撤回自己的单。
func TestWithdrawRequiresReason(t *testing.T) {
	ctx := context.Background()
	store := &fakeApprovalStore{current: newPendingApproval()}
	svc := NewQuoteApprovalService(store)
	if _, err := svc.Withdraw(ctx, 7, "  ", OwnerScope{}, 1, "req"); !errors.Is(err, ErrQuoteApprovalWithdrawNote) {
		t.Fatalf("want ErrQuoteApprovalWithdrawNote, got %v", err)
	}
	if _, err := svc.Withdraw(ctx, 7, "客户改需求", OwnerScope{}, 2, "req"); !errors.Is(err, ErrQuoteApprovalNotOwner) {
		t.Fatalf("want ErrQuoteApprovalNotOwner, got %v", err)
	}
	if _, err := svc.Withdraw(ctx, 7, "客户改需求", OwnerScope{}, 1, "req"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.withdrawnNote != "客户改需求" {
		t.Fatalf("撤回原因没落库: %q", store.withdrawnNote)
	}
}

// 保存进度：允许不完整，只保留本单待决行；已结束的单不能再存。
func TestSaveDraftKeepsPartialInput(t *testing.T) {
	ctx := context.Background()
	store := &fakeApprovalStore{current: newPendingApproval()}
	svc := NewQuoteApprovalService(store)
	_, err := svc.SaveDraft(ctx, 7, DecideInput{
		Rows: []ApprovalDecision{
			{RowID: 11, Result: "APPROVED"},
			{RowID: 99, Result: "APPROVED"}, // 不属于本单，丢弃
			{RowID: 12, Result: "REJECTED"}, // 驳回但原因空着：草稿允许
		},
		Note:          " 先批一个  ",
		OpsAssigneeID: 9,
	}, OwnerScope{}, 2, "req")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if store.savedDraft == nil {
		t.Fatal("草稿没落库")
	}
	if len(store.savedDraft.Rows) != 2 {
		t.Fatalf("应只保留本单两行，got %+v", store.savedDraft.Rows)
	}
	if store.savedDraft.Note != "先批一个" || store.savedDraft.OpsAssigneeID != 9 {
		t.Fatalf("草稿内容不对: %+v", store.savedDraft)
	}

	done := newPendingApproval()
	done.Status = QuoteApprovalApproved
	doneStore := &fakeApprovalStore{current: done}
	if _, err := NewQuoteApprovalService(doneStore).SaveDraft(ctx, 7, DecideInput{}, OwnerScope{}, 2, "req"); !errors.Is(err, ErrQuoteApprovalNotPending) {
		t.Fatalf("已结束的单不能存草稿，got %v", err)
	}
}

// 报价审批只有超管能做：没有 can_approve_customer_quote 标记位的人（销售）一律 403。
//
// 与「不能自审」是两道不同的门：自审是"提交人不能批自己的单"，
// 这里是"压根没有审批权"。销售持有 M9:A，所以必须靠标记位挡。
func TestDecideRequiresApprover(t *testing.T) {
	store := &fakeApprovalStore{current: newPendingApproval(), notApprover: true}
	svc := NewQuoteApprovalService(store)
	_, err := svc.Decide(context.Background(), 7,
		DecideInput{Rows: approveAll(), Note: "同意", OpsAssigneeID: 9}, OwnerScope{}, 2, "req")
	if !errors.Is(err, ErrQuoteApprovalNotApprover) {
		t.Fatalf("want ErrQuoteApprovalNotApprover, got %v", err)
	}
	if store.decided != nil {
		t.Fatal("没有审批权时不得写库")
	}
}
