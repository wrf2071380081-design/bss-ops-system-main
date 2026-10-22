package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"testing"
)

func TestResourceAdmissionAndEvidenceWorkRequireActualDependencies(t *testing.T) {
	ctx := context.Background()
	calls := 0
	state, _ := NewResourceStateStore("public", func(context.Context, *sql.Tx, ResourceStateLog) error { return nil })
	configs, _ := NewFormalConfigStore("public", nil)
	resolve := func(context.Context) (*sql.Tx, error) { calls++; return nil, nil }
	incomplete := NewResourceAdmissionWork(state, configs, resolve, ResourceAdmissionHooks{})
	if err := incomplete.WithinAdmission(ctx, func(supply.ResourceAdmissionTransaction) error { return nil }); !errors.Is(err, supply.ErrDependencyUnavailable) || calls != 0 {
		t.Fatal("missing admission hooks reached SQL", err)
	}
	evidence := NewTestEvidenceWork(state, resolve, nil)
	if err := evidence.WithinTestEvidence(ctx, func(supply.TestEvidenceTransaction) error { return nil }); !errors.Is(err, supply.ErrDependencyUnavailable) || calls != 0 {
		t.Fatal("missing evidence authority reached SQL", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := incomplete.WithinAdmission(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("admission cancellation hidden", err)
	}
	if err := evidence.WithinTestEvidence(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("evidence cancellation hidden", err)
	}
}
