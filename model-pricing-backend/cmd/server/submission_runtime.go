package main

import (
	"time"

	"gorm.io/gorm"

	"model_bss/internal/api"
	"model_bss/internal/infra/config"
	"model_bss/internal/infra/supplypg"
)

// newSupplierSubmissionService 装配供应商申报（B-1）服务。
//
// 独立成文件：避免改动 A 的 supply_runtime.go，减少共享文件冲突。
// 凭据策略为路线 A：Key 原样入库（A 侧重构后加密层已移除），因此这里不再注入 sealer。
func newSupplierSubmissionService(db *gorm.DB, cfg *config.Config, starter api.TestJobStarter) (*api.SupplierSubmissionService, error) {
	if db == nil {
		return nil, nil
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	store, err := supplypg.NewSubmissionStore(raw, "public")
	if err != nil {
		return nil, err
	}
	return api.NewSupplierSubmissionService(store, starter, time.Now), nil
}

// newSupplierCredentialService 装配供应商**主体级**测试凭据（000044，原型对齐）。
//
// 与申报服务分开构造：凭据的生命周期比单张申报长，且「改凭据 → 刷新未提交申报快照」
// 这个副作用只归它管，混进申报服务会让依赖方向变乱。
func newSupplierCredentialService(db *gorm.DB) (*api.SupplierCredentialService, error) {
	if db == nil {
		return nil, nil
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	store, err := supplypg.NewSupplierCredentialStore(raw, "public")
	if err != nil {
		return nil, err
	}
	return api.NewSupplierCredentialService(store), nil
}
