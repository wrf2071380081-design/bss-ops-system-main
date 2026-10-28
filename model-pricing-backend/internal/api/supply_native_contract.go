package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"strconv"
	"time"
)

// No client-supplied 'ready' or attachment IDs can complete first onboarding.
// F12 must provide flow-scoped, current version evidence in this same transaction.
func (r *SupplyChainRuntime) completeNativeContract(ctx context.Context, meta supply.WriteMeta, nodeID, expected uint64) error {
	tx, err := supplypg.GORMModelTxFromContext(ctx)
	if err != nil {
		return err
	}
	var supplier, submission, flow, actor, status, flowStatus, current string
	var decision uint64
	err = tx.QueryRowContext(ctx, `SELECT s.supplier_id::text,s.id::text,n.flow_id::text,l.id FROM public.onboarding_node n JOIN public.operation_log l ON l.entity_type='submission' AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' AND l."after"->>'flow_id'=n.flow_id::text JOIN public.submission s ON s.id=l.entity_id WHERE n.id=$1 AND n.node_key='CONTRACT' ORDER BY l.id DESC LIMIT 1`, nodeID).Scan(&supplier, &submission, &flow, &decision)
	if errors.Is(err, sql.ErrNoRows) {
		return supply.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT id FROM public.supplier WHERE id=$1 FOR UPDATE`, supplier); err != nil {
		return err
	}
	var subStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM public.submission WHERE id=$1 FOR UPDATE`, submission).Scan(&subStatus); err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT coalesce(n.assignee_id::text,''),n.status,f.status,f.current_node FROM public.onboarding_node n JOIN public.onboarding_flow f ON f.id=n.flow_id WHERE n.id=$1 AND n.enabled FOR UPDATE OF n,f`, nodeID).Scan(&actor, &status, &flowStatus, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return supply.ErrForbidden
	}
	if err != nil {
		return err
	}
	if actor != meta.ActorID {
		return supply.ErrForbidden
	}
	if decision != expected || status != "IN_PROGRESS" || flowStatus != "PROCESSING" || current != "CONTRACT" || subStatus != "PROCESSING" {
		return supply.ErrRevisionConflict
	}
	var predecessorReady bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.onboarding_node WHERE flow_id=$1 AND node_key='CONFIG' AND enabled AND status='COMPLETED') AND NOT EXISTS(SELECT 1 FROM public.onboarding_node WHERE flow_id=$1 AND node_key='CONFIG' AND enabled AND status<>'COMPLETED')`, flow).Scan(&predecessorReady); err != nil {
		return err
	}
	if !predecessorReady {
		return supply.ErrRevisionConflict
	}
	if r.f12Readiness == nil {
		return supply.ErrDependencyUnavailable
	}
	snapshot, err := r.f12Readiness.Get(ctx, supply.F12ReadinessRequest{SubjectType: "SUPPLIER", SubjectID: supplier, FlowID: flow, ModuleKey: "CONTRACT"})
	if err != nil {
		return err
	}
	if snapshot == nil || snapshot.SubjectType != "SUPPLIER" || snapshot.SubjectID != supplier || snapshot.FlowID != flow || snapshot.ModuleKey != "CONTRACT" || snapshot.FlowRevision != decision {
		return supply.ErrRevisionConflict
	}
	if !snapshot.Ready || len(snapshot.Contracts) == 0 || len(snapshot.Credits) == 0 {
		return supply.ErrRevisionConflict
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE public.onboarding_node SET status='COMPLETED',completed_at=$2,note=$3 WHERE id=$1`, nodeID, now, meta.Reason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.onboarding_flow SET status='COMPLETED',updated_at=$2 WHERE id=$1`, flow, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.submission SET status='COMPLETED',revision=revision+1,completed_at=$2,updated_at=$2 WHERE id=$1`, submission, now); err != nil {
		return err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,operator_name_snapshot,"after",reason,request_id,created_at) VALUES('onboarding_node',$1,'COMPLETE_CONTRACT',$2,(SELECT name FROM public.internal_staff WHERE id=$2),$3::jsonb,$4,$5,$6)`, strconv.FormatUint(nodeID, 10), meta.ActorID, string(raw), meta.Reason, meta.RequestID, now)
	return err
}
