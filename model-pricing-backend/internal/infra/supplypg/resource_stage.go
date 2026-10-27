package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"strconv"
)

type stageBinder interface {
	BindFormalConfig(context.Context, supply.WriteMeta, supply.ResourceConfigBindingCommand) (*supply.ResourceConfigBindingChange, error)
}
type stageAdmitter interface {
	Admit(context.Context, supply.WriteMeta, supply.ResourceAdmissionCommand) (*supply.Resource, error)
}
type ResourceStageWriter struct {
	state    *ResourceStateStore
	resolve  ModelTxResolver
	binder   stageBinder
	admitter stageAdmitter
}

func NewResourceStageWriter(state *ResourceStateStore, resolve ModelTxResolver, binder stageBinder, admitter stageAdmitter) *ResourceStageWriter {
	return &ResourceStageWriter{state, resolve, binder, admitter}
}

// The HTTP batch owns one outer idempotency transaction and a savepoint per
// resource. Every stage still consumes the real B node/source/target proof.
func (w *ResourceStageWriter) Apply(ctx context.Context, meta supply.WriteMeta, supplier, action, note string, item supply.ResourceStageItem) (*supply.ResourceStageReceipt, error) {
	if w == nil || w.state == nil || w.resolve == nil || w.binder == nil || w.admitter == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	expected, err := strconv.ParseUint(item.ExpectedRevision, 10, 64)
	if err != nil || expected == 0 || strconv.FormatUint(expected, 10) != item.ExpectedRevision {
		return nil, &supply.FieldError{Path: "expected_revision", Reason: "canonical positive revision required"}
	}
	if !modelIdentity(item.ResourceID) || !modelIdentity(supplier) {
		return nil, supply.ErrReadContract
	}
	tx, err := w.resolve(ctx)
	if err != nil {
		return nil, err
	}
	var actual string
	err = tx.QueryRowContext(ctx, `SELECT supplier_id::text FROM `+w.state.table("resource")+` WHERE id=$1`, item.ResourceID).Scan(&actual)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if actual != supplier {
		return nil, supply.ErrForbidden
	}
	if _, err = tx.ExecContext(ctx, `SELECT id FROM `+w.state.table("supplier")+` WHERE id=$1 FOR UPDATE`, supplier); err != nil {
		return nil, err
	}
	r, err := w.state.ForUpdate(ctx, tx, item.ResourceID)
	if err != nil {
		return nil, err
	}
	if r.Revision != expected || r.EverAdmitted || (r.State != "PENDING_TEST" && r.State != "PENDING_ACCESS") {
		return nil, supply.ErrRevisionConflict
	}
	native := &NativeHandling{State: w.state}
	proof, err := native.Proof(ctx, tx, r)
	if err != nil {
		return nil, err
	}
	if proof.AssigneeID != meta.ActorID {
		return nil, supply.ErrForbidden
	}
	if action == "CONFIGURE" {
		configRevision, err := strconv.ParseUint(item.ExpectedConfigRevision, 10, 64)
		if err != nil || configRevision == 0 || strconv.FormatUint(configRevision, 10) != item.ExpectedConfigRevision || !modelIdentity(item.FormalConfigID) {
			return nil, &supply.FieldError{Path: "formal_config", Reason: "select a current formal configuration"}
		}
		change, err := w.binder.BindFormalConfig(ctx, meta, supply.ResourceConfigBindingCommand{ResourceID: r.ID, ExpectedRevision: r.Revision, ConfigID: item.FormalConfigID, ExpectedConfigRevision: configRevision})
		if err != nil {
			return nil, err
		}
		r = change.Resource
		if _, err = tx.ExecContext(ctx, `UPDATE `+w.state.table("submission_row")+` SET row_status='CONFIGURED' WHERE id=$1`, r.OriginRowID); err != nil {
			return nil, err
		}
	} else if action == "CONFIRM" {
		var configured bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+w.state.table("operation_log")+` WHERE entity_type='resource' AND entity_id=$1 AND action='CONFIG_COMPLETE' AND "after"->>'contract'='supply.config_completion/v1' AND "after"->>'resource_revision'=$2 AND "after"->>'target_version'=$3 AND "after"->>'formal_config_id'=$4 AND "after"->>'formal_config_revision'=$5 AND "after"->>'source_row_id'=$6 AND "after"->>'flow_revision'=$7)`, r.ID, versionText(r.Revision), versionText(r.TargetVersion), r.FormalConfigID, versionText(r.FormalConfigRevision), r.OriginRowID, versionText(proof.FlowRevision)).Scan(&configured)
		if err != nil {
			return nil, err
		}
		if !configured {
			return nil, supply.ErrRevisionConflict
		}
		after, err := w.admitter.Admit(ctx, meta, supply.ResourceAdmissionCommand{ResourceID: r.ID, ExpectedRevision: r.Revision, ExternalConfirmed: true, CompletionNote: note})
		if err != nil {
			return nil, err
		}
		return &supply.ResourceStageReceipt{ResourceID: r.ID, Stage: "ADMITTED", Revision: versionText(after.Revision)}, nil
	} else if action == "CANCEL" {
		if err = native.CancelAdmission(ctx, tx, r.OriginRowID, meta.ActorID, note); err != nil {
			return nil, err
		}
	} else {
		return nil, supply.ErrReadContract
	}
	marker := map[string]string{"contract": "supply.config_completion/v1", "resource_revision": versionText(r.Revision), "target_version": versionText(r.TargetVersion), "formal_config_id": r.FormalConfigID, "formal_config_revision": versionText(r.FormalConfigRevision), "source_row_id": r.OriginRowID, "flow_revision": versionText(proof.FlowRevision), "node_id": proof.NodeID, "completion_note": note}
	event, stage := "CONFIG_COMPLETE", "CONFIGURED"
	if action == "CANCEL" {
		event, stage = "CANCEL_ACCESS", "CANCELED"
		marker["contract"] = "supply.access_cancellation/v1"
	}
	raw, err := json.Marshal(marker)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+w.state.table("operation_log")+` (entity_type,entity_id,action,operator_id,operator_name_snapshot,"after",reason,request_id) VALUES('resource',$1,$2,$3,(SELECT name FROM `+w.state.table("internal_staff")+` WHERE id=$3),$4::jsonb,$5,$6)`, r.ID, event, meta.ActorID, string(raw), meta.Reason, meta.RequestID)
	if err != nil {
		return nil, err
	}
	return &supply.ResourceStageReceipt{ResourceID: r.ID, Stage: stage, Revision: versionText(r.Revision)}, nil
}
