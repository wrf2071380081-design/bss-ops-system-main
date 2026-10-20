package supply

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Persistence, encryption and environment policy are the unavailable boundaries.
// This envelope is deliberately not a production cipher.
type formalConfigMemory struct {
	configs    map[string]*FormalConfig
	suppliers  map[string]FormalConfigSupplier
	operations []FormalConfigOperation
	referenced bool
	failLog    bool
	next       int
}

func newFormalMemory() *formalConfigMemory {
	return &formalConfigMemory{configs: map[string]*FormalConfig{}, suppliers: map[string]FormalConfigSupplier{
		"11": {ID: "11", BuyerID: "7", Region: "CN", Status: "ACTIVE"},
		"12": {ID: "12", BuyerID: "8", Region: "OVERSEAS", Status: "ACTIVE"},
	}}
}
func (db *formalConfigMemory) Within(ctx context.Context, run func(FormalConfigTransaction) error) error {
	work := *db
	work.configs = map[string]*FormalConfig{}
	for id, c := range db.configs {
		work.configs[id] = c.Clone()
	}
	work.operations = append([]FormalConfigOperation(nil), db.operations...)
	if err := run(&work); err != nil {
		return err
	}
	*db = work
	return nil
}
func (db *formalConfigMemory) SupplierForUpdate(_ context.Context, id string) (FormalConfigSupplier, error) {
	s, ok := db.suppliers[id]
	if !ok {
		return s, ErrNotFound
	}
	return s, nil
}
func (db *formalConfigMemory) NextID(context.Context) (string, error) {
	db.next++
	return strconv.Itoa(100 + db.next), nil
}
func (db *formalConfigMemory) ConfigForUpdate(_ context.Context, supplierID, id string) (*FormalConfig, error) {
	c := db.configs[id]
	if c == nil || c.SupplierID != supplierID {
		return nil, ErrNotFound
	}
	return c.Clone(), nil
}
func (db *formalConfigMemory) CreateConfig(_ context.Context, c *FormalConfig) error {
	if db.configs[c.ID] != nil {
		return ErrIdentityConflict
	}
	db.configs[c.ID] = c.Clone()
	return nil
}
func (db *formalConfigMemory) UpdateConfig(_ context.Context, c *FormalConfig, expected uint64) error {
	if old := db.configs[c.ID]; old == nil {
		return ErrNotFound
	} else if old.Revision != expected {
		return ErrRevisionConflict
	}
	db.configs[c.ID] = c.Clone()
	return nil
}
func (db *formalConfigMemory) HasResourceReferences(context.Context, string) (bool, error) {
	return db.referenced, nil
}
func (db *formalConfigMemory) AppendConfigOperation(_ context.Context, op FormalConfigOperation) error {
	if db.failLog {
		return errOperationLog
	}
	db.operations = append(db.operations, op)
	return nil
}

type testSecretSealer func(context.Context, string, SecretBinding) (SecretEnvelope, error)

func (f testSecretSealer) Seal(ctx context.Context, text string, binding SecretBinding) (SecretEnvelope, error) {
	return f(ctx, text, binding)
}

type testFormalURLPolicy func(context.Context, string) error

func (f testFormalURLPolicy) ValidateFormalURL(ctx context.Context, value string) error {
	return f(ctx, value)
}
func formalService(db *formalConfigMemory) *FormalConfigService {
	return NewFormalConfigService(db, testSecretSealer(func(_ context.Context, _ string, binding SecretBinding) (SecretEnvelope, error) {
		return SecretEnvelope{Ciphertext: []byte("test-envelope:" + binding.ConfigID), KeyVersion: "test-v1"}, nil
	}), testFormalURLPolicy(func(context.Context, string) error { return nil }), func() time.Time { return modelNow })
}
func formalCommand() FormalConfigInput {
	key := "formal-secret-not-test-credential"
	return FormalConfigInput{Scope: "文本服务", BaseURL: "https://api.vendor.example/v1", APIKey: &key, ExternalConfirmed: true}
}

