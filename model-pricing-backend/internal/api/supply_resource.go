package api

import (
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

type supplyResourceHandler struct{ reader *supply.ResourceReader }

// @Summary 模型广场远程筛选选项
// @Tags supply-resources
// @Security BearerAuth
// @Param region query string false "CN/OVERSEAS"
// @Param keyword query string false "名称匹配；每类至多50项，has_more表示仍有匹配"
// @Success 200 {object} response.Body{data=supply.ResourceFilterOptions}
// @Failure 400,401,403,503 {object} response.Body
// @Router /api/internal/resources/filter-options [get]
func (h *supplyResourceHandler) options(c *gin.Context) {
	values, err := supplyQuery(c, "region", "keyword")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.Options(c.Request.Context(), values.Get("region"), values.Get("keyword"))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}

type supplyResourcePermissions struct {
	CanEdit bool `json:"can_edit"`
}
type supplyResourceDTO struct {
	*supply.ResourceSnapshot
	Permissions supplyResourcePermissions `json:"permissions"`
}

func resourceResponse(row *supply.ResourceSnapshot, actor string) supplyResourceDTO {
	return supplyResourceDTO{ResourceSnapshot: row, Permissions: supplyResourcePermissions{CanEdit: row.BuyerID == actor}}
}

type supplyResourcePageDTO struct {
	View  string `json:"view"`
	List  any    `json:"list"`
	Total int64  `json:"total"`
	Page  int    `json:"page"`
	Size  int    `json:"size"`
}

// @Summary 模型广场或供应资源列表
// @Tags supply-resources
// @Security BearerAuth
// @Param view query string false "grouped(默认)/flat"
// @Param page query int false "页码"
// @Param size query int false "每页1至100"
// @Param company query string false "模型公司"
// @Param model_type query string false "模型类型"
// @Param supplier_id query string false "供应商ID"
// @Param standard_model_id query string false "标准模型ID，展开来源时保留其他筛选"
// @Param region query string false "CN/OVERSEAS"
// @Param state query string false "资源状态"
// @Param keyword query string false "关键词"
// @Param sort_by query string false "随view校验的排序白名单"
// @Param order query string false "asc/desc"
// @Success 200 {object} response.Body{data=supplyResourcePageDTO}
// @Failure 400,401,403,503 {object} response.Body
// @Router /api/internal/resources [get]
func (h *supplyResourceHandler) list(c *gin.Context) {
	values, err := supplyQuery(c, "view", "page", "size", "company", "model_type", "supplier_id", "standard_model_id", "region", "state", "keyword", "sort_by", "order", "companies", "model_types", "supplier_ids", "regions", "states", "coefficient_min", "coefficient_max", "connection_min", "connection_max", "rps_min", "rps_max", "tps_min", "tps_max")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	query := supply.ResourceListQuery{View: values.Get("view"), Page: page, Size: size, Company: values.Get("company"), ModelType: values.Get("model_type"), SupplierID: values.Get("supplier_id"), StandardModelID: values.Get("standard_model_id"), Region: values.Get("region"), State: values.Get("state"), Keyword: values.Get("keyword"), SortBy: values.Get("sort_by"), Order: values.Get("order")}
	arrays := []struct {
		key    string
		target *[]string
	}{{"companies", &query.Companies}, {"model_types", &query.ModelTypes}, {"supplier_ids", &query.SupplierIDs}, {"regions", &query.Regions}, {"states", &query.States}}
	for _, array := range arrays {
		v, e := supplyModelArray(values, array.key)
		if e != nil {
			supplyHTTPError(c, e)
			return
		}
		*array.target = v
	}
	query.CoefficientMin = values.Get("coefficient_min")
	query.CoefficientMax = values.Get("coefficient_max")
	query.ConnectionMin = values.Get("connection_min")
	query.ConnectionMax = values.Get("connection_max")
	query.RPSMin = values.Get("rps_min")
	query.RPSMax = values.Get("rps_max")
	query.TPSMin = values.Get("tps_min")
	query.TPSMax = values.Get("tps_max")
	result, err := h.reader.List(c.Request.Context(), query)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	var rows any = result.Groups
	if result.View == "flat" {
		flat := make([]supplyResourceDTO, 0, len(result.Resources))
		for _, resource := range result.Resources {
			flat = append(flat, resourceResponse(resource, supplyActor(c)))
		}
		rows = flat
	}
	response.Success(c, supplyResourcePageDTO{View: result.View, List: rows, Total: result.Total, Page: result.Page, Size: result.Size})
}

// @Summary 供应资源安全详情
// @Tags supply-resources
// @Security BearerAuth
// @Param id path string true "资源bigint字符串ID"
// @Param region query string false "当前区域，跨区域返回404"
// @Success 200 {object} response.Body{data=supplyResourceDTO}
// @Failure 400,401,403,404,503 {object} response.Body
// @Router /api/internal/resources/{id} [get]
func (h *supplyResourceHandler) get(c *gin.Context) {
	values, err := supplyQuery(c, "region")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	row, err := h.reader.Get(c.Request.Context(), c.Param("id"), values.Get("region"))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, resourceResponse(row, supplyActor(c)))
}
