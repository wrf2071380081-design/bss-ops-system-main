package supplypg

import (
	"context"
	"errors"
	"model_bss/internal/domain/supply"
	"testing"
)

func TestPlanStoreRejectsInvalidDependencies(t *testing.T) {
	if _, err := NewPlanStore("public;drop", nil, nil, nil); err == nil {
		t.Fatal("unsafe schema accepted")
	}
	s, err := NewPlanStore("public", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPlanSummary(context.Background(), "1"); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal("missing snapshot runner accepted", err)
	}
	if err := s.WithinPlanMaintenance(context.Background(), func(supply.PlanMaintenanceTransaction) error {
		t.Fatal("callback invoked without dependencies")
		return nil
	}); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal("missing write dependencies accepted", err)
	}
}
