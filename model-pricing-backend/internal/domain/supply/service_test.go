package supply

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Only unavailable persistence and source verification are replaced. Real model rules run unchanged.
type memoryModelDB struct {
	mu      sync.Mutex
	state   *memoryModelTx
	failLog bool
}
type memoryModelTx struct {
	next       uint64
	models     map[string]*StandardModel
	versions   map[string][]*StandardModel
	operations []ModelOperation
	failLog    bool
}

func newMemoryDB() *memoryModelDB {
	return &memoryModelDB{state: &memoryModelTx{models: map[string]*StandardModel{}, versions: map[string][]*StandardModel{}}}
}
func (db *memoryModelDB) Within(ctx context.Context, run func(ModelTransaction) error) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	work := &memoryModelTx{next: db.state.next, models: map[string]*StandardModel{}, versions: map[string][]*StandardModel{}, failLog: db.failLog}
	for id, m := range db.state.models {
		work.models[id] = m.Clone()
	}
	for id, vs := range db.state.versions {
		for _, v := range vs {
			work.versions[id] = append(work.versions[id], v.Clone())
		}
	}
	work.operations = append(work.operations, db.state.operations...)
	if err := run(work); err != nil {
		return err
	}
	db.state = work
	return nil
}
func (tx *memoryModelTx) NextID(context.Context) (string, error) {
	tx.next++
	return fmt.Sprint(tx.next), nil
}
func (tx *memoryModelTx) GetForUpdate(_ context.Context, id string) (*StandardModel, error) {
	m := tx.models[id]
	if m == nil {
		return nil, ErrNotFound
	}
	return m.Clone(), nil
}
func (tx *memoryModelTx) identityConflict(m *StandardModel) bool {
	for id, other := range tx.models {
		if id != m.ID && m.Data.Company != "" && m.Data.OfficialID != "" && m.Data.Company == other.Data.Company && m.Data.OfficialID == other.Data.OfficialID {
			return true
		}
	}
	return false
}
func (tx *memoryModelTx) Create(_ context.Context, m *StandardModel) error {
	if tx.models[m.ID] != nil || tx.identityConflict(m) {
		return ErrIdentityConflict
	}
	tx.models[m.ID] = m.Clone()
	return nil
}
func (tx *memoryModelTx) Update(_ context.Context, m *StandardModel, expected uint64) error {
	old := tx.models[m.ID]
	if old == nil {
		return ErrNotFound
	}
	if old.Revision != expected {
		return ErrRevisionConflict
	}
	if tx.identityConflict(m) {
		return ErrIdentityConflict
	}
	tx.models[m.ID] = m.Clone()
	return nil
}
func (tx *memoryModelTx) AppendVersion(_ context.Context, m *StandardModel) error {
	for _, v := range tx.versions[m.ID] {
		if v.VersionNo == m.VersionNo {
			return ErrRevisionConflict
		}
	}
	tx.versions[m.ID] = append(tx.versions[m.ID], m.Clone())
	return nil
}

var errOperationLog = errors.New("operation log unavailable")

func (tx *memoryModelTx) AppendOperation(_ context.Context, op ModelOperation) error {
	if tx.failLog {
		return errOperationLog
	}
	tx.operations = append(tx.operations, op)
	return nil
}

type sourcePolicyFunc func(context.Context, string, []Source) error

func (f sourcePolicyFunc) Validate(ctx context.Context, company string, sources []Source) error {
	return f(ctx, company, sources)
}
func validSourcePolicy() OfficialSourcePolicy {
	return sourcePolicyFunc(func(_ context.Context, company string, sources []Source) error {
		if company != "Example" || len(sources) != 1 || sources[0].URL != "https://vendor.example/pricing" {
			return ErrSourceRejected
		}
		return nil
	})
}
func newService(db *memoryModelDB) *ModelService {
	return NewModelService(db, validSourcePolicy(), func() time.Time { return modelNow })
}
func writeMeta(actor string) WriteMeta {
	return WriteMeta{ActorID: actor, Reason: "人工维护官方资料", RequestID: "request-test"}
}

