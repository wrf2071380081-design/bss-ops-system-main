package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"strconv"
	"time"
)

// NativePair pins two distinct B reports from the exact task slots on one
// approved submission row. It never merges arbitrary latest resource reports.
type NativePair struct {
	Contract       string `json:"contract"`
	ResourceID     string `json:"resource_id"`
	SourceRevision uint64 `json:"source_revision,string"`
	TargetVersion  uint64 `json:"target_version,string"`
	Fingerprint    string `json:"fingerprint"`
	BaselineJob    string `json:"baseline_job"`
	BaselineReport string `json:"baseline_report"`
	StressJob      string `json:"stress_job"`
	StressReport   string `json:"stress_report"`
	ReportID       string `json:"report_id"`
}

func (n *NativeHandling) pair(ctx context.Context, tx *sql.Tx, r *supply.Resource) (NativePair, bool, error) {
	var p NativePair
	a, row, _, err := n.approval(ctx, tx, r.OriginRowID)
	if err != nil {
		return p, false, err
	}
	if a.SourceRevision != r.OriginRowVersion || row.ResourceID != r.ID || row.ModelID != r.StandardModelID || row.Model != r.SupplierModelID || a.SupplierID != r.SupplierID || a.Region != r.Region || row.Fingerprint == "" {
		return p, false, supply.ErrRevisionConflict
	}
	p = NativePair{Contract: "supply.native_report_pair", ResourceID: r.ID, SourceRevision: r.OriginRowVersion, TargetVersion: r.TargetVersion, Fingerprint: row.Fingerprint}
	var currentFP, baselineFP, stressFP, bstatus, sstatus string
	err = tx.QueryRowContext(ctx, `SELECT coalesce(sr.test_fingerprint,''),b.id::text,bp.id::text,st.id::text,sp.id::text,coalesce(b.target_fingerprint,''),coalesce(st.target_fingerprint,''),b.status,st.status
 FROM `+n.State.table("submission_row")+` sr
 JOIN `+n.State.table("test_job")+` b ON b.id=sr.test_job_id AND b.resource_id=$2 AND b.category='baseline'
 JOIN `+n.State.table("test_job")+` st ON st.id=sr.stress_job_id AND st.resource_id=$2 AND st.category='stress'
 JOIN LATERAL (SELECT id FROM `+n.State.table("test_report")+` WHERE test_job_id=b.id AND resource_id=$2 ORDER BY id DESC LIMIT 1) bp ON TRUE
 JOIN LATERAL (SELECT id FROM `+n.State.table("test_report")+` WHERE test_job_id=st.id AND resource_id=$2 ORDER BY id DESC LIMIT 1) sp ON TRUE
 WHERE sr.id=$1 AND sr.resource_id=$2`, row.ID, r.ID).Scan(&currentFP, &p.BaselineJob, &p.BaselineReport, &p.StressJob, &p.StressReport, &baselineFP, &stressFP, &bstatus, &sstatus)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, supply.ErrTestEvidenceRequired
	}
	if err != nil {
		return p, false, err
	}
	bi, _ := strconv.ParseUint(p.BaselineReport, 10, 64)
	si, _ := strconv.ParseUint(p.StressReport, 10, 64)
	p.ReportID = p.BaselineReport
	if si > bi {
		p.ReportID = p.StressReport
	}
	valid := currentFP == p.Fingerprint && baselineFP == p.Fingerprint && stressFP == p.Fingerprint && bstatus == "SUCCEEDED" && sstatus == "SUCCEEDED"
	return p, valid, nil
}
func pairReference(r *supply.Resource, p NativePair, valid bool) *ReportEvidenceRef {
	seq, _ := strconv.ParseUint(p.ReportID, 10, 64)
	job := p.BaselineJob
	if p.ReportID == p.StressReport {
		job = p.StressJob
	}
	return &ReportEvidenceRef{ReportID: p.ReportID, JobID: job, ResourceID: r.ID, SupplierID: r.SupplierID, StandardModelID: r.StandardModelID, Region: r.Region, OriginRowID: r.OriginRowID, OriginRowVersion: r.OriginRowVersion, TargetVersion: r.TargetVersion, Sequence: seq, Valid: valid, BaselineReportID: p.BaselineReport, StressReportID: p.StressReport, TargetFingerprint: p.Fingerprint}
}

