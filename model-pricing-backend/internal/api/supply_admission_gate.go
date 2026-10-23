package api

import (
	"context"

	"model_bss/internal/domain/supply"
)

// AdmissionEvidenceChecker 判断资源是否具备可用的测试证据（infra 实现）。
type AdmissionEvidenceChecker interface {
	Check(ctx context.Context, resourceID string) error
}

// WithAdmissionGate 给登记接入套一层证据闸门（T-13）。
//
// 为什么放在装饰器而不是改 A 的领域服务：准入的核心逻辑（快照、版本、审计）不该被测试业务牵着走；
// 闸门是「受理前的前置条件」，放外层既能拦住，也方便单独关掉（传入 nil 即原样返回）。
func WithAdmissionGate(inner SupplyResourceAdmitter, checker AdmissionEvidenceChecker) SupplyResourceAdmitter {
	if inner == nil || checker == nil {
		return inner
	}
	return admissionGate{inner: inner, checker: checker}
}

type admissionGate struct {
	inner   SupplyResourceAdmitter
	checker AdmissionEvidenceChecker
}

func (g admissionGate) Admit(ctx context.Context, meta supply.WriteMeta, cmd supply.ResourceAdmissionCommand) (*supply.Resource, error) {
	if err := g.checker.Check(ctx, cmd.ResourceID); err != nil {
		return nil, err
	}
	return g.inner.Admit(ctx, meta, cmd)
}
