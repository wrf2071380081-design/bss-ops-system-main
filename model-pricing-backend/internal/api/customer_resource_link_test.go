package api

import (
	"testing"

	"github.com/stretchr/testify/require"

	"model_bss/internal/domain/customer"
	"model_bss/pkg/apperr"
)

// TestCustomerResourceLinkErrorMapping 钉住「哪个领域错误映射成哪个 HTTP 状态」。
//
// 核心口径（G4-A 验证时暴露过的地基问题）：
//   - 乐观锁冲突 / 已归档 → **409**（前端提示「刷新后重试」，用户可操作）
//   - 真的非法迁移 → 400（用户操作不合法）
//
// 两者混了会让并发/重复提交看起来像「按钮点错了」，所以在这一层也钉一次：
// 领域层比顺序（link_service.go），映射层比状态码，两层一起才算防线。
//
// 另外 23503 的外键错误必须落到「到底谁不存在」：
// 资源/模型缺失不能被报成「客户不存在」，否则排查方向直接跑偏。
func TestCustomerResourceLinkErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		code   int
		status int
	}{
		{"版本冲突必须是 409", customer.ErrLinkRevisionConflict, apperr.ErrIdempotency.Code, 409},
		{"已归档也是 409", customer.ErrLinkArchived, apperr.ErrIdempotency.Code, 409},
		{"同模型已有生效来源 409", customer.ErrLinkModelAlreadyActive, apperr.ErrConflict.Code, 409},
		{"同资源重复绑定 409", customer.ErrLinkDuplicateResource, apperr.ErrConflict.Code, 409},
		{"非法迁移 400", customer.ErrLinkTransitionInvalid, apperr.ErrInvalidParams.Code, 400},
		{"不支持的动作 400", customer.ErrLinkActionUnsupported, apperr.ErrInvalidParams.Code, 400},
		{"资源未准入 400", customer.ErrLinkResourceNotAdmitted, apperr.ErrInvalidParams.Code, 400},
		{"模型与资源不匹配 400", customer.ErrLinkModelMismatch, apperr.ErrInvalidParams.Code, 400},
		{"缺 expected_revision 400", customer.ErrLinkRevisionRequired, apperr.ErrInvalidParams.Code, 400},
		{"客户不存在 404", customer.ErrLinkCustomerNotFound, apperr.ErrNotFound.Code, 404},
		{"标准模型不存在 404", customer.ErrLinkStandardModelNotFound, apperr.ErrNotFound.Code, 404},
		{"供应资源不存在 404", customer.ErrLinkResourceNotFound, apperr.ErrNotFound.Code, 404},
		{"外键兜底 404", customer.ErrLinkReferenceMissing, apperr.ErrNotFound.Code, 404},
	}
	for _, c := range cases {
		got := resourceLinkErrToAppErr(c.err)
		require.Equalf(t, c.code, got.Code, "%s：业务码不符", c.name)
		require.Equalf(t, c.status, got.Status, "%s：HTTP 状态不符", c.name)
	}

	// 明确反向断言：revision 冲突永远不能落到 400。
	require.NotEqual(t, 400, resourceLinkErrToAppErr(customer.ErrLinkRevisionConflict).Status,
		"revision 冲突必须 409；落到 400 会让前端把并发当成「操作不合法」")

	// 409 文案要能指导用户下一步（这里是「先归档旧绑定」）。
	require.Contains(t, resourceLinkErrToAppErr(customer.ErrLinkModelAlreadyActive).Message, "归档",
		"同模型冲突文案必须告诉用户该归档旧绑定")
}
