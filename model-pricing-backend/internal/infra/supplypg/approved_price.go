package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"model_bss/internal/domain/supply"
)

// Verify must lock and validate the approved submission, assignment and exact
// row/revision/amount. Complete persists B's row completion in this SAME tx.
type ApprovedPriceHooks struct {
	Verify   func(context.Context, *sql.Tx, supply.CoefficientSource, string, *supply.Resource) (supply.CoefficientSourceProof, error)
	Complete func(context.Context, *sql.Tx, supply.ResourceCoefficientOperation) error
	Outbox   AdmissionOutboxWriter
}
type ApprovedPriceWork struct {
	state   *ResourceStateStore
	resolve ModelTxResolver
	hooks   ApprovedPriceHooks
}
type approvedPriceCompletionKey struct{}

// Completion evidence is private request context, never a source of authority.
func WithApprovedPriceCompletion(ctx context.Context, note string) context.Context {
	return context.WithValue(ctx, approvedPriceCompletionKey{}, strings.TrimSpace(note))
}
func approvedPriceCompletion(ctx context.Context) string {
	note, _ := ctx.Value(approvedPriceCompletionKey{}).(string)
	return note
}
func uintText(n uint64) string { return strconv.FormatUint(n, 10) }
func NewApprovedPriceWork(state *ResourceStateStore, resolve ModelTxResolver, hooks ApprovedPriceHooks) *ApprovedPriceWork {
	return &ApprovedPriceWork{state, resolve, hooks}
}
func (w *ApprovedPriceWork) WithinCoefficient(ctx context.Context, run func(supply.ResourceCoefficientTransaction) error) error {
	if w == nil || w.state == nil || w.state.log == nil || w.resolve == nil || w.hooks.Verify == nil || w.hooks.Complete == nil || w.hooks.Outbox == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	if note := approvedPriceCompletion(ctx); note == "" || len(note) > 2000 || !utf8.ValidString(note) || strings.ContainsRune(note, 0) {
		return &supply.FieldError{Path: "completion_note", Reason: "completion evidence required"}
	}
	tx, err := w.resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	t := &approvedPriceTransaction{work: w, tx: tx}
	if err := run(t); err != nil {
		return err
	}
	if t.operation == nil || (!t.event && t.operation.Action != "CHANGE_COEFFICIENT_NOOP") {
		return supply.ErrReadContract
	}
	return nil
}

type approvedPriceTransaction struct {
	work            *ApprovedPriceWork
	tx              *sql.Tx
	before, staged  *supply.Resource
	source          supply.CoefficientSource
	actor           string
	verified, event bool
	operation       *supply.ResourceCoefficientOperation
}