func TestModelServiceStoresVersionsAndOperationAtomically(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	ctx := context.Background()
	created, err := svc.Create(ctx, writeMeta("7"), completeModelData())
	if err != nil {
		t.Fatal(err)
	}
	if created.MaintainerID != "7" || len(db.state.versions[created.ID]) != 1 || len(db.state.operations) != 1 {
		t.Fatal("create did not bind owner and persist snapshot/log")
	}
	verified, err := svc.Verify(ctx, writeMeta("7"), created.ID, 1, "2026-10-20")
	if err != nil {
		t.Fatal(err)
	}
	changed := cloneData(verified.Data)
	changed.PriceItems[0].Amount = "3.00000000"
	updated, err := svc.Update(ctx, writeMeta("7"), created.ID, 2, changed)
	if err != nil {
		t.Fatal(err)
	}
	if updated.VerifyStatus != "UNVERIFIED" || len(db.state.versions[created.ID]) != 3 || db.state.versions[created.ID][1].Data.PriceItems[0].Amount != "2.00000000" {
		t.Fatal("update changed historical verified price")
	}
	op := db.state.operations[2]
	if op.Action != "UPDATE" || op.RequestID != "request-test" || op.Before.Revision != 2 || op.After.Revision != 3 || op.ActorID != "7" || op.Reason != "人工维护官方资料" {
		t.Fatalf("operation lost version/actor/trace: %+v", op)
	}
	updated.Data.PriceItems[0].Amount = "99"
	changed.PriceItems[0].Amount = "98"
	if db.state.models[created.ID].Data.PriceItems[0].Amount != "3.00000000" || op.After.Data.PriceItems[0].Amount != "3.00000000" {
		t.Fatal("returned command/result aliases persisted state or audit snapshot")
	}
}

func TestModelServiceNoOpRecordsOperationWithoutNewVersion(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	m, err := svc.Create(context.Background(), writeMeta("7"), completeModelData())
	if err != nil {
		t.Fatal(err)
	}
	d := cloneData(m.Data)
	d.PriceItems[0].Amount = "2" // Same decimal amount, not an official price change.
	svc.now = func() time.Time { return modelNow.Add(time.Hour) }
	same, err := svc.Update(context.Background(), writeMeta("7"), m.ID, 1, d)
	if err != nil || same.Revision != 1 || len(db.state.versions[m.ID]) != 1 || len(db.state.operations) != 2 {
		t.Fatalf("no-op must preserve version and record intent: %v", err)
	}
	op := db.state.operations[1]
	if op.Action != "UPDATE_NOOP" || op.ActorID != "7" || op.RequestID != "request-test" || op.Reason != "人工维护官方资料" || op.Before.Revision != 1 || op.After.Revision != 1 || !op.OccurredAt.Equal(modelNow.Add(time.Hour)) || !same.UpdatedAt.Equal(m.UpdatedAt) {
		t.Fatalf("no-op lost trace or changed model timestamp: %+v", op)
	}
}

func TestModelServiceRepeatedVerificationRecordsOperationWithoutNewVersion(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	ctx := context.Background()
	m, err := svc.Create(ctx, writeMeta("7"), completeModelData())
	if err != nil {
		t.Fatal(err)
	}
	verified, err := svc.Verify(ctx, writeMeta("7"), m.ID, 1, "2026-10-20")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return modelNow.Add(time.Hour) }
	same, err := svc.Verify(ctx, writeMeta("7"), m.ID, 2, "2026-10-20")
	if err != nil || same.Revision != 2 || len(db.state.versions[m.ID]) != 2 || len(db.state.operations) != 3 {
		t.Fatalf("repeated verification lost audit or created a version: %v", err)
	}
	op := db.state.operations[2]
	if op.Action != "VERIFY_NOOP" || op.Before.Revision != 2 || op.After.Revision != 2 || op.RequestID != "request-test" || !op.OccurredAt.Equal(modelNow.Add(time.Hour)) || !same.UpdatedAt.Equal(verified.UpdatedAt) {
		t.Fatalf("repeated verification lost trace: %+v", op)
	}
}

func TestModelServiceNoOpLogFailureReturnsNoSuccess(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	ctx := context.Background()
	m, err := svc.Create(ctx, writeMeta("7"), completeModelData())
	if err != nil {
		t.Fatal(err)
	}
	db.failLog = true
	if result, err := svc.Update(ctx, writeMeta("7"), m.ID, 1, m.Data); result != nil || !errors.Is(err, errOperationLog) {
		t.Fatalf("no-op log failure returned success: result=%+v error=%v", result, err)
	}
	if len(db.state.versions[m.ID]) != 1 || len(db.state.operations) != 1 || db.state.models[m.ID].Revision != 1 {
		t.Fatal("failed no-op partially committed")
	}
}

