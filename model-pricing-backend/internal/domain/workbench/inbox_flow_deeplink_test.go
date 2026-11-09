package workbench

import (
	"context"
	"testing"
)

// 收件箱 ONBOARDING_NODE 的 deeplink 分派（B3a 第 4 项）。
//
// 背景：`v_work_item` 对办理节点只给 `biz_id = onboarding_flow.id`，没有 kind/owner_id；
// 历史实现把所有 ONBOARDING_NODE 一律拼成「供应商列表按名字搜」，于是**客户的**开通待办
// 点进去会落到供应商列表。修法：在 service 层对当页节点**批量**补 kind/owner_id，再按 kind 分派。
//
// 本文件钉三件事：
//  1. CUSTOMER 节点 → 客户详情「接入模型」Tab（走查手册 3.9c）
//  2. SUPPLIER 节点 → 供应商「接入模型」页（原先落到供应商列表按名字搜，多绕一步）
//  3. 查询次数与行数无关：一页 N 条节点只说 1 次 FlowOwners；无节点时 0 次

func TestInboxDeeplinkDispatchesOnboardingNodeByKind(t *testing.T) {
	cases := []struct {
		name string
		item InboxItem
		want string
	}{
		{
			name: "客户节点落客户详情接入模型 Tab",
			item: InboxItem{BizType: "ONBOARDING_NODE", BizID: 11, BizObject: "蓝海电商", FlowKind: "CUSTOMER", FlowOwnerID: 5},
			want: "/internal/customers/5?tab=models",
		},
		{
			// 供应商有独立的接入页（接入/上架/下架 + 本批办理进度），从「供应资源接入」
			// 待办点进来就是要看"这批还差哪些模型没接上"，落列表再按名字搜纯属多绕一步。
			name: "供应商节点落供应链接入页",
			item: InboxItem{BizType: "ONBOARDING_NODE", BizID: 12, BizObject: "示例供应商", FlowKind: "SUPPLIER", FlowOwnerID: 9},
			want: "/internal/suppliers/9/models",
		},
		{
			name: "取不到 kind 时回落到原行为（防御分支）",
			item: InboxItem{BizType: "ONBOARDING_NODE", BizID: 13, BizObject: "未知主体"},
			want: "/internal/suppliers?keyword=%E6%9C%AA%E7%9F%A5%E4%B8%BB%E4%BD%93",
		},
		{
			name: "客户节点但 owner 缺失时也要回落，不能拼出 /customers/0",
			item: InboxItem{BizType: "ONBOARDING_NODE", BizID: 14, BizObject: "孤儿流程", FlowKind: "CUSTOMER"},
			want: "/internal/suppliers?keyword=%E5%AD%A4%E5%84%BF%E6%B5%81%E7%A8%8B",
		},
	}
	for _, c := range cases {
		if got := inboxItemDeeplink(c.item); got != c.want {
			t.Fatalf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// TestInboxAttachesFlowOwnersWithSingleQuery 一页 N 条节点待办 → 只 1 次批量查询。
func TestInboxAttachesFlowOwnersWithSingleQuery(t *testing.T) {
	store := &fakeInboxStore{
		inbox: []InboxItem{
			{BizType: "ONBOARDING_NODE", BizID: 100, BizObject: "客户甲", State: "TODO"},
			{BizType: "ONBOARDING_NODE", BizID: 101, BizObject: "客户乙", State: "TODO"},
			{BizType: "ONBOARDING_NODE", BizID: 100, BizObject: "客户甲", State: "TODO"}, // 同页重复 flow
			{BizType: "PLAN", BizID: 7, BizObject: "上架计划", State: "TODO"},
		},
		total:  4,
		owners: map[int64]FlowOwner{100: {Kind: "CUSTOMER", OwnerID: 5}, 101: {Kind: "CUSTOMER", OwnerID: 6}},
	}
	svc := NewInboxService(store)

	res, err := svc.Inbox(context.Background(), InboxQuery{}, 3)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if store.flowOwnerCalls != 1 {
		t.Fatalf("一页应只查 1 次 FlowOwners，got %d 次（N+1 回归）", store.flowOwnerCalls)
	}
	// 去重：100 出现两次，但只查一次、只传一次。
	if len(store.flowOwnerIDs) != 2 || store.flowOwnerIDs[0] != 100 || store.flowOwnerIDs[1] != 101 {
		t.Fatalf("批量查询的 id 应去重为 [100 101]，got %v", store.flowOwnerIDs)
	}
	want := []string{
		"/internal/customers/5?tab=models",
		"/internal/customers/6?tab=models",
		"/internal/customers/5?tab=models",
		"/internal/model-plaza/7", // PLAN 分支不受影响
	}
	for i, w := range want {
		if res.List[i].Deeplink != w {
			t.Fatalf("第 %d 行 deeplink got %q want %q", i, res.List[i].Deeplink, w)
		}
	}
	if res.List[0].FlowKind != "CUSTOMER" || res.List[0].FlowOwnerID != 5 {
		t.Fatalf("kind/owner 未回填：%+v", res.List[0])
	}
}

// TestInboxSkipsFlowOwnersWhenNoNodes 页内没有节点待办时不应产生任何额外查询。
func TestInboxSkipsFlowOwnersWhenNoNodes(t *testing.T) {
	store := &fakeInboxStore{inbox: []InboxItem{
		{BizType: "NOTICE", BizID: 1, State: "READ"},
		{BizType: "PLAN", BizID: 2, BizObject: "x", State: "TODO"},
	}, total: 2}
	svc := NewInboxService(store)
	if _, err := svc.Inbox(context.Background(), InboxQuery{}, 3); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if store.flowOwnerCalls != 0 {
		t.Fatalf("无节点待办时不应调用 FlowOwners，got %d 次", store.flowOwnerCalls)
	}
}