func TestFormalConfigEncryptsWithEntityBindingAndReturnsSafeViews(t *testing.T) {
	db := newFormalMemory()
	svc := formalService(db)
	command := formalCommand()
	secret := *command.APIKey
	svc.sealer = testSecretSealer(func(_ context.Context, value string, binding SecretBinding) (SecretEnvelope, error) {
		if value != secret || binding.SupplierID != "11" || binding.ConfigID != "101" || binding.Purpose != "SUPPLIER_FORMAL_API_KEY" {
			t.Fatalf("wrong encryption context: %+v", binding)
		}
		return SecretEnvelope{Ciphertext: []byte("opaque-authenticated-envelope"), KeyVersion: "test-v2"}, nil
	})
	view, err := svc.Create(context.Background(), writeMeta("7"), "11", command)
	if err != nil {
		t.Fatal(err)
	}
	stored := db.configs[view.ID]
	if stored.Secret.KeyVersion != "test-v2" || string(stored.Secret.Ciphertext) != "opaque-authenticated-envelope" || view.Region != "CN" || !view.HasAPIKey || view.APIKeyMask != "********" || view.Revision != 1 || view.ConfirmedRevision != 1 || view.ConfirmedBy != "7" {
		t.Fatalf("lost config facts: %+v", view)
	}
	for _, value := range []any{view, db.operations, stored} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "opaque-authenticated-envelope") || strings.Contains(string(encoded), "test-v2") {
			t.Fatalf("secret material leaked: %s", encoded)
		}
	}
	if len(db.operations) != 1 || db.operations[0].Action != "CREATE" || db.operations[0].ActorID != "7" || db.operations[0].RequestID != "request-test" || db.operations[0].Before != nil || db.operations[0].After.ID != view.ID {
		t.Fatal("create lost audit")
	}
	view.Scope = "caller mutation"
	if db.configs[view.ID].Scope != "文本服务" || db.operations[0].After.Scope != "文本服务" {
		t.Fatal("returned view aliases storage or log")
	}
}

func TestFormalConfigRejectsInvalidInputAndUntrustedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name            string
		mutate          func(*FormalConfigInput)
		actor, supplier string
		want            error
	}{
		{"confirmation", func(c *FormalConfigInput) { c.ExternalConfirmed = false }, "7", "11", nil},
		{"empty scope", func(c *FormalConfigInput) { c.Scope = " " }, "7", "11", nil},
		{"missing key", func(c *FormalConfigInput) { c.APIKey = nil }, "7", "11", nil},
		{"empty key", func(c *FormalConfigInput) { v := " "; c.APIKey = &v }, "7", "11", nil},
		{"url scheme", func(c *FormalConfigInput) { c.BaseURL = "file:///tmp/key" }, "7", "11", nil},
		{"url credentials", func(c *FormalConfigInput) { c.BaseURL = "https://user:secret@api.example/v1" }, "7", "11", nil},
		{"url key", func(c *FormalConfigInput) { c.BaseURL = "https://api.example/v1?api_key=secret" }, "7", "11", nil},
		{"other buyer", func(*FormalConfigInput) {}, "8", "11", ErrForbidden},
		{"unknown supplier", func(*FormalConfigInput) {}, "7", "99", ErrNotFound},
		{"bad actor", func(*FormalConfigInput) {}, "0", "11", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newFormalMemory()
			svc := formalService(db)
			command := formalCommand()
			tc.mutate(&command)
			view, err := svc.Create(context.Background(), writeMeta(tc.actor), tc.supplier, command)
			if err == nil || view != nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("invalid write accepted: view=%+v err=%v", view, err)
			}
			if len(db.configs) != 0 || len(db.operations) != 0 {
				t.Fatal("rejected write persisted")
			}
		})
	}
}

