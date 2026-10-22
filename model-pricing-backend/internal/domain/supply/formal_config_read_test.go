package supply

import (
	"context"
	"errors"
	"testing"
)

type formalConfigReadMemory struct {
	views       []*FormalConfigView
	gotSupplier string
	err         error
}

func (f *formalConfigReadMemory) ListFormalConfigs(_ context.Context, supplierID string) ([]*FormalConfigView, error) {
	f.gotSupplier = supplierID
	return f.views, f.err
}

func formalConfigReadView(id, supplierID, status string, revision uint64) *FormalConfigView {
	now := modelNow
	confirmed := revision
	if status == "INACTIVE" && revision > 1 {
		confirmed--
	}
	return &FormalConfigView{ID: id, SupplierID: supplierID, Region: "CN", Scope: "生产接口", BaseURL: "https://api.vendor.example/v1", HasAPIKey: true, APIKeyMask: "********", Status: status, Revision: revision, CreatedBy: "7", UpdatedBy: "7", CreatedAt: now, UpdatedAt: now, ConfirmedBy: "7", ConfirmedAt: now, ConfirmedRevision: confirmed}
}

func TestFormalConfigReaderReturnsDetachedSafeViews(t *testing.T) {
	first := formalConfigReadView("91", "21", "ACTIVE", 2)
	second := formalConfigReadView("92", "21", "INACTIVE", 3)
	store := &formalConfigReadMemory{views: []*FormalConfigView{first, second}}
	reader := NewFormalConfigReader(store)
	got, err := reader.List(context.Background(), "21")
	if err != nil || len(got) != 2 || store.gotSupplier != "21" {
		t.Fatalf("list failed: %+v %v", got, err)
	}
	got[0].Scope = "mutated"
	if first.Scope != "生产接口" || got[1].APIKeyMask != "********" {
		t.Fatal("reader aliased storage view or exposed an unsafe projection")
	}
}

func TestFormalConfigReaderFailsClosedForInvalidOrCorruptRows(t *testing.T) {
	for _, tc := range []struct {
		name     string
		supplier string
		views    []*FormalConfigView
		err      error
		want     error
	}{
		{name: "invalid supplier", supplier: "021"},
		{name: "cross supplier", supplier: "21", views: []*FormalConfigView{formalConfigReadView("91", "22", "ACTIVE", 1)}, want: ErrReadContract},
		{name: "duplicate", supplier: "21", views: []*FormalConfigView{formalConfigReadView("91", "21", "ACTIVE", 1), formalConfigReadView("91", "21", "ACTIVE", 1)}, want: ErrReadContract},
		{name: "missing mask", supplier: "21", views: []*FormalConfigView{{ID: "91", SupplierID: "21", Region: "CN", Scope: "scope", BaseURL: "https://api.example", HasAPIKey: true, Status: "ACTIVE", Revision: 1, ConfirmedRevision: 1, CreatedBy: "7", UpdatedBy: "7", ConfirmedBy: "7", CreatedAt: modelNow, UpdatedAt: modelNow, ConfirmedAt: modelNow}}, want: ErrReadContract},
		{name: "store error", supplier: "21", err: errors.New("store"), want: errors.New("store")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &formalConfigReadMemory{views: tc.views, err: tc.err}
			got, err := NewFormalConfigReader(store).List(context.Background(), tc.supplier)
			if got != nil || err == nil {
				t.Fatalf("expected fail-closed result, got=%+v err=%v", got, err)
			}
			if tc.name == "store error" {
				if err.Error() != tc.want.Error() {
					t.Fatalf("wrong store error %v", err)
				}
				return
			}
			if tc.name == "invalid supplier" {
				var field *FieldError
				if !errors.As(err, &field) {
					t.Fatalf("expected field error, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestFormalConfigReaderHonorsCanceledContextAndMissingStore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := NewFormalConfigReader(&formalConfigReadMemory{}).List(ctx, "21"); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read was not stopped: %+v %v", got, err)
	}
	if got, err := NewFormalConfigReader(nil).List(context.Background(), "21"); got != nil || !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("missing store was not closed: %+v %v", got, err)
	}
}
