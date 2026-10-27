package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	infra_middleware "model_bss/internal/infra/middleware"
)

type qualificationUploadBlobFake struct {
	file      *supply.SupplierQualificationFile
	opened    *supply.SupplierQualificationFile
	deleted   int
	openCalls int
	savedName string
	saveErr   error
}

func (f *qualificationUploadBlobFake) Save(_ context.Context, name string, content []byte) (*supply.SupplierQualificationFile, error) {
	f.savedName = name
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	file := *f.file
	file.Size = len(content)
	return &file, nil
}
func (f *qualificationUploadBlobFake) Open(context.Context, string) (*supply.SupplierQualificationFile, error) {
	f.openCalls++
	return f.opened, nil
}
func (f *qualificationUploadBlobFake) Delete(context.Context, string) error { f.deleted++; return nil }

type qualificationUploadStoreFake struct {
	profile       *supply.Supplier
	row           *supply.SupplierQualification
	ref           string
	authorizeCalls int
	createCalls   int
	metadata      *supply.SupplierQualificationMetadata
}

func (f *qualificationUploadStoreFake) AuthorizeSupplierQualificationUpload(_ context.Context, meta supply.WriteMeta, supplierID string) error {
	f.authorizeCalls++
	if f.profile == nil || f.profile.ID != supplierID || f.profile.BuyerID != meta.ActorID {
		return supply.ErrForbidden
	}
	return nil
}

func (f *qualificationUploadStoreFake) CreateQualification(_ context.Context, meta supply.WriteMeta, supplierID, name, ref string, at time.Time) (*supply.SupplierQualification, error) {
	f.createCalls++
	if f.profile == nil || f.profile.ID != supplierID || f.profile.BuyerID != meta.ActorID {
		return nil, supply.ErrForbidden
	}
	row := *f.row
	row.SupplierID, row.Name, row.FileReference, row.UploadedAt = supplierID, name, ref, at
	return &row, nil
}
func (f *qualificationUploadStoreFake) CreateQualificationWithMetadata(ctx context.Context, meta supply.WriteMeta, supplierID, name, ref string, details supply.SupplierQualificationMetadata, at time.Time) (*supply.SupplierQualification, error) {
	f.metadata = &details
	return f.CreateQualification(ctx, meta, supplierID, name, ref, at)
}
func (f *qualificationUploadStoreFake) UpdateQualificationMetadata(_ context.Context, meta supply.WriteMeta, supplierID, qualificationID string, expectedRevision uint64, name string, details supply.SupplierQualificationMetadata, at time.Time) (*supply.SupplierQualification, error) {
	if f.profile == nil || f.profile.ID != supplierID || f.profile.BuyerID != meta.ActorID || f.row == nil || f.row.ID != qualificationID {
		return nil, supply.ErrForbidden
	}
	f.metadata = &details
	row := *f.row
	revision := expectedRevision + 1
	row.SupplierID, row.Name, row.Revision, row.UploadedAt = supplierID, name, &revision, at
	row.QualificationType = details.Type
	validFrom, validTo := details.ValidFrom, details.ValidTo
	row.ValidFrom, row.ValidTo = &validFrom, &validTo
	return &row, nil
}
func (f *qualificationUploadStoreFake) QualificationFileReference(_ context.Context, actorID, supplierID, qualificationID string) (string, string, error) {
	if f.profile == nil || f.profile.ID != supplierID || f.profile.BuyerID != actorID || f.row == nil || f.row.ID != qualificationID {
		return "", "", supply.ErrForbidden
	}
	return f.ref, "营业执照", nil
}

func supplierQualificationFilesHTTPRouter(op *middleware.Operator, files *supply.SupplierQualificationService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if op != nil {
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	r.Use(infra_middleware.RequestID())
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{IdemStore: newSupplyHTTPIdemStore(), SupplierQualificationFiles: files})
	return r
}

