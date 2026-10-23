package main

import (
	"errors"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"model_bss/internal/api"
	"model_bss/internal/domain/testplatform"
	"model_bss/internal/infra/config"
	"model_bss/internal/infra/supplypg"
	tpinfra "model_bss/internal/infra/testplatform"
)

// testPlatformRuntime 汇总 B 线测试对接所需的运行期对象。
//
// 拆成独立文件（而不是写进 A 的 supply_runtime.go）是为了减少与 A/C 两侧的
// 共享文件冲突：这里只新增、不改别人已有的装配代码。
type testPlatformRuntime struct {
	// Service 为空表示"出站未启用"（未配置地址/token），此时仍可读历史报告。
	Service *testplatform.Service
	// Reader 是 A 侧模型广场报告面板的数据源，注入后不再返回 503。
	Reader *supplypg.TestReportReader
}

// testJobStarter 把可能为空的运行期对象转成接口值。
// 直接传 (*Service)(nil) 会得到"非空接口"，路由就会注册出来然后 500，因此显式判空。
func testJobStarter(runtime *testPlatformRuntime) api.TestJobStarter {
	if runtime == nil || runtime.Service == nil {
		return nil
	}
	return runtime.Service
}

// newTestPlatformRuntime 构造测试平台运行期依赖。
//
// 失败策略：这是旁路能力，构造失败不应让服务起不来——
// 返回 (nil, err) 由调用方记录告警后继续启动。
func newTestPlatformRuntime(cfg *config.Config, db *gorm.DB, log *zap.Logger) (*testPlatformRuntime, error) {
	if db == nil {
		return nil, errors.New("test platform: nil database")
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	reader, err := supplypg.NewTestReportReader(raw, "public", cfg.TestPlatform.ReportDir)
	if err != nil {
		return nil, err
	}
	runtime := &testPlatformRuntime{Reader: reader}
	if !cfg.TestPlatform.Enable {
		log.Info("test platform disabled by config, report reader only")
		return runtime, nil
	}
	store, err := supplypg.NewTestPlatformStore(raw, "public", cfg.TestPlatform.ReportDir)
	if err != nil {
		return nil, err
	}
	gateway, err := tpinfra.NewGateway(tpinfra.Config{
		BaseURL: cfg.TestPlatform.BaseURL,
		Token:   cfg.TestPlatform.Token,
		Timeout: cfg.TestPlatform.Timeout,
	})
	if err != nil {
		// 地址或 token 缺失：保留读取能力，出站关闭（不阻断启动）。
		log.Warn("test platform outbound disabled: base_url/token not configured")
		return runtime, nil
	}
	service, err := testplatform.NewService(gateway, store, time.Now)
	if err != nil {
		return nil, err
	}
	runtime.Service = service
	log.Info("test platform outbound enabled", zap.String("base_url", cfg.TestPlatform.BaseURL))
	return runtime, nil
}
