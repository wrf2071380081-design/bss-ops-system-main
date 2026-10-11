package supplier

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---- 状态白名单 ----

// TestIsWithdrawableStatus 白名单口径：只有 SUBMITTED / APPROVING 可撤回。
// 新增状态默认不可撤回（白名单而非黑名单）——避免漏判直接放行。
func TestIsWithdrawableStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{QuoteStatusSubmitted, true},
		{QuoteStatusApproving, true},
		{"APPROVED_PENDING", false}, // 已出审批结论：撤回等于推翻他人决策
		{"EFFECTIVE", false},
		{"EXPIRED", false},
		{"REJECTED", false},
		{QuoteStatusVoided, false}, // 已撤回不可重复撤回
		{"", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, IsWithdrawableStatus(c.status), "status=%q", c.status)
	}
}

// ---- service ----

func withdrawService(st *fakeQuoteStore) *QuoteService {
	return NewQuoteService(st)
}

// TestWithdrawQuote_PassesOwnershipAndIdentity 撤回必须把 supplier_id（归属）
// 与 operator/request_id（留痕）完整传给仓储。
//
// 变异验证：service 里漏传 SupplierID → 本用例必须失败（等于放弃行级过滤）。
func TestWithdrawQuote_PassesOwnershipAndIdentity(t *testing.T) {
	st := baseStore()
	svc := withdrawService(st)

	res, err := svc.WithdrawQuote(context.Background(), 7, 42, WithdrawInput{Reason: " 报错了重提 "}, 9, "req-1")
	require.NoError(t, err)
	require.Equal(t, QuoteStatusVoided, res.Status)
	require.Equal(t, int64(42), res.ID)

	require.NotNil(t, st.withdrawParams)
	require.Equal(t, int64(42), st.withdrawParams.QuoteID)
	require.Equal(t, int64(7), st.withdrawParams.SupplierID, "supplier_id 是行级过滤键，必须传")
	require.Equal(t, int64(9), st.withdrawParams.OperatorID)
	require.Equal(t, "SUPPLIER", st.withdrawParams.OperatorRole)
	require.Equal(t, "req-1", st.withdrawParams.RequestID)
	require.Equal(t, "报错了重提", st.withdrawParams.Reason, "原因应 trim 后再入库")
}

// TestWithdrawQuote_ReasonOptional 撤回原因选填：不填也能撤（供应商可以只点撤回）。
// 与驳回（reason 10~500 必填）刻意不同——撤回是供应商对自己数据的处置，不该被文本长度卡住。
func TestWithdrawQuote_ReasonOptional(t *testing.T) {
	st := baseStore()
	_, err := withdrawService(st).WithdrawQuote(context.Background(), 7, 42, WithdrawInput{}, 9, "req-1")
	require.NoError(t, err)
	require.Equal(t, "", st.withdrawParams.Reason)
}

// TestWithdrawQuote_ReasonTooLong 超长原因 → ErrQuoteWithdrawReasonTooLong。
// 500 rune 上限；用多字节字符确认按 rune 计数而不是 byte。
func TestWithdrawQuote_ReasonTooLong(t *testing.T) {
	st := baseStore()
	svc := withdrawService(st)

	_, err := svc.WithdrawQuote(context.Background(), 7, 42, WithdrawInput{Reason: strings.Repeat("字", 501)}, 9, "req-1")
	require.True(t, errors.Is(err, ErrQuoteWithdrawReasonTooLong), "got %v", err)

	_, err = svc.WithdrawQuote(context.Background(), 7, 42, WithdrawInput{Reason: strings.Repeat("字", 500)}, 9, "req-1")
	require.NoError(t, err, "500 rune 应放行")
}

// TestWithdrawQuote_StoreErrorPropagates 仓储的状态/归属冲突错误必须原样透传，
// 不能被吞成通用系统错误（否则前端只能显示"请求失败"）。
func TestWithdrawQuote_StoreErrorPropagates(t *testing.T) {
	st := baseStore()
	st.withdrawErr = ErrQuoteNotWithdrawable
	_, err := withdrawService(st).WithdrawQuote(context.Background(), 7, 42, WithdrawInput{}, 9, "req-1")
	require.True(t, errors.Is(err, ErrQuoteNotWithdrawable), "got %v", err)
}
