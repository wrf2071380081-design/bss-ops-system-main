package middleware

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestIdempotency_MissingKeyRejectsWithoutRunningHandler 缺 Idempotency-Key 时必须 400 且**不执行 handler**。
//
// 这条是「零写入」的直接证明：写接口（例如 POST /customers）被拒绝时，
// 业务 handler 一次都没有跑，因此不可能产生 legal_subject / customer_profile / 审计行。
func TestIdempotency_MissingKeyRejectsWithoutRunningHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeIdemStore()

	calls := 0
	rec := performIdemRequest(t, store, DefaultBizKey, func(c *gin.Context) {
		calls++
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}, "", `{"a":1}`)

	require.Equal(t, http.StatusBadRequest, rec.Code, "缺幂等键应返回 400")
	require.Zero(t, calls, "缺幂等键时业务 handler 不得执行（保证零写入）")
	require.Empty(t, store.byRequest, "缺幂等键不应在幂等表里留下记录")
}
