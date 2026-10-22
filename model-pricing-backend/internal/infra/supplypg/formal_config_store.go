package supplypg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"model_bss/internal/domain/supply"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type FormalConfigLog ResourceStateLog
type FormalConfigLogAppender func(context.Context, *sql.Tx, FormalConfigLog) error
type FormalConfigStore struct {
	schema string
	log    FormalConfigLogAppender
}
type FormalConfigWork struct {
	store   *FormalConfigStore
	resolve ModelTxResolver
}

var _ supply.FormalConfigUnitOfWork = (*FormalConfigWork)(nil)

func NewFormalConfigStore(schema string, log FormalConfigLogAppender) (*FormalConfigStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	return &FormalConfigStore{schema, log}, nil
}
func NewFormalConfigWork(store *FormalConfigStore, resolve ModelTxResolver) *FormalConfigWork {
	return &FormalConfigWork{store, resolve}
}
func (w *FormalConfigWork) Within(ctx context.Context, run func(supply.FormalConfigTransaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || w.store == nil || w.store.log == nil || w.resolve == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	tx, err := w.resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	t := &formalConfigTransaction{store: w.store, tx: tx}
	if err := run(t); err != nil {
		return err
	}
	if !t.logged {
		return supply.ErrReadContract
	}
	return nil
}

const formalConfigRevisionAction = "FORMAL_CONFIG_REVISION"

type formalConfigEnvelope struct {
	Contract          string                   `json:"contract"`
	SchemaVersion     int                      `json:"schema_version"`
	Operation         string                   `json:"operation"`
	View              *supply.FormalConfigView `json:"view"`
	SealedFingerprint string                   `json:"sealed_fingerprint"`
}

func encodeFormalSecret(secret supply.SecretEnvelope) (string, error) {
	if len(secret.Ciphertext) == 0 || strings.TrimSpace(secret.KeyVersion) == "" || !utf8.ValidString(secret.KeyVersion) || strings.ContainsRune(secret.KeyVersion, 0) || len(secret.KeyVersion) > 64 {
		return "", supply.ErrReadContract
	}
	wire := "se1." + base64.RawURLEncoding.EncodeToString([]byte(secret.KeyVersion)) + "." + base64.RawURLEncoding.EncodeToString(secret.Ciphertext)
	if len(wire) > 512 {
		return "", &supply.FieldError{Path: "api_key", Reason: "sealed credential exceeds native storage capacity"}
	}
	return wire, nil
}
func decodeFormalSecret(wire string) (supply.SecretEnvelope, error) {
	var secret supply.SecretEnvelope
	p := strings.Split(wire, ".")
	if len(p) != 3 || p[0] != "se1" {
		return secret, supply.ErrReadContract
	}
	version, err := base64.RawURLEncoding.DecodeString(p[1])
	if err != nil {
		return secret, supply.ErrReadContract
	}
	sealed, err := base64.RawURLEncoding.DecodeString(p[2])
	if err != nil {
		return secret, supply.ErrReadContract
	}
	secret = supply.SecretEnvelope{KeyVersion: string(version), Ciphertext: sealed}
	canonical, err := encodeFormalSecret(secret)
	if err != nil || canonical != wire {
		return supply.SecretEnvelope{}, supply.ErrReadContract
	}
	return secret, nil
}
func formalFingerprint(wire string) string {
	sum := sha256.Sum256([]byte(wire))
	return hex.EncodeToString(sum[:])
}
func formalText(value string, limit int) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= limit
}
func formalSafeURL(value string) bool {
	u, err := url.ParseRequestURI(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key := range query {
		if slices.Contains([]string{"key", "api_key", "apikey", "token", "access_token", "password", "secret", "authorization", "signature", "sig", "x-amz-signature", "credential"}, strings.ToLower(key)) {
			return false
		}
	}
	return true
}
func canonicalFormalConfig(input *supply.FormalConfig) (*supply.FormalConfig, error) {
	if input == nil {
		return nil, supply.ErrReadContract
	}
	c := input.Clone()
	for _, id := range []string{c.ID, c.SupplierID, c.CreatedBy, c.UpdatedBy, c.ConfirmedBy} {
		if !modelIdentity(id) {
			return nil, supply.ErrReadContract
		}
	}
	if (c.Region != "CN" && c.Region != "OVERSEAS") || (c.Status != "ACTIVE" && c.Status != "INACTIVE") || c.Revision == 0 || c.Revision > math.MaxInt64 || c.ConfirmedRevision == 0 || c.ConfirmedRevision > c.Revision {
		return nil, supply.ErrReadContract
	}
	if !formalText(c.Scope, 255) || !formalText(c.BaseURL, 512) || !formalSafeURL(c.BaseURL) {
		return nil, supply.ErrReadContract
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) || c.ConfirmedAt.IsZero() || c.ConfirmedAt.Before(c.CreatedAt) || c.ConfirmedAt.After(c.UpdatedAt) {
		return nil, supply.ErrReadContract
	}
	if c.Status == "ACTIVE" && c.ConfirmedRevision != c.Revision {
		return nil, supply.ErrReadContract
	}
	if _, err := encodeFormalSecret(c.Secret); err != nil {
		return nil, err
	}
	c.CreatedAt = c.CreatedAt.UTC().Truncate(time.Microsecond)
	c.UpdatedAt = c.UpdatedAt.UTC().Truncate(time.Microsecond)
	c.ConfirmedAt = c.ConfirmedAt.UTC().Truncate(time.Microsecond)
	return c, nil
}
func (s *FormalConfigStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }
func (s *FormalConfigStore) Current(ctx context.Context, tx *sql.Tx, supplier, id string) (*supply.FormalConfig, error) {
	if !modelIdentity(supplier) || !modelIdentity(id) {
		return nil, supply.ErrReadContract
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || tx == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	var c supply.FormalConfig
	var scope, createdBy, updatedBy, region sql.NullString
	var wire string
	err := tx.QueryRowContext(ctx, `SELECT c.id::text,c.supplier_id::text,c.scope,c.base_url,c.api_key_enc,c.created_by::text,c.updated_by::text,c.created_at,c.updated_at,s.region FROM `+s.table("supplier_formal_config")+` c LEFT JOIN `+s.table("supplier")+` s ON s.id=c.supplier_id WHERE c.id=$1 AND c.supplier_id=$2`, id, supplier).Scan(&c.ID, &c.SupplierID, &scope, &c.BaseURL, &wire, &createdBy, &updatedBy, &c.CreatedAt, &c.UpdatedAt, &region)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !scope.Valid || !createdBy.Valid || !updatedBy.Valid || !region.Valid {
		return nil, supply.ErrReadContract
	}
	c.Scope = scope.String
	c.CreatedBy = createdBy.String
	c.UpdatedBy = updatedBy.String
	c.Region = region.String
	c.Secret, err = decodeFormalSecret(wire)
	if err != nil {
		return nil, err
	}
	var raw []byte
	var actor, reason, request sql.NullString
	var at time.Time
	err = tx.QueryRowContext(ctx, `SELECT "after",operator_id::text,reason,request_id,created_at FROM `+s.table("operation_log")+` WHERE entity_type='supplier_formal_config' AND entity_id=$1 AND action=$2 ORDER BY id DESC LIMIT 1`, id, formalConfigRevisionAction).Scan(&raw, &actor, &reason, &request, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrReadContract
	}
	if err != nil {
		return nil, err
	}
	var e formalConfigEnvelope
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return nil, supply.ErrReadContract
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, supply.ErrReadContract
	}
	if e.Contract != "supply.formal_config" || e.SchemaVersion != 1 || !slices.Contains([]string{"CREATE", "UPDATE", "DISABLE"}, e.Operation) || e.View == nil || e.SealedFingerprint != formalFingerprint(wire) {
		return nil, supply.ErrReadContract
	}
	v := e.View
	c.Status = v.Status
	c.Revision = v.Revision
	c.ConfirmedBy = v.ConfirmedBy
	c.ConfirmedAt = v.ConfirmedAt
	c.ConfirmedRevision = v.ConfirmedRevision
	stored, err := canonicalFormalConfig(&c)
	if err != nil {
		return nil, supply.ErrReadContract
	}
	if !reflect.DeepEqual(stored.View(), v) || !at.Equal(stored.UpdatedAt) || actor.String != stored.UpdatedBy || validateResourceControlMeta(supply.WriteMeta{ActorID: actor.String, Reason: reason.String, RequestID: request.String}, e.Operation) != nil {
		return nil, supply.ErrReadContract
	}
	return stored, nil
}
func (s *FormalConfigStore) ResourceView(ctx context.Context, tx *sql.Tx, r *supply.Resource) (*supply.FormalConfigView, error) {
	if r == nil || !modelIdentity(r.SupplierID) || !modelIdentity(r.FormalConfigID) {
		return nil, supply.ErrReadContract
	}
	c, err := s.Current(ctx, tx, r.SupplierID, r.FormalConfigID)
	if errors.Is(err, supply.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c.View(), nil
}

type formalConfigTransaction struct {
	store          *FormalConfigStore
	tx             *sql.Tx
	supplier       supply.FormalConfigSupplier
	allocated      string
	before, staged *supply.FormalConfig
	logged         bool
}

var _ supply.FormalConfigTransaction = (*formalConfigTransaction)(nil)

func (t *formalConfigTransaction) SupplierForUpdate(ctx context.Context, id string) (supply.FormalConfigSupplier, error) {
	var s supply.FormalConfigSupplier
	if !modelIdentity(id) || t.supplier.ID != "" {
		return s, supply.ErrReadContract
	}
	err := t.tx.QueryRowContext(ctx, `SELECT id::text,region,status FROM `+t.store.table("supplier")+` WHERE id=$1 FOR UPDATE`, id).Scan(&s.ID, &s.Region, &s.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return s, supply.ErrNotFound
	}
	if err != nil {
		return s, err
	}
	rows, err := t.tx.QueryContext(ctx, `SELECT old_staff_id::text,new_staff_id::text,changed_by::text FROM `+t.store.table("assignment_log")+` WHERE entity_type='supplier' AND entity_id=$1 AND field='buyer_id' ORDER BY id ASC`, id)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var old, next, actor sql.NullString
		if err := rows.Scan(&old, &next, &actor); err != nil {
			rows.Close()
			return s, err
		}
		if (s.BuyerID == "" && old.Valid) || (s.BuyerID != "" && (!old.Valid || old.String != s.BuyerID)) || !next.Valid || !modelIdentity(next.String) || !actor.Valid || !modelIdentity(actor.String) {
			rows.Close()
			return s, supply.ErrReadContract
		}
		s.BuyerID = next.String
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	if !modelIdentity(s.BuyerID) {
		return s, supply.ErrReadContract
	}
	t.supplier = s
	return s, nil
}
func (t *formalConfigTransaction) NextID(ctx context.Context) (string, error) {
	if t.supplier.ID == "" || t.allocated != "" || t.before != nil {
		return "", supply.ErrReadContract
	}
	if err := t.tx.QueryRowContext(ctx, `SELECT nextval(pg_get_serial_sequence($1,'id'))::text`, t.store.table("supplier_formal_config")).Scan(&t.allocated); err != nil {
		return "", err
	}
	if !modelIdentity(t.allocated) {
		return "", supply.ErrReadContract
	}
	return t.allocated, nil
}
func (t *formalConfigTransaction) ConfigForUpdate(ctx context.Context, supplier, id string) (*supply.FormalConfig, error) {
	if supplier != t.supplier.ID || !modelIdentity(id) || t.before != nil || t.allocated != "" {
		return nil, supply.ErrReadContract
	}
	var locked string
	if err := t.tx.QueryRowContext(ctx, `SELECT id::text FROM `+t.store.table("supplier_formal_config")+` WHERE supplier_id=$1 AND id=$2 FOR UPDATE`, supplier, id).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrNotFound
		}
		return nil, err
	}
	c, err := t.store.Current(ctx, t.tx, supplier, id)
	if err != nil {
		return nil, err
	}
	t.before = c.Clone()
	return c, nil
}
func (t *formalConfigTransaction) validateStage(c *supply.FormalConfig) error {
	if t.staged != nil || t.logged || t.supplier.ID == "" || t.supplier.Status != "ACTIVE" || c.SupplierID != t.supplier.ID || c.Region != t.supplier.Region || c.UpdatedBy != t.supplier.BuyerID {
		return supply.ErrReadContract
	}
	return nil
}
func (t *formalConfigTransaction) CreateConfig(ctx context.Context, input *supply.FormalConfig) error {
	c, err := canonicalFormalConfig(input)
	if err != nil {
		return err
	}
	if err := t.validateStage(c); err != nil {
		return err
	}
	if c.ID != t.allocated || c.Revision != 1 || c.Status != "ACTIVE" || c.CreatedBy != c.UpdatedBy {
		return supply.ErrReadContract
	}
	wire, err := encodeFormalSecret(c.Secret)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO `+t.store.table("supplier_formal_config")+` (id,supplier_id,scope,base_url,api_key_enc,created_by,updated_by,created_at,updated_at) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, c.ID, c.SupplierID, c.Scope, c.BaseURL, wire, c.CreatedBy, c.UpdatedBy, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return modelUniqueError(err, supply.ErrIdentityConflict)
	}
	t.staged = c
	return nil
}
func (t *formalConfigTransaction) UpdateConfig(ctx context.Context, input *supply.FormalConfig, expected uint64) error {
	c, err := canonicalFormalConfig(input)
	if err != nil {
		return err
	}
	if err := t.validateStage(c); err != nil {
		return err
	}
	b := t.before
	if b == nil || b.ID != c.ID || expected == 0 || expected >= math.MaxInt64 || b.Revision != expected || c.Revision != expected+1 {
		return supply.ErrRevisionConflict
	}
	if c.CreatedBy != b.CreatedBy || !c.CreatedAt.Equal(b.CreatedAt) || c.UpdatedAt.Before(b.UpdatedAt) {
		return supply.ErrReadContract
	}
	wire, err := encodeFormalSecret(c.Secret)
	if err != nil {
		return err
	}
	result, err := t.tx.ExecContext(ctx, `UPDATE `+t.store.table("supplier_formal_config")+` SET scope=$1,base_url=$2,api_key_enc=$3,updated_by=$4,updated_at=$5 WHERE id=$6 AND supplier_id=$7 AND created_at=$8 AND updated_at=$9`, c.Scope, c.BaseURL, wire, c.UpdatedBy, c.UpdatedAt, c.ID, c.SupplierID, c.CreatedAt, b.UpdatedAt)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return supply.ErrRevisionConflict
	}
	t.staged = c
	return nil
}
func (t *formalConfigTransaction) HasResourceReferences(ctx context.Context, id string) (bool, error) {
	if t.before == nil || t.before.ID != id || t.supplier.ID == "" {
		return false, supply.ErrReadContract
	}
	rows, err := t.tx.QueryContext(ctx, `SELECT id::text FROM `+t.store.table("resource")+` WHERE supplier_id=$1 ORDER BY id`, t.supplier.ID)
	if err != nil {
		return false, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	state := &ResourceStateStore{schema: t.store.schema}
	for _, resource := range ids {
		r, err := state.Current(ctx, t.tx, resource)
		if err != nil {
			return false, err
		}
		if r.FormalConfigID == id {
			return true, nil
		}
	}
	return false, nil
}
func (t *formalConfigTransaction) AppendConfigOperation(ctx context.Context, op supply.FormalConfigOperation) error {
	if t.logged || op.EntityType != "supplier_formal_config" || op.SupplierID != t.supplier.ID || op.ActorID != t.supplier.BuyerID || op.After == nil {
		return supply.ErrReadContract
	}
	meta := supply.WriteMeta{ActorID: op.ActorID, Reason: op.Reason, RequestID: op.RequestID}
	if err := validateResourceControlMeta(meta, op.Action); err != nil {
		return err
	}
	after := t.staged
	if after == nil {
		after = t.before
	}
	if after == nil {
		return supply.ErrReadContract
	}
	// Domain clocks can carry nanoseconds; all persisted safe views use PG precision.
	canonicalView := func(v *supply.FormalConfigView) *supply.FormalConfigView {
		if v == nil {
			return nil
		}
		c := *v
		c.CreatedAt = c.CreatedAt.UTC().Truncate(time.Microsecond)
		c.UpdatedAt = c.UpdatedAt.UTC().Truncate(time.Microsecond)
		c.ConfirmedAt = c.ConfirmedAt.UTC().Truncate(time.Microsecond)
		return &c
	}
	if op.EntityID != after.ID || !reflect.DeepEqual(canonicalView(op.After), after.View()) || !reflect.DeepEqual(canonicalView(op.Before), t.before.View()) {
		return supply.ErrReadContract
	}
	changed := t.staged != nil
	if changed && !slices.Contains([]string{"CREATE", "UPDATE", "DISABLE"}, op.Action) {
		return supply.ErrReadContract
	}
	if !changed && !slices.Contains([]string{"UPDATE_NOOP", "DISABLE_NOOP"}, op.Action) {
		return supply.ErrReadContract
	}
	wire, err := encodeFormalSecret(after.Secret)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(formalConfigEnvelope{"supply.formal_config", 1, op.Action, after.View(), formalFingerprint(wire)})
	if err != nil {
		return supply.ErrReadContract
	}
	var before []byte
	if t.before != nil {
		oldWire, err := encodeFormalSecret(t.before.Secret)
		if err != nil {
			return err
		}
		before, err = json.Marshal(formalConfigEnvelope{"supply.formal_config", 1, op.Action, t.before.View(), formalFingerprint(oldWire)})
		if err != nil {
			return err
		}
	}
	action := op.Action
	if changed {
		action = formalConfigRevisionAction
	}
	var previous int64
	if !changed {
		if err := t.tx.QueryRowContext(ctx, `SELECT COALESCE(max(id),0) FROM `+t.store.table("operation_log")+` WHERE entity_type='supplier_formal_config' AND entity_id=$1`, op.EntityID).Scan(&previous); err != nil {
			return err
		}
	}
	entry := FormalConfigLog{EntityType: op.EntityType, EntityID: op.EntityID, Action: action, BusinessAction: op.Action, ActorID: op.ActorID, Reason: op.Reason, RequestID: op.RequestID, Before: before, After: raw, OccurredAt: op.OccurredAt.UTC().Truncate(time.Microsecond)}
	if err := t.store.log(ctx, t.tx, entry); err != nil {
		return err
	}
	if !changed {
		var n int
		if err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM `+t.store.table("operation_log")+` WHERE id>$1 AND entity_type='supplier_formal_config' AND entity_id=$2 AND action=$3 AND operator_id=$4 AND reason=$5 AND request_id=$6 AND "after"=$7::jsonb`, previous, op.EntityID, action, op.ActorID, op.Reason, op.RequestID, string(raw)).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return supply.ErrReadContract
		}
	}
	stored, err := t.store.Current(ctx, t.tx, after.SupplierID, after.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(stored, after) {
		return supply.ErrReadContract
	}
	t.logged = true
	return nil
}
