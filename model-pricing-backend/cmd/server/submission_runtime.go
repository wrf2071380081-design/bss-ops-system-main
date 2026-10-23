package main

import (
	"time"

	"gorm.io/gorm"

	"model_bss/internal/api"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/infra/supplysecurity"
)

// newSupplierSubmissionService 装配供应商申报（B-1）服务。
//
// 独立成文件：避免改动 A 的 supply_runtime.go，减少共享文件冲突。
// 加密密钥来自环境变量（MODEL_BSS_FORMAL_CONFIG_KEY），未配置时 sealer 为 nil，
// 此时「带 API Key 的写入」会返回 503，而不是把明文落库。
func newSupplierSubmissionService(db *gorm.DB, starter api.TestJobStarter) (*api.SupplierSubmissionService, error) {
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
	sealer, err := supplysecurity.AESGCMSealerFromEnv()
	if err != nil {
		return nil, err
	}
	return api.NewSupplierSubmissionService(store, sealer, starter, time.Now), nil
}
