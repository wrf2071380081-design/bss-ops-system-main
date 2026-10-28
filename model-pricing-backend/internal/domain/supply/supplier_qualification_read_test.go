package supply

import (
	"context"
	"errors"
	"testing"
	"time"
)

type supplierQualificationReadMemory struct {
	profile *Supplier
	rows    []SupplierQualification
	query   SupplierQualificationQuery
	err     error
}

func (s *supplierQualificationReadMemory) Get(_ context.Context, id string) (*Supplier, error) {
	if s.profile == nil || s.profile.ID != id {
		return nil, ErrNotFound
	}
	return s.profile.Clone(), nil
}

func (s *supplierQualificationReadMemory) List(context.Context, SupplierListQuery) (*SupplierPage, error) {
	return &SupplierPage{}, nil
}

func (s *supplierQualificationReadMemory) ListQualifications(_ context.Context, id string, query SupplierQualificationQuery) (*SupplierQualificationPage, error) {
	s.query = query
	if s.err != nil {
		return nil, s.err
	}
	if s.profile == nil || s.profile.ID != id {
		return nil, ErrNotFound
	}
	start := (query.Page - 1) * query.Size
	if start > len(s.rows) {
		start = len(s.rows)
	}
	end := start + query.Size
	if end > len(s.rows) {
		end = len(s.rows)
	}
	return &SupplierQualificationPage{List: append([]SupplierQualification(nil), s.rows[start:end]...), Total: int64(len(s.rows)), Page: query.Page, Size: query.Size}, nil
}

func TestSupplierQualificationsReadUsesSafeMetadataAndPagination(t *testing.T) {
	profile, err := NewSupplier("11", "7", supplierProfileData(), modelNow)
	if err != nil {
		t.Fatal(err)
	}
	store := &supplierQualificationReadMemory{profile: profile, rows: []SupplierQualification{
		{ID: "20", SupplierID: "11", Name: "营业执照", HasAttachment: true, UploadedAt: modelNow.Add(time.Hour)},
		{ID: "19", SupplierID: "11", Name: "开户许可证", HasAttachment: false, UploadedAt: modelNow},
	}}
	reader := NewSupplierReader(store)
	page, err := reader.ListQualifications(context.Background(), "11", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if store.query.Page != 2 || store.query.Size != 1 || page.Total != 2 || page.Page != 2 || page.Size != 1 || len(page.List) != 1 || page.List[0].ID != "19" || page.List[0].HasAttachment {
		t.Fatalf("qualification metadata or pagination was lost: query=%+v page=%+v", store.query, page)
	}
	if page.List[0].Name != "开户许可证" || page.List[0].SupplierID != "11" || !page.List[0].UploadedAt.Equal(modelNow) {
		t.Fatalf("unexpected safe qualification projection: %+v", page.List[0])
	}
}

func TestSupplierQualificationsReadRejectsUnknownSupplierAndBadPagination(t *testing.T) {
	profile, err := NewSupplier("11", "7", supplierProfileData(), modelNow)
	if err != nil {
		t.Fatal(err)
	}
	store := &supplierQualificationReadMemory{profile: profile}
	reader := NewSupplierReader(store)
	if _, err := reader.ListQualifications(context.Background(), "12", 1, 20); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown supplier qualification read should be not found, got %v", err)
	}
	for _, tc := range []struct{ page, size int }{{-1, 20}, {1, 101}} {
		if _, err := reader.ListQualifications(context.Background(), "11", tc.page, tc.size); err == nil {
			t.Fatalf("invalid pagination accepted: page=%d size=%d", tc.page, tc.size)
		}
	}
}