func TestFormalConfigPreservesOmittedKeyAndRotatesProvidedKey(t *testing.T) {
	db := newFormalMemory()
	svc := formalService(db)
	ctx := context.Background()
	first, err := svc.Create(ctx, writeMeta("7"), "11", formalCommand())
	if err != nil {
		t.Fatal(err)
	}
	original := db.configs[first.ID].Secret.Clone()
	command := formalCommand()
	command.APIKey = nil
	command.Scope = "图片服务"
	svc.now = func() time.Time { return modelNow.Add(time.Hour) }
	updated, err := svc.Update(ctx, writeMeta("7"), "11", first.ID, 1, command)
	if err != nil || updated.Revision != 2 || updated.ConfirmedRevision != 2 || !updated.ConfirmedAt.Equal(modelNow.Add(time.Hour)) || string(db.configs[first.ID].Secret.Ciphertext) != string(original.Ciphertext) {
		t.Fatalf("omitted key not preserved: %+v %v", updated, err)
	}
	newKey := "rotated-secret"
	command.APIKey = &newKey
	svc.sealer = testSecretSealer(func(_ context.Context, value string, _ SecretBinding) (SecretEnvelope, error) {
		if value != "rotated-secret" {
			t.Fatal("wrong rotated key")
		}
		return SecretEnvelope{Ciphertext: []byte("new-envelope"), KeyVersion: "test-v3"}, nil
	})
	rotated, err := svc.Update(ctx, writeMeta("7"), "11", first.ID, 2, command)
	if err != nil || rotated.Revision != 3 || rotated.ConfirmedRevision != 3 || db.configs[first.ID].Secret.KeyVersion != "test-v3" {
		t.Fatalf("rotation reused old confirmation: %+v %v", rotated, err)
	}
	empty := ""
	command.APIKey = &empty
	if _, err := svc.Update(ctx, writeMeta("7"), "11", first.ID, 3, command); err == nil {
		t.Fatal("empty key cleared credentials")
	}
	if len(db.operations) != 3 {
		t.Fatal("failed update logged success")
	}
}

func TestFormalConfigNoOpKeepsRevisionButRecordsNewIntent(t *testing.T) {
	db := newFormalMemory()
	svc := formalService(db)
	ctx := context.Background()
	first, err := svc.Create(ctx, writeMeta("7"), "11", formalCommand())
	if err != nil {
		t.Fatal(err)
	}
	command := formalCommand()
	command.APIKey = nil
	svc.now = func() time.Time { return modelNow.Add(time.Hour) }
	same, err := svc.Update(ctx, writeMeta("7"), "11", first.ID, 1, command)
	if err != nil || same.Revision != 1 || !same.UpdatedAt.Equal(first.UpdatedAt) || len(db.operations) != 2 || db.operations[1].Action != "UPDATE_NOOP" || !db.operations[1].OccurredAt.Equal(modelNow.Add(time.Hour)) {
		t.Fatalf("no-op changed config or lost trace: %+v %v", same, err)
	}
}

func TestFormalConfigRejectsCrossSupplierAndStaleWrites(t *testing.T) {
	db := newFormalMemory()
	svc := formalService(db)
	ctx := context.Background()
	first, err := svc.Create(ctx, writeMeta("7"), "11", formalCommand())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, writeMeta("8"), "12", first.ID, 1, formalCommand()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross supplier write: %v", err)
	}
	for _, revision := range []uint64{0, 2} {
		if _, err := svc.Update(ctx, writeMeta("7"), "11", first.ID, revision, formalCommand()); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("stale revision accepted: %v", err)
		}
	}
	if len(db.operations) != 1 || db.configs[first.ID].Revision != 1 {
		t.Fatal("failed write changed config")
	}
}

func TestFormalConfigReferencedCannotDisable(t *testing.T) {
	db := newFormalMemory()
	svc := formalService(db)
	ctx := context.Background()
	first, err := svc.Create(ctx, writeMeta("7"), "11", formalCommand())
	if err != nil {
		t.Fatal(err)
	}
	db.referenced = true
	if _, err := svc.Disable(ctx, writeMeta("7"), "11", first.ID, 1); !errors.Is(err, ErrConfigInUse) {
		t.Fatalf("referenced config disabled: %v", err)
	}
	if db.configs[first.ID].Status != "ACTIVE" || len(db.operations) != 1 {
		t.Fatal("blocked disable persisted")
	}
	db.referenced = false
	disabled, err := svc.Disable(ctx, writeMeta("7"), "11", first.ID, 1)
	if err != nil || disabled.Status != "INACTIVE" || disabled.Revision != 2 {
		t.Fatalf("disable failed: %+v %v", disabled, err)
	}
	if _, err := svc.Update(ctx, writeMeta("7"), "11", first.ID, 2, formalCommand()); !errors.Is(err, ErrConfigInactive) {
		t.Fatalf("disabled config edited: %v", err)
	}
	if _, err := svc.Disable(ctx, writeMeta("7"), "11", first.ID, 2); err != nil || len(db.operations) != 3 || db.operations[2].Action != "DISABLE_NOOP" || db.configs[first.ID].Revision != 2 {
		t.Fatalf("repeat disable lost audit: %v", err)
	}
}