func TestModelServiceRejectsNonOwnerAndStaleRevisionWithoutWrites(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	ctx := context.Background()
	m, _ := svc.Create(ctx, writeMeta("7"), completeModelData())
	if _, err := svc.Update(ctx, writeMeta("8"), m.ID, 1, m.Data); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong owner accepted: %v", err)
	}
	if _, err := svc.Verify(ctx, writeMeta("8"), m.ID, 1, "2026-10-20"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong verifier accepted: %v", err)
	}
	if _, err := svc.Update(ctx, writeMeta("7"), m.ID, 0, m.Data); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update accepted: %v", err)
	}
	if len(db.state.operations) != 1 || db.state.models[m.ID].Revision != 1 {
		t.Fatal("failed write mutated state")
	}
}

func TestModelServiceLogFailureRollsBackCreateAndUpdate(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	ctx := context.Background()
	db.failLog = true
	if _, err := svc.Create(ctx, writeMeta("7"), completeModelData()); !errors.Is(err, errOperationLog) {
		t.Fatalf("log failure hidden: %v", err)
	}
	if len(db.state.models) != 0 || len(db.state.versions) != 0 || len(db.state.operations) != 0 {
		t.Fatal("failed create partially committed")
	}
	db.failLog = false
	m, err := svc.Create(ctx, writeMeta("7"), completeModelData())
	if err != nil {
		t.Fatal(err)
	}
	db.failLog = true
	d := cloneData(m.Data)
	d.Name = "Changed model"
	if _, err := svc.Update(ctx, writeMeta("7"), m.ID, 1, d); !errors.Is(err, errOperationLog) {
		t.Fatalf("update log failure hidden: %v", err)
	}
	if db.state.models[m.ID].Data.Name != "Example Model" || len(db.state.versions[m.ID]) != 1 || len(db.state.operations) != 1 {
		t.Fatal("failed update partially committed")
	}
}

func TestModelServiceConcurrentRevisionOnlyOneWins(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	ctx := context.Background()
	m, _ := svc.Create(ctx, writeMeta("7"), completeModelData())
	results := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		go func(name string) {
			d := completeModelData()
			d.Name = name
			_, err := svc.Update(ctx, writeMeta("7"), m.ID, 1, d)
			results <- err
		}(name)
	}
	wins, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			wins++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 || len(db.state.versions[m.ID]) != 2 || db.state.models[m.ID].Revision != 2 {
		t.Fatal("same revision committed more than once")
	}
}

func TestModelServiceVerificationFailsClosed(t *testing.T) {
	db := newMemoryDB()
	ctx := context.Background()
	svc := newService(db)
	m, _ := svc.Create(ctx, writeMeta("7"), completeModelData())
	missing := NewModelService(db, nil, func() time.Time { return modelNow })
	if _, err := missing.Verify(ctx, writeMeta("7"), m.ID, 1, "2026-10-20"); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("missing source policy passed: %v", err)
	}
	rejected := NewModelService(db, sourcePolicyFunc(func(context.Context, string, []Source) error { return ErrSourceRejected }), func() time.Time { return modelNow })
	if _, err := rejected.Verify(ctx, writeMeta("7"), m.ID, 1, "2026-10-20"); !errors.Is(err, ErrSourceRejected) {
		t.Fatalf("rejected source passed: %v", err)
	}
	if len(db.state.operations) != 1 || db.state.models[m.ID].VerifyStatus != "UNVERIFIED" {
		t.Fatal("failed source verification changed state")
	}
}

func TestModelServiceIdentityCollisionAndMissingTraceDoNotWrite(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	ctx := context.Background()
	_, err := svc.Create(ctx, writeMeta("7"), completeModelData())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Create(ctx, writeMeta("8"), completeModelData()); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("duplicate official identity: %v", err)
	}
	bad := writeMeta("7")
	bad.RequestID = ""
	if _, err = svc.Create(ctx, bad, ModelData{Name: "draft"}); err == nil {
		t.Fatal("untraceable write accepted")
	}
	if len(db.state.models) != 1 || len(db.state.operations) != 1 {
		t.Fatal("rejected command wrote data")
	}
}

