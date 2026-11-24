package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"model_bss/internal/domain/auth"
)

var errCatalogForbidden = errors.New("CATALOG_FORBIDDEN")
var errCatalogStale = errors.New("STALE_CATALOG_REVIEW")

type CatalogBusinessStore struct{ db *sql.DB }

func NewCatalogBusinessStore(db *sql.DB) *CatalogBusinessStore { return &CatalogBusinessStore{db: db} }

type catalogCommand struct {
	SchemaVersion string          `json:"schemaVersion"`
	Attempt       int64           `json:"attempt,omitempty"`
	Operation     string          `json:"operation"`
	Vendor        string          `json:"vendor"`
	RunID         string          `json:"runId"`
	Key           string          `json:"key"`
	Trigger       string          `json:"trigger"`
	StaffID       string          `json:"staffId"`
	ModelID       string          `json:"modelId"`
	ModelIDs      []string        `json:"modelIds"`
	Result        json.RawMessage `json:"result"`
	Overrides     json.RawMessage `json:"overrides"`
	Published     json.RawMessage `json:"published"`
	Evidence      json.RawMessage `json:"evidence"`
	Plan          json.RawMessage `json:"plan"`
	Error         string          `json:"error"`
}
type catalogActor struct {
	staff, account int64
	edit, manage   bool
}
type catalogRun struct {
	ID       string          `json:"id"`
	Trigger  string          `json:"trigger"`
	Status   string          `json:"status"`
	Started  time.Time       `json:"startedAt"`
	Finished *time.Time      `json:"finishedAt,omitempty"`
	Error    *string         `json:"error,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Scope    string          `json:"-"`
	Owner    int64           `json:"-"`
}

func catalogVisible(scope string, owner, actor int64) bool {
	return scope == "platform" || ((scope == "manual" || scope == "personal") && owner == actor)
}
func catalogOrigin(key string) (string, int64, error) {
	p := strings.SplitN(key, ":", 3)
	if len(p) >= 2 && p[0] == "daily" {
		return "platform", 0, nil
	}
	if len(p) != 3 || (p[0] != "manual" && p[0] != "personal") {
		return "", 0, errCatalogForbidden
	}
	id, err := strconv.ParseInt(p[1], 10, 64)
	if err != nil || id <= 0 {
		return "", 0, errCatalogForbidden
	}
	return p[0], id, nil
}
func hasCatalogPerm(p []string, value string) bool {
	for _, v := range p {
		if v == value {
			return true
		}
	}
	return false
}

// This worker API accepts named domain operations only; it never accepts SQL or a database DSN.
func registerCatalogBusiness(r *gin.Engine, store *CatalogBusinessStore, sessions auth.SessionReader, secret string) {
	if store == nil || sessions == nil || len(secret) < 32 {
		return
	}
	r.POST("/api/agent/catalog/business", func(c *gin.Context) {
		got := sha256.Sum256([]byte(c.GetHeader("Authorization")))
		want := sha256.Sum256([]byte("Bearer " + secret))
		if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			c.Status(401)
			return
		}
		c.Header("Cache-Control", "no-store")
		var q catalogCommand
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<20)
		dec := json.NewDecoder(c.Request.Body)
		dec.DisallowUnknownFields()
		if dec.Decode(&q) != nil || q.SchemaVersion != "catalog-business.v1" {
			c.JSON(400, gin.H{"error": "CATALOG_REQUEST_INVALID"})
			return
		}
		actor := catalogActor{}
		if token := c.GetHeader("X-BSS-Employee-Token"); token != "" {
			h := sha256.Sum256([]byte(token))
			s, err := sessions.FindSessionByTokenHash(c.Request.Context(), hex.EncodeToString(h[:]))
			if err != nil || s == nil || s.Revoked || time.Now().After(s.ExpiresAt) || s.Snapshot == nil || s.Snapshot.PortalType != "INTERNAL" || s.Snapshot.OperatorType != "STAFF" || s.Snapshot.StaffID <= 0 {
				c.Status(403)
				return
			}
			snap := s.Snapshot
			if !hasCatalogPerm(snap.Perms, "M1:V") && !strings.HasPrefix(q.Operation, "plan") {
				c.Status(403)
				return
			}
			actor.staff = snap.StaffID
			actor.account = snap.AccountID
			actor.edit = hasCatalogPerm(snap.Perms, "M1:E")
			// Agent operation grants remain authoritative in DSH; only the authenticated gateway can attest them.
			actor.manage = c.GetHeader("X-Agent-Schedule-Manage") == "1"
			if q.StaffID != "" && q.StaffID != strconv.FormatInt(actor.staff, 10) {
				c.Status(403)
				return
			}
		} else if q.StaffID != "" {
			c.Status(403)
			return
		}
		out, err := store.execute(c.Request.Context(), q, actor)
		if err != nil {
			status := 503
			code := "CATALOG_BUSINESS_UNAVAILABLE"
			if errors.Is(err, errCatalogForbidden) {
				status = 403
				code = err.Error()
			}
			if errors.Is(err, errCatalogStale) {
				status = 409
				code = err.Error()
			}
			c.JSON(status, gin.H{"error": code})
			return
		}
		c.JSON(200, out)
	})
}

const catalogRunColumns = "id,trigger,status,started_at,finished_at,error,result,scope,owner_staff_id"

func scanCatalogRun(row interface{ Scan(...any) error }) (*catalogRun, error) {
	var x catalogRun
	var result []byte
	err := row.Scan(&x.ID, &x.Trigger, &x.Status, &x.Started, &x.Finished, &x.Error, &result, &x.Scope, &x.Owner)
	x.Result = result
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &x, err
}
func (s *CatalogBusinessStore) run(ctx context.Context, id string, vendor string) (*catalogRun, error) {
	return scanCatalogRun(s.db.QueryRowContext(ctx, "SELECT "+catalogRunColumns+" FROM model_catalog.runs WHERE id=$1 AND vendor=$2", id, vendor))
}
func (s *CatalogBusinessStore) allowed(ctx context.Context, id string, a catalogActor, edit bool, vendor string) (*catalogRun, error) {
	r, e := s.run(ctx, id, vendor)
	if e != nil {
		return nil, e
	}
	if r == nil {
		return nil, errCatalogStale
	}
	if a.staff == 0 || !catalogVisible(r.Scope, r.Owner, a.staff) || (edit && !a.edit) {
		return nil, errCatalogForbidden
	}
	if edit {
		current, err := s.current(ctx, r, vendor)
		if err != nil {
			return nil, err
		}
		if !current {
			return nil, errCatalogStale
		}
	}
	return r, nil
}
func (s *CatalogBusinessStore) current(ctx context.Context, r *catalogRun, vendor string) (bool, error) {
	var current bool
	err := s.db.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM model_catalog.runs WHERE vendor=$1 AND scope=$2 AND owner_staff_id=$3 AND status='SUCCEEDED' AND (started_at,id)>($4::timestamptz,$5::uuid))`, vendor, r.Scope, r.Owner, r.Started, r.ID).Scan(&current)
	return current, err
}
func catalogModels(raw json.RawMessage) []map[string]any {
	var r struct {
		Models []map[string]any `json:"models"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.Models
}
func (s *CatalogBusinessStore) execute(ctx context.Context, q catalogCommand, a catalogActor) (any, error) {
	if q.Operation == "plans-list" || q.Operation == "plan-save" || q.Operation == "plan-pause" || q.Operation == "plans-bulk" || q.Operation == "plans-claim" || q.Operation == "plans-settle" {
		return s.plans(ctx, q, a)
	}
	if q.Operation == "evidence-record" || q.Operation == "evidence-read" {
		return s.evidence(ctx, q)
	}
	if !hasCatalogPerm([]string{"deepseek", "openai", "google", "aliyun", "zhipu", "anthropic"}, q.Vendor) {
		return nil, errCatalogForbidden
	}
	switch q.Operation {
	case "claim":
		return s.claim(ctx, q, a)
	case "complete", "fail":
		if a.staff != 0 {
			return nil, errCatalogForbidden
		}
		status := "SUCCEEDED"
		if q.Operation == "fail" {
			status = "FAILED"
		}
		result := q.Result
		if len(result) == 0 {
			result = json.RawMessage("null")
		}
		r, e := s.db.ExecContext(ctx, `UPDATE model_catalog.runs SET status=$3,result=$4,error=nullif($5,''),finished_at=clock_timestamp(),revision=revision+1 WHERE id=$1 AND vendor=$2 AND status='RUNNING'`, q.RunID, q.Vendor, status, string(result), q.Error)
		if e != nil {
			return nil, e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return nil, errCatalogStale
		}
		return gin.H{"ok": true}, nil
	case "status":
		return s.status(ctx, q, a)
	case "request":
		if a.staff == 0 {
			return nil, errCatalogForbidden
		}
		var id string
		e := s.db.QueryRowContext(ctx, "SELECT run_id FROM model_catalog.requests WHERE vendor=$1 AND request_key=$2", q.Vendor, "manual:"+strconv.FormatInt(a.staff, 10)+":"+q.Key).Scan(&id)
		if errors.Is(e, sql.ErrNoRows) {
			return gin.H{"id": nil}, nil
		}
		return gin.H{"id": id}, e
	case "baseline":
		return s.baseline(ctx, q, a)
	case "draft-list", "draft-save", "draft-remove":
		return s.drafts(ctx, q, a)
	case "approve", "reject":
		return s.decide(ctx, q, a)
	case "receipt-save":
		if _, e := s.allowed(ctx, q.RunID, a, true, q.Vendor); e != nil {
			return nil, e
		}
		_, e := s.db.ExecContext(ctx, `INSERT INTO model_catalog.write_receipts(run_id,model_id,request_key,staff_id,result) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, q.RunID, q.ModelID, q.Key, a.staff, string(q.Result))
		return gin.H{"ok": e == nil}, e
	}
	return nil, errCatalogForbidden
}
func (s *CatalogBusinessStore) claim(ctx context.Context, q catalogCommand, a catalogActor) (any, error) {
	scope, owner, err := catalogOrigin(q.Key)
	if err != nil {
		return nil, err
	}
	if scope != "platform" && (a.staff != owner || !a.edit) {
		return nil, errCatalogForbidden
	}
	if scope == "platform" && a.staff != 0 {
		return nil, errCatalogForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(731209,1)"); err != nil {
		return nil, err
	}
	var id string
	err = tx.QueryRowContext(ctx, "SELECT run_id FROM model_catalog.requests WHERE vendor=$1 AND request_key=$2", q.Vendor, q.Key).Scan(&id)
	if err == nil {
		return gin.H{"id": id, "start": false}, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE model_catalog.runs SET status='FAILED',error='RUN_INTERRUPTED',finished_at=now() WHERE status='RUNNING' AND started_at<now()-interval '5 minutes'`)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM model_catalog.runs WHERE vendor=$1 AND scope=$2 AND owner_staff_id=$3 AND status='RUNNING'`, q.Vendor, scope, owner).Scan(&id)
	start := false
	if errors.Is(err, sql.ErrNoRows) {
		id = uuid.NewString()
		start = true
		_, err = tx.ExecContext(ctx, `INSERT INTO model_catalog.runs(id,vendor,trigger,scope,owner_staff_id,origin_key,status,baseline_id) SELECT $1,$2,$3,$4,$5,$6,'RUNNING',(SELECT id FROM model_catalog.runs WHERE vendor=$2 AND status='SUCCEEDED' AND finished_at<=now() AND (scope='platform' OR owner_staff_id=$5) ORDER BY (owner_staff_id=$5 AND scope<>'platform') DESC,started_at DESC,id DESC LIMIT 1)`, id, q.Vendor, q.Trigger, scope, owner, q.Key)
	}
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO model_catalog.requests(vendor,request_key,run_id) VALUES($1,$2,$3)", q.Vendor, q.Key, id)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO model_catalog.events(kind,staff_id,run_id,detail) VALUES('CRAWL_STARTED',$1,$2,jsonb_build_object('vendor',$3::text,'scope',$4::text))", owner, id, q.Vendor, scope)
	if err != nil {
		return nil, err
	}
	return gin.H{"id": id, "start": start}, tx.Commit()
}
func (s *CatalogBusinessStore) status(ctx context.Context, q catalogCommand, a catalogActor) (any, error) {
	var r *catalogRun
	var err error
	if q.RunID != "" {
		r, err = s.run(ctx, q.RunID, q.Vendor)
	} else {
		r, err = scanCatalogRun(s.db.QueryRowContext(ctx, "SELECT "+catalogRunColumns+` FROM model_catalog.runs WHERE vendor=$1 AND ($2::bigint=0 OR scope='platform' OR owner_staff_id=$2) ORDER BY started_at DESC,id DESC LIMIT 1`, q.Vendor, a.staff))
	}
	if err != nil {
		return nil, err
	}
	empty := gin.H{"latestAttempt": nil, "lastSuccess": nil, "platformAttempt": nil, "pendingModelIds": []string{}, "publishedModels": []any{}, "canReview": false, "reviewCounts": gin.H{}}
	if r == nil {
		return empty, nil
	}
	if a.staff != 0 && !catalogVisible(r.Scope, r.Owner, a.staff) {
		return nil, errCatalogForbidden
	}
	if r.Status == "RUNNING" && time.Since(r.Started) > 5*time.Minute {
		_, err = s.db.ExecContext(ctx, "UPDATE model_catalog.runs SET status='FAILED',error='RUN_INTERRUPTED',finished_at=now() WHERE id=$1 AND status='RUNNING'", r.ID)
		if err != nil {
			return nil, err
		}
		r, err = s.run(ctx, r.ID, q.Vendor)
		if err != nil {
			return nil, err
		}
	}
	pending := []string{}
	counts := map[string]int{}
	decided := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, "SELECT model_id,decision FROM model_catalog.decisions WHERE run_id=$1", r.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, d string
		if err = rows.Scan(&id, &d); err != nil {
			rows.Close()
			return nil, err
		}
		decided[id] = true
		counts[d]++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, m := range catalogModels(r.Result) {
		id, _ := m["id"].(string)
		if id != "" && m["status"] != "NOT_OBSERVED" && !decided[id] {
			pending = append(pending, id)
		}
	}
	published := []any{}
	pr, err := s.db.QueryContext(ctx, `SELECT DISTINCT ON(d.model_id) d.published_model FROM model_catalog.decisions d JOIN model_catalog.runs r ON r.id=d.run_id WHERE r.vendor=$1 AND d.decision='APPROVED' AND ($2::bigint=0 OR r.scope='platform' OR r.owner_staff_id=$2) ORDER BY d.model_id,d.approved_at DESC`, q.Vendor, a.staff)
	if err != nil {
		return nil, err
	}
	for pr.Next() {
		var v json.RawMessage
		if err = pr.Scan(&v); err != nil {
			pr.Close()
			return nil, err
		}
		published = append(published, v)
	}
	err = pr.Err()
	pr.Close()
	if err != nil {
		return nil, err
	}
	out := gin.H{"latestAttempt": r, "lastSuccess": nil, "platformAttempt": nil, "pendingModelIds": pending, "publishedModels": published, "canReview": a.staff != 0 && a.edit && r.Status == "SUCCEEDED", "reviewCounts": counts}
	if a.staff != 0 && r.Status == "SUCCEEDED" {
		current, e := s.current(ctx, r, q.Vendor)
		if e != nil {
			return nil, e
		}
		out["canReview"] = a.edit && current
	}
	if r.Status == "SUCCEEDED" {
		out["lastSuccess"] = r
	}
	if a.staff == 0 {
		last, e := scanCatalogRun(s.db.QueryRowContext(ctx, "SELECT "+catalogRunColumns+" FROM model_catalog.runs WHERE vendor=$1 AND status='SUCCEEDED' ORDER BY started_at DESC,id DESC LIMIT 1", q.Vendor))
		if e != nil {
			return nil, e
		}
		out["lastSuccess"] = last
	}
	return out, nil
}
func (s *CatalogBusinessStore) baseline(ctx context.Context, q catalogCommand, a catalogActor) (any, error) {
	r, err := s.run(ctx, q.RunID, q.Vendor)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	if a.staff != 0 && !catalogVisible(r.Scope, r.Owner, a.staff) {
		return nil, errCatalogForbidden
	}
	if a.staff != 0 {
		_, err = s.db.ExecContext(ctx, `INSERT INTO model_catalog.comparison_baselines(run_id,staff_id,baseline_id)
 SELECT r.id,$2,(SELECT b.id FROM model_catalog.runs b WHERE b.vendor=r.vendor AND b.status='SUCCEEDED'
 AND b.finished_at<=r.started_at AND b.id<>r.id AND (b.scope='platform' OR b.owner_staff_id=$2)
 ORDER BY (b.owner_staff_id=$2 AND b.scope<>'platform') DESC,b.started_at DESC,b.id DESC LIMIT 1)
 FROM model_catalog.runs r WHERE r.id=$1 ON CONFLICT DO NOTHING`, q.RunID, a.staff)
		if err != nil {
			return nil, err
		}
	}
	var id, scope string
	var owner int64
	var at time.Time
	var raw json.RawMessage
	// The baseline is fixed on claim. Never substitute another employee's personal run.
	err = s.db.QueryRowContext(ctx, `SELECT b.id,b.scope,b.owner_staff_id,b.started_at,b.result FROM model_catalog.runs r LEFT JOIN model_catalog.comparison_baselines fixed ON fixed.run_id=r.id AND fixed.staff_id=$2 JOIN model_catalog.runs b ON b.id=CASE WHEN $2::bigint=0 THEN r.baseline_id ELSE fixed.baseline_id END WHERE r.id=$1 AND ($2::bigint=0 OR b.scope='platform' OR b.owner_staff_id=$2)`, q.RunID, a.staff).Scan(&id, &scope, &owner, &at, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	kind := "GLOBAL"
	if owner == a.staff && scope != "platform" {
		kind = "PERSONAL"
	}
	return gin.H{"id": id, "kind": kind, "startedAt": at, "result": raw}, nil
}
func (s *CatalogBusinessStore) drafts(ctx context.Context, q catalogCommand, a catalogActor) (any, error) {
	if _, err := s.allowed(ctx, q.RunID, a, q.Operation != "draft-list", q.Vendor); err != nil {
		return nil, err
	}
	if q.Operation == "draft-save" {
		_, err := s.db.ExecContext(ctx, `INSERT INTO model_catalog.review_drafts(run_id,model_id,staff_id,overrides) VALUES($1,$2,$3,$4) ON CONFLICT(run_id,model_id,staff_id) DO UPDATE SET overrides=excluded.overrides,updated_at=clock_timestamp()`, q.RunID, q.ModelID, a.staff, string(q.Overrides))
		if err != nil {
			return nil, err
		}
	}
	if q.Operation == "draft-remove" {
		_, err := s.db.ExecContext(ctx, "DELETE FROM model_catalog.review_drafts WHERE run_id=$1 AND model_id=$2 AND staff_id=$3", q.RunID, q.ModelID, a.staff)
		return gin.H{"ok": err == nil}, err
	}
	var raw json.RawMessage
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(d)),'[]') FROM model_catalog.review_drafts d WHERE run_id=$1 AND staff_id=$2`, q.RunID, a.staff).Scan(&raw)
	if err != nil {
		return nil, err
	}
	if q.Operation == "draft-save" {
		var list []map[string]any
		_ = json.Unmarshal(raw, &list)
		for _, v := range list {
			if v["model_id"] == q.ModelID {
				return v, nil
			}
		}
	}
	return raw, nil
}
func (s *CatalogBusinessStore) decide(ctx context.Context, q catalogCommand, a catalogActor) (any, error) {
	r, err := s.allowed(ctx, q.RunID, a, true, q.Vendor)
	if err != nil {
		return nil, err
	}
	if r.Status != "SUCCEEDED" || len(q.ModelIDs) == 0 || len(q.ModelIDs) > 50 {
		return nil, errCatalogStale
	}
	models := map[string]bool{}
	for _, m := range catalogModels(r.Result) {
		if m["status"] != "NOT_OBSERVED" {
			id, _ := m["id"].(string)
			models[id] = true
		}
	}
	pub := map[string]json.RawMessage{}
	_ = json.Unmarshal(q.Published, &pub)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT id FROM model_catalog.runs WHERE id=$1 FOR UPDATE", r.ID); err != nil {
		return nil, err
	}
	for _, id := range q.ModelIDs {
		if !models[id] {
			return nil, errCatalogStale
		}
		decision := "REJECTED"
		var v any
		if q.Operation == "approve" {
			decision = "APPROVED"
			if len(pub[id]) == 0 {
				return nil, errCatalogStale
			}
			v = string(pub[id])
			var ok bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM model_catalog.write_receipts WHERE run_id=$1 AND model_id=$2 AND staff_id=$3 AND result->>'status'='WRITTEN')`, r.ID, id, a.staff).Scan(&ok); err != nil {
				return nil, err
			}
			if !ok {
				return nil, errCatalogStale
			}
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO model_catalog.decisions(run_id,model_id,staff_id,decision,published_model) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, r.ID, id, a.staff, decision, v)
		if e != nil {
			return nil, e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return nil, errCatalogStale
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO model_catalog.events(kind,staff_id,run_id,model_id) VALUES($1,$2,$3,$4)", decision, a.staff, r.ID, id)
		if e != nil {
			return nil, e
		}
	}
	return true, tx.Commit()
}
func (s *CatalogBusinessStore) evidence(ctx context.Context, q catalogCommand) (any, error) {
	if q.Operation == "evidence-read" {
		var raw json.RawMessage
		err := s.db.QueryRowContext(ctx, "SELECT to_jsonb(e) FROM model_catalog.evidence e WHERE evidence_id=$1", q.Key).Scan(&raw)
		return raw, err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO model_catalog.evidence(evidence_id,vendor,source_url,sha256,bytes,transport,rendered,observed_at,parser_version,storage_path,model_id,run_id) SELECT e.evidence_id,e.vendor,e.source_url,e.sha256,e.bytes,e.transport,coalesce(e.rendered,false),e.observed_at,e.parser_version,e.storage_path,e.model_id,e.run_id FROM jsonb_populate_record(NULL::model_catalog.evidence,$1::jsonb) e ON CONFLICT DO NOTHING`, string(q.Evidence))
	return gin.H{"ok": err == nil}, err
}
