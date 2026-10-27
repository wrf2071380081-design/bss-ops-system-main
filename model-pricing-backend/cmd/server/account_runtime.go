package main

import (
	"gorm.io/gorm"

	"model_bss/internal/api"
	"model_bss/internal/infra/supplypg"
)

// newSupplierAccountService 装配供应商门户「账户设置」服务（M2，2026-10-27）。
//
// 独立成文件：避免改动 A/B 已有的 runtime 装配文件，减少共享文件冲突。
// 仓储走 supplypg（裸 SQL，schema 前缀），与 submission/onboarding 同一套写法。
func newSupplierAccountService(db *gorm.DB) (*api.SupplierAccountService, error) {
	if db == nil {
		return nil, nil
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	store, err := supplypg.NewAccountStore(raw, "public")
	if err != nil {
		return nil, err
	}
	return api.NewSupplierAccountService(store), nil
}
