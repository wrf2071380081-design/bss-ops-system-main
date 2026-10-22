package api

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

type supplySupplierHandler struct {
	writer SupplySupplierWriter
	reader *supply.SupplierReader
}

type supplySupplierDTO struct {
	ID                 string    `json:"id"`
	BuyerID            string    `json:"buyer_id"`
	Name               string    `json:"name"`
	Region             string    `json:"region"`
	RegistrationNo     string    `json:"registration_no"`
	Contact            string    `json:"contact"`
	Phone              string    `json:"phone"`
	TaxRate            string    `json:"tax_rate"`
	SettlementCurrency string    `json:"settlement_currency"`
	Established        bool      `json:"established"`
	Status             string    `json:"status"`
	Revision           uint64    `json:"revision,string"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	CanEdit            bool      `json:"can_edit"`
}

type supplySupplierPageDTO struct {
	List  []supplySupplierDTO `json:"list"`
	Total int64               `json:"total"`
	Page  int                 `json:"page"`
	Size  int                 `json:"size"`
}

type supplySupplierRequest struct {
	Name               string `json:"name"`
	Region             string `json:"region"`
	RegistrationNo     string `json:"registration_no"`
	Contact            string `json:"contact"`
	Phone              string `json:"phone"`
	TaxRate            string `json:"tax_rate"`
	SettlementCurrency string `json:"settlement_currency"`
	ExpectedRevision   string `json:"expected_revision"`
	Reason             string `json:"reason"`
}

func supplierDTO(profile *supply.Supplier, actor string) supplySupplierDTO {
	return supplySupplierDTO{ID: profile.ID, BuyerID: profile.BuyerID, Name: profile.Data.Name, Region: profile.Data.Region, RegistrationNo: profile.Data.RegistrationNo, Contact: profile.Data.Contact, Phone: profile.Data.Phone, TaxRate: profile.Data.TaxRate, SettlementCurrency: profile.Data.SettlementCurrency, Established: profile.Established, Status: profile.Status, Revision: profile.Revision, CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt, CanEdit: actor == profile.BuyerID}
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
	dto := supplySupplierPageDTO{List: make([]supplySupplierDTO, 0, len(result.List)), Total: result.Total, Page: result.Page, Size: result.Size}
	for i := range result.List {
		dto.List = append(dto.List, supplierDTO(&result.List[i], supplyActor(c)))
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
	response.Success(c, supplierDTO(profile, supplyActor(c)))
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
	dto := supplierDTO(profile, supplyActor(c))
	if _, err := strconv.ParseInt(dto.ID, 10, 64); err != nil {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	middleware.SetIdempotencyResult(c, dto)
	response.Success(c, dto)
}
