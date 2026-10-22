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
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// PlanHooks are transaction-local public/B providers, never network calls.
type PlanHooks struct {
	Resolve              ModelTxResolver
	PendingConfiguration func(context.Context, *sql.Tx, string) (bool, error)
	Log                  ResourceStateLogAppender
	Outbox               AdmissionOutboxWriter
}
type PlanStore struct {
	schema string
	read   ModelReadRunner
	state  *ResourceStateStore
	hooks  PlanHooks
}

var _ supply.PlanReadStore = (*PlanStore)(nil)
var _ supply.PlanListStore = (*PlanStore)(nil)
var _ supply.PlanMaintenanceUnitOfWork = (*PlanStore)(nil)

func NewPlanStore(schema string, read ModelReadRunner, state *ResourceStateStore, hooks *PlanHooks) (*PlanStore, error) {
	if !modelSchemaName.MatchString(schema) || (state != nil && state.schema != schema) {
		return nil, mappingError("schema")
	}
	s := &PlanStore{schema: schema, read: read, state: state}
	if hooks != nil {
		s.hooks = *hooks
	}
	return s, nil
}
func (s *PlanStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }

const planRevisionAction = "PLAN_REVISION"

type planEnvelope struct {
	Contract      string              `json:"contract"`
	SchemaVersion int                 `json:"schema_version"`
	Operation     string              `json:"operation"`
	Plan          supply.ResourcePlan `json:"plan"`
	Before        resourceControlData `json:"before_resource"`
	After         resourceControlData `json:"forecast_resource"`
	Execution     *planExecutionData  `json:"execution,omitempty"`
}

