package main

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"model_bss/internal/api"
	"model_bss/internal/domain/testplatform"
	"model_bss/internal/infra/config"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/infra/supplysecurity"
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
	// Receiver 处理平台终态回调；未配置 webhook secret 时为空（只靠轮询）。
	Receiver *tpinfra.Receiver
	// Store 供内部复测读取资源测试目标（T-10）。
	Store *supplypg.TestPlatformStore
	// Decode 解密测试凭据；为 nil 表示禁止解密。
	Decode func(ctx context.Context, stored, supplierID string) (string, error)
}

// resourceTestTargets 把存储层的资源测试目标适配成 api 侧端口。
type resourceTestTargets struct{ store *supplypg.TestPlatformStore }

func (r resourceTestTargets) TestTarget(ctx context.Context, resourceID string) (*api.ResourceTestTarget, error) {
	target, err := r.store.ResourceTestConfig(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return &api.ResourceTestTarget{
		Model: target.Model, BaseURL: target.BaseURL, SealedAPIKey: target.SealedAPIKey,
		SupplierID: target.SupplierID, SupplierName: target.SupplierName,
	}, nil
}

// Targets 返回内部复测用的目标加载器；未构造存储时返回 nil。
func (r *testPlatformRuntime) Targets() api.ResourceTestTargetLoader {
	if r == nil || r.Store == nil {
		return nil
	}
	return resourceTestTargets{store: r.Store}
}

// testCallbackReceiver 把可能为空的接收器转成接口值（避免"非空接口、空实现"）。
func testCallbackReceiver(runtime *testPlatformRuntime) api.TestCallbackReceiver {
	if runtime == nil || runtime.Receiver == nil {
		return nil
	}
	return runtime.Receiver
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
	runtime.Store = store
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
	// 解密端口（T-10 内部复测要还原真实目标凭据）。
	if sealer, sealErr := supplysecurity.AESGCMSealerFromEnv(); sealErr == nil && sealer != nil && cfg.TestPlatform.AllowCredentialOpen {
		runtime.Decode = func(ctx context.Context, stored, supplierID string) (string, error) {
			return supplysecurity.DecodeTestCredential(ctx, sealer, stored, supplierID)
		}
	}
	receiver, err := tpinfra.NewReceiver(cfg.TestPlatform.WebhookSecret, service, time.Now)
	if err != nil {
		log.Warn("test platform callback disabled: webhook secret not configured")
		return runtime, nil
	}
	runtime.Receiver = receiver
	log.Info("test platform callback enabled")
	return runtime, nil
}

// testTargets 把运行期对象转成接口值（nil 安全）。
func testTargets(runtime *testPlatformRuntime) api.ResourceTestTargetLoader {
	if runtime == nil {
		return nil
	}
	return runtime.Targets()
}

// testCredentialDecoder 返回解密端口；未启用时返回 nil。
func testCredentialDecoder(runtime *testPlatformRuntime) func(ctx context.Context, stored, supplierID string) (string, error) {
	if runtime == nil {
		return nil
	}
	return runtime.Decode
}
