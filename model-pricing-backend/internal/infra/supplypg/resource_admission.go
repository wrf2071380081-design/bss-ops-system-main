package supplypg

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type AdmissionOutboxEntry struct {
	EventType, ActorID, RequestID string
	Payload                       json.RawMessage
	OccurredAt                    time.Time
}
type AdmissionOutboxWriter func(context.Context, *sql.Tx, AdmissionOutboxEntry) (string, error)
type ResourceAdmissionHooks struct {
	Proof       AdmissionProofLoader
	Report      ResourceReportRefLoader
	CompleteRow func(context.Context, *sql.Tx, supply.ResourceAdmissionEffect) error
	Outbox      AdmissionOutboxWriter
}
type ResourceAdmissionWork struct {
	state   *ResourceStateStore
	configs *FormalConfigStore
	resolve ModelTxResolver
	hooks   ResourceAdmissionHooks
}

var _ supply.ResourceAdmissionUnitOfWork = (*ResourceAdmissionWork)(nil)

func NewResourceAdmissionWork(state *ResourceStateStore, configs *FormalConfigStore, resolve ModelTxResolver, hooks ResourceAdmissionHooks) *ResourceAdmissionWork {
	return &ResourceAdmissionWork{state, configs, resolve, hooks}
}
func (w *ResourceAdmissionWork) WithinAdmission(ctx context.Context, run func(supply.ResourceAdmissionTransaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || w.state == nil || w.state.log == nil || w.configs == nil || w.state.schema != w.configs.schema || w.resolve == nil || w.hooks.Proof == nil || w.hooks.Report == nil || w.hooks.CompleteRow == nil || w.hooks.Outbox == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	tx, err := w.resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	t := &resourceAdmissionTransaction{work: w, tx: tx}
	if err := run(t); err != nil {
		return err
	}
	if !t.completed {
		return supply.ErrReadContract
	}
	return nil
}

type resourceAdmissionTransaction struct {
	work           *ResourceAdmissionWork
	tx             *sql.Tx
	before, staged *supply.Resource
	proof          *supply.AdmissionProof
	completed      bool
}

var _ supply.ResourceAdmissionTransaction = (*resourceAdmissionTransaction)(nil)

func (t *resourceAdmissionTransaction) LockAdmission(ctx context.Context, id string) (*supply.ResourceRecord, *supply.AdmissionProof, error) {
	if t.before != nil {
		return nil, nil, supply.ErrReadContract
	}
	r, supplier, err := lockResourceUnderSupplier(ctx, t.tx, t.work.state, id)
	if err != nil {
		return nil, nil, fmt.Errorf("admission lock: %w", err)
	}
	if supplier.Status != "ACTIVE" {
		return nil, nil, supply.ErrInactive
	}
	if r.FormalConfigID != "" {
		var locked string
		err := t.tx.QueryRowContext(ctx, `SELECT id::text FROM `+t.work.state.table("supplier_formal_config")+` WHERE id=$1 AND supplier_id=$2 FOR UPDATE`, r.FormalConfigID, r.SupplierID).Scan(&locked)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}
	}
	copy := *r
	proof, err := t.work.hooks.Proof(ctx, t.tx, &copy)
	if err != nil {
		return nil, nil, err
	}
	if proof == nil {
		return nil, nil, supply.ErrDependencyUnavailable
	}
	reader := &ResourceReadStore{schema: t.work.state.schema, facts: t.work.state.FactsLoader(t.work.hooks.Report, t.work.configs.ResourceView)}
	rows, err := reader.load(ctx, t.tx, "r.id=$1", []any{id})
	if err != nil {
		return nil, nil, fmt.Errorf("admission record load: %w", err)
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
	rows[0].record.Resource = canonical
	p := *proof
	t.before = r
	t.proof = &p
	domainProof := p
	return rows[0].record, &domainProof, nil
}
func (t *resourceAdmissionTransaction) SaveAdmittedResource(ctx context.Context, r *supply.Resource, expected uint64) error {
	if t.before == nil || t.staged != nil || t.completed || r == nil || t.before.ID != r.ID || t.before.Revision != expected {
		return supply.ErrReadContract
	}
	c, err := canonicalStoredResource(r)
	if err != nil {
		return err
	}
	if c.Revision != expected+1 || c.State != "ADMITTED" || !c.EverAdmitted {
		return supply.ErrReadContract
	}
	t.staged = c
	return nil
}
func (t *resourceAdmissionTransaction) AppendAdmissionEffect(ctx context.Context, e supply.ResourceAdmissionEffect) error {
	if t.before == nil || t.staged == nil || t.proof == nil || t.completed || !reflect.DeepEqual(&e.Proof, t.proof) || e.ActorID != t.proof.AssigneeID || e.EventType != "RESOURCE_ADMITTED" {
		return supply.ErrReadContract
	}
	if strings.TrimSpace(e.CompletionNote) == "" || !utf8.ValidString(e.CompletionNote) || strings.ContainsRune(e.CompletionNote, 0) {
		return &supply.FieldError{Path: "completion_note", Reason: "valid completion evidence required"}
	}
	before, err := canonicalStoredResource(&e.Before)
	if err != nil {
		return err
	}
	after, err := canonicalStoredResource(&e.After)
	if err != nil {
		return err
	}
	if *before != *t.before || *after != *t.staged {
		return supply.ErrReadContract
	}
	meta := supply.WriteMeta{ActorID: e.ActorID, Reason: e.Reason, RequestID: e.RequestID}
	if _, err := t.work.state.updateWithContext(ctx, t.tx, after, before.Revision, meta, "ADMIT", resourceWriteContext{Proof: t.proof, CompletionNote: e.CompletionNote}); err != nil {
		return err
	}
	if err := t.work.hooks.CompleteRow(ctx, t.tx, e); err != nil {
		return err
	}
	payload, err := json.Marshal(admissionEventPayload(e))
	if err != nil {
		return supply.ErrReadContract
	}
	entry := AdmissionOutboxEntry{EventType: e.EventType, ActorID: e.ActorID, RequestID: e.RequestID, Payload: bytes.Clone(payload), OccurredAt: e.OccurredAt}
	id, err := t.work.hooks.Outbox(ctx, t.tx, entry)
	if err != nil {
		return err
	}
	if !modelIdentity(id) {
		return supply.ErrReadContract
	}
	var count int
	if err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM `+t.work.state.table("event_outbox")+` WHERE id=$1 AND event_type=$2 AND payload=$3::jsonb AND request_id=$4 AND created_by=$5 AND status='PENDING'`, id, e.EventType, string(payload), e.RequestID, e.ActorID).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return supply.ErrReadContract
	}
	t.completed = true
	return nil
}
func admissionEventPayload(e supply.ResourceAdmissionEffect) map[string]any {
	r := e.After
	p := e.Proof
	version := func(n uint64) string { return strconv.FormatUint(n, 10) }
	return map[string]any{
		"contract": "supply.resource_admitted", "schema_version": 1, "resource_id": r.ID, "supplier_id": r.SupplierID, "standard_model_id": r.StandardModelID, "region": r.Region, "resource_revision": version(r.Revision),
		"origin_row_id": r.OriginRowID, "origin_row_version": version(r.OriginRowVersion), "target_version": version(r.TargetVersion), "formal_config_id": r.FormalConfigID, "formal_config_revision": version(r.FormalConfigRevision),
		"report_id": r.LatestReportID, "report_sequence": version(r.LatestReportSequence), "flow_id": p.FlowID, "node_id": p.NodeID, "flow_revision": version(p.FlowRevision), "actor_id": e.ActorID, "reason": e.Reason, "request_id": e.RequestID, "completion_note": e.CompletionNote, "occurred_at": e.OccurredAt.UTC().Truncate(time.Microsecond),
	}
}