func (t *approvedPriceTransaction) ResourceForUpdate(ctx context.Context, id string) (*supply.Resource, error) {
	if t.before != nil {
		return nil, supply.ErrReadContract
	}
	r, supplier, err := lockResourceUnderSupplier(ctx, t.tx, t.work.state, id)
	if err != nil {
		return nil, err
	}
	if supplier.Status != "ACTIVE" {
		return nil, supply.ErrInactive
	}
	t.before = r
	copy := *r
	return &copy, nil
}
func (t *approvedPriceTransaction) VerifyCoefficientSource(ctx context.Context, source supply.CoefficientSource, actor string, r *supply.Resource) (supply.CoefficientSourceProof, error) {
	if source.Kind != "SUBMISSION" || t.before == nil || r == nil || *r != *t.before || t.verified {
		return supply.CoefficientSourceProof{}, supply.ErrReadContract
	}
	copy := *r
	proof, err := t.work.hooks.Verify(ctx, t.tx, source, actor, &copy)
	if err != nil {
		return proof, err
	}
	t.verified = proof.Authorized && proof.ResourceID == r.ID && proof.ExpectedResourceRevision == r.Revision
	t.source = source
	t.actor = actor
	return proof, nil
}
func (t *approvedPriceTransaction) UpdateCoefficient(ctx context.Context, r *supply.Resource, expected uint64) error {
	if !t.verified || t.staged != nil || t.before == nil || r == nil || expected != t.before.Revision {
		return supply.ErrReadContract
	}
	allowed := *t.before
	allowed.Coefficient = r.Coefficient
	allowed.Revision++
	allowed.UpdatedAt = r.UpdatedAt
	if *r != allowed {
		return supply.ErrReadContract
	}
	copy := *r
	t.staged = &copy
	return nil
}
func (t *approvedPriceTransaction) AppendCoefficientOperation(ctx context.Context, e supply.ResourceCoefficientOperation) error {
	if !t.verified || t.operation != nil || t.before == nil || e.Source != t.source || e.ActorID != t.actor || e.Before != *t.before || e.ResourceID != t.before.ID {
		return supply.ErrReadContract
	}
	changed := e.Action == "CHANGE_COEFFICIENT"
	if changed {
		if t.staged == nil || e.After != *t.staged {
			return supply.ErrReadContract
		}
	} else if e.Action != "CHANGE_COEFFICIENT_NOOP" || t.staged != nil || e.After != e.Before {
		return supply.ErrReadContract
	}
	meta := supply.WriteMeta{ActorID: e.ActorID, Reason: e.Reason, RequestID: e.RequestID}
	if changed {
		if _, err := t.work.state.updateWithContext(ctx, t.tx, &e.After, e.Before.Revision, meta, "APPLY_APPROVED_PRICE", resourceWriteContext{CompletionNote: approvedPriceCompletion(ctx)}); err != nil {
			return err
		}
	}
	// Store an explicit safe source receipt, without credentials or report payloads.
	before, _ := json.Marshal(map[string]string{"coefficient": e.Before.Coefficient})
	after, _ := json.Marshal(map[string]string{"coefficient": e.After.Coefficient, "source_row_id": e.Source.ID, "source_revision": uintText(e.Source.Revision), "resource_revision": uintText(e.After.Revision), "completion_note": approvedPriceCompletion(ctx)})
	if err := t.work.state.log(ctx, t.tx, ResourceStateLog{EntityType: "resource", EntityID: e.ResourceID, Action: e.Action, BusinessAction: e.Action, ActorID: e.ActorID, Reason: e.Reason, RequestID: e.RequestID, Before: before, After: after, OccurredAt: e.OccurredAt}); err != nil {
		return err
	}
	if err := t.work.hooks.Complete(ctx, t.tx, e); err != nil {
		return err
	}
	copy := e
	t.operation = &copy
	return nil
}
func (t *approvedPriceTransaction) AppendCoefficientEvent(ctx context.Context, e supply.ResourceCoefficientEvent) error {
	op := t.operation
	if op == nil || t.event || op.Action != "CHANGE_COEFFICIENT" || e.ResourceID != op.ResourceID || e.Source != op.Source || e.Revision != op.After.Revision || e.ActorID != op.ActorID || e.RequestID != op.RequestID || e.Reason != op.Reason || e.Region != op.After.Region || e.BeforeCoefficient != op.Before.Coefficient || e.AfterCoefficient != op.After.Coefficient || !e.OccurredAt.Equal(op.OccurredAt) {
		return supply.ErrReadContract
	}
	payload, err := json.Marshal(map[string]any{"contract": "supply.resource_coefficient_changed", "schema_version": 1, "resource_id": e.ResourceID, "supplier_id": op.After.SupplierID, "standard_model_id": op.After.StandardModelID, "region": e.Region, "resource_revision": uintText(e.Revision), "before_coefficient": e.BeforeCoefficient, "after_coefficient": e.AfterCoefficient, "before_supplier_model_id": op.Before.SupplierModelID, "after_supplier_model_id": op.After.SupplierModelID, "before_target_version": uintText(op.Before.TargetVersion), "target_version": uintText(op.After.TargetVersion), "source": map[string]string{"kind": e.Source.Kind, "id": e.Source.ID, "revision": uintText(e.Source.Revision)}, "actor_id": e.ActorID, "reason": e.Reason, "request_id": e.RequestID, "occurred_at": e.OccurredAt})
	if err != nil {
		return err
	}
	id, err := t.work.hooks.Outbox(ctx, t.tx, AdmissionOutboxEntry{EventType: "RESOURCE_COEFFICIENT_CHANGED", ActorID: e.ActorID, RequestID: e.RequestID, Payload: payload, OccurredAt: e.OccurredAt})
	if err != nil {
		return err
	}
	if !modelIdentity(id) {
		return supply.ErrReadContract
	}
	var stored json.RawMessage
	if err := t.tx.QueryRowContext(ctx, `SELECT payload FROM `+t.work.state.table("event_outbox")+` WHERE id=$1 AND event_type='RESOURCE_COEFFICIENT_CHANGED' AND request_id=$2 AND created_by=$3 AND status='PENDING'`, id, e.RequestID, e.ActorID).Scan(&stored); err != nil {
		return err
	}
	var a, b any
	if json.Unmarshal(stored, &a) != nil || json.Unmarshal(payload, &b) != nil || !reflect.DeepEqual(a, b) {
		return supply.ErrReadContract
	}
	n := &supply.Notice{ResourceID: e.ResourceID, ResourceName: op.After.SupplierModelID, Region: e.Region, Kind: "COST_CHANGE", Title: "供应商批准改价已完成", Content: strings.Join([]string{e.BeforeCoefficient, e.AfterCoefficient}, " → "), Reason: e.Reason, SourceEventID: id, AffectedStatus: "PENDING"}
	notice, err := EncodeNoticePayload(n)
	if err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO `+t.work.state.table("notice")+` (type,target_type,target_id,payload,created_at) VALUES('COST_CHANGE','RESOURCE',$1,$2::jsonb,$3)`, e.ResourceID, string(notice), e.OccurredAt); err != nil {
		return err
	}
	t.event = true
	return nil
}
