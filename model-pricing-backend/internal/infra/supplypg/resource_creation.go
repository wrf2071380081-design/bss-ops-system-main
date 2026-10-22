package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
)

// B owns source authentication, immutable versions and the shared lock order,
// row CAS and conversion of draft evidence after a resource ID exists.
type ResourceCreationHooks struct {
	LockSource    func(context.Context, *sql.Tx, string, uint64) (*supply.ResourceCreationContext, error)
	AttachRow     func(context.Context, *sql.Tx, supply.ResourceCreationEffect) error
	PrepareReport func(context.Context, *sql.Tx, *supply.Resource, *supply.ResourceReportSummary) error
}
type ResourceCreationWork struct {
	store   *ResourceStateStore
	resolve ModelTxResolver
	hooks   ResourceCreationHooks
}

var _ supply.ResourceCreationUnitOfWork = (*ResourceCreationWork)(nil)

func NewResourceCreationWork(store *ResourceStateStore, resolve ModelTxResolver, hooks ResourceCreationHooks) *ResourceCreationWork {
	return &ResourceCreationWork{store, resolve, hooks}
}
func (w *ResourceCreationWork) WithinResourceCreation(ctx context.Context, run func(supply.ResourceCreationTransaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || w.store == nil || w.store.log == nil || w.resolve == nil || w.hooks.LockSource == nil || w.hooks.AttachRow == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	tx, err := w.resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	t := &resourceCreationTransaction{work: w, tx: tx}
	if err := run(t); err != nil {
		return err
	}
	if !t.appended {
		return supply.ErrReadContract
	}
	return nil
}

type resourceCreationTransaction struct {
	work     *ResourceCreationWork
	tx       *sql.Tx
	source   *supply.ResourceCreationContext
	staged   *supply.Resource
	appended bool
}

var _ supply.ResourceCreationTransaction = (*resourceCreationTransaction)(nil)

func (t *resourceCreationTransaction) LockSubmissionCreation(ctx context.Context, row string, version uint64) (*supply.ResourceCreationContext, error) {
	if t.source != nil || !modelIdentity(row) || version == 0 {
		return nil, supply.ErrReadContract
	}
	source, err := t.work.hooks.LockSource(ctx, t.tx, row, version)
	if err != nil {
		return nil, err
	}
	if source == nil || source.Proof == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	p := *source.Proof
	if p.RowID != row || p.RowVersion != version {
		return nil, supply.ErrRevisionConflict
	}
	if !modelIdentity(p.SupplierID) || !modelIdentity(p.StandardModelID) || (p.Region != "CN" && p.Region != "OVERSEAS") {
		return nil, supply.ErrReadContract
	}
	// Supplier/model facts and existing A state are read from SQL, not copied
	// from a B response that might have retained old ownership or activity.
	var supplier supply.ResourceSupplier
	err = t.tx.QueryRowContext(ctx, `SELECT id::text,name,region,status FROM `+t.work.store.table("supplier")+` WHERE id=$1`, p.SupplierID).Scan(&supplier.ID, &supplier.Name, &supplier.Region, &supplier.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var model string
	if err := t.tx.QueryRowContext(ctx, `SELECT id::text FROM `+t.work.store.table("standard_model")+` WHERE id=$1`, p.StandardModelID).Scan(&model); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrNotFound
		}
		return nil, err
	}
	var existingID string
	err = t.tx.QueryRowContext(ctx, `SELECT id::text FROM `+t.work.store.table("resource")+` WHERE supplier_id=$1 AND standard_model_id=$2 AND region=$3 FOR UPDATE`, p.SupplierID, p.StandardModelID, p.Region).Scan(&existingID)
	var existing *supply.Resource
	if err == nil {
		existing, err = t.work.store.Current(ctx, t.tx, existingID)
	} else if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if p.Report != nil {
		report := *p.Report
		p.Report = &report
	}
	t.source = &supply.ResourceCreationContext{Proof: &p, Supplier: supplier, ModelID: model, Existing: existing}
	copy := *t.source
	return &copy, nil
}
func (t *resourceCreationTransaction) NextResourceID(ctx context.Context) (string, error) {
	if t.source == nil || t.source.Existing != nil {
		return "", supply.ErrReadContract
	}
	return t.work.store.NextID(ctx, t.tx)
}
func (t *resourceCreationTransaction) InsertSubmissionResource(ctx context.Context, r *supply.Resource) error {
	if t.source == nil || t.source.Existing != nil || t.staged != nil || r == nil {
		return supply.ErrReadContract
	}
	stored, err := canonicalStoredResource(r)
	if err != nil {
		return err
	}
	t.staged = stored
	return nil
}
func (t *resourceCreationTransaction) prepareReport(ctx context.Context, tx *sql.Tx, r *supply.Resource) (*resourceTestEvidenceData, error) {
	proof := t.source.Proof.Report
	if proof == nil || t.work.hooks.PrepareReport == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	report := *proof
	resourceCopy := *r
	if err := t.work.hooks.PrepareReport(ctx, tx, &resourceCopy, &report); err != nil {
		return nil, err
	}
	var job string
	if err := tx.QueryRowContext(ctx, `SELECT test_job_id::text FROM `+t.work.store.table("test_report")+` WHERE id=$1`, r.LatestReportID).Scan(&job); err != nil {
		return nil, err
	}
	ref := ReportEvidenceRef{ReportID: r.LatestReportID, JobID: job, ResourceID: r.ID, SupplierID: r.SupplierID, StandardModelID: r.StandardModelID, Region: r.Region, OriginRowID: proof.OriginRowID, OriginRowVersion: proof.OriginRowVersion, TargetVersion: proof.TargetVersion, Sequence: proof.Sequence, Valid: proof.Valid}
	actual, err := ReadSelectedReport(ctx, tx, t.work.store.schema, ref)
	if err != nil {
		return nil, err
	}
	if actual.ConnectivityPassed != proof.ConnectivityPassed || actual.PressurePassed != proof.PressurePassed || actual.Valid != proof.Valid {
		return nil, supply.ErrReadContract
	}
	return &resourceTestEvidenceData{ReportID: actual.ID, Sequence: actual.Sequence, OriginRowVersion: actual.OriginRowVersion, TargetVersion: actual.TargetVersion, Outcome: "SELECTED", Valid: actual.Valid}, nil
}
func (t *resourceCreationTransaction) AppendResourceCreationEffect(ctx context.Context, e supply.ResourceCreationEffect) error {
	if t.source == nil || t.appended {
		return supply.ErrReadContract
	}
	p := t.source.Proof
	if e.RowID != p.RowID || e.RowVersion != p.RowVersion || e.FlowID != p.FlowID || e.FlowRevision != p.FlowRevision || e.ActorID != p.AuthorizedActorID {
		return supply.ErrReadContract
	}
	r, err := canonicalStoredResource(&e.Resource)
	if err != nil {
		return err
	}
	meta := supply.WriteMeta{ActorID: e.ActorID, Reason: e.Reason, RequestID: e.RequestID}
	if t.staged != nil {
		if e.Action != "CREATE" || *r != *t.staged {
			return supply.ErrReadContract
		}
		if _, err := t.work.store.insertPrepared(ctx, t.tx, r, meta, "CREATE", t.prepareReport); err != nil {
			return err
		}
	} else {
		before, err := canonicalStoredResource(t.source.Existing)
		if err != nil {
			return err
		}
		if e.Action != "CREATE_NOOP" || *r != *before {
			return supply.ErrReadContract
		}
		if err := t.appendNoop(ctx, e, meta); err != nil {
			return err
		}
	}
	if err := t.work.hooks.AttachRow(ctx, t.tx, e); err != nil {
		return err
	}
	t.appended = true
	return nil
}
func (t *resourceCreationTransaction) appendNoop(ctx context.Context, e supply.ResourceCreationEffect, meta supply.WriteMeta) error {
	if err := validateResourceControlMeta(meta, e.Action); err != nil {
		return err
	}
	raw, err := json.Marshal(resourceControlFrom(&e.Resource, e.Action))
	if err != nil {
		return supply.ErrReadContract
	}
	var previous int64
	if err := t.tx.QueryRowContext(ctx, `SELECT COALESCE(max(id),0) FROM `+t.work.store.table("operation_log")+` WHERE entity_type='resource' AND entity_id=$1`, e.Resource.ID).Scan(&previous); err != nil {
		return err
	}
	entry := ResourceStateLog{EntityType: "resource", EntityID: e.Resource.ID, Action: e.Action, BusinessAction: e.Action, ActorID: e.ActorID, Reason: e.Reason, RequestID: e.RequestID, Before: raw, After: raw, OccurredAt: e.OccurredAt}
	if err := t.work.store.log(ctx, t.tx, entry); err != nil {
		return err
	}
	var count int
	if err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM `+t.work.store.table("operation_log")+` WHERE id>$1 AND entity_type='resource' AND entity_id=$2 AND action=$3 AND operator_id=$4 AND reason=$5 AND request_id=$6 AND "before"=$7::jsonb AND "after"=$7::jsonb`, previous, e.Resource.ID, e.Action, e.ActorID, e.Reason, e.RequestID, string(raw)).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return supply.ErrReadContract
	}
	return nil
}
