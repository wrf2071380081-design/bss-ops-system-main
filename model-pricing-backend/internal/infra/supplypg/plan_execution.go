package supplypg

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"reflect"
	"strconv"
)

type PlanExecutionWork struct {
	plans   *PlanStore
	configs *FormalConfigStore
	report  ResourceReportRefLoader
}

func NewPlanExecutionWork(plans *PlanStore, configs *FormalConfigStore, report ResourceReportRefLoader) *PlanExecutionWork {
	return &PlanExecutionWork{plans, configs, report}
}

var _ supply.PlanExecutionUnitOfWork = (*PlanExecutionWork)(nil)

func (w *PlanExecutionWork) WithinPlanExecution(ctx context.Context, run func(supply.PlanExecutionTransaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || w.plans == nil || w.plans.state == nil || w.plans.state.log == nil || w.plans.hooks.Resolve == nil || w.plans.hooks.Log == nil || w.plans.hooks.Outbox == nil || run == nil || (w.configs != nil && w.configs.schema != w.plans.schema) {
		return supply.ErrDependencyUnavailable
	}
	tx, err := w.plans.hooks.Resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	t := &planExecutionTransaction{work: w, maintenance: &planMaintenanceTransaction{store: w.plans, tx: tx}, tx: tx}
	if err := run(t); err != nil {
		return err
	}
	if !t.completed {
		return supply.ErrReadContract
	}
	return nil
}

type planExecutionTransaction struct {
	work           *PlanExecutionWork
	maintenance    *planMaintenanceTransaction
	tx             *sql.Tx
	record         *supply.ResourceRecord
	before, staged *supply.StatePlan
	resource       *supply.Resource
	completed      bool
}

func (t *planExecutionTransaction) LockPlanExecution(ctx context.Context, id string) (*supply.StatePlan, *supply.ResourceRecord, error) {
	if t.before != nil || !modelIdentity(id) {
		return nil, nil, supply.ErrReadContract
	}
	var resourceID string
	err := t.tx.QueryRowContext(ctx, `SELECT resource_id::text FROM `+t.work.plans.table("plan")+` WHERE id=$1`, id).Scan(&resourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	r, supplier, err := lockResourceUnderSupplier(ctx, t.tx, t.work.plans.state, resourceID)
	if err != nil {
		return nil, nil, err
	}
	p, err := t.maintenance.LockPlanMaintenance(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if p.ResourceID != r.ID || *t.maintenance.resource != *r {
		return nil, nil, supply.ErrReadContract
	}
	record := &supply.ResourceRecord{Resource: r, Supplier: supplier}
	if p.Kind == "ONLINE" {
		if t.work.configs == nil || t.work.report == nil {
			return nil, nil, supply.ErrDependencyUnavailable
		}
		var modelID string
		if err := t.tx.QueryRowContext(ctx, `SELECT id::text FROM `+t.work.plans.table("standard_model")+` WHERE id=$1 FOR SHARE`, r.StandardModelID).Scan(&modelID); err != nil {
			return nil, nil, err
		}
		reader := &ResourceReadStore{schema: t.work.plans.schema, facts: t.work.plans.state.FactsLoader(t.work.report, t.work.configs.ResourceView)}
		rows, err := reader.load(ctx, t.tx, "r.id=$1", []any{r.ID})
		if err != nil {
			return nil, nil, err
		}
		if len(rows) != 1 {
			return nil, nil, supply.ErrReadContract
		}
		canonical, err := canonicalStoredResource(rows[0].record.Resource)
		if err != nil {
			return nil, nil, err
		}
		if *canonical != *r {
			return nil, nil, supply.ErrReadContract
		}
		record = rows[0].record
		record.Resource = canonical
	}
	t.before = cloneStatePlan(p)
	t.record = record
	// Domain callers receive a separate resource value; the locked basis is private.
	copyRecord := *record
	copyResource := *record.Resource
	copyRecord.Resource = &copyResource
	return cloneStatePlan(p), &copyRecord, nil
}
func (t *planExecutionTransaction) SaveExecutedPlan(ctx context.Context, p *supply.StatePlan, expected uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	canonical, err := canonicalStatePlan(p)
	if err != nil {
		return err
	}
	if t.before == nil || t.staged != nil || t.before.Revision != expected || canonical.Revision != expected+1 || !samePlanBasis(canonical, t.before) || (canonical.Status != "BLOCKED" && canonical.Status != "EXECUTED") {
		return supply.ErrReadContract
	}
	t.staged = canonical
	return nil
}
func (t *planExecutionTransaction) SavePlanResource(ctx context.Context, r *supply.Resource, expected uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	canonical, err := canonicalStoredResource(r)
	if err != nil {
		return err
	}
	if t.record == nil || t.resource != nil || t.record.Resource.Revision != expected || canonical.Revision != expected+1 || canonical.ID != t.record.Resource.ID {
		return supply.ErrReadContract
	}
	t.resource = canonical
	return nil
}
func (t *planExecutionTransaction) AppendPlanExecutionEffect(ctx context.Context, e supply.PlanExecutionEffect) error {
	if t.completed || t.before == nil || t.staged == nil || !reflect.DeepEqual(t.before, e.BeforePlan) || !reflect.DeepEqual(t.record.Resource, e.BeforeResource) || !reflect.DeepEqual(t.staged, e.AfterPlan) || !reflect.DeepEqual(t.resource, e.AfterResource) {
		return supply.ErrReadContract
	}
	meta := supply.WriteMeta{ActorID: e.ActorID, Reason: e.Reason, RequestID: e.RequestID}
	if err := validateResourceControlMeta(meta, "EXECUTE"); err != nil {
		return err
	}
	proposal, err := t.before.ExecuteStatePlan(e.ActorID, t.before.Revision, t.record, true, e.CompletionNote, e.OccurredAt)
	blocked := errors.Is(err, supply.ErrRevisionConflict) && proposal != nil && proposal.Plan.Status == "BLOCKED"
	if err != nil && !blocked {
		return err
	}
	if proposal == nil || !reflect.DeepEqual(proposal.Plan, t.staged) || !reflect.DeepEqual(proposal.Resource, t.resource) || proposal.FailureReason != e.FailureReason || proposal.CompletionNote != e.CompletionNote {
		return supply.ErrReadContract
	}
	sourceEventID := ""
	operation := "BLOCK"
	if blocked {
		if e.EventType != "" || e.AffectedStatus != "" || t.resource != nil {
			return supply.ErrReadContract
		}
	} else {
		operation = "EXECUTE"
		eventType := "RESOURCE_OFFLINED"
		if t.before.Kind == "ONLINE" {
			eventType = "RESOURCE_ONLINED"
		}
		if e.EventType != eventType || e.AffectedStatus != "PENDING" {
			return supply.ErrReadContract
		}
		payload, _ := json.Marshal(map[string]any{"contract": "supply.resource_state_changed", "schema_version": 1, "plan_id": t.staged.ID, "plan_revision": strconv.FormatUint(t.staged.Revision, 10), "resource_id": t.resource.ID, "resource_revision": strconv.FormatUint(t.resource.Revision, 10), "region": t.resource.Region, "state": t.resource.State, "previous_state": t.record.Resource.State, "actor_id": e.ActorID, "reason": e.Reason, "completion_note": e.CompletionNote, "request_id": e.RequestID, "affected_status": "PENDING", "occurred_at": e.OccurredAt})
		var err error
		sourceEventID, err = t.work.plans.hooks.Outbox(ctx, t.tx, AdmissionOutboxEntry{EventType: eventType, Payload: bytes.Clone(payload), ActorID: e.ActorID, RequestID: e.RequestID, OccurredAt: e.OccurredAt})
		if err != nil {
			return err
		}
		if !modelIdentity(sourceEventID) {
			return supply.ErrReadContract
		}
		var count int
		if err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM `+t.work.plans.table("event_outbox")+` WHERE id=$1 AND event_type=$2 AND payload=$3::jsonb AND status='PENDING' AND created_by=$4 AND request_id=$5 AND created_at=$6`, sourceEventID, eventType, string(payload), e.ActorID, e.RequestID, e.OccurredAt).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return supply.ErrReadContract
		}
		write := resourceWriteContext{CompletionNote: e.CompletionNote, PlanExecution: &resourcePlanExecutionData{PlanID: t.staged.ID, PlanRevision: t.staged.Revision, SourceEventID: sourceEventID}}
		if _, err := t.work.plans.state.updateWithContext(ctx, t.tx, t.resource, t.record.Resource.Revision, meta, "PLAN_"+t.before.Kind, write); err != nil {
			return err
		}
	}
	var old []byte
	if err := t.tx.QueryRowContext(ctx, `SELECT "after" FROM `+t.work.plans.table("plan")+` WHERE id=$1`, t.staged.ID).Scan(&old); err != nil {
		return err
	}
	raw, err := encodeStatePlan(t.staged, operation)
	if err != nil {
		return err
	}
	var envelope planEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	envelope.Execution = &planExecutionData{CompletionNote: e.CompletionNote, FailureReason: e.FailureReason, SourceEventID: sourceEventID, OccurredAt: e.OccurredAt}
	raw, err = json.Marshal(envelope)
	if err != nil {
		return err
	}
	if _, _, err := decodeStatePlan(raw); err != nil {
		return err
	}
	changed, err := t.tx.ExecContext(ctx, `UPDATE `+t.work.plans.table("plan")+` SET status=$2,"after"=$3::jsonb WHERE id=$1 AND "after"=$4::jsonb`, t.staged.ID, nativePlanStatus(t.staged.Status), string(raw), string(old))
	if err != nil {
		return err
	}
	count, err := changed.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return supply.ErrRevisionConflict
	}
	if err := t.work.plans.hooks.Log(ctx, t.tx, ResourceStateLog{EntityType: "plan", EntityID: t.staged.ID, Action: planRevisionAction, BusinessAction: operation, ActorID: e.ActorID, Reason: e.Reason, RequestID: e.RequestID, Before: bytes.Clone(old), After: bytes.Clone(raw), OccurredAt: e.OccurredAt}); err != nil {
		return err
	}
	persisted, err := t.work.plans.current(ctx, t.tx, t.staged.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(persisted, t.staged) {
		return supply.ErrReadContract
	}
	var logs int
	if err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM `+t.work.plans.table("operation_log")+` WHERE entity_type='plan' AND entity_id=$1 AND action=$2 AND "before"=$3::jsonb AND "after"=$4::jsonb AND operator_id=$5 AND reason=$6 AND request_id=$7 AND created_at=$8`, t.staged.ID, planRevisionAction, string(old), string(raw), e.ActorID, e.Reason, e.RequestID, e.OccurredAt).Scan(&logs); err != nil {
		return err
	}
	if logs != 1 {
		return supply.ErrReadContract
	}
	if !blocked {
		title := "资源下架完成"
		if t.before.Kind == "ONLINE" {
			title = "资源上架完成"
		}
		notice := &supply.Notice{ResourceID: t.resource.ID, ResourceName: t.resource.SupplierModelID, Region: t.resource.Region, Kind: "RESOURCE_CHANGE", Title: title, Content: e.CompletionNote, Reason: e.Reason, SourceEventID: sourceEventID, AffectedStatus: "PENDING"}
		payload, err := EncodeNoticePayload(notice)
		if err != nil {
			return err
		}
		if _, err := t.tx.ExecContext(ctx, `INSERT INTO `+t.work.plans.table("notice")+` (type,target_type,target_id,payload,created_at) VALUES('RESOURCE_CHANGE','RESOURCE',$1,$2::jsonb,$3)`, t.resource.ID, string(payload), e.OccurredAt); err != nil {
			return err
		}
	}
	t.completed = true
	return nil
}
