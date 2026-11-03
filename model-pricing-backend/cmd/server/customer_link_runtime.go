// Package main 的 customer_link_runtime.go：装配客户「接入模型」绑定服务（G4 地基，000049）。
//
// 独立成文件：与 account_runtime.go 同样的理由——避免改动已有的 A/B runtime 装配文件，
// 减少共享文件冲突。仓储走 supplypg（裸 SQL + schema 前缀），与其它 supplypg 用法一致。
package main

import (
	"gorm.io/gorm"

	"model_bss/internal/api"
	"model_bss/internal/domain/customer"
	"model_bss/internal/infra/supplypg"
)

// newCustomerResourceLinkService 构造客户绑定服务。
// 返回 nil 表示未装配（路由不注册），由调用方降级为非致命告警。
func newCustomerResourceLinkService(db *gorm.DB) (*customer.ResourceLinkService, error) {
	if db == nil {
		return nil, nil
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	store, err := supplypg.NewCustomerResourceLinkStore(raw, "public")
	if err != nil {
		return nil, err
	}
	return customer.NewResourceLinkService(store), nil
}

// newCustomerOnboardingService 构造客户开通流程服务（G4-B2）。
//
// readiness 来自 supply runtime 的 F12 就绪读取端口；nil 时合同授信节点一律不可完成
// （明确报「能力未装配」，不伪造就绪数据）。
func newCustomerOnboardingService(db *gorm.DB, readiness api.SupplyF12ReadinessReader) (*customer.OnboardingService, error) {
	if db == nil {
		return nil, nil
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	links, err := supplypg.NewCustomerResourceLinkStore(raw, "public")
	if err != nil {
		return nil, err
	}
	store, err := supplypg.NewCustomerOnboardingStore(raw, "public", links)
	if err != nil {
		return nil, err
	}
	return customer.NewOnboardingService(store, links, api.NewCustomerContractReadiness(readiness)), nil
}

// newCustomerRegistryService 构造客户创建/基础档案服务（G4-B1）。
// 返回 nil 表示未装配（POST /customers、GET /customers/:id 不注册）。
func newCustomerRegistryService(db *gorm.DB) (*customer.RegistryService, error) {
	if db == nil {
		return nil, nil
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	store, err := supplypg.NewCustomerRegistryStore(raw, "public")
	if err != nil {
		return nil, err
	}
	return customer.NewRegistryService(store), nil
}
