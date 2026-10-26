package api

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/repo"
)

type compositionURLPolicy struct{}

func (compositionURLPolicy) ValidateFormalURL(context.Context, string) error { return nil }

func newCompositionOptions(t *testing.T) SupplyRouteModuleOptions {
	t.Helper()
	raw, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	orm, err := gorm.Open(postgres.New(postgres.Config{Conn: raw}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := supplypg.NewOfficialSourcePolicy(map[string][]string{"Example": {"https://docs.example"}})
	if err != nil {
		t.Fatal(err)
	}
	return SupplyRouteModuleOptions{
		StandardModelSupplyOptions: StandardModelSupplyOptions{
			DB: raw, Schema: "public", IdemStore: repo.NewIdempotencyRepo(orm), SourcePolicy: policy,
		},
		FormalURLPolicy: compositionURLPolicy{},
		ResourceLog:     func(context.Context, *sql.Tx, supplypg.ResourceStateLog) error { return nil },
		FormalConfigLog: func(context.Context, *sql.Tx, supplypg.FormalConfigLog) error { return nil },
		NoticeLog:       func(context.Context, *sql.Tx, supply.NoticeReadOperation) error { return nil },
		Admission: supplypg.ResourceAdmissionHooks{
			Proof:       func(context.Context, *sql.Tx, *supply.Resource) (*supply.AdmissionProof, error) { return nil, nil },
			Report:      func(context.Context, *sql.Tx, *supply.Resource) (*supplypg.ReportEvidenceRef, error) { return nil, nil },
			CompleteRow: func(context.Context, *sql.Tx, supply.ResourceAdmissionEffect) error { return nil },
			Outbox:      func(context.Context, *sql.Tx, supplypg.AdmissionOutboxEntry) (string, error) { return "event-1", nil },
		},
		ResourceCreation: supplypg.ResourceCreationHooks{
			LockSource: func(context.Context, *sql.Tx, string, uint64) (*supply.ResourceCreationContext, error) {
				return nil, nil
			},
			AttachRow:     func(context.Context, *sql.Tx, supply.ResourceCreationEffect) error { return nil },
			PrepareReport: func(context.Context, *sql.Tx, *supply.Resource, *supply.ResourceReportSummary) error { return nil },
		},
		TestEvidenceAuthority: func(context.Context, *sql.Tx, *supply.Resource, string) (*supplypg.ReportEvidenceRef, bool, error) {
			return nil, false, nil
		},
		PendingConfiguration: func(context.Context, *sql.Tx, string) (bool, error) { return false, nil },
	}
}

func TestNewSupplyRouteModuleReportsMissingRealProviders(t *testing.T) {
	_, err := NewSupplyRouteModule(SupplyRouteModuleOptions{})
	if !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatalf("missing providers should fail closed with dependency error, got %v", err)
	}
	for _, name := range []string{"database", "idempotency", "resource_log", "admission.proof", "resource_creation.lock_source", "test_evidence_authority", "pending_configuration"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("missing-provider error omitted %q: %v", name, err)
		}
	}
}

func TestNewSupplyRouteModuleComposesEveryARegisteredRouteAndBService(t *testing.T) {
	module, err := NewSupplyRouteModule(newCompositionOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	if module == nil || module.ResourceCreation == nil || module.TestEvidence == nil {
		t.Fatal("A module omitted B-callable resource creation or test-evidence services")
	}
	deps := module.Deps
	if deps.Reader == nil || deps.Writer == nil || deps.Resources == nil || deps.ResourceAdmitter == nil || deps.ResourceConfigBinder == nil ||
		deps.FormalConfigs == nil || deps.FormalConfigReader == nil || deps.Notices == nil || deps.NoticeWriter == nil ||
		deps.Plans == nil || deps.PlanWriter == nil || deps.PlanExecutor == nil || deps.IdemStore == nil {
		t.Fatalf("A route composition is incomplete: %+v", deps)
	}
}

func TestNewSupplyRouteModuleRejectsIdempotencyPoolMismatch(t *testing.T) {
	options := newCompositionOptions(t)
	other, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	orm, err := gorm.Open(postgres.New(postgres.Config{Conn: other}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	options.IdemStore = repo.NewIdempotencyRepo(orm)
	if _, err := NewSupplyRouteModule(options); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatalf("split idempotency/business pools must fail closed, got %v", err)
	}
}
