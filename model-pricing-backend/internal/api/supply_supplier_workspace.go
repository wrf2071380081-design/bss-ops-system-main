package api

import (
	"net/url"
	"strings"
)

func parseSupplierWorkspaceQuery(values url.Values) (SupplierWorkspaceQuery, error) {
	page, size, err := supplyPageParams(values)
	if err != nil {
		return SupplierWorkspaceQuery{}, err
	}
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if size > 100 || page > int(^uint(0)>>1)/size {
		return SupplierWorkspaceQuery{}, supplyRequestError("page", "pagination is out of range")
	}
	region := strings.TrimSpace(values.Get("region"))
	if region != "" && region != "CN" && region != "OVERSEAS" {
		return SupplierWorkspaceQuery{}, supplyRequestError("region", "expected CN or OVERSEAS")
	}
	contractStatus := strings.TrimSpace(values.Get("contract_status"))
	if contractStatus != "" && contractStatus != "办理中" && contractStatus != "已归档" && contractStatus != "未登记" {
		return SupplierWorkspaceQuery{}, supplyRequestError("contract_status", "unsupported contract status")
	}
	cooperationStage := strings.TrimSpace(values.Get("cooperation_stage"))
	if cooperationStage != "" && cooperationStage != "潜在" && cooperationStage != "已有" {
		return SupplierWorkspaceQuery{}, supplyRequestError("cooperation_stage", "unsupported cooperation stage")
	}
	handlingStatus := strings.TrimSpace(values.Get("handling_status"))
	if handlingStatus != "" && handlingStatus != "有未完成事项" && handlingStatus != "无未完成事项" {
		return SupplierWorkspaceQuery{}, supplyRequestError("handling_status", "unsupported handling status")
	}
	sortBy := strings.TrimSpace(values.Get("sort_by"))
	if sortBy == "" {
		sortBy = "id"
	}
	sortColumns := map[string]bool{"id": true, "name": true, "region": true, "resource_count": true, "contract_status": true, "cooperation_stage": true, "handling_status": true}
	if !sortColumns[sortBy] {
		return SupplierWorkspaceQuery{}, supplyRequestError("sort_by", "unsupported supplier sort field")
	}
	sortOrder := strings.ToLower(strings.TrimSpace(values.Get("sort_order")))
	if sortOrder == "" {
		sortOrder = "asc"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		return SupplierWorkspaceQuery{}, supplyRequestError("sort_order", "expected asc or desc")
	}
	return SupplierWorkspaceQuery{
		Keyword: strings.TrimSpace(values.Get("keyword")), Region: region, ContractStatus: contractStatus,
		CooperationStage: cooperationStage, HandlingStatus: handlingStatus,
		SortBy: sortBy, SortOrder: sortOrder, Page: page, Size: size,
	}, nil
}