// SelectedReport returns the persisted exact pair, invalidating it immediately
// when B replaces a designated job/target. It remains readable after completion.
func (n *NativeHandling) SelectedReport(ctx context.Context, tx *sql.Tx, r *supply.Resource) (*ReportEvidenceRef, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT "after" FROM `+n.State.table("operation_log")+` WHERE entity_type='resource' AND entity_id=$1 AND action='SELECT_REPORT_PAIR' AND "after"->>'report_id'=$2 ORDER BY id DESC LIMIT 1`, r.ID, r.LatestReportID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p NativePair
	if json.Unmarshal(raw, &p) != nil || p.Contract != "supply.native_report_pair" || p.ResourceID != r.ID {
		return nil, supply.ErrReadContract
	}
	valid, err := n.State.SelectedReportValid(ctx, tx, r)
	if err != nil {
		return nil, err
	}
	current, currentValid, currentErr := n.pair(ctx, tx, r)
	if currentErr != nil && !errors.Is(currentErr, supply.ErrForbidden) && !errors.Is(currentErr, supply.ErrRevisionConflict) && !errors.Is(currentErr, supply.ErrTestEvidenceRequired) {
		return nil, currentErr
	}
	if currentErr != nil || current != p {
		valid = false
	}
	valid = valid && currentValid
	return pairReference(r, p, valid), nil
}

// RefreshEvidence is a real handling action. SQL row locks and the authenticated
// actor precede server-side pair selection; no report/boolean is accepted from HTTP.
func (n *NativeHandling) RefreshEvidence(ctx context.Context, meta supply.WriteMeta, id string, expected uint64) (*supply.Resource, error) {
	tx, err := GORMModelTxFromContext(ctx)
	if err != nil {
		return nil, err
	}
	r, _, err := lockResourceUnderSupplier(ctx, tx, n.State, id)
	if err != nil {
		return nil, err
	}
	if r.Revision != expected {
		return nil, supply.ErrRevisionConflict
	}
	proof, err := n.Proof(ctx, tx, r)
	if err != nil {
		return nil, err
	}
	if proof.AssigneeID != meta.ActorID {
		return nil, supply.ErrForbidden
	}
	p, valid, err := n.pair(ctx, tx, r)
	if err != nil {
		return nil, err
	}
	ref := pairReference(r, p, valid)
	report, err := ReadSelectedReport(ctx, tx, n.State.schema, *ref)
	if err != nil {
		return nil, err
	}
	change, err := supply.ProposeTestEvidence(r, report, time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return nil, err
	}
	if change.Outcome == "IGNORED_OLDER" || change.Outcome == "IGNORED_STALE_TARGET" {
		return nil, supply.ErrRevisionConflict
	}
	raw, _ := json.Marshal(p)
	if err := n.State.log(ctx, tx, ResourceStateLog{EntityType: "resource", EntityID: r.ID, Action: "SELECT_REPORT_PAIR", ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, After: raw, OccurredAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	write := resourceWriteContext{TestEvidence: &resourceTestEvidenceData{ReportID: report.ID, Sequence: report.Sequence, OriginRowVersion: report.OriginRowVersion, TargetVersion: report.TargetVersion, Outcome: change.Outcome, Valid: report.Valid}}
	if change.Changed {
		return n.State.updateWithContext(ctx, tx, change.Resource, r.Revision, meta, "ATTACH_TEST_EVIDENCE", write)
	}
	if err := n.State.appendUnchangedContext(ctx, tx, r, meta, "TEST_NOOP", time.Now().UTC(), write); err != nil {
		return nil, err
	}
	return r, nil
}

func readNativeReportPair(ctx context.Context, tx *sql.Tx, schema string, ref ReportEvidenceRef) (*supply.ResourceReportSummary, error) {
	if !modelIdentity(ref.BaselineReportID) || !modelIdentity(ref.StressReportID) || ref.TargetFingerprint == "" {
		return nil, supply.ErrReadContract
	}
	table := func(s string) string { return `"` + schema + `"."` + s + `"` }
	var baselineJob, stressJob, bfp, sfp, bstatus, sstatus string
	var bp, sp sql.NullBool
	var ms, rps, tps sql.NullString
	var cache ReportCacheRow
	err := tx.QueryRowContext(ctx, `SELECT b.test_job_id::text,s.test_job_id::text,coalesce(bj.target_fingerprint,''),coalesce(sj.target_fingerprint,''),bj.status,sj.status,b.connection_passed,s.stress_passed,b.connection_ms::text,s.rps::text,s.tps::text,s.cache_hit_tokens,s.cache_input_tokens,s.scene
 FROM `+table("test_report")+` b JOIN `+table("test_job")+` bj ON bj.id=b.test_job_id AND bj.category='baseline' AND bj.resource_id=$3
 JOIN `+table("test_report")+` s ON s.id=$2 AND s.resource_id=$3 JOIN `+table("test_job")+` sj ON sj.id=s.test_job_id AND sj.category='stress' AND sj.resource_id=$3
 JOIN `+table("resource")+` r ON r.id=$3 AND r.supplier_id=$4 AND r.standard_model_id=$5 AND r.region=$6
 WHERE b.id=$1 AND b.resource_id=$3`, ref.BaselineReportID, ref.StressReportID, ref.ResourceID, ref.SupplierID, ref.StandardModelID, ref.Region).Scan(&baselineJob, &stressJob, &bfp, &sfp, &bstatus, &sstatus, &bp, &sp, &ms, &rps, &tps, &cache.CacheHitTokens, &cache.CacheInputTokens, &cache.Scene)
	if err != nil {
		return nil, err
	}
	anchor, job := ref.BaselineReportID, baselineJob
	bi, _ := strconv.ParseUint(ref.BaselineReportID, 10, 64)
	si, _ := strconv.ParseUint(ref.StressReportID, 10, 64)
	if si > bi {
		anchor, job = ref.StressReportID, stressJob
	}
	if ref.ReportID != anchor || ref.JobID != job {
		return nil, supply.ErrReadContract
	}
	str := func(v sql.NullString) *string {
		if !v.Valid {
			return nil
		}
		s := v.String
		return &s
	}
	return &supply.ResourceReportSummary{ID: ref.ReportID, ResourceID: ref.ResourceID, SupplierID: ref.SupplierID, StandardModelID: ref.StandardModelID, Region: ref.Region, OriginRowID: ref.OriginRowID, OriginRowVersion: ref.OriginRowVersion, TargetVersion: ref.TargetVersion, Sequence: ref.Sequence, ConnectivityPassed: bp.Valid && bp.Bool, PressurePassed: sp.Valid && sp.Bool, Valid: ref.Valid && bp.Valid && sp.Valid && bfp == ref.TargetFingerprint && sfp == ref.TargetFingerprint && bstatus == "SUCCEEDED" && sstatus == "SUCCEEDED", Metrics: supply.ResourceReportMetrics{ConnectionMS: str(ms), RPS: str(rps), TPS: str(tps)}, Cache: cache.DomainCache()}, nil
}
