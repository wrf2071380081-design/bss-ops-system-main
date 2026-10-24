package supplypg

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"model_bss/internal/domain/supply"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type ResourceStateLog struct {
	EntityType, EntityID, Action, BusinessAction, ActorID, Reason, RequestID string
	Before, After                                                            json.RawMessage
	OccurredAt                                                               time.Time
}
type ResourceStateLogAppender func(context.Context, *sql.Tx, ResourceStateLog) error
type ResourceReportRefLoader func(context.Context, *sql.Tx, *supply.Resource) (*ReportEvidenceRef, error)
type ResourceConfigViewLoader func(context.Context, *sql.Tx, *supply.Resource) (*supply.FormalConfigView, error)
type ResourceStateStore struct {
	schema string
	log    ResourceStateLogAppender
}

func NewResourceStateStore(schema string, log ResourceStateLogAppender) (*ResourceStateStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	return &ResourceStateStore{schema, log}, nil
}

const resourceRevisionAction = "RESOURCE_REVISION"

var resourceControlAction = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,31}$`)
var resourceStoredCoefficient = regexp.MustCompile(`^(0|1)(\.[0-9]{1,4})?$`)

type resourceControlData struct {
	ID                   string    `json:"id"`
	SupplierID           string    `json:"supplier_id"`
	StandardModelID      string    `json:"standard_model_id"`
	Region               string    `json:"region"`
	SupplierModelID      string    `json:"supplier_model_id"`
	Coefficient          string    `json:"coefficient"`
	BuyerID              string    `json:"buyer_id"`
	State                string    `json:"state"`
	EverAdmitted         bool      `json:"ever_admitted"`
	Revision             uint64    `json:"revision,string"`
	FormalConfigID       string    `json:"formal_config_id"`
	FormalConfigRevision uint64    `json:"formal_config_revision,string"`
	OriginRowID          string    `json:"origin_row_id"`
	OriginRowVersion     uint64    `json:"origin_row_version,string"`
	TargetVersion        uint64    `json:"target_version,string"`
	LatestReportID       string    `json:"latest_report_id"`
	LatestReportSequence uint64    `json:"latest_report_sequence,string"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}
type resourceControlEnvelope struct {
	Contract       string                     `json:"contract"`
	SchemaVersion  int                        `json:"schema_version"`
	Operation      string                     `json:"operation"`
	Resource       resourceControlData        `json:"resource"`
	Proof          *resourceProofData         `json:"proof,omitempty"`
	TestEvidence   *resourceTestEvidenceData  `json:"test_evidence,omitempty"`
	CompletionNote string                     `json:"completion_note,omitempty"`
	PlanExecution  *resourcePlanExecutionData `json:"plan_execution,omitempty"`
}

type resourcePlanExecutionData struct {
	PlanID        string `json:"plan_id"`
	PlanRevision  uint64 `json:"plan_revision,string"`
	SourceEventID string `json:"source_event_id"`
}
type resourceTestEvidenceData struct {
	ReportID         string `json:"report_id"`
	Sequence         uint64 `json:"sequence,string"`
	OriginRowVersion uint64 `json:"origin_row_version,string"`
	TargetVersion    uint64 `json:"target_version,string"`
	Outcome          string `json:"outcome"`
	Valid            bool   `json:"valid"`
}
type resourceWriteContext struct {
	Proof          *supply.AdmissionProof
	TestEvidence   *resourceTestEvidenceData
	CompletionNote string
	PlanExecution  *resourcePlanExecutionData
}
type resourceProofData struct {
	FlowID       string `json:"flow_id"`
	NodeID       string `json:"node_id"`
	AssigneeID   string `json:"assignee_id"`
	FlowRevision uint64 `json:"flow_revision,string"`
}

func controlProof(p *supply.AdmissionProof) *resourceProofData {
	if p == nil {
		return nil
	}
	return &resourceProofData{p.FlowID, p.NodeID, p.AssigneeID, p.FlowRevision}
}

