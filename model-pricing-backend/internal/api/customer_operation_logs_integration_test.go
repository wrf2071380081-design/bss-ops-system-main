package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"model_bss/internal/domain/auth"
	"model_bss/internal/domain/customer"
	"model_bss/internal/infra/supplypg"
)

// 客户操作记录接口的真库回归（B3a 第 1 项）。
//
// 验收（team-lead 工单）：
//   - 建客户 + 发起开通 + 完成两个节点后，返回 **4 条**（CREATE / START / COMPLETE_CONTRACT / COMPLETE_CONFIG），时间倒序
//   - 不存在的客户 → 404 语义；别的销售名下的客户（SELF 域）→ 403 语义
//
// 这里直接打 runtime + service（不起 gin）：404/403 的 HTTP 映射由既有的
// `customerRegistryErrToAppErr` 单测覆盖，本测试负责"数据真的能按客户串起来"。

func TestCustomerOperationLogsTimeline(t *testing.T) {
	db := openB3ATestDB(t)
	f := openCustomerWorkspaceTest(t, db)
	registry, err := supplypg.NewCustomerRegistryStore(db, "public")
	if err != nil {
		t.Fatalf("registry store: %v", err)
	}
	links, err := supplypg.NewCustomerResourceLinkStore(db, "public")
	if err != nil {
		t.Fatalf("link store: %v", err)
	}
	onboarding, err := supplypg.NewCustomerOnboardingStore(db, "public", links)
	if err != nil {
		t.Fatalf("onboarding store: %v", err)
	}

	profile := f.createCustomer(t, registry, "oplogs")
	svc := customer.NewOnboardingService(onboarding, links)
	ctx := context.Background()

	flow, err := svc.Start(ctx, customer.StartOnboardingInput{
		CustomerProfileID:  profile.ID,
		FirstOrder:         true,
		ContractAssigneeID: f.ownerID,
		ConfigAssigneeID:   f.ownerID,
		Models:             []customer.OnboardingModelInput{{StandardModelID: f.modelID, ResourceID: f.resActive}},
		Reason:             "操作记录回归：首单",
	}, f.ownerID, "qa-oplogs-start")
	if err != nil {
		t.Fatalf("start onboarding: %v", err)
	}
	if _, err := svc.CompleteNode(ctx, customer.CompleteNodeInput{
		NodeID: findNodeByKey(t, flow, "CONTRACT").ID, Reason: "合同与授信已归档",
		F12Reference: "HT-OPLOG-1", Operator: f.ownerID, Request: "qa-oplogs-contract",
	}); err != nil {
		t.Fatalf("complete contract: %v", err)
	}
	if _, err := svc.CompleteNode(ctx, customer.CompleteNodeInput{
		NodeID: findNodeByKey(t, flow, "CONFIG").ID, Reason: "资源配置完成",
		Operator: f.ownerID, Request: "qa-oplogs-config",
	}); err != nil {
		t.Fatalf("complete config: %v", err)
	}

	rt := NewCustomerWorkspaceRuntime(db)
	list, total, err := rt.ListOperationLogs(ctx, profile.ID, 1, 20, OperationLogFilters{})
	if err != nil {
		t.Fatalf("list operation logs: %v", err)
	}
	if total != 4 || len(list) != 4 {
		t.Fatalf("期望 4 条（CREATE/START/COMPLETE_CONTRACT/COMPLETE_CONFIG），got total=%d len=%d", total, len(list))
	}

	// 时间倒序：最新的在最前。
	wantOrder := []string{"COMPLETE_CONFIG", "COMPLETE_CONTRACT", "START", "CREATE"}
	for i, want := range wantOrder {
		if list[i].Action != want {
			t.Fatalf("第 %d 条动作期望 %s，got %s（全序列：%v）", i, want, list[i].Action, actionSeq(list))
		}
		if list[i].ActionLabel == "" || list[i].ActionLabel == list[i].Action {
			t.Fatalf("%s 缺少中文文案（ActionLabel=%q）", want, list[i].ActionLabel)
		}
	}

	// 两个实体都来自同一客户：建客户在 customer_profile 上，开通动作在 onboarding_flow 上。
	if list[3].EntityType != "customer_profile" || list[3].EntityID != profile.ID {
		t.Fatalf("CREATE 应挂在 customer_profile/%d 上，got %s/%d", profile.ID, list[3].EntityType, list[3].EntityID)
	}
	for _, row := range list[:3] {
		if row.EntityType != "onboarding_flow" || row.EntityID != flow.ID {
			t.Fatalf("%s 应挂在 onboarding_flow/%d 上，got %s/%d", row.Action, flow.ID, row.EntityType, row.EntityID)
		}
	}

	// 审计字段要能追溯：操作人、请求号、after 快照、可选 F12 凭据。
	if list[0].OperatorName == "" || list[0].RequestID != "qa-oplogs-config" {
		t.Fatalf("COMPLETE_CONFIG 缺少操作人/请求号：%+v", list[0])
	}
	if list[1].RequestID != "qa-oplogs-contract" || !strings.Contains(list[1].After, "HT-OPLOG-1") {
		t.Fatalf("COMPLETE_CONTRACT 缺少请求号或 F12 凭据：%+v", list[1])
	}

	// 不存在 / 越权：复用档案服务的 404 / 403 语义（HTTP 层由既有映射测试覆盖）。
	registrySvc := customer.NewRegistryService(registry)
	if _, err := registrySvc.Profile(ctx, 9223372036854775806, customer.OwnerScope{DataScope: auth.ScopeALL}); !errors.Is(err, customer.ErrCustomerProfileNotFound) {
		t.Fatalf("不存在的客户应返回 ErrCustomerProfileNotFound，got %v", err)
	}
	if _, err := registrySvc.Profile(ctx, profile.ID, customer.OwnerScope{DataScope: auth.ScopeSELF, StaffID: f.ownerID + 999999}); !errors.Is(err, customer.ErrCustomerOutOfScope) {
		t.Fatalf("非归属销售应返回 ErrCustomerOutOfScope，got %v", err)
	}
}

func findNodeByKey(t *testing.T, flow *customer.OnboardingFlow, key string) *customer.OnboardingNode {
	t.Helper()
	for i := range flow.Nodes {
		if flow.Nodes[i].Key == key {
			return &flow.Nodes[i]
		}
	}
	t.Fatalf("流程里没有节点 %s：%+v", key, flow.Nodes)
	return nil
}

func actionSeq(list []CustomerOperationLogRow) []string {
	out := make([]string, 0, len(list))
	for _, row := range list {
		out = append(out, row.Action)
	}
	return out
}
