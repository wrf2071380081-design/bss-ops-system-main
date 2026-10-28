package api

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

type supplySupplierHandler struct {
	writer             SupplySupplierWriter
	reader             *supply.SupplierReader
	qualificationFiles *supply.SupplierQualificationService
}

const supplierQualificationRequestMaxBytes = 3 << 20

type supplierQualificationUploadRequest struct {
	Name              string          `json:"name"`
	QualificationType json.RawMessage `json:"qualification_type"`
	ValidFrom         json.RawMessage `json:"valid_from"`
	ValidTo           json.RawMessage `json:"valid_to"`
	FileName          string          `json:"file_name"`
	ContentBase64     string          `json:"content_base64"`
	Reason            string          `json:"reason"`
}

type supplierQualificationUpdateRequest struct {
	Name              string `json:"name"`
	QualificationType string `json:"qualification_type"`
	ValidFrom         string `json:"valid_from"`
	ValidTo           string `json:"valid_to"`
	ExpectedRevision  string `json:"expected_revision"`
	Reason            string `json:"reason"`
}

type supplySupplierDTO struct {
	ID                 string    `json:"id"`
	BuyerID            string    `json:"buyer_id"`
	Name               string    `json:"name"`
	Aliases            []string  `json:"aliases"`
	AliasesSupported   bool      `json:"aliases_supported"`
	Region             string    `json:"region"`
	RegistrationNo     string    `json:"registration_no"`
	Contact            string    `json:"contact"`
	Phone              string    `json:"phone"`
	TaxRate            *string   `json:"tax_rate"`
	SettlementCurrency string    `json:"settlement_currency"`
	Established        bool      `json:"established"`
	Status             string    `json:"status"`
	Revision           uint64    `json:"revision,string"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	CanEdit            bool      `json:"can_edit"`
}

type supplySupplierPageDTO struct {
	List             []supplySupplierDTO `json:"list"`
	Total            int64               `json:"total"`
	Page             int                 `json:"page"`
	Size             int                 `json:"size"`
	AliasesSupported bool                `json:"aliases_supported"`
}

type supplySupplierRequest struct {
	Name               string  `json:"name"`
	Region             string  `json:"region"`
	RegistrationNo     string  `json:"registration_no"`
	Contact            string  `json:"contact"`
	Phone              string  `json:"phone"`
	TaxRate            *string `json:"tax_rate"`
	SettlementCurrency string  `json:"settlement_currency"`
	ExpectedRevision   string  `json:"expected_revision"`
	Reason             string  `json:"reason"`
}

func supplierDTO(profile *supply.Supplier, actor string, aliasesSupported bool) supplySupplierDTO {
	aliases := append([]string{}, profile.Aliases...)
	return supplySupplierDTO{ID: profile.ID, BuyerID: profile.BuyerID, Name: profile.Data.Name, Aliases: aliases, AliasesSupported: aliasesSupported, Region: profile.Data.Region, RegistrationNo: profile.Data.RegistrationNo, Contact: profile.Data.Contact, Phone: profile.Data.Phone, TaxRate: profile.Data.TaxRate, SettlementCurrency: profile.Data.SettlementCurrency, Established: profile.Established, Status: profile.Status, Revision: profile.Revision, CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt, CanEdit: actor == profile.BuyerID}
}

func (h *supplySupplierHandler) list(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	values, err := supplyQuery(c, "page", "size", "keyword", "region")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.List(c.Request.Context(), supply.SupplierListQuery{Page: page, Size: size, Keyword: values.Get("keyword"), Region: values.Get("region")})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	aliasesSupported, err := h.reader.AliasesSupported(c.Request.Context())
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	dto := supplySupplierPageDTO{List: make([]supplySupplierDTO, 0, len(result.List)), Total: result.Total, Page: result.Page, Size: result.Size, AliasesSupported: aliasesSupported}
	for i := range result.List {
		dto.List = append(dto.List, supplierDTO(&result.List[i], supplyActor(c), aliasesSupported))
	}
	response.Success(c, dto)
}

func (h *supplySupplierHandler) get(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	id := c.Param("id")
	if _, err := planPositiveID(id); err != nil {
		supplyHTTPError(c, err)
		return
	}
	profile, err := h.reader.Get(c.Request.Context(), id)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	aliasesSupported, err := h.reader.AliasesSupported(c.Request.Context())
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, supplierDTO(profile, supplyActor(c), aliasesSupported))
}

func (h *supplySupplierHandler) qualifications(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	values, err := supplyQuery(c, "page", "size")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	id := c.Param("id")
	if _, err := planPositiveID(id); err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.ListQualifications(c.Request.Context(), id, page, size)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	profile, err := h.reader.Get(c.Request.Context(), id)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	canDownload := profile.BuyerID == supplyActor(c)
	for i := range result.List {
		if result.List[i].SupplierID != id {
			supplyHTTPError(c, supply.ErrReadContract)
			return
		}
		if !canDownload {
			result.List[i].CanDownload = false
			result.List[i].FileName = ""
			continue
		}
		if h.qualificationFiles != nil {
			fileName, canDownload := h.qualificationFiles.DownloadableFileName(c.Request.Context(), result.List[i].FileReference)
			result.List[i].CanDownload = canDownload
			if canDownload {
				result.List[i].FileName = fileName
			}
		}
	}
	response.Success(c, result)
}

func (h *supplySupplierHandler) uploadQualification(c *gin.Context) {
	if h == nil || h.qualificationFiles == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	supplierID := c.Param("id")
	if _, err := planPositiveID(supplierID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req supplierQualificationUploadRequest
	if err := decodeSupplyRequestLimit(c, &req, []string{"name", "file_name", "content_base64", "reason"}, supplierQualificationRequestMaxBytes); err != nil {
		supplyHTTPError(c, err)
		return
	}
	content, err := base64.StdEncoding.Strict().DecodeString(req.ContentBase64)
	if err != nil || len(content) == 0 || len(content) > supply.SupplierQualificationMaxFileBytes {
		supplyHTTPError(c, supplyRequestError("file", "invalid qualification attachment or size exceeds 2 MiB"))
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	qualificationType, err := supplierQualificationText(req.QualificationType, "qualification_type")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	validFrom, err := supplierQualificationText(req.ValidFrom, "valid_from")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	validTo, err := supplierQualificationText(req.ValidTo, "valid_to")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	var row *supply.SupplierQualification
	metadataProvided := len(req.QualificationType) > 0 || len(req.ValidFrom) > 0 || len(req.ValidTo) > 0
	if metadataProvided {
		row, err = h.qualificationFiles.UploadWithMetadata(c.Request.Context(), meta, supplierID, req.Name, req.FileName, content, supply.SupplierQualificationMetadata{Type: qualificationType, ValidFrom: validFrom, ValidTo: validTo})
	} else {
		row, err = h.qualificationFiles.Upload(c.Request.Context(), meta, supplierID, req.Name, req.FileName, content)
	}
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if row == nil || row.ID == "" || row.SupplierID != supplierID || !row.HasAttachment || !row.CanDownload {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	middleware.SetIdempotencyResult(c, row)
	response.Success(c, row)
}

func supplierQualificationText(raw json.RawMessage, field string) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return "", supplyRequestError(field, "expected text when the field is provided")
	}
	return *value, nil
}

func (h *supplySupplierHandler) updateQualification(c *gin.Context) {
	if h == nil || h.qualificationFiles == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	supplierID, qualificationID := c.Param("id"), c.Param("qualificationId")
	if _, err := planPositiveID(supplierID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(qualificationID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req supplierQualificationUpdateRequest
	if err := decodeSupplyRequest(c, &req, []string{"name", "qualification_type", "valid_from", "valid_to", "expected_revision", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := strconv.ParseUint(req.ExpectedRevision, 10, 64)
	if err != nil || expected == 0 || expected >= math.MaxInt32 || strconv.FormatUint(expected, 10) != req.ExpectedRevision {
		supplyHTTPError(c, supplyRequestError("expected_revision", "expected a current positive revision"))
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	row, err := h.qualificationFiles.UpdateMetadata(c.Request.Context(), meta, supplierID, qualificationID, expected, req.Name, supply.SupplierQualificationMetadata{Type: req.QualificationType, ValidFrom: req.ValidFrom, ValidTo: req.ValidTo})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if row == nil || row.ID != qualificationID || row.SupplierID != supplierID || row.Revision == nil || *row.Revision != expected+1 {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	middleware.SetIdempotencyResult(c, row)
	response.Success(c, row)
}

func (h *supplySupplierHandler) qualificationFile(c *gin.Context) {
	if h == nil || h.qualificationFiles == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	supplierID, qualificationID := c.Param("id"), c.Param("qualificationId")
	if _, err := planPositiveID(supplierID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(qualificationID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	file, err := h.qualificationFiles.Download(c.Request.Context(), supplyActor(c), supplierID, qualificationID)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if file == nil || file.Size != len(file.Content) || file.Size < 1 || file.Size > supply.SupplierQualificationMaxFileBytes || file.Name == "" || strings.ContainsAny(file.Name, `/\\`) {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	contentType := ""
	switch file.ContentType {
	case "application/pdf", "image/png", "image/jpeg":
		contentType = file.ContentType
	default:
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.Name})
	if disposition == "" {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", disposition)
	c.Data(http.StatusOK, contentType, file.Content)
}

func (h *supplySupplierHandler) ready(c *gin.Context) bool {
	if h == nil || h.writer == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return false
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return false
	}
	return true
}

func (h *supplySupplierHandler) create(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	var req supplySupplierRequest
	if err := decodeSupplyRequest(c, &req, []string{"name", "region", "tax_rate", "settlement_currency", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	profile, err := h.writer.Create(c.Request.Context(), meta, supplierData(req))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if profile == nil || profile.ID == "" || profile.BuyerID != meta.ActorID || profile.Revision != 1 || profile.Established || profile.Status != "ACTIVE" || profile.CreatedAt.IsZero() || profile.UpdatedAt.Before(profile.CreatedAt) {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	h.result(c, profile)
}

func (h *supplySupplierHandler) update(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	id := c.Param("id")
	if _, err := planPositiveID(id); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req supplySupplierRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "name", "region", "tax_rate", "settlement_currency", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedRevision)
	if err != nil {
		supplyHTTPError(c, supplyRequestError("expected_revision", "invalid revision"))
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	profile, err := h.writer.Update(c.Request.Context(), meta, id, expected, supplierData(req))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if profile == nil || profile.ID != id || profile.Revision < expected || profile.Revision > expected+1 || expected >= math.MaxInt64 || profile.BuyerID == "" {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	h.result(c, profile)
}

func supplierData(req supplySupplierRequest) supply.SupplierData {
	return supply.SupplierData{Name: req.Name, Region: req.Region, RegistrationNo: req.RegistrationNo, Contact: req.Contact, Phone: req.Phone, TaxRate: req.TaxRate, SettlementCurrency: req.SettlementCurrency}
}

func (h *supplySupplierHandler) result(c *gin.Context, profile *supply.Supplier) {
	aliasesSupported, err := h.reader.AliasesSupported(c.Request.Context())
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	dto := supplierDTO(profile, supplyActor(c), aliasesSupported)
	if _, err := strconv.ParseInt(dto.ID, 10, 64); err != nil {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	middleware.SetIdempotencyResult(c, dto)
	response.Success(c, dto)
}