func canonicalStoredResource(input *supply.Resource) (*supply.Resource, error) {
	if input == nil {
		return nil, supply.ErrReadContract
	}
	r := *input
	for _, id := range []string{r.ID, r.SupplierID, r.StandardModelID, r.BuyerID, r.OriginRowID} {
		if !modelIdentity(id) {
			return nil, supply.ErrReadContract
		}
	}
	for _, v := range []uint64{r.Revision, r.OriginRowVersion, r.TargetVersion} {
		if v == 0 || v > math.MaxInt64 {
			return nil, supply.ErrReadContract
		}
	}
	if (r.Region != "CN" && r.Region != "OVERSEAS") || !slices.Contains([]string{"PENDING_TEST", "PENDING_ACCESS", "ADMITTED", "OFFLINE"}, r.State) || r.EverAdmitted != (r.State == "ADMITTED" || r.State == "OFFLINE") {
		return nil, supply.ErrReadContract
	}
	if strings.TrimSpace(r.SupplierModelID) == "" || !utf8.ValidString(r.SupplierModelID) || strings.ContainsRune(r.SupplierModelID, 0) || utf8.RuneCountInString(r.SupplierModelID) > 128 {
		return nil, supply.ErrReadContract
	}
	if (r.FormalConfigID == "") != (r.FormalConfigRevision == 0) || r.FormalConfigRevision > math.MaxInt64 || (r.FormalConfigID != "" && !modelIdentity(r.FormalConfigID)) {
		return nil, supply.ErrReadContract
	}
	if (r.LatestReportID == "") != (r.LatestReportSequence == 0) || r.LatestReportSequence > math.MaxInt64 || (r.LatestReportID != "" && !modelIdentity(r.LatestReportID)) {
		return nil, supply.ErrReadContract
	}
	if !resourceStoredCoefficient.MatchString(r.Coefficient) {
		return nil, supply.ErrReadContract
	}
	n := resourceNumber(r.Coefficient)
	if n == nil || n.Sign() <= 0 || n.Cmp(resourceNumber("1")) > 0 {
		return nil, supply.ErrReadContract
	}
	r.Coefficient = n.FloatString(4)
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		return nil, supply.ErrReadContract
	}
	r.CreatedAt = r.CreatedAt.UTC().Truncate(time.Microsecond)
	r.UpdatedAt = r.UpdatedAt.UTC().Truncate(time.Microsecond)
	return &r, nil
}
func resourceControlFrom(r *supply.Resource, operation string) resourceControlEnvelope {
	return resourceControlEnvelope{Contract: "supply.resource", SchemaVersion: 1, Operation: operation, Resource: resourceControlData{
		ID: r.ID, SupplierID: r.SupplierID, StandardModelID: r.StandardModelID, Region: r.Region, SupplierModelID: r.SupplierModelID, Coefficient: r.Coefficient, BuyerID: r.BuyerID, State: r.State, EverAdmitted: r.EverAdmitted, Revision: r.Revision, FormalConfigID: r.FormalConfigID, FormalConfigRevision: r.FormalConfigRevision, OriginRowID: r.OriginRowID, OriginRowVersion: r.OriginRowVersion, TargetVersion: r.TargetVersion, LatestReportID: r.LatestReportID, LatestReportSequence: r.LatestReportSequence, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}}
}
func (e resourceControlEnvelope) domain() (*supply.Resource, error) {
	if e.Contract != "supply.resource" || e.SchemaVersion != 1 || !resourceControlAction.MatchString(e.Operation) {
		return nil, supply.ErrReadContract
	}
	if p := e.Proof; p != nil {
		if !modelIdentity(p.FlowID) || !modelIdentity(p.NodeID) || !modelIdentity(p.AssigneeID) || p.FlowRevision == 0 || p.FlowRevision > math.MaxInt64 {
			return nil, supply.ErrReadContract
		}
	}
	if v := e.TestEvidence; v != nil {
		if !modelIdentity(v.ReportID) || v.Sequence == 0 || v.Sequence > math.MaxInt64 || v.OriginRowVersion == 0 || v.OriginRowVersion > math.MaxInt64 || v.TargetVersion == 0 || v.TargetVersion > math.MaxInt64 || !slices.Contains([]string{"SELECTED", "NOOP", "IGNORED_OLDER", "IGNORED_STALE_TARGET"}, v.Outcome) {
			return nil, supply.ErrReadContract
		}
		if (v.Outcome == "SELECTED" || v.Outcome == "NOOP") && (v.ReportID != e.Resource.LatestReportID || v.Sequence != e.Resource.LatestReportSequence || v.OriginRowVersion != e.Resource.OriginRowVersion || v.TargetVersion != e.Resource.TargetVersion) {
			return nil, supply.ErrReadContract
		}
	}
	if e.CompletionNote != "" && (!utf8.ValidString(e.CompletionNote) || strings.ContainsRune(e.CompletionNote, 0) || strings.TrimSpace(e.CompletionNote) == "") {
		return nil, supply.ErrReadContract
	}
	if e.Operation == "ADMIT" && (strings.TrimSpace(e.CompletionNote) == "" || e.Proof == nil || e.Resource.State != "ADMITTED" || !e.Resource.EverAdmitted) {
		return nil, supply.ErrReadContract
	}
	if p := e.PlanExecution; p != nil {
		if !modelIdentity(p.PlanID) || p.PlanRevision == 0 || p.PlanRevision > math.MaxInt64 || !modelIdentity(p.SourceEventID) {
			return nil, supply.ErrReadContract
		}
	}
	if e.Operation == "PLAN_ONLINE" || e.Operation == "PLAN_OFFLINE" || e.Operation == "PLAN_PRICE_CHANGE" {
		if e.PlanExecution == nil || strings.TrimSpace(e.CompletionNote) == "" || !e.Resource.EverAdmitted || (e.Operation == "PLAN_ONLINE" && e.Resource.State != "ADMITTED") || (e.Operation == "PLAN_OFFLINE" && e.Resource.State != "OFFLINE") || (e.Operation == "PLAN_PRICE_CHANGE" && e.Resource.State != "ADMITTED" && e.Resource.State != "OFFLINE") {
			return nil, supply.ErrReadContract
		}
	}
	r := e.Resource
	return canonicalStoredResource(&supply.Resource{ID: r.ID, SupplierID: r.SupplierID, StandardModelID: r.StandardModelID, Region: r.Region, SupplierModelID: r.SupplierModelID, Coefficient: r.Coefficient, BuyerID: r.BuyerID, State: r.State, EverAdmitted: r.EverAdmitted, Revision: r.Revision, FormalConfigID: r.FormalConfigID, FormalConfigRevision: r.FormalConfigRevision, OriginRowID: r.OriginRowID, OriginRowVersion: r.OriginRowVersion, TargetVersion: r.TargetVersion, LatestReportID: r.LatestReportID, LatestReportSequence: r.LatestReportSequence, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
}
func decodeResourceControl(raw []byte) (resourceControlEnvelope, *supply.Resource, error) {
	var e resourceControlEnvelope
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, nil, supply.ErrReadContract
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return e, nil, supply.ErrReadContract
	}
	r, err := e.domain()
	return e, r, err
}
func storedResourceProjection(r *supply.Resource) ResourceProjection {
	return ResourceProjection{ID: r.ID, SupplierID: r.SupplierID, StandardModelID: r.StandardModelID, Region: r.Region, SupplierModelID: r.SupplierModelID, Coefficient: r.Coefficient, BuyerID: r.BuyerID, State: r.State, EverAdmitted: r.EverAdmitted, LatestReportID: r.LatestReportID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}
func (s *ResourceStateStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }
func (s *ResourceStateStore) ready(ctx context.Context, tx *sql.Tx) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || tx == nil {
		return supply.ErrDependencyUnavailable
	}
	return nil
}
func (s *ResourceStateStore) NextID(ctx context.Context, tx *sql.Tx) (string, error) {
	if err := s.ready(ctx, tx); err != nil {
		return "", err
	}
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT nextval(pg_get_serial_sequence($1,'id'))::text`, s.table("resource")).Scan(&id); err != nil {
		return "", err
	}
	if !modelIdentity(id) {
		return "", supply.ErrReadContract
	}
	return id, nil
}
func (s *ResourceStateStore) native(ctx context.Context, tx *sql.Tx, id string) (ResourceProjection, error) {
	var p ResourceProjection
	var buyer, report sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id::text,supplier_id::text,standard_model_id::text,region,supplier_model_name,coefficient::text,buyer_id::text,state,ever_admitted,latest_test_report_id::text,created_at,updated_at FROM `+s.table("resource")+` WHERE id=$1`, id).Scan(&p.ID, &p.SupplierID, &p.StandardModelID, &p.Region, &p.SupplierModelID, &p.Coefficient, &buyer, &p.State, &p.EverAdmitted, &report, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, supply.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if !buyer.Valid {
		return p, supply.ErrReadContract
	}
	p.BuyerID = buyer.String
	p.LatestReportID = report.String
	if p.State == "PENDING_ADMIT" {
		p.State = "PENDING_ACCESS"
	}
	return p, nil
}

type resourceControlHead struct {
	envelope resourceControlEnvelope
	resource *supply.Resource
	meta     supply.WriteMeta
	at       time.Time
}

func (s *ResourceStateStore) head(ctx context.Context, tx *sql.Tx, id string) (*resourceControlHead, error) {
	var raw []byte
	var actor, reason, request sql.NullString
	var at time.Time
	err := tx.QueryRowContext(ctx, `SELECT "after",operator_id::text,reason,request_id,created_at FROM `+s.table("operation_log")+` WHERE entity_type='resource' AND entity_id=$1 AND action=$2 ORDER BY id DESC LIMIT 1`, id, resourceRevisionAction).Scan(&raw, &actor, &reason, &request, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrReadContract
	}
	if err != nil {
		return nil, err
	}
	e, r, err := decodeResourceControl(raw)
	if err != nil {
		return nil, err
	}
	meta := supply.WriteMeta{ActorID: actor.String, Reason: reason.String, RequestID: request.String}
	if r.ID != id || validateResourceControlMeta(meta, e.Operation) != nil || !at.Equal(r.UpdatedAt) {
		return nil, supply.ErrReadContract
	}
	if e.Proof != nil && e.Proof.AssigneeID != meta.ActorID {
		return nil, supply.ErrReadContract
	}
	return &resourceControlHead{e, r, meta, at}, nil
}
func (s *ResourceStateStore) Current(ctx context.Context, tx *sql.Tx, id string) (*supply.Resource, error) {
	if !modelIdentity(id) {
		return nil, supply.ErrReadContract
	}
	if err := s.ready(ctx, tx); err != nil {
		return nil, err
	}
	p, err := s.native(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	h, err := s.head(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if !resourceProjectionEqual(storedResourceProjection(h.resource), p) {
		return nil, supply.ErrReadContract
	}
	if err := s.checkReportOwnership(ctx, tx, h.resource); err != nil {
		return nil, err
	}
	return h.resource, nil
}
func (s *ResourceStateStore) checkReportOwnership(ctx context.Context, tx *sql.Tx, r *supply.Resource) error {
	if r.LatestReportID == "" {
		return nil
	}
	var resource string
	var jobResource sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT p.resource_id::text,j.resource_id::text FROM `+s.table("test_report")+` p LEFT JOIN `+s.table("test_job")+` j ON j.id=p.test_job_id WHERE p.id=$1`, r.LatestReportID).Scan(&resource, &jobResource)
	if errors.Is(err, sql.ErrNoRows) {
		return supply.ErrReadContract
	}
	if err != nil {
		return err
	}
	if resource != r.ID || !jobResource.Valid || jobResource.String != r.ID {
		return supply.ErrReadContract
	}
	return nil
}

func selectedReportEvidenceMatches(r *supply.Resource, evidence *resourceTestEvidenceData) bool {
	return r != nil && evidence != nil && evidence.Valid &&
		evidence.ReportID == r.LatestReportID && evidence.Sequence == r.LatestReportSequence &&
		evidence.OriginRowVersion == r.OriginRowVersion && evidence.TargetVersion == r.TargetVersion &&
		(evidence.Outcome == "SELECTED" || evidence.Outcome == "NOOP")
}

// SelectedReportValid returns B's validity only when a persisted A control
// record shows that this exact report and target version was selected. A report
// row by itself is not evidence that B accepted it.
func (s *ResourceStateStore) SelectedReportValid(ctx context.Context, tx *sql.Tx, r *supply.Resource) (bool, error) {
	if err := s.ready(ctx, tx); err != nil {
		return false, err
	}
	if r == nil || !modelIdentity(r.ID) || !modelIdentity(r.LatestReportID) {
		return false, supply.ErrReadContract
	}
	head, err := s.head(ctx, tx, r.ID)
	if err != nil {
		return false, err
	}
	if head.resource.ID != r.ID || head.resource.LatestReportID != r.LatestReportID || head.resource.LatestReportSequence != r.LatestReportSequence || head.resource.OriginRowVersion != r.OriginRowVersion || head.resource.TargetVersion != r.TargetVersion {
		return false, supply.ErrReadContract
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT "after" FROM `+s.table("operation_log")+`
WHERE entity_type='resource' AND entity_id=$1 AND action=$2
  AND "after"->'test_evidence'->>'report_id'=$3
  AND "after"->'test_evidence'->>'outcome' IN ('SELECTED','NOOP')
ORDER BY id DESC LIMIT 1`, r.ID, resourceRevisionAction, r.LatestReportID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	envelope, selectedResource, err := decodeResourceControl(raw)
	if err != nil {
		return false, err
	}
	evidence := envelope.TestEvidence
	if selectedResource.ID != r.ID || selectedResource.LatestReportID != r.LatestReportID || selectedResource.LatestReportSequence != r.LatestReportSequence || selectedResource.OriginRowVersion != r.OriginRowVersion || selectedResource.TargetVersion != r.TargetVersion {
		return false, nil
	}
	return selectedReportEvidenceMatches(r, evidence), nil
}
func (s *ResourceStateStore) ForUpdate(ctx context.Context, tx *sql.Tx, id string) (*supply.Resource, error) {
	if !modelIdentity(id) {
		return nil, supply.ErrReadContract
	}
	if err := s.ready(ctx, tx); err != nil {
		return nil, err
	}
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM `+s.table("resource")+` WHERE id=$1 FOR UPDATE`, id).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrNotFound
		}
		return nil, err
	}
	// A separate statement sees the prior lock holder's committed control head.
	return s.Current(ctx, tx, id)
}
func validateResourceControlMeta(meta supply.WriteMeta, action string) error {
	if !modelIdentity(meta.ActorID) || !resourceControlAction.MatchString(action) {
		return supply.ErrReadContract
	}
	for _, f := range []struct {
		name, value string
		max         int
	}{{"reason", meta.Reason, 500}, {"request_id", meta.RequestID, 64}} {
		if strings.TrimSpace(f.value) == "" || !utf8.ValidString(f.value) || strings.ContainsRune(f.value, 0) || utf8.RuneCountInString(f.value) > f.max {
			return &supply.FieldError{Path: f.name, Reason: "required value exceeds storage contract or contains invalid text"}
		}
	}
	return nil
}
func (s *ResourceStateStore) append(ctx context.Context, tx *sql.Tx, before, after *supply.Resource, meta supply.WriteMeta, operation string) error {
	return s.appendProof(ctx, tx, before, after, meta, operation, nil)
}
func (s *ResourceStateStore) appendProof(ctx context.Context, tx *sql.Tx, before, after *supply.Resource, meta supply.WriteMeta, operation string, proof *supply.AdmissionProof) error {
	return s.appendContext(ctx, tx, before, after, meta, operation, resourceWriteContext{Proof: proof})
}
func (s *ResourceStateStore) appendContext(ctx context.Context, tx *sql.Tx, before, after *supply.Resource, meta supply.WriteMeta, operation string, write resourceWriteContext) error {
	if operation == "PLAN_PRICE_CHANGE" {
		if before == nil || after == nil || before.ID != after.ID || (before.Coefficient == after.Coefficient && before.SupplierModelID == after.SupplierModelID) || before.State != after.State || !before.EverAdmitted || !after.EverAdmitted || after.Revision != before.Revision+1 || write.PlanExecution == nil || strings.TrimSpace(write.CompletionNote) == "" {
			return supply.ErrReadContract
		}
		if before.SupplierModelID != after.SupplierModelID && (before.TargetVersion >= math.MaxInt64 || after.TargetVersion != before.TargetVersion+1) || before.SupplierModelID == after.SupplierModelID && after.TargetVersion != before.TargetVersion {
			return supply.ErrReadContract
		}
	}
	e := resourceControlFrom(after, operation)
	e.Proof = controlProof(write.Proof)
	e.TestEvidence = write.TestEvidence
	e.CompletionNote = write.CompletionNote
	e.PlanExecution = write.PlanExecution
	if _, err := e.domain(); err != nil {
		return err
	}
	if write.Proof != nil && write.Proof.AssigneeID != meta.ActorID {
		return supply.ErrReadContract
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return supply.ErrReadContract
	}
	var old []byte
	if before != nil {
		old, err = json.Marshal(resourceControlFrom(before, operation))
		if err != nil {
			return supply.ErrReadContract
		}
	}
	entry := ResourceStateLog{EntityType: "resource", EntityID: after.ID, Action: resourceRevisionAction, BusinessAction: operation, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, Before: old, After: raw, OccurredAt: after.UpdatedAt}
	if err := s.log(ctx, tx, entry); err != nil {
		return err
	}
	h, err := s.head(ctx, tx, after.ID)
	if err != nil {
		return err
	}
	if *h.resource != *after || h.envelope.Operation != operation || h.meta != meta {
		return supply.ErrReadContract
	}
	if (e.Proof == nil) != (h.envelope.Proof == nil) || (e.Proof != nil && *e.Proof != *h.envelope.Proof) {
		return supply.ErrReadContract
	}
	if (e.TestEvidence == nil) != (h.envelope.TestEvidence == nil) || (e.TestEvidence != nil && *e.TestEvidence != *h.envelope.TestEvidence) || e.CompletionNote != h.envelope.CompletionNote {
		return supply.ErrReadContract
	}
	if (e.PlanExecution == nil) != (h.envelope.PlanExecution == nil) || (e.PlanExecution != nil && *e.PlanExecution != *h.envelope.PlanExecution) {
		return supply.ErrReadContract
	}
	return nil
}
func nativeResourceState(state string) string {
	if state == "PENDING_ACCESS" {
		return "PENDING_ADMIT"
	}
	return state
}
func (s *ResourceStateStore) Insert(ctx context.Context, tx *sql.Tx, input *supply.Resource, meta supply.WriteMeta, operation string) (*supply.Resource, error) {
	return s.insertPrepared(ctx, tx, input, meta, operation, nil)
}
func (s *ResourceStateStore) insertPrepared(ctx context.Context, tx *sql.Tx, input *supply.Resource, meta supply.WriteMeta, operation string, prepare func(context.Context, *sql.Tx, *supply.Resource) (*resourceTestEvidenceData, error)) (*supply.Resource, error) {
	r, err := canonicalStoredResource(input)
	if err != nil {
		return nil, err
	}
	if r.Revision != 1 || r.EverAdmitted || operation != "CREATE" {
		return nil, supply.ErrReadContract
	}
	if err := validateResourceControlMeta(meta, operation); err != nil {
		return nil, err
	}
	if err := s.ready(ctx, tx); err != nil {
		return nil, err
	}
	if s.log == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if r.LatestReportID != "" && prepare == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.table("resource")+` (id,standard_model_id,supplier_id,supplier_model_name,standard_model_name_snapshot,region,coefficient,state,ever_admitted,buyer_id,latest_test_report_id,created_at,updated_at,created_by,updated_by,request_id) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,(SELECT name FROM `+s.table("standard_model")+` WHERE id=$2),$5,$6,$7,$8,$9,NULL,$10,$11,$12,$12,$13)`, r.ID, r.StandardModelID, r.SupplierID, r.SupplierModelID, r.Region, r.Coefficient, nativeResourceState(r.State), r.EverAdmitted, r.BuyerID, r.CreatedAt, r.UpdatedAt, meta.ActorID, meta.RequestID)
	if err != nil {
		return nil, modelUniqueError(err, supply.ErrIdentityConflict)
	}
	var selectedEvidence *resourceTestEvidenceData
	if r.LatestReportID != "" {
		copy := *r
		evidence, err := prepare(ctx, tx, &copy)
		if err != nil {
			return nil, err
		}
		if evidence == nil || evidence.Outcome != "SELECTED" || evidence.ReportID != r.LatestReportID || evidence.Sequence != r.LatestReportSequence || evidence.OriginRowVersion != r.OriginRowVersion || evidence.TargetVersion != r.TargetVersion {
			return nil, supply.ErrReadContract
		}
		selectedEvidence = evidence
		if err := s.checkReportOwnership(ctx, tx, r); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+s.table("resource")+` SET latest_test_report_id=$1 WHERE id=$2`, r.LatestReportID, r.ID); err != nil {
			return nil, err
		}
	}
	// The report was validated against B's submission proof above; retain that
	// exact evidence in the initial resource revision snapshot.
	write := resourceWriteContext{TestEvidence: selectedEvidence}
	if err := s.appendContext(ctx, tx, nil, r, meta, operation, write); err != nil {
		return nil, err
	}
	return s.Current(ctx, tx, r.ID)
}
func nullableResourceID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
func (s *ResourceStateStore) Update(ctx context.Context, tx *sql.Tx, input *supply.Resource, expected uint64, meta supply.WriteMeta, operation string) (*supply.Resource, error) {
	return s.updateWithProof(ctx, tx, input, expected, meta, operation, nil)
}
func (s *ResourceStateStore) updateWithProof(ctx context.Context, tx *sql.Tx, input *supply.Resource, expected uint64, meta supply.WriteMeta, operation string, proof *supply.AdmissionProof) (*supply.Resource, error) {
	return s.updateWithContext(ctx, tx, input, expected, meta, operation, resourceWriteContext{Proof: proof})
}
func (s *ResourceStateStore) updateWithContext(ctx context.Context, tx *sql.Tx, input *supply.Resource, expected uint64, meta supply.WriteMeta, operation string, write resourceWriteContext) (*supply.Resource, error) {
	r, err := canonicalStoredResource(input)
	if err != nil {
		return nil, err
	}
	if expected == 0 || expected >= math.MaxInt64 || r.Revision != expected+1 {
		return nil, supply.ErrRevisionConflict
	}
	if err := validateResourceControlMeta(meta, operation); err != nil {
		return nil, err
	}
	if err := s.ready(ctx, tx); err != nil {
		return nil, err
	}
	if s.log == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	before, err := s.ForUpdate(ctx, tx, r.ID)
	if err != nil {
		return nil, err
	}
	if before.Revision != expected {
		return nil, supply.ErrRevisionConflict
	}
	if r.SupplierID != before.SupplierID || r.StandardModelID != before.StandardModelID || r.Region != before.Region || !r.CreatedAt.Equal(before.CreatedAt) || r.UpdatedAt.Before(before.UpdatedAt) {
		return nil, supply.ErrReadContract
	}
	if err := s.checkReportOwnership(ctx, tx, r); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE `+s.table("resource")+` SET supplier_model_name=$2,coefficient=$3,state=$4,ever_admitted=$5,buyer_id=$6,latest_test_report_id=$7,updated_at=$8,updated_by=$9,request_id=$10 WHERE id=$1 AND created_at=$11 AND updated_at=$12`, r.ID, r.SupplierModelID, r.Coefficient, nativeResourceState(r.State), r.EverAdmitted, r.BuyerID, nullableResourceID(r.LatestReportID), r.UpdatedAt, meta.ActorID, meta.RequestID, r.CreatedAt, before.UpdatedAt)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, supply.ErrRevisionConflict
	}
	if err := s.appendContext(ctx, tx, before, r, meta, operation, write); err != nil {
		return nil, err
	}
	return s.Current(ctx, tx, r.ID)
}
func (s *ResourceStateStore) appendUnchanged(ctx context.Context, tx *sql.Tx, r *supply.Resource, meta supply.WriteMeta, action string, at time.Time, proof *supply.AdmissionProof) error {
	return s.appendUnchangedContext(ctx, tx, r, meta, action, at, resourceWriteContext{Proof: proof})
}
func (s *ResourceStateStore) appendUnchangedContext(ctx context.Context, tx *sql.Tx, r *supply.Resource, meta supply.WriteMeta, action string, at time.Time, write resourceWriteContext) error {
	if err := validateResourceControlMeta(meta, action); err != nil {
		return err
	}
	current, err := s.Current(ctx, tx, r.ID)
	if err != nil {
		return err
	}
	canonical, err := canonicalStoredResource(r)
	if err != nil {
		return err
	}
	if *current != *canonical {
		return supply.ErrReadContract
	}
	e := resourceControlFrom(canonical, action)
	e.Proof = controlProof(write.Proof)
	e.TestEvidence = write.TestEvidence
	e.CompletionNote = write.CompletionNote
	if write.Proof != nil && write.Proof.AssigneeID != meta.ActorID {
		return supply.ErrReadContract
	}
	if _, err := e.domain(); err != nil {
		return err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return supply.ErrReadContract
	}
	var previous int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(id),0) FROM `+s.table("operation_log")+` WHERE entity_type='resource' AND entity_id=$1`, r.ID).Scan(&previous); err != nil {
		return err
	}
	if err := s.log(ctx, tx, ResourceStateLog{EntityType: "resource", EntityID: r.ID, Action: action, BusinessAction: action, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, Before: raw, After: raw, OccurredAt: at.UTC().Truncate(time.Microsecond)}); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("operation_log")+` WHERE id>$1 AND entity_type='resource' AND entity_id=$2 AND action=$3 AND operator_id=$4 AND reason=$5 AND request_id=$6 AND "before"=$7::jsonb AND "after"=$7::jsonb`, previous, r.ID, action, meta.ActorID, meta.Reason, meta.RequestID, string(raw)).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return supply.ErrReadContract
	}
	return nil
}
func (s *ResourceStateStore) FactsLoader(report ResourceReportRefLoader, config ResourceConfigViewLoader) ResourceFactsLoader {
	return func(ctx context.Context, tx *sql.Tx, p ResourceProjection) (*ResourceReadFacts, error) {
		r, err := s.Current(ctx, tx, p.ID)
		if err != nil {
			return nil, err
		}
		projection := storedResourceProjection(r)
		if !resourceProjectionEqual(projection, p) {
			return nil, supply.ErrReadContract
		}
		f := &ResourceReadFacts{ResourceID: r.ID, Projection: projection, ProjectionUpdatedAt: r.UpdatedAt, Revision: r.Revision, OriginRowID: r.OriginRowID, OriginRowVersion: r.OriginRowVersion, TargetVersion: r.TargetVersion, LatestReportSequence: r.LatestReportSequence, FormalConfigID: r.FormalConfigID, FormalConfigRevision: r.FormalConfigRevision}
		if r.LatestReportID != "" {
			if report == nil {
				return nil, supply.ErrDependencyUnavailable
			}
			copy := *r
			f.Report, err = report(ctx, tx, &copy)
			if err != nil {
				return nil, err
			}
			if f.Report == nil {
				return nil, supply.ErrReadContract
			}
		}
		if r.FormalConfigID != "" {
			if config == nil {
				return nil, supply.ErrDependencyUnavailable
			}
			copy := *r
			f.Config, err = config(ctx, tx, &copy)
			if err != nil {
				return nil, err
			}
		}
		return f, nil
	}
}
