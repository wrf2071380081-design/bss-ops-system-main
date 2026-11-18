package api

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
)

type nativeWorkflowActions struct{ native *supplypg.NativeHandling }

func (n *nativeWorkflowActions) GetResourceWorkflow(ctx context.Context, actor, id string) (*ResourceWorkflowActions, error) {
	tx, err := n.native.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Keep the same supplier -> resource -> B lock order as write operations.
	var supplier string
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id::text FROM public.resource WHERE id=$1`, id).Scan(&supplier); errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM public.supplier WHERE id=$1 FOR UPDATE`, supplier).Scan(&supplier); err != nil {
		return nil, err
	}
	resource, err := n.native.State.ForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	result := &ResourceWorkflowActions{ApprovedPrices: []ApprovedPriceOption{}}
	proof, err := n.native.Proof(ctx, tx, resource)
	// 2026-11-18 口径：本版不做细分权限，只区分「领导（超管）/ 普通员工」。
	// 只要能进到这一步的内部员工、且资源还没正式接入，就允许绑定配置与登记接入。
	// 不再要求"恰好是指定的那个技术办理人"——那会把被派活的人挡在门外。
	_ = proof
	// 不依赖 Proof 的授权结果：它按"CONTRACT 节点办理人"判定，
	// 技术办理人会被误判成无权（2026-11-18 实测：沈安 can_bind=false）。
	// 本版只要求"内部员工 + 资源未正式接入"。
	if !resource.EverAdmitted {
		result.CanBind = true
		result.CanRefreshTests = true
		// 待测试也可直接接入：测试是可选的质量动作，不作为技术接入的前置条件。
		result.CanAdmit = resource.State == "PENDING_TEST" || resource.State == "PENDING_ACCESS"
	}
	if err != nil && !errors.Is(err, supply.ErrForbidden) && !errors.Is(err, supply.ErrRevisionConflict) {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT sr.id::text,l."after"->>'source_revision' FROM public.submission_row sr JOIN public.submission s ON s.id=sr.submission_id JOIN public.operation_log l ON l.entity_type='submission' AND l.entity_id=s.id AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' WHERE sr.resource_id=$1 AND s.kind='PRICE_CHANGE' AND s.status='PROCESSING' AND sr.row_status NOT IN ('ADMITTED','COMPLETED') ORDER BY sr.id`, id)
	if err != nil {
		return nil, err
	}
	var sources []supply.CoefficientSource
	for rows.Next() {
		var sourceID, revision string
		if err := rows.Scan(&sourceID, &revision); err != nil {
			rows.Close()
			return nil, err
		}
		v, err := planPositiveID(revision)
		if err != nil {
			rows.Close()
			return nil, supply.ErrReadContract
		}
		sources = append(sources, supply.CoefficientSource{Kind: "SUBMISSION", ID: sourceID, Revision: v})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		proof, err := n.native.VerifyPrice(ctx, tx, source, actor, resource)
		if errors.Is(err, supply.ErrForbidden) || errors.Is(err, supply.ErrRevisionConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result.ApprovedPrices = append(result.ApprovedPrices, ApprovedPriceOption{SourceRowID: source.ID, SourceRevision: uintString(source.Revision), Coefficient: proof.Coefficient})
	}
	return result, nil
}
func NewNativeSupplyWorkflowProviders(db *sql.DB, state *supplypg.ResourceStateStore) SupplyResourceWorkflowProviders {
	native := &supplypg.NativeHandling{DB: db, State: state}
	return SupplyResourceWorkflowProviders{Admission: supplypg.ResourceAdmissionHooks{Proof: native.Proof, CompleteRow: native.CompleteAdmission}, ApprovedPrice: supplypg.ApprovedPriceHooks{Verify: native.VerifyPrice, Complete: native.CompletePrice}, Actions: &nativeWorkflowActions{native: native}, Evidence: native}
}
