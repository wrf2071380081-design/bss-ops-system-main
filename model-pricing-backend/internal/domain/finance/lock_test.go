package finance

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// ============================================================
// fakeLockStore
// ============================================================

type fakeLockStore struct {
	row    *FxLockRow
	found  bool
	getErr error

	upsertRes   *LockTxResult
	upsertErr   error
	upsertCalls int
	lastUpsert  LockTxInput
	lastGetYM   string
}

func (f *fakeLockStore) GetFxLock(_ context.Context, ym string) (*FxLockRow, bool, error) {
	f.lastGetYM = ym
	return f.row, f.found, f.getErr
}

func (f *fakeLockStore) UpsertFxLockTx(_ context.Context, in LockTxInput) (*LockTxResult, error) {
	f.upsertCalls++
	f.lastUpsert = in
	if f.upsertRes != nil {
		return f.upsertRes, f.upsertErr
	}
	return &LockTxResult{ID: 1, Ym: in.Ym, FxTier: in.FxTier, Adjusted: f.found, LockedAt: fixedNow(), LockedByName: "财务冒烟"}, f.upsertErr
}

func newLockSvc(s *fakeLockStore) *LockService { return NewLockService(s, fixedNow) }

// ============================================================
// GET 当月状态
// ============================================================

// TestGetCurrent_Pending: 当月无记录 → PENDING_LOCK，回 8 档可选值。
func TestGetCurrent_Pending(t *testing.T) {
	s := &fakeLockStore{found: false}
	view, err := newLockSvc(s).GetCurrent(context.Background())
	require.NoError(t, err)
	require.Equal(t, "2026-10", view.Month)
	require.Equal(t, LockStatusPendingLock, view.Status)
	require.Equal(t, "", view.FxTier)
	require.Nil(t, view.LockedAt)
	require.Len(t, view.Tiers, 8)
	require.Equal(t, "6.5", view.Tiers[0])
	require.Equal(t, "7.0", view.Tiers[7])
}

// TestGetCurrent_Locked: 当月有记录 → LOCKED，档位规范化、回填锁定人与理由。
func TestGetCurrent_Locked(t *testing.T) {
	s := &fakeLockStore{
		found: true,
		row: &FxLockRow{
			ID: 12, Ym: "2026-10", FxTier: "7.000",
			LockedByName: "财务冒烟", LockedAt: fixedNow(), AdjustReason: "美元大涨",
		},
	}
	view, err := newLockSvc(s).GetCurrent(context.Background())
	require.NoError(t, err)
	require.Equal(t, LockStatusLocked, view.Status)
	require.Equal(t, "7.0", view.FxTier)
	require.Equal(t, "财务冒烟", view.LockedBy)
	require.Equal(t, "美元大涨", view.AdjustReason)
	require.NotNil(t, view.LockedAt)
}

// ============================================================
// POST 锁定 / 调整
// ============================================================

// TestLock_InvalidTier: 非 8 档 → ErrFxTierInvalid，且不触达存储。
func TestLock_InvalidTier(t *testing.T) {
	for _, tier := range []string{"7.1", "6.6", "abc", ""} {
		s := &fakeLockStore{}
		_, err := newLockSvc(s).Lock(context.Background(), LockInput{FxTier: tier}, 10, "FINANCE", "rid")
		require.ErrorIsf(t, err, ErrFxTierInvalid, "tier=%q", tier)
		require.Zero(t, s.upsertCalls, "非法档位不得写库（tier=%q）", tier)
	}
}

// TestLock_FirstAllowsEmptyReason: 当月无记录 → 首次锁定，理由可空。
func TestLock_FirstAllowsEmptyReason(t *testing.T) {
	s := &fakeLockStore{found: false}
	res, err := newLockSvc(s).Lock(context.Background(), LockInput{FxTier: "7.0"}, 10, "FINANCE", "rid")
	require.NoError(t, err)
	require.False(t, res.Adjusted)
	require.Equal(t, "7.0", res.FxTier)
	require.Equal(t, LockStatusLocked, res.Status)
	require.Equal(t, 1, s.upsertCalls)
	require.Equal(t, "", s.lastUpsert.AdjustReason, "首次锁定理由应被清空")
	require.Equal(t, "2026-10", s.lastUpsert.Ym)
	require.Equal(t, int64(10), s.lastUpsert.OperatorID)
	require.Equal(t, "FINANCE", s.lastUpsert.OperatorRole)
}

// TestLock_ExistingRequiresReason: 同月已有记录 → 调整必须带理由，否则 400 且不写库。
func TestLock_ExistingRequiresReason(t *testing.T) {
	s := &fakeLockStore{found: true, row: &FxLockRow{ID: 12, Ym: "2026-10", FxTier: "6.900"}}
	_, err := newLockSvc(s).Lock(context.Background(), LockInput{FxTier: "7.0", AdjustReason: "   "}, 10, "FINANCE", "rid")
	require.ErrorIs(t, err, ErrFxAdjustReasonRequired)
	require.Zero(t, s.upsertCalls, "缺理由不得写库")
}

// TestLock_ExistingWithReason: 调整带理由 → 写库，adjusted=true，理由透传。
func TestLock_ExistingWithReason(t *testing.T) {
	s := &fakeLockStore{found: true, row: &FxLockRow{ID: 12, Ym: "2026-10", FxTier: "6.900"}}
	res, err := newLockSvc(s).Lock(context.Background(), LockInput{FxTier: "7.0", AdjustReason: " 美元大涨 "}, 10, "FINANCE", "rid")
	require.NoError(t, err)
	require.True(t, res.Adjusted)
	require.Equal(t, 1, s.upsertCalls)
	require.Equal(t, "美元大涨", s.lastUpsert.AdjustReason, "理由应 trim 后透传")
}

// TestLock_CanonicalTier: 输入 "6.800" → 落库/回显规范形式 "6.8"。
func TestLock_CanonicalTier(t *testing.T) {
	s := &fakeLockStore{found: false}
	res, err := newLockSvc(s).Lock(context.Background(), LockInput{FxTier: "6.800"}, 10, "FINANCE", "rid")
	require.NoError(t, err)
	require.Equal(t, "6.8", res.FxTier)
	require.Equal(t, "6.8", s.lastUpsert.FxTier)
}

// TestLock_GetError: 读现有记录失败 → 向上传播，不写库。
func TestLock_GetError(t *testing.T) {
	s := &fakeLockStore{getErr: errors.New("boom")}
	_, err := newLockSvc(s).Lock(context.Background(), LockInput{FxTier: "7.0"}, 10, "FINANCE", "rid")
	require.Error(t, err)
	require.Zero(t, s.upsertCalls)
}

// TestGetCurrent_UsesCurrentMonth: 查询月份 = 当前月（Asia/Shanghai）。
func TestGetCurrent_UsesCurrentMonth(t *testing.T) {
	s := &fakeLockStore{}
	_, err := newLockSvc(s).GetCurrent(context.Background())
	require.NoError(t, err)
	require.Equal(t, "2026-10", s.lastGetYM)
}
