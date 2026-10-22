package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"testing"
)

func TestFormalConfigReadStoreConstructionAndMissingDependencies(t *testing.T) {
	if got, err := NewFormalConfigReadStore("public;DROP", nil, nil); got != nil || err == nil {
		t.Fatal("accepted an unsafe schema")
	}
	config, err := NewFormalConfigStore("public", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := NewFormalConfigReadStore("other", nil, config); got != nil || err == nil {
		t.Fatal("accepted mixed-schema collaborators")
	}
	store, err := NewFormalConfigReadStore("public", nil, config)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.ListFormalConfigs(context.Background(), "21"); got != nil || !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatalf("missing read runner was not closed: %+v %v", got, err)
	}
	store, err = NewFormalConfigReadStore("public", ModelReadRunner(func(context.Context, func(*sql.Tx) error) error { return supply.ErrDependencyUnavailable }), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.ListFormalConfigs(context.Background(), "21"); got != nil || !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatalf("missing config reader was not closed: %+v %v", got, err)
	}
}

func TestFormalConfigReadStoreRejectsInvalidIdentityAndCanceledContext(t *testing.T) {
	store, err := NewFormalConfigReadStore("public", ModelReadRunner(func(context.Context, func(*sql.Tx) error) error { return nil }), &FormalConfigStore{schema: "public"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.ListFormalConfigs(context.Background(), "021"); got != nil || !errors.Is(err, supply.ErrReadContract) {
		t.Fatalf("invalid supplier ID accepted: %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := store.ListFormalConfigs(ctx, "21"); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read accepted: %+v %v", got, err)
	}
}
