package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 「发起测试」这类接口 body 是固定的（categories 就那两个值），
// 默认语义键会导致第二次点击起全部命中同一条 DONE 记录被回放旧响应。
// 语义键带上客户端幂等键后：换键 = 新命令，同键 = 仍然重放（幂等保护不丢）。
func TestClientKeyBizKeySeparatesClicks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	build := func(idemKey string) string {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/api/supplier/submissions/9/tests",
			strings.NewReader(`{"categories":["CONNECTIVITY","STRESS"]}`))
		ctx.Request.Header.Set("Idempotency-Key", idemKey)
		return ClientKeyBizKey(ctx)
	}

	first := build("key-1")
	if again := build("key-1"); again != first {
		t.Fatalf("同一个幂等键必须得到同一个语义键（重试要能重放）")
	}
	if second := build("key-2"); second == first {
		t.Fatalf("换了幂等键必须得到不同语义键，否则重复点击会被回放成同一次请求")
	}
	if !strings.HasSuffix(first, "|client:key-1") {
		t.Fatalf("语义键应带上客户端键，实际 %q", first)
	}

	// 默认语义键（回归锚点）：同 body 同路径时两次完全相同 —— 正是要修的那个行为。
	defaults := func(idemKey string) string {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/api/supplier/submissions/9/tests",
			strings.NewReader(`{"categories":["CONNECTIVITY","STRESS"]}`))
		ctx.Request.Header.Set("Idempotency-Key", idemKey)
		return DefaultBizKey(ctx)
	}
	if defaults("key-1") != defaults("key-2") {
		t.Fatalf("DefaultBizKey 只看 body/path/operator，不该受幂等键影响")
	}
}
