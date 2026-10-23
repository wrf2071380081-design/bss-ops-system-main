package main

import (
	"context"
	"time"

	"gorm.io/gorm"

	"model_bss/internal/api"
	"model_bss/internal/infra/config"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/infra/supplysecurity"
)

// newSupplierSubmissionService 装配供应商申报（B-1）服务。
//
// 独立成文件：避免改动 A 的 supply_runtime.go，减少共享文件冲突。
// 加密密钥来自环境变量（MODEL_BSS_FORMAL_CONFIG_KEY），未配置时 sealer 为 nil，
// 此时「带 API Key 的写入」会返回 503，而不是把明文落库。
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
	sealer, err := supplysecurity.AESGCMSealerFromEnv()
	if err != nil {
		return nil, err
	}
	// 解密端口：未配置密钥或开关关闭时保持 nil，真实目标测试会明确返回 503。
	var decode func(ctx context.Context, stored, supplierID string) (string, error)
	if sealer != nil && cfg != nil && cfg.TestPlatform.AllowCredentialOpen {
		decode = func(ctx context.Context, stored, supplierID string) (string, error) {
			return supplysecurity.DecodeTestCredential(ctx, sealer, stored, supplierID)
		}
	}
	return api.NewSupplierSubmissionService(store, sealer, decode, starter, time.Now), nil
}