func TestFormalConfigDependenciesFailClosedAndHideSecretErrors(t *testing.T) {
	for _, mode := range []string{"no work", "no cipher", "no policy", "cipher error", "bad envelope", "policy reject", "inactive supplier", "bad supplier identity"} {
		t.Run(mode, func(t *testing.T) {
			db := newFormalMemory()
			svc := formalService(db)
			switch mode {
			case "no work":
				svc.work = nil
			case "no cipher":
				svc.sealer = nil
			case "no policy":
				svc.urls = nil
			case "cipher error":
				svc.sealer = testSecretSealer(func(context.Context, string, SecretBinding) (SecretEnvelope, error) {
					return SecretEnvelope{}, errors.New("failed with formal-secret-not-test-credential")
				})
			case "bad envelope":
				svc.sealer = testSecretSealer(func(context.Context, string, SecretBinding) (SecretEnvelope, error) {
					return SecretEnvelope{Ciphertext: []byte("opaque")}, nil
				})
			case "policy reject":
				svc.urls = testFormalURLPolicy(func(context.Context, string) error { return errors.New("environment rejects URL") })
			case "inactive supplier":
				s := db.suppliers["11"]
				s.Status = "INACTIVE"
				db.suppliers["11"] = s
			case "bad supplier identity":
				s := db.suppliers["11"]
				s.ID = "12"
				db.suppliers["11"] = s
			}
			view, err := svc.Create(context.Background(), writeMeta("7"), "11", formalCommand())
			if err == nil || view != nil || strings.Contains(err.Error(), "formal-secret") || len(db.configs) != 0 || len(db.operations) != 0 {
				t.Fatalf("unsafe dependency failure: view=%+v err=%v", view, err)
			}
		})
	}
}

func TestFormalConfigLogFailureRollsBackEveryWrite(t *testing.T) {
	for _, mode := range []string{"create", "update", "rotate", "noop", "disable"} {
		t.Run(mode, func(t *testing.T) {
			db := newFormalMemory()
			svc := formalService(db)
			ctx := context.Background()
			var first *FormalConfigView
			if mode != "create" {
				var err error
				first, err = svc.Create(ctx, writeMeta("7"), "11", formalCommand())
				if err != nil {
					t.Fatal(err)
				}
			}
			db.failLog = true
			var result *FormalConfigView
			var err error
			command := formalCommand()
			switch mode {
			case "create":
				result, err = svc.Create(ctx, writeMeta("7"), "11", command)
			case "update":
				command.APIKey = nil
				command.Scope = "changed"
				result, err = svc.Update(ctx, writeMeta("7"), "11", first.ID, 1, command)
			case "rotate":
				result, err = svc.Update(ctx, writeMeta("7"), "11", first.ID, 1, command)
			case "noop":
				command.APIKey = nil
				result, err = svc.Update(ctx, writeMeta("7"), "11", first.ID, 1, command)
			case "disable":
				result, err = svc.Disable(ctx, writeMeta("7"), "11", first.ID, 1)
			}
			if result != nil || !errors.Is(err, errOperationLog) {
				t.Fatalf("log failure returned success: %+v %v", result, err)
			}
			if mode == "create" {
				if len(db.configs) != 0 || len(db.operations) != 0 {
					t.Fatal("create partially committed")
				}
			} else if db.configs[first.ID].Revision != 1 || db.configs[first.ID].Status != "ACTIVE" || db.configs[first.ID].Scope != "文本服务" || len(db.operations) != 1 {
				t.Fatal("write partially committed")
			}
		})
	}
}
