package supplypg

import (
	"context"
	"database/sql"
	"model_bss/internal/domain/supply"
)

type TestEvidenceAuthority func(context.Context, *sql.Tx, *supply.Resource, string) (*ReportEvidenceRef, bool, error)
type TestEvidenceWork struct {
	state     *ResourceStateStore
	resolve   ModelTxResolver
	authority TestEvidenceAuthority
}

var _ supply.TestEvidenceUnitOfWork = (*TestEvidenceWork)(nil)

func NewTestEvidenceWork(state *ResourceStateStore, resolve ModelTxResolver, authority TestEvidenceAuthority) *TestEvidenceWork {
	return &TestEvidenceWork{state, resolve, authority}
}
func (w *TestEvidenceWork) WithinTestEvidence(ctx context.Context, run func(supply.TestEvidenceTransaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || w.state == nil || w.state.log == nil || w.resolve == nil || w.authority == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	tx, err := w.resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	t := &testEvidenceTransaction{work: w, tx: tx}
	if err := run(t); err != nil {
		return err
	}
	if !t.logged {
		return supply.ErrReadContract
	}
	return nil
}

type testEvidenceTransaction struct {
	work           *TestEvidenceWork
	tx             *sql.Tx
	before, staged *supply.Resource
	report         *supply.ResourceReportSummary
	logged         bool
}

var _ supply.TestEvidenceTransaction = (*testEvidenceTransaction)(nil)

func (t *testEvidenceTransaction) LockTestEvidence(ctx context.Context, id, reportID string) (*supply.TestEvidenceContext, error) {
	if t.before != nil || !modelIdentity(reportID) {
		return nil, supply.ErrReadContract
	}
	r, _, err := lockResourceUnderSupplier(ctx, t.tx, t.work.state, id)
	if err != nil {
		return nil, err
	}
	copy := *r
	ref, trusted, err := t.work.authority(ctx, t.tx, &copy, reportID)
	if err != nil {
		return nil, err
	}
	if !trusted {
		return nil, supply.ErrForbidden
	}
	if ref == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if ref.ReportID != reportID || ref.ResourceID != r.ID || ref.SupplierID != r.SupplierID || ref.StandardModelID != r.StandardModelID || ref.Region != r.Region {
		return nil, supply.ErrReadContract
	}
	report, err := ReadSelectedReport(ctx, t.tx, t.work.state.schema, *ref)
	if err != nil {
		return nil, err
	}
	t.before = r
	t.report = report
	resource := *r
	summary := *report
	return &supply.TestEvidenceContext{Resource: &resource, Report: &summary, TrustedB: true}, nil
}
func (t *testEvidenceTransaction) SaveTestEvidence(ctx context.Context, r *supply.Resource, expected uint64) error {
	if t.before == nil || t.staged != nil || t.logged || r == nil || t.before.ID != r.ID || t.before.Revision != expected {
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
func (t *testEvidenceTransaction) AppendTestEvidenceEffect(ctx context.Context, e supply.TestEvidenceEffect) error {
	if t.before == nil || t.report == nil || t.logged || e.ReportID != t.report.ID || e.Sequence != t.report.Sequence || e.TargetVersion != t.report.TargetVersion || e.OriginRowVersion != t.report.OriginRowVersion {
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
	write := resourceWriteContext{TestEvidence: &resourceTestEvidenceData{ReportID: e.ReportID, Sequence: e.Sequence, OriginRowVersion: e.OriginRowVersion, TargetVersion: e.TargetVersion, Outcome: e.Outcome, Valid: t.report.Valid}}
	if t.staged != nil {
		if e.Outcome != "SELECTED" || *after != *t.staged {
			return supply.ErrReadContract
		}
		if _, err := t.work.state.updateWithContext(ctx, t.tx, after, t.before.Revision, meta, "ATTACH_TEST_EVIDENCE", write); err != nil {
			return err
		}
	} else {
		if e.Outcome == "SELECTED" || *after != *before {
			return supply.ErrReadContract
		}
		if err := t.work.state.appendUnchangedContext(ctx, t.tx, after, meta, "TEST_"+e.Outcome, e.OccurredAt, write); err != nil {
			return err
		}
	}
	t.logged = true
	return nil
}