type planExecutionData struct {
	CompletionNote string    `json:"completion_note"`
	FailureReason  string    `json:"failure_reason,omitempty"`
	SourceEventID  string    `json:"source_event_id,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
}

func canonicalStatePlan(input *supply.StatePlan) (*supply.StatePlan, error) {
	if input == nil {
		return nil, supply.ErrReadContract
	}
	p := *input
	p.ResourcePlan = *input.ResourcePlan.Clone()
	for _, id := range []string{p.ID, p.ResourceID, p.CreatedBy} {
		if !modelIdentity(id) {
			return nil, supply.ErrReadContract
		}
	}
	if p.Revision == 0 || p.Revision > math.MaxInt64 || p.ExpectedResourceRevision == 0 || p.ExpectedResourceRevision > math.MaxInt64 || (p.Kind != "ONLINE" && p.Kind != "OFFLINE") || !slices.Contains([]string{"PENDING", "CANCELED", "BLOCKED", "EXECUTED"}, p.Status) || p.CreatedAt.IsZero() || p.DueAt.IsZero() || strings.TrimSpace(p.Reason) == "" || !utf8.ValidString(p.Reason) || strings.ContainsRune(p.Reason, 0) || utf8.RuneCountInString(p.Reason) > 500 || p.CreatorName != "" {
		return nil, supply.ErrReadContract
	}
	p.CreatedAt = p.CreatedAt.UTC().Truncate(time.Microsecond)
	p.DueAt = p.DueAt.UTC().Truncate(time.Microsecond)
	if (p.Status == "EXECUTED") != (p.ExecutedAt != nil) {
		return nil, supply.ErrReadContract
	}
	if p.ExecutedAt != nil {
		at := p.ExecutedAt.UTC().Truncate(time.Microsecond)
		if at.Before(p.CreatedAt) {
			return nil, supply.ErrReadContract
		}
		p.ExecutedAt = &at
	}
	b, err := canonicalStoredResource(&p.BeforeSnapshot)
	if err != nil {
		return nil, err
	}
	a, err := canonicalStoredResource(&p.AfterSnapshot)
	if err != nil {
		return nil, err
	}
	if b.ID != p.ResourceID || b.Region != p.Region || b.BuyerID != p.CreatedBy || b.Revision != p.ExpectedResourceRevision || !b.EverAdmitted || b.UpdatedAt.After(p.CreatedAt) {
		return nil, supply.ErrReadContract
	}
	forecast := *b
	if p.Kind == "OFFLINE" {
		if b.State != "ADMITTED" {
			return nil, supply.ErrReadContract
		}
		forecast.State = "OFFLINE"
	} else {
		if b.State != "OFFLINE" {
			return nil, supply.ErrReadContract
		}
		forecast.State = "ADMITTED"
	}
	if forecast != *a {
		return nil, supply.ErrReadContract
	}
	p.BeforeSnapshot = *b
	p.AfterSnapshot = *a
	return &p, nil
}
func encodeStatePlan(p *supply.StatePlan, operation string) ([]byte, error) {
	c, err := canonicalStatePlan(p)
	if err != nil {
		return nil, err
	}
	if !planOperationMatches(c, operation) {
		return nil, supply.ErrReadContract
	}
	return json.Marshal(planEnvelope{Contract: "supply.state_plan", SchemaVersion: 1, Operation: operation, Plan: c.ResourcePlan, Before: resourceControlFrom(&c.BeforeSnapshot, "PLAN_BASIS").Resource, After: resourceControlFrom(&c.AfterSnapshot, "PLAN_FORECAST").Resource})
}
func planOperationMatches(p *supply.StatePlan, op string) bool {
	return (op == "CREATE" && p.Revision == 1 && p.Status == "PENDING") || (p.Revision > 1 && ((op == "REVISE" && p.Status == "PENDING") || (op == "CANCEL" && p.Status == "CANCELED") || (op == "BLOCK" && p.Status == "BLOCKED") || (op == "EXECUTE" && p.Status == "EXECUTED")))
}
func decodeStatePlan(raw []byte) (*supply.StatePlan, string, error) {
	var e planEnvelope
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return nil, "", supply.ErrReadContract
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, "", supply.ErrReadContract
	}
	if e.Contract != "supply.state_plan" || e.SchemaVersion != 1 {
		return nil, "", supply.ErrReadContract
	}
	b, err := (resourceControlEnvelope{Contract: "supply.resource", SchemaVersion: 1, Operation: "PLAN_BASIS", Resource: e.Before}).domain()
	if err != nil {
		return nil, "", err
	}
	a, err := (resourceControlEnvelope{Contract: "supply.resource", SchemaVersion: 1, Operation: "PLAN_FORECAST", Resource: e.After}).domain()
	if err != nil {
		return nil, "", err
	}
	p, err := canonicalStatePlan(&supply.StatePlan{ResourcePlan: e.Plan, BeforeSnapshot: *b, AfterSnapshot: *a})
	if err != nil || !planOperationMatches(p, e.Operation) {
		return nil, "", supply.ErrReadContract
	}
	if p.Status == "EXECUTED" || p.Status == "BLOCKED" {
		x := e.Execution
		if x == nil || strings.TrimSpace(x.CompletionNote) == "" || !utf8.ValidString(x.CompletionNote) || strings.ContainsRune(x.CompletionNote, 0) || x.OccurredAt.Before(p.CreatedAt) {
			return nil, "", supply.ErrReadContract
		}
		if p.Status == "EXECUTED" && (!modelIdentity(x.SourceEventID) || x.FailureReason != "" || !x.OccurredAt.Equal(*p.ExecutedAt)) {
			return nil, "", supply.ErrReadContract
		}
		if p.Status == "BLOCKED" && (x.SourceEventID != "" || !slices.Contains([]string{"RESOURCE_CHANGED", "RESOURCE_NOT_READY"}, x.FailureReason)) {
			return nil, "", supply.ErrReadContract
		}
	} else if e.Execution != nil {
		return nil, "", supply.ErrReadContract
	}
	return p, e.Operation, nil
}
func nativePlanStatus(status string) string {
	if status == "EXECUTED" {
		return "DONE"
	}
	return status
}
func samePlanBasis(a, b *supply.StatePlan) bool {
	return a.ID == b.ID && a.ResourceID == b.ResourceID && a.CreatedBy == b.CreatedBy && a.Kind == b.Kind && a.Region == b.Region && a.CreatedAt.Equal(b.CreatedAt) && a.ExpectedResourceRevision == b.ExpectedResourceRevision && a.BeforeSnapshot == b.BeforeSnapshot && a.AfterSnapshot == b.AfterSnapshot
}
func (s *PlanStore) current(ctx context.Context, tx *sql.Tx, id string) (*supply.StatePlan, error) {
	var resource, kind, status string
	var reason, actor sql.NullString
	var due sql.NullTime
	var created time.Time
	var before, after []byte
	err := tx.QueryRowContext(ctx, `SELECT resource_id::text,kind,status,reason,created_by::text,due_at,created_at,"before","after" FROM `+s.table("plan")+` WHERE id=$1`, id).Scan(&resource, &kind, &status, &reason, &actor, &due, &created, &before, &after)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p, _, err := decodeStatePlan(after)
	if err != nil {
		return nil, err
	}
	original, op, err := decodeStatePlan(before)
	if err != nil || op != "CREATE" || !samePlanBasis(p, original) || p.ID != id || p.ResourceID != resource || p.Kind != kind || nativePlanStatus(p.Status) != status || !reason.Valid || p.Reason != reason.String || !actor.Valid || p.CreatedBy != actor.String || !due.Valid || !p.DueAt.Equal(due.Time) || !p.CreatedAt.Equal(created) {
		return nil, supply.ErrReadContract
	}
	var logged []byte
	var by, why, request sql.NullString
	var at time.Time
	err = tx.QueryRowContext(ctx, `SELECT "after",operator_id::text,reason,request_id,created_at FROM `+s.table("operation_log")+` WHERE entity_type='plan' AND entity_id=$1 AND action=$2 ORDER BY id DESC LIMIT 1`, id, planRevisionAction).Scan(&logged, &by, &why, &request, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrReadContract
	}
	if err != nil {
		return nil, err
	}
	logPlan, logOp, err := decodeStatePlan(logged)
	if err != nil {
		return nil, err
	}
	_, rowOp, _ := decodeStatePlan(after)
	if !reflect.DeepEqual(p, logPlan) || logOp != rowOp || by.String != p.CreatedBy || !why.Valid || strings.TrimSpace(why.String) == "" || !request.Valid || strings.TrimSpace(request.String) == "" || at.Before(p.CreatedAt) {
		return nil, supply.ErrReadContract
	}
	var rowEnvelope, logEnvelope planEnvelope
	if json.Unmarshal(after, &rowEnvelope) != nil || json.Unmarshal(logged, &logEnvelope) != nil || !reflect.DeepEqual(rowEnvelope.Execution, logEnvelope.Execution) {
		return nil, supply.ErrReadContract
	}
	return p, nil
}
func (s *PlanStore) withinRead(ctx context.Context, run func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.read == nil || s.state == nil {
		return supply.ErrDependencyUnavailable
	}
	return s.read(ctx, func(tx *sql.Tx) error {
		if tx == nil {
			return supply.ErrDependencyUnavailable
		}
		return run(tx)
	})
}
func (s *PlanStore) readRecord(ctx context.Context, tx *sql.Tx, id string) (supply.PlanReadRecord, error) {
	p, err := s.current(ctx, tx, id)
	if err != nil {
		return supply.PlanReadRecord{}, err
	}
	r, err := s.state.Current(ctx, tx, p.ResourceID)
	if err != nil {
		return supply.PlanReadRecord{}, err
	}
	var failure string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE("after"->'execution'->>'failure_reason','') FROM `+s.table("plan")+` WHERE id=$1`, id).Scan(&failure); err != nil {
		return supply.PlanReadRecord{}, err
	}
	return supply.PlanReadRecord{Plan: p.ResourcePlan.Clone(), ResourceID: r.ID, ResourceRegion: r.Region, ResourceBuyerID: r.BuyerID, ResourceRevision: r.Revision, ExecutionFailureReason: failure}, nil
}
func (s *PlanStore) GetPlanSummary(ctx context.Context, id string) (supply.PlanReadRecord, error) {
	var record supply.PlanReadRecord
	if !modelIdentity(id) {
		return record, mappingError("id")
	}
	err := s.withinRead(ctx, func(tx *sql.Tx) error { var err error; record, err = s.readRecord(ctx, tx, id); return err })
	return record, err
}
func (s *PlanStore) ListPlanSummaries(ctx context.Context, q supply.PlanListQuery) ([]supply.PlanReadRecord, int64, error) {
	if !modelPageValid(q.Page, q.Size) || q.SortBy != "created_at" || q.Order != "desc" || q.TieBreakBy != "id" || (q.ResourceID != "" && !modelIdentity(q.ResourceID)) || (q.Region != "" && q.Region != "CN" && q.Region != "OVERSEAS") || (q.Kind != "" && !slices.Contains([]string{"ONLINE", "OFFLINE", "PRICE_CHANGE"}, q.Kind)) || (q.Status != "" && !slices.Contains([]string{"PENDING", "EXECUTING", "EXECUTED", "BLOCKED", "CANCELED"}, q.Status)) {
		return nil, 0, supply.ErrReadContract
	}
	result := []supply.PlanReadRecord{}
	var total int64
	err := s.withinRead(ctx, func(tx *sql.Tx) error {
		// PRICE_CHANGE is owned by B; this repository only projects A state plans.
		filter := ` FROM ` + s.table("plan") + ` p JOIN ` + s.table("resource") + ` r ON r.id=p.resource_id WHERE p.kind IN ('ONLINE','OFFLINE') AND ($1='' OR p.resource_id=NULLIF($1,'')::bigint) AND ($2='' OR r.region=$2) AND ($3='' OR p.kind=$3) AND ($4='' OR p.status=$4)`
		args := []any{q.ResourceID, q.Region, q.Kind, nativePlanStatus(q.Status)}
		if err := tx.QueryRowContext(ctx, `SELECT count(*)`+filter, args...).Scan(&total); err != nil {
			return err
		}
		args = append(args, q.Size, (q.Page-1)*q.Size)
		rows, err := tx.QueryContext(ctx, `SELECT p.id::text`+filter+` ORDER BY p.created_at DESC,p.id DESC LIMIT $5 OFFSET $6`, args...)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			r, err := s.readRecord(ctx, tx, id)
			if err != nil {
				return err
			}
			result = append(result, r)
		}
		return nil
	})
	return result, total, err
}
func (s *PlanStore) WithinPlanMaintenance(ctx context.Context, run func(supply.PlanMaintenanceTransaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.state == nil || s.hooks.Resolve == nil || s.hooks.Log == nil || s.hooks.Outbox == nil || s.hooks.PendingConfiguration == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	tx, err := s.hooks.Resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	t := &planMaintenanceTransaction{store: s, tx: tx}
	if err := run(t); err != nil {
		return err
	}
	if !t.completed {
		return supply.ErrReadContract
	}
	return nil
}

type planMaintenanceTransaction struct {
	store          *PlanStore
	tx             *sql.Tx
	resource       *supply.Resource
	before, staged *supply.StatePlan
	completed      bool
}

func (t *planMaintenanceTransaction) ResourceForPlanCreation(ctx context.Context, id string) (*supply.Resource, error) {
	if t.resource != nil || t.staged != nil || !modelIdentity(id) {
		return nil, supply.ErrReadContract
	}
	r, err := t.store.state.ForUpdate(ctx, t.tx, id)
	if err != nil {
		return nil, err
	}
	t.resource = r
	copy := *r
	return &copy, nil
}
func (t *planMaintenanceTransaction) HasPendingStatePlan(ctx context.Context, id string) (bool, error) {
	if t.resource == nil || t.resource.ID != id {
		return false, supply.ErrReadContract
	}
	var exists bool
	err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+t.store.table("plan")+` WHERE resource_id=$1 AND kind IN ('ONLINE','OFFLINE') AND status='PENDING')`, id).Scan(&exists)
	return exists, err
}
func (t *planMaintenanceTransaction) HasPendingConfigurationChange(ctx context.Context, id string) (bool, error) {
	if t.resource == nil || t.resource.ID != id {
		return false, supply.ErrReadContract
	}
	return t.store.hooks.PendingConfiguration(ctx, t.tx, id)
}
func (t *planMaintenanceTransaction) NextPlanID(ctx context.Context) (string, error) {
	if t.resource == nil || t.staged != nil {
		return "", supply.ErrReadContract
	}
	var id string
	err := t.tx.QueryRowContext(ctx, `SELECT nextval(pg_get_serial_sequence($1,'id'))::text`, t.store.table("plan")).Scan(&id)
	return id, err
}
func (t *planMaintenanceTransaction) CreateStatePlan(ctx context.Context, p *supply.StatePlan) error {
	c, err := canonicalStatePlan(p)
	if err != nil {
		return err
	}
	if t.before != nil || t.staged != nil || t.resource == nil || !reflect.DeepEqual(&c.BeforeSnapshot, t.resource) || c.Revision != 1 || c.Status != "PENDING" {
		return supply.ErrReadContract
	}
	pending, err := t.HasPendingStatePlan(ctx, c.ResourceID)
	if err != nil {
		return err
	}
	if pending {
		return supply.ErrRevisionConflict
	}
	t.staged = c
	return nil
}
func (t *planMaintenanceTransaction) LockPlanMaintenance(ctx context.Context, id string) (*supply.StatePlan, error) {
	if t.resource != nil || t.before != nil || t.staged != nil || !modelIdentity(id) {
		return nil, supply.ErrReadContract
	}
	var resourceID string
	err := t.tx.QueryRowContext(ctx, `SELECT resource_id::text FROM `+t.store.table("plan")+` WHERE id=$1`, id).Scan(&resourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := t.ResourceForPlanCreation(ctx, resourceID); err != nil {
		return nil, err
	}
	var locked string
	err = t.tx.QueryRowContext(ctx, `SELECT resource_id::text FROM `+t.store.table("plan")+` WHERE id=$1 FOR UPDATE`, id).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if locked != resourceID {
		return nil, supply.ErrReadContract
	}
	p, err := t.store.current(ctx, t.tx, id)
	if err != nil {
		return nil, err
	}
	t.before = p
	return cloneStatePlan(p), nil
}
func cloneStatePlan(p *supply.StatePlan) *supply.StatePlan {
	if p == nil {
		return nil
	}
	c := *p
	c.ResourcePlan = *p.ResourcePlan.Clone()
	return &c
}
func (t *planMaintenanceTransaction) UpdateStatePlan(ctx context.Context, p *supply.StatePlan, expected uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := canonicalStatePlan(p)
	if err != nil {
		return err
	}
	if t.before == nil || t.staged != nil || t.before.Revision != expected || c.Revision != expected+1 || !samePlanBasis(c, t.before) {
		return supply.ErrReadContract
	}
	t.staged = c
	return nil
}
func (t *planMaintenanceTransaction) AppendPlanMaintenanceEffect(ctx context.Context, e supply.PlanMaintenanceEffect) error {
	if t.completed || t.staged == nil || !reflect.DeepEqual(t.before, e.Before) {
		return supply.ErrReadContract
	}
	c, err := canonicalStatePlan(e.After)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(c, t.staged) || e.ActorID != c.CreatedBy || (e.Action != "CREATE" && e.Action != "REVISE" && e.Action != "CANCEL") || e.ScheduleReminder != (e.Action != "CANCEL") || e.OccurredAt.Before(c.CreatedAt) {
		return supply.ErrReadContract
	}
	// Recompute the domain transition so a direct adapter caller cannot bypass it.
	var expected *supply.StatePlan
	switch e.Action {
	case "CREATE":
		if t.before != nil {
			return supply.ErrReadContract
		}
		expected, err = supply.NewStatePlan(c.ID, e.ActorID, t.resource, c.Kind, c.DueAt, e.Reason, c.CreatedAt)
	case "REVISE":
		if t.before == nil {
			return supply.ErrReadContract
		}
		expected, err = t.before.RevisedStatePlan(e.ActorID, t.before.Revision, c.DueAt, e.Reason)
	case "CANCEL":
		if t.before == nil {
			return supply.ErrReadContract
		}
		expected, err = t.before.CanceledStatePlan(e.ActorID, t.before.Revision)
	}
	if err != nil {
		return err
	}
	expected, err = canonicalStatePlan(expected)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(c, expected) {
		return supply.ErrReadContract
	}
	meta := supply.WriteMeta{ActorID: e.ActorID, Reason: e.Reason, RequestID: e.RequestID}
	if err := validateResourceControlMeta(meta, e.Action); err != nil {
		return err
	}
	after, err := encodeStatePlan(c, e.Action)
	if err != nil {
		return err
	}
	var before []byte
	if t.before != nil {
		// The previous revision operation is preserved in the row snapshot.
		if err := t.tx.QueryRowContext(ctx, `SELECT "after" FROM `+t.store.table("plan")+` WHERE id=$1`, c.ID).Scan(&before); err != nil {
			return err
		}
		var count int64
		result, err := t.tx.ExecContext(ctx, `UPDATE `+t.store.table("plan")+` SET reason=$2,due_at=$3,"after"=$4::jsonb,status=$5 WHERE id=$1 AND resource_id=$6 AND "after"=$7::jsonb`, c.ID, c.Reason, c.DueAt, string(after), nativePlanStatus(c.Status), c.ResourceID, string(before))
		if err != nil {
			return err
		}
		count, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return supply.ErrRevisionConflict
		}
	} else {
		pending, err := t.HasPendingStatePlan(ctx, c.ResourceID)
		if err != nil {
			return err
		}
		if pending {
			return supply.ErrRevisionConflict
		}
		if _, err := t.tx.ExecContext(ctx, `INSERT INTO `+t.store.table("plan")+` (id,resource_id,kind,reason,due_at,"before","after",status,created_by,created_at) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6::jsonb,$6::jsonb,$7,$8,$9)`, c.ID, c.ResourceID, c.Kind, c.Reason, c.DueAt, string(after), nativePlanStatus(c.Status), c.CreatedBy, c.CreatedAt); err != nil {
			return err
		}
	}
	at := e.OccurredAt.UTC().Truncate(time.Microsecond)
	if err := t.store.hooks.Log(ctx, t.tx, ResourceStateLog{EntityType: "plan", EntityID: c.ID, Action: planRevisionAction, BusinessAction: e.Action, ActorID: e.ActorID, Reason: e.Reason, RequestID: e.RequestID, Before: bytes.Clone(before), After: bytes.Clone(after), OccurredAt: at}); err != nil {
		return err
	}
	stored, err := t.store.current(ctx, t.tx, c.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(c, stored) {
		return supply.ErrReadContract
	}
	var acknowledged int
	if err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM `+t.store.table("operation_log")+` WHERE entity_type='plan' AND entity_id=$1 AND action=$2 AND "after"=$3::jsonb AND operator_id=$4 AND reason=$5 AND request_id=$6 AND created_at=$7 AND "before" IS NOT DISTINCT FROM $8::jsonb`, c.ID, planRevisionAction, string(after), e.ActorID, e.Reason, e.RequestID, at, nullablePlanJSON(before)).Scan(&acknowledged); err != nil {
		return err
	}
	if acknowledged != 1 {
		return supply.ErrReadContract
	}
	payload, _ := json.Marshal(struct {
		Contract   string    `json:"contract"`
		Version    int       `json:"schema_version"`
		PlanID     string    `json:"plan_id"`
		Revision   uint64    `json:"revision,string"`
		ResourceID string    `json:"resource_id"`
		Region     string    `json:"region"`
		Status     string    `json:"status"`
		DueAt      time.Time `json:"due_at"`
		Schedule   bool      `json:"schedule"`
	}{"supply.plan_reminder", 1, c.ID, c.Revision, c.ResourceID, c.Region, c.Status, c.DueAt, e.ScheduleReminder})
	eventType := "PLAN_REMINDER_SCHEDULED"
	if !e.ScheduleReminder {
		eventType = "PLAN_REMINDER_INVALIDATED"
	}
	id, err := t.store.hooks.Outbox(ctx, t.tx, AdmissionOutboxEntry{EventType: eventType, ActorID: e.ActorID, RequestID: e.RequestID, Payload: bytes.Clone(payload), OccurredAt: at})
	if err != nil {
		return err
	}
	if !modelIdentity(id) {
		return supply.ErrReadContract
	}
	if err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM `+t.store.table("event_outbox")+` WHERE id=$1 AND event_type=$2 AND payload=$3::jsonb AND status='PENDING' AND created_by=$4 AND request_id=$5 AND created_at=$6`, id, eventType, string(payload), e.ActorID, e.RequestID, at).Scan(&acknowledged); err != nil {
		return err
	}
	if acknowledged != 1 {
		return supply.ErrReadContract
	}
	t.completed = true
	return nil
}
func nullablePlanJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