func TestModelServiceChecksEachPriceSourceAgainstOfficialPolicy(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	ctx := context.Background()
	d := completeModelData()
	d.PriceItems[0].SourceURL = "https://untrusted.example/pricing"
	m, err := svc.Create(ctx, writeMeta("7"), d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Verify(ctx, writeMeta("7"), m.ID, 1, "2026-10-20"); !errors.Is(err, ErrSourceRejected) {
		t.Fatalf("untrusted item source bypassed policy: %v", err)
	}
	if db.state.models[m.ID].VerifyStatus != "UNVERIFIED" || len(db.state.versions[m.ID]) != 1 {
		t.Fatal("rejected item source changed state")
	}
}

func TestModelServiceRejectsEmptyTargetID(t *testing.T) {
	svc := newService(newMemoryDB())
	_, err := svc.Update(context.Background(), writeMeta("7"), "", 1, completeModelData())
	var field *FieldError
	if !errors.As(err, &field) || field.Path != "id" {
		t.Fatalf("empty target was not field validation: %v", err)
	}
}

func TestModelServiceManualSaveAttestsCompleteVersionAtomically(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	ctx := context.Background()
	created, err := svc.SaveManual(ctx, writeMeta("7"), "", 0, completeModelData())
	if err != nil {
		t.Fatal(err)
	}
	if created.VerifyStatus != "VERIFIED" || created.CheckedBy != "7" || created.VersionNo != 1 || created.Revision != 1 || !created.Readiness().Ready {
		t.Fatalf("complete manual creation should be attested as its initial version: %+v", created)
	}
	if len(db.state.versions[created.ID]) != 1 || len(db.state.operations) != 1 || db.state.operations[0].Action != "MANUAL_SAVE" || db.state.operations[0].After.VerifyStatus != "VERIFIED" {
		t.Fatal("model, attested version and manual-save audit were not persisted together")
	}

	changed := cloneData(created.Data)
	changed.PriceItems[0].Amount = "3.00000000"
	updated, err := svc.SaveManual(ctx, writeMeta("7"), created.ID, created.Revision, changed)
	if err != nil {
		t.Fatal(err)
	}
	if updated.VerifyStatus != "VERIFIED" || updated.CheckedBy != "7" || updated.VersionNo != 2 || updated.Revision != 2 || !updated.Readiness().Ready {
		t.Fatalf("complete edit should produce one verified version: %+v", updated)
	}
	if len(db.state.versions[created.ID]) != 2 || db.state.versions[created.ID][0].Data.PriceItems[0].Amount != "2.00000000" || db.state.versions[created.ID][1].Data.PriceItems[0].Amount != "3.00000000" {
		t.Fatal("manual completion did not preserve the prior immutable version")
	}
}

func TestModelServiceManualSaveKeepsIncompleteDraftUnverifiedAndSourceFailureAtomic(t *testing.T) {
	db := newMemoryDB()
	ctx := context.Background()
	incomplete := NewModelService(db, nil, func() time.Time { return modelNow })
	draft, err := incomplete.SaveManual(ctx, writeMeta("7"), "", 0, ModelData{Name: "待补齐"})
	if err != nil {
		t.Fatal(err)
	}
	if draft.VerifyStatus != "INCOMPLETE" || draft.CheckedBy != "" || draft.Readiness().Ready || len(db.state.versions[draft.ID]) != 1 {
		t.Fatalf("incomplete save should remain blocked from quotation: %+v", draft)
	}

	rejected := NewModelService(db, sourcePolicyFunc(func(context.Context, string, []Source) error { return ErrSourceRejected }), func() time.Time { return modelNow })
	if result, err := rejected.SaveManual(ctx, writeMeta("8"), "", 0, completeModelData()); result != nil || !errors.Is(err, ErrSourceRejected) {
		t.Fatalf("complete save without approved official sources should fail: result=%+v error=%v", result, err)
	}
	if len(db.state.models) != 1 || len(db.state.operations) != 1 || db.state.models[draft.ID].VerifyStatus != "INCOMPLETE" {
		t.Fatal("rejected source policy partially persisted a complete manual save")
	}
}

func TestModelServiceManualSaveEditInvalidatesVerificationWhenIncomplete(t *testing.T) {
	db := newMemoryDB()
	svc := newService(db)
	created, err := svc.SaveManual(context.Background(), writeMeta("7"), "", 0, completeModelData())
	if err != nil {
		t.Fatal(err)
	}
	incomplete := cloneData(created.Data)
	incomplete.PriceItems = nil
	updated, err := svc.SaveManual(context.Background(), writeMeta("7"), created.ID, created.Revision, incomplete)
	if err != nil {
		t.Fatal(err)
	}
	if updated.VerifyStatus != "INCOMPLETE" || updated.CheckedBy != "" || updated.Readiness().Ready || updated.VersionNo != 2 || updated.Revision != 2 {
		t.Fatalf("incomplete edit must invalidate quotation readiness: %+v", updated)
	}
}