func TestSupplierQualificationUploadAndDownloadRoutesDoNotExposeStorageReference(t *testing.T) {
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	fileContent := []byte("%PDF-1.7\nbody")
	file := &supply.SupplierQualificationFile{ID: "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "license.pdf", ContentType: "application/pdf", Size: len(fileContent), Content: fileContent}
	profile, err := supply.NewSupplier("21", "7", supply.SupplierData{Name: "供应商甲", Region: "CN", SettlementCurrency: "CNY"}, at)
	if err != nil {
		t.Fatal(err)
	}
	profile.BuyerID = "7"
	blobs := &qualificationUploadBlobFake{file: file, opened: file}
	store := &qualificationUploadStoreFake{profile: profile, row: &supply.SupplierQualification{ID: "81", SupplierID: "21", Name: "营业执照", HasAttachment: true, UploadedAt: at}, ref: file.ID}
	service := supply.NewSupplierQualificationService(store, blobs, func() time.Time { return at })
	router := supplierQualificationFilesHTTPRouter(supplyHTTPStaff(), service)
	body, err := json.Marshal(map[string]string{"name": "营业执照", "file_name": "license.pdf", "content_base64": base64.StdEncoding.EncodeToString(fileContent), "reason": "补充资质"})
	if err != nil {
		t.Fatal(err)
	}
	created := supplyHTTPCall(router, http.MethodPost, "/api/internal/supplier-entities/21/qualifications", string(body), "qualification-upload-once", "trace-qualification-upload")
	if created.Code != http.StatusOK || store.authorizeCalls != 1 || strings.Contains(created.Body.String(), "file_ref") || strings.Contains(created.Body.String(), "storage_path") || strings.Contains(created.Body.String(), file.ID) || !strings.Contains(created.Body.String(), `"can_download":true`) || !strings.Contains(created.Body.String(), `"file_name":"license.pdf"`) {
		t.Fatalf("upload response status=%d authorization checks=%d body=%s", created.Code, store.authorizeCalls, created.Body.String())
	}
	response := supplyHTTPCall(router, http.MethodGet, "/api/internal/supplier-entities/21/qualifications/81/file", "", "", "trace-qualification-download")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/pdf" || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Header().Get("Content-Disposition"), "license.pdf") || response.Body.String() != string(fileContent) {
		t.Fatalf("download response status=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
}

func TestSupplierQualificationDownloadRejectsNonOwnerBeforeOpeningBlob(t *testing.T) {
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	profile, err := supply.NewSupplier("21", "8", supply.SupplierData{Name: "供应商乙", Region: "CN", SettlementCurrency: "CNY"}, at)
	if err != nil {
		t.Fatal(err)
	}
	ref := "squal:v1:" + strings.Repeat("a", 32)
	content := []byte("%PDF-1.7\\nbody")
	blob := &supply.SupplierQualificationFile{ID: ref, Name: "license.pdf", ContentType: "application/pdf", Size: len(content), Content: content}
	blobs := &qualificationUploadBlobFake{opened: blob}
	store := &qualificationUploadStoreFake{profile: profile, row: &supply.SupplierQualification{ID: "81", SupplierID: "21", Name: "营业执照", HasAttachment: true}, ref: ref}
	service := supply.NewSupplierQualificationService(store, blobs, time.Now)
	router := supplierQualificationFilesHTTPRouter(supplyHTTPStaff(), service)
	rec := supplyHTTPCall(router, http.MethodGet, "/api/internal/supplier-entities/21/qualifications/81/file", "", "", "trace-qualification-download-non-owner")
	if rec.Code != http.StatusForbidden || blobs.openCalls != 0 || strings.Contains(rec.Body.String(), "license.pdf") || strings.Contains(rec.Body.String(), ref) {
		t.Fatalf("non-owner qualification download was not rejected before blob access: status=%d open=%d body=%s", rec.Code, blobs.openCalls, rec.Body.String())
	}
}

func TestSupplierQualificationListHidesDownloadFromNonOwner(t *testing.T) {
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	profile, err := supply.NewSupplier("21", "8", supply.SupplierData{Name: "供应商乙", Region: "CN", SettlementCurrency: "CNY"}, at)
	if err != nil {
		t.Fatal(err)
	}
	ref := "squal:v1:" + strings.Repeat("b", 32)
	reader := &supplierQualificationHTTPFake{profile: profile, fileRef: ref}
	content := []byte("%PDF-1.7\\nbody")
	blob := &supply.SupplierQualificationFile{ID: ref, Name: "license.pdf", ContentType: "application/pdf", Size: len(content), Content: content}
	blobs := &qualificationUploadBlobFake{opened: blob}
	files := supply.NewSupplierQualificationService(&qualificationUploadStoreFake{profile: profile, row: &supply.SupplierQualification{ID: "81", SupplierID: "21"}, ref: ref}, blobs, time.Now)
	rec := supplyHTTPCall(supplierQualificationHTTPRouter(supplyHTTPStaff(), reader, files), http.MethodGet, "/api/internal/supplier-entities/21/qualifications", "", "", "trace-qualification-list-non-owner")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"can_download":false`) || strings.Contains(rec.Body.String(), "license.pdf") || blobs.openCalls != 0 {
		t.Fatalf("non-owner qualification list exposed download metadata: status=%d open=%d body=%s", rec.Code, blobs.openCalls, rec.Body.String())
	}
}

func TestSupplierQualificationUploadRejectsUnauthenticatedAndBadPayload(t *testing.T) {
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	file := &supply.SupplierQualificationFile{ID: "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "license.pdf", ContentType: "application/pdf"}
	profile, _ := supply.NewSupplier("21", "7", supply.SupplierData{Name: "供应商甲", Region: "CN", SettlementCurrency: "CNY"}, at)
	blobs := &qualificationUploadBlobFake{file: file, saveErr: errors.New("invalid PDF bytes")}
	store := &qualificationUploadStoreFake{profile: profile, row: &supply.SupplierQualification{ID: "81", SupplierID: "21", Name: "营业执照", HasAttachment: true, UploadedAt: at}}
	service := supply.NewSupplierQualificationService(store, blobs, time.Now)
	router := supplierQualificationFilesHTTPRouter(nil, service)
	body := `{"name":"营业执照","file_name":"license.pdf","content_base64":"bm90IGEgcGRm","reason":"补充资质"}`
	if rec := supplyHTTPCall(router, http.MethodPost, "/api/internal/supplier-entities/21/qualifications", body, "qualification-upload-unauthorized", "trace-qualification-unauthorized"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated upload returned %d: %s", rec.Code, rec.Body.String())
	}
	badRouter := supplierQualificationFilesHTTPRouter(supplyHTTPStaff(), service)
	bad := supplyHTTPCall(badRouter, http.MethodPost, "/api/internal/supplier-entities/21/qualifications", body, "qualification-upload-invalid", "trace-qualification-invalid")
	if bad.Code != http.StatusBadRequest || store.createCalls != 0 {
		t.Fatalf("invalid file was accepted or reached storage: %d %s", bad.Code, bad.Body.String())
	}
}

func TestSupplierQualificationUploadRejectsNonOwnerBeforeWritingBlob(t *testing.T) {
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	profile, _ := supply.NewSupplier("21", "8", supply.SupplierData{Name: "供应商乙", Region: "CN", SettlementCurrency: "CNY"}, at)
	fileContent := []byte("%PDF-1.7\nbody")
	blobs := &qualificationUploadBlobFake{file: &supply.SupplierQualificationFile{ID: "squal:v1:" + strings.Repeat("a", 32), Name: "license.pdf", ContentType: "application/pdf"}}
	store := &qualificationUploadStoreFake{profile: profile, row: &supply.SupplierQualification{ID: "81", SupplierID: "21", Name: "营业执照", HasAttachment: true, UploadedAt: at}}
	service := supply.NewSupplierQualificationService(store, blobs, time.Now)
	router := supplierQualificationFilesHTTPRouter(supplyHTTPStaff(), service)
	body, err := json.Marshal(map[string]string{"name": "营业执照", "file_name": "license.pdf", "content_base64": base64.StdEncoding.EncodeToString(fileContent), "reason": "补充资质"})
	if err != nil {
		t.Fatal(err)
	}
	rec := supplyHTTPCall(router, http.MethodPost, "/api/internal/supplier-entities/21/qualifications", string(body), "qualification-upload-non-owner", "trace-qualification-upload-non-owner")
	if rec.Code != http.StatusForbidden || store.authorizeCalls != 1 || store.createCalls != 0 || blobs.savedName != "" {
		t.Fatalf("non-owner upload was not rejected before blob write: status=%d authorize=%d create=%d saved=%q body=%s", rec.Code, store.authorizeCalls, store.createCalls, blobs.savedName, rec.Body.String())
	}
}

func TestSupplierQualificationUploadForwardsMetadata(t *testing.T) {
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	file := &supply.SupplierQualificationFile{ID: "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "license.pdf", ContentType: "application/pdf"}
	profile, _ := supply.NewSupplier("21", "7", supply.SupplierData{Name: "供应商甲", Region: "CN", SettlementCurrency: "CNY"}, at)
	store := &qualificationUploadStoreFake{profile: profile, row: &supply.SupplierQualification{ID: "81", SupplierID: "21", Name: "营业执照", HasAttachment: true, UploadedAt: at}}
	blobs := &qualificationUploadBlobFake{file: file}
	service := supply.NewSupplierQualificationService(store, blobs, time.Now)
	router := supplierQualificationFilesHTTPRouter(supplyHTTPStaff(), service)
	encoded := base64.StdEncoding.EncodeToString([]byte("%PDF-1.7\nbody"))
	body := `{"name":"营业执照","qualification_type":"经营资质","valid_from":"2026-01-01","valid_to":"2027-01-01","file_name":"license.pdf","content_base64":"` + encoded + `","reason":"登记有效期"}`
	rec := supplyHTTPCall(router, http.MethodPost, "/api/internal/supplier-entities/21/qualifications", body, "qualification-upload-metadata", "trace-qualification-metadata")
	if rec.Code != http.StatusOK || store.metadata == nil || store.metadata.Type != "经营资质" || store.metadata.ValidFrom != "2026-01-01" || store.metadata.ValidTo != "2027-01-01" {
		t.Fatalf("qualification metadata was not forwarded: status=%d body=%s metadata=%+v", rec.Code, rec.Body.String(), store.metadata)
	}
}

func TestSupplierQualificationUploadDoesNotTreatPresentEmptyMetadataAsLegacyRequest(t *testing.T) {
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	fileContent := []byte("%PDF-1.7\nbody")
	cases := []struct {
		name     string
		metadata map[string]any
	}{
		{name: "empty fields", metadata: map[string]any{"qualification_type": "", "valid_from": "", "valid_to": ""}},
		{name: "null type", metadata: map[string]any{"qualification_type": nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := &supply.SupplierQualificationFile{ID: "squal:v1:" + strings.Repeat("a", 32), Name: "license.pdf", ContentType: "application/pdf"}
			profile, _ := supply.NewSupplier("21", "7", supply.SupplierData{Name: "供应商甲", Region: "CN", SettlementCurrency: "CNY"}, at)
			store := &qualificationUploadStoreFake{profile: profile, row: &supply.SupplierQualification{ID: "81", SupplierID: "21", Name: "营业执照", HasAttachment: true, UploadedAt: at}}
			blobs := &qualificationUploadBlobFake{file: file}
			service := supply.NewSupplierQualificationService(store, blobs, func() time.Time { return at })
			router := supplierQualificationFilesHTTPRouter(supplyHTTPStaff(), service)
			payload := map[string]any{
				"name": "营业执照", "file_name": "license.pdf", "content_base64": base64.StdEncoding.EncodeToString(fileContent), "reason": "补充资质",
			}
			for key, value := range tc.metadata {
				payload[key] = value
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			rec := supplyHTTPCall(router, http.MethodPost, "/api/internal/supplier-entities/21/qualifications", string(body), "qualification-upload-metadata-"+strings.ReplaceAll(tc.name, " ", "-"), "trace-qualification-metadata-empty")
			if rec.Code != http.StatusBadRequest || blobs.savedName != "" || store.createCalls != 0 || store.authorizeCalls != 0 {
				t.Fatalf("present invalid metadata fell back to legacy upload: status=%d saved=%q create=%d authorize=%d body=%s", rec.Code, blobs.savedName, store.createCalls, store.authorizeCalls, rec.Body.String())
			}
		})
	}
}

func TestSupplierQualificationUpdateForwardsValidityMetadata(t *testing.T) {
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	profile, _ := supply.NewSupplier("21", "7", supply.SupplierData{Name: "供应商甲", Region: "CN", SettlementCurrency: "CNY"}, at)
	store := &qualificationUploadStoreFake{profile: profile, row: &supply.SupplierQualification{ID: "81", SupplierID: "21", Name: "营业执照", HasAttachment: true}}
	service := supply.NewSupplierQualificationService(store, &qualificationUploadBlobFake{}, time.Now)
	router := supplierQualificationFilesHTTPRouter(supplyHTTPStaff(), service)
	body := `{"name":"营业执照","qualification_type":"经营资质","valid_from":"2026-02-01","valid_to":"2027-02-01","expected_revision":"3","reason":"更新有效期"}`
	rec := supplyHTTPCall(router, http.MethodPut, "/api/internal/supplier-entities/21/qualifications/81", body, "qualification-update-metadata", "trace-qualification-update-metadata")
	if rec.Code != http.StatusOK || store.metadata == nil || store.metadata.Type != "经营资质" || store.metadata.ValidFrom != "2026-02-01" || store.metadata.ValidTo != "2027-02-01" {
		t.Fatalf("qualification update metadata was not forwarded: status=%d body=%s metadata=%+v", rec.Code, rec.Body.String(), store.metadata)
	}
}

func TestSupplierQualificationDownloadRejectsLegacyPathReference(t *testing.T) {
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	profile, _ := supply.NewSupplier("21", "7", supply.SupplierData{Name: "供应商甲", Region: "CN", SettlementCurrency: "CNY"}, at)
	file := &supply.SupplierQualificationFile{ID: "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "license.pdf", ContentType: "application/pdf"}
	store := &qualificationUploadStoreFake{profile: profile, row: &supply.SupplierQualification{ID: "81", SupplierID: "21"}, ref: `C:\private\old-qualification.pdf`}
	blobs := &qualificationUploadBlobFake{file: file, opened: file}
	service := supply.NewSupplierQualificationService(store, blobs, time.Now)
	router := supplierQualificationFilesHTTPRouter(supplyHTTPStaff(), service)
	rec := supplyHTTPCall(router, http.MethodGet, "/api/internal/supplier-entities/21/qualifications/81/file", "", "", "trace-legacy-qualification")
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "private") || strings.Contains(rec.Body.String(), "file_ref") {
		t.Fatalf("legacy physical path was exposed/downloaded: %d %s", rec.Code, rec.Body.String())
	}
}
