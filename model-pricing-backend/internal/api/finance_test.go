// Package api_test 的 finance_test.go：汇率锁定 handler 的服务层角色收敛钉死。
//
// 与 cost_lock_test.go 同款防线：handler 里的
//
//	if !perm.AnyRoleCan(op.Roles, perm.CanLockFx) { 403 }
//
// 若被改成 perm.CanLockFx(op.Roles[0])，则「FINANCE 不在 Roles[0]」的合法操作员会被误拒。
// 本文件用真 handler + 真 middleware.Operator 上下文打真实 HTTP 调用：
//   - 只含 FINANCE → 200；
//   - 含 FINANCE 但不为首元素 → 必须放行（AnyRoleCan 扫全角色的实证）；
//   - 不含 FINANCE（PLATFORM_ADMIN/MODEL_OPS/PRICING_OP）→ 403；
//   - 空角色 → 403；
//   - FINANCE + 非法档位 → 400（档位白名单在 service 层）。
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"model_bss/internal/api"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/finance"
)

// fakeLockStore 是 finance.LockStore 的最小实现（首次锁定场景：当月无记录）。
type fakeLockStore struct{}

func (fakeLockStore) GetFxLock(context.Context, string) (*finance.FxLockRow, bool, error) {
	return nil, false, nil
}

func (fakeLockStore) UpsertFxLockTx(_ context.Context, in finance.LockTxInput) (*finance.LockTxResult, error) {
	return &finance.LockTxResult{ID: 1, Ym: in.Ym, FxTier: in.FxTier, LockedByName: "财务冒烟"}, nil
}

func performPostFxLock(t *testing.T, roles []string, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("auth.operator", &middleware.Operator{
			PortalType:   "INTERNAL",
			OperatorType: "STAFF",
			OperatorID:   10,
			Roles:        roles,
		})
		c.Next()
	})
	h := api.NewFinanceHandler(nil, finance.NewLockService(fakeLockStore{}, nil))
	r.POST("/x/fx-lock", h.PostFxLock)

	req := httptest.NewRequest(http.MethodPost, "/x/fx-lock", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func bodyCode(t *testing.T, rec *httptest.ResponseRecorder) float64 {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
	code, _ := m["code"].(float64)
	return code
}

// TestPostFxLock_FinanceAllowed: 只含 FINANCE → 200。
func TestPostFxLock_FinanceAllowed(t *testing.T) {
	rec := performPostFxLock(t, []string{"FINANCE"}, `{"fx_tier":"7.0"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, float64(0), bodyCode(t, rec))
}

// TestPostFxLock_FinanceNotFirstAllowed: FINANCE 不在 Roles[0] 也必须放行（AnyRoleCan 分水岭）。
func TestPostFxLock_FinanceNotFirstAllowed(t *testing.T) {
	rec := performPostFxLock(t, []string{"PLATFORM_ADMIN", "FINANCE"}, `{"fx_tier":"7.0"}`)
	require.Equal(t, http.StatusOK, rec.Code,
		"AnyRoleCan 应放行；若 handler 用 Roles[0]，PLATFORM_ADMIN 会导致 403")
}

// TestPostFxLock_NonFinanceDenied: PLATFORM_ADMIN/MODEL_OPS/PRICING_OP 均不含 FINANCE → 403。
func TestPostFxLock_NonFinanceDenied(t *testing.T) {
	for _, roles := range [][]string{
		{"PLATFORM_ADMIN", "MODEL_OPS", "PRICING_OP"},
		{"PROCUREMENT"},
		{"SALES"},
		{},
	} {
		rec := performPostFxLock(t, roles, `{"fx_tier":"7.0"}`)
		require.Equalf(t, http.StatusForbidden, rec.Code, "roles=%v 应 403", roles)
		require.Equalf(t, float64(10003), bodyCode(t, rec), "roles=%v", roles)
	}
}

// TestPostFxLock_FinanceInvalidTierBadRequest: FINANCE 但档位非法 → 400（白名单在 service 层）。
func TestPostFxLock_FinanceInvalidTierBadRequest(t *testing.T) {
	rec := performPostFxLock(t, []string{"FINANCE"}, `{"fx_tier":"7.1"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, float64(10001), bodyCode(t, rec))
}
