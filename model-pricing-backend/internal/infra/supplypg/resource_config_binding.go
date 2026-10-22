package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"reflect"
)

type AdmissionProofLoader func(context.Context, *sql.Tx, *supply.Resource) (*supply.AdmissionProof, error)
type ResourceBindingWork struct {
	state   *ResourceStateStore
	configs *FormalConfigStore
	resolve ModelTxResolver
	proof   AdmissionProofLoader
}

var _ supply.ResourceConfigBindingUnitOfWork = (*ResourceBindingWork)(nil)

func NewResourceBindingWork(state *ResourceStateStore, configs *FormalConfigStore, resolve ModelTxResolver, proof AdmissionProofLoader) *ResourceBindingWork {
	return &ResourceBindingWork{state, configs, resolve, proof}
}
func (w *ResourceBindingWork) WithinResourceConfigBinding(ctx context.Context, run func(supply.ResourceConfigBindingTransaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || w.state == nil || w.state.log == nil || w.configs == nil || w.state.schema != w.configs.schema || w.resolve == nil || w.proof == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	tx, err := w.resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	t := &resourceBindingTransaction{work: w, tx: tx}
	if err := run(t); err != nil {
		return err
	}
	if !t.logged {
		return supply.ErrReadContract
	}
	return nil
}

type resourceBindingTransaction struct {
	work           *ResourceBindingWork
	tx             *sql.Tx
	before, staged *supply.Resource
	proof          *supply.AdmissionProof
	logged         bool
}

var _ supply.ResourceConfigBindingTransaction = (*resourceBindingTransaction)(nil)

func (t *resourceBindingTransaction) LockResourceConfigBinding(ctx context.Context, id, config string) (*supply.ResourceConfigBindingContext, error) {
	if t.before != nil || !modelIdentity(id) || !modelIdentity(config) {
		return nil, supply.ErrReadContract
	}
	// Supplier identity is immutable; reread it after acquiring the common
	// supplier -> resource -> configuration -> B proof lock order.
	var supplier string
	if err := t.tx.QueryRowContext(ctx, `SELECT supplier_id::text FROM `+t.work.state.table("resource")+` WHERE id=$1`, id).Scan(&supplier); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrNotFound
		}
		return nil, err
	}
	var locked, status string
	if err := t.tx.QueryRowContext(ctx, `SELECT id::text,status FROM `+t.work.state.table("supplier")+` WHERE id=$1 FOR UPDATE`, supplier).Scan(&locked, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrReadContract
		}
		return nil, err
	}
	if status != "ACTIVE" {
		return nil, supply.ErrInactive
	}
	r, err := t.work.state.ForUpdate(ctx, t.tx, id)
	if err != nil {
		return nil, err
	}
	if r.SupplierID != supplier {
		return nil, supply.ErrReadContract
	}
	if err := t.tx.QueryRowContext(ctx, `SELECT id::text FROM `+t.work.state.table("supplier_formal_config")+` WHERE id=$1 AND supplier_id=$2 FOR UPDATE`, config, supplier).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrNotFound
		}
		return nil, err
	}
	c, err := t.work.configs.Current(ctx, t.tx, supplier, config)
	if err != nil {
		return nil, err
	}
	copy := *r
	proof, err := t.work.proof(ctx, t.tx, &copy)
	if err != nil {
		return nil, err
	}
	if proof == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	p := *proof
	t.before = r
	t.proof = &p
	resource := *r
	domainProof := p
	return &supply.ResourceConfigBindingContext{Resource: &resource, Proof: &domainProof, Config: &supply.ResourceFormalConfig{ID: c.ID, SupplierID: c.SupplierID, Region: c.Region, Status: c.Status, Revision: c.Revision, ConfirmedRevision: c.ConfirmedRevision, HasAPIKey: c.View().HasAPIKey}}, nil
}
func (t *resourceBindingTransaction) SaveResourceConfigBinding(ctx context.Context, r *supply.Resource, expected uint64) error {
	if t.before == nil || t.staged != nil || t.logged || t.before.Revision != expected || r == nil || r.ID != t.before.ID {
		return supply.ErrReadContract
	}
	c, err := canonicalStoredResource(r)
	if err != nil {
		return err
	}
	if c.Revision != expected+1 {
		return supply.ErrRevisionConflict
	}
	t.staged = c
	return nil
}
func (t *resourceBindingTransaction) AppendResourceConfigBindingEffect(ctx context.Context, e supply.ResourceConfigBindingEffect) error {
	if t.before == nil || t.logged || !reflect.DeepEqual(t.proof, &e.Proof) || e.ActorID != t.proof.AssigneeID {
		return supply.ErrReadContract
	}
	before, err := canonicalStoredResource(&e.Before)
	if err != nil {
		return err
	}
	after, err := canonicalStoredResource(&e.After)
	if err != nil {
		return err
	}
	if *before != *t.before {
		return supply.ErrReadContract
	}
	meta := supply.WriteMeta{ActorID: e.ActorID, Reason: e.Reason, RequestID: e.RequestID}
	if t.staged != nil {
		if e.Action != "BIND" || *after != *t.staged {
			return supply.ErrReadContract
		}
		if _, err := t.work.state.updateWithProof(ctx, t.tx, after, t.before.Revision, meta, e.Action, t.proof); err != nil {
			return err
		}
	} else {
		if e.Action != "BIND_NOOP" || *after != *before {
			return supply.ErrReadContract
		}
		if err := t.work.state.appendUnchanged(ctx, t.tx, after, meta, e.Action, e.OccurredAt, t.proof); err != nil {
			return err
		}
	}
	t.logged = true
	return nil
}
