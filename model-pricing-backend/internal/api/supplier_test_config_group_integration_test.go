package api

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"model_bss/internal/infra/supplypg"
)

// 000052 真库验收：多配置组时，**改 A 组只刷新挂在 A 组上的行**。
//
// 为什么必须真库：这条语义全靠 SQL 的 `WHERE r.test_config_group_id = $2` 落地，
// 单测（纯内存）证明不了；而它正是多组改造最核心的价值——改 A 组不该让 B 组的测试结果失效
// （000046 时代是「改主体凭据 → 全量刷新」，多组下会互相打死）。
//
// 全程不碰测试平台：只走 CreateDraft / Update / RefreshOpenRowSnapshotsByGroup 三个写路径。
func TestTestConfigGroupRefreshOnlyTouchesItsOwnRows(t *testing.T) {
	db := openB3ATestDB(t)
	ctx := context.Background()
	store, err := supplypg.NewTestConfigGroupStore(db, "public")
	if err != nil {
		t.Fatalf("group store: %v", err)
	}
	submissions, err := supplypg.NewSubmissionStore(db, "public")
	if err != nil {
		t.Fatalf("submission store: %v", err)
	}

	// 用库里已有的供应商 3（云桥算力，CN）。不新建供应商：那要一整条审计链，与本用例无关。
	const supplierID int64 = 3
	stamp := time.Now().UnixNano()
	nameA := fmt.Sprintf("集成A-%d", stamp)
	nameB := fmt.Sprintf("集成B-%d", stamp)
	groupA, err := store.Create(ctx, supplierID, nameA, "https://a.example/v1", ptrString("key-a"), 3)
	if err != nil {
		t.Fatalf("create group A: %v", err)
	}
	groupB, err := store.Create(ctx, supplierID, nameB, "https://b.example/v1", ptrString("key-b"), 3)
	if err != nil {
		t.Fatalf("create group B: %v", err)
	}
	var submissionID int64
	t.Cleanup(func() {
		c := context.Background()
		if submissionID > 0 {
			_, _ = db.ExecContext(c, `DELETE FROM public.submission WHERE id=$1`, submissionID)
		}
		_, _ = db.ExecContext(c, `DELETE FROM public.supplier_test_config_group WHERE id = ANY($1)`,
			fmt.Sprintf("{%d,%d}", groupA.ID, groupB.ID))
	})

	// 两行分别挂 A / B 组（行上的 base_url 取各自组的值，与 buildRows 的行为一致）。
	submissionID, err = submissions.CreateDraft(ctx, supplierID, "MODEL_NEW", "CN", "集成测试：多配置组", []supplypg.SubmissionRowInput{
		{StandardModelID: 13, SupplierModelName: "gpt-4o-mini", Coefficient: "1", TestBaseURL: groupA.BaseURL, SealedAPIKey: "key-a", TestConfigGroupID: groupA.ID},
		{StandardModelID: 14, SupplierModelName: "qwen3-max", Coefficient: "1", TestBaseURL: groupB.BaseURL, SealedAPIKey: "key-b", TestConfigGroupID: groupB.ID},
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	rowsOf := func() map[int64]string {
		t.Helper()
		out := map[int64]string{}
		result, err := db.QueryContext(ctx, `SELECT standard_model_id, COALESCE(test_base_url,'') FROM public.submission_row WHERE submission_id=$1`, submissionID)
		if err != nil {
			t.Fatalf("load rows: %v", err)
		}
		defer func() { _ = result.Close() }()
		for result.Next() {
			var modelID int64
			var baseURL string
			if err := result.Scan(&modelID, &baseURL); err != nil {
				t.Fatalf("scan rows: %v", err)
			}
			out[modelID] = baseURL
		}
		return out
	}

	initial := rowsOf()
	if initial[13] != groupA.BaseURL || initial[14] != groupB.BaseURL {
		t.Fatalf("建单后两行应各取自己组的地址：%v", initial)
	}

	// 改 A 组的 URL 与 Key，只刷新 A 组上的行。
	newURL := "https://a-new.example/v2"
	if _, err := store.Update(ctx, supplierID, groupA.ID, supplypg.TestConfigGroupPatch{
		BaseURL: &newURL, APIKey: ptrString("key-a2"),
	}, 3); err != nil {
		t.Fatalf("update group A: %v", err)
	}
	if err := store.RefreshOpenRowSnapshotsByGroup(ctx, supplierID, groupA.ID); err != nil {
		t.Fatalf("refresh group A rows: %v", err)
	}

	after := rowsOf()
	if after[13] != newURL {
		t.Fatalf("A 组的行应刷成新地址，实际 %q", after[13])
	}
	if after[14] != groupB.BaseURL {
		t.Fatalf("B 组的行**不应**被 A 组的改动影响，实际 %q", after[14])
	}

	// 指纹隔离：A 组刷新后应写上新指纹；B 组的行**连指纹都不该被写**（一个字都没动）。
	// 新建行本来没有指纹（insertRows 不写），所以 B 仍为空恰好证明"没被 A 组的刷新波及"。
	var fpA, fpB string
	if err := db.QueryRowContext(ctx, `
SELECT COALESCE(MAX(test_fingerprint) FILTER (WHERE standard_model_id=13),''),
       COALESCE(MAX(test_fingerprint) FILTER (WHERE standard_model_id=14),'')
FROM public.submission_row WHERE submission_id=$1`, submissionID).Scan(&fpA, &fpB); err != nil {
		t.Fatalf("load fingerprints: %v", err)
	}
	if fpA == "" {
		t.Fatalf("A 组的行刷新后应有指纹")
	}
	if fpB != "" {
		t.Fatalf("B 组的行不该被 A 组的刷新写指纹，实际 %q", fpB)
	}
	// 再对 B 组做一次刷新，确认它写的是 B 自己的指纹（并且与 A 不同）。
	if err := store.RefreshOpenRowSnapshotsByGroup(ctx, supplierID, groupB.ID); err != nil {
		t.Fatalf("refresh group B rows: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(test_fingerprint),'') FROM public.submission_row WHERE submission_id=$1 AND standard_model_id=14`, submissionID).Scan(&fpB); err != nil {
		t.Fatalf("reload fingerprint B: %v", err)
	}
	if fpB == "" || fpB == fpA {
		t.Fatalf("B 组指纹应非空且与 A 不同：A=%q B=%q", fpA, fpB)
	}
}

func ptrString(value string) *string { return &value }

// TestSubmissionDTOAlwaysCarriesConfigGroup P0 回归：读接口必须把组三列搬进 DTO。
//
// QA 验出来的真实故障：仓储已经把 test_config_group_id/name/enabled 查进
// SubmissionRowRecord，但 buildSubmissionDTO 的字面量里**忘了搬**——
// 编译不报错、类型不报错，只有接口层能抓。后果是前端「准入配置组」下拉全空、
// rowsWithoutGroup 恒真 → 提交按钮永久禁用。
//
// 这条用例从库读到 API 响应完整走一遍：建组 → 建草稿（行挂该组）→ Rows → buildSubmissionDTO。
func TestSubmissionDTOAlwaysCarriesConfigGroup(t *testing.T) {
	db := openB3ATestDB(t)
	ctx := context.Background()
	store, err := supplypg.NewTestConfigGroupStore(db, "public")
	if err != nil {
		t.Fatalf("group store: %v", err)
	}
	submissions, err := supplypg.NewSubmissionStore(db, "public")
	if err != nil {
		t.Fatalf("submission store: %v", err)
	}
	const supplierID int64 = 3
	group, err := store.Create(ctx, supplierID, fmt.Sprintf("DTO组-%d", time.Now().UnixNano()), "https://dto.example/v1", ptrString("k"), 3)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	var submissionID int64
	t.Cleanup(func() {
		c := context.Background()
		if submissionID > 0 {
			_, _ = db.ExecContext(c, `DELETE FROM public.submission WHERE id=$1`, submissionID)
		}
		_, _ = db.ExecContext(c, `DELETE FROM public.supplier_test_config_group WHERE id=$1`, group.ID)
	})

	submissionID, err = submissions.CreateDraft(ctx, supplierID, "MODEL_NEW", "CN", "集成测试：DTO 搬字段", []supplypg.SubmissionRowInput{
		{StandardModelID: 13, SupplierModelName: "gpt-4o-mini", Coefficient: "1", TestBaseURL: group.BaseURL, SealedAPIKey: "k", TestConfigGroupID: group.ID},
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	header, err := submissions.Get(ctx, supplierID, submissionID)
	if err != nil {
		t.Fatalf("load header: %v", err)
	}
	rows, err := submissions.Rows(ctx, []int64{submissionID})
	if err != nil {
		t.Fatalf("load rows: %v", err)
	}
	dto := buildSubmissionDTO(*header, rows[submissionID])
	if len(dto.Rows) != 1 {
		t.Fatalf("应回 1 行，实际 %d", len(dto.Rows))
	}
	item := dto.Rows[0]
	if item.TestConfigGroupID != strconv.FormatInt(group.ID, 10) {
		t.Fatalf("test_config_group_id 必须搬进 DTO：期望 %d，实际 %q", group.ID, item.TestConfigGroupID)
	}
	if item.TestConfigGroupName != group.Name {
		t.Fatalf("test_config_group_name 必须搬进 DTO：期望 %q，实际 %q", group.Name, item.TestConfigGroupName)
	}
	if !item.TestConfigGroupEnabled {
		t.Fatal("test_config_group_enabled 必须搬进 DTO（新组默认启用，应为 true）")
	}
}

// TestTestConfigGroupPatchIsPartial PARTIAL PATCH 契约：传哪个改哪个，都不传是空操作。
//
// 之前的实现在 name 为 nil 时仍走完整校验 → 只传 base_url 会 400
// 「请填写配置组名称」，与函数注释承诺的"局部更新"不符。
// 前端将来只改 Key（最常见操作）会直接撞墙。
func TestTestConfigGroupPatchIsPartial(t *testing.T) {
	db := openB3ATestDB(t)
	ctx := context.Background()
	store, err := supplypg.NewTestConfigGroupStore(db, "public")
	if err != nil {
		t.Fatalf("group store: %v", err)
	}
	service := NewSupplierTestConfigGroupService(store)
	const supplierID int64 = 3
	group, err := store.Create(ctx, supplierID, fmt.Sprintf("PATCH组-%d", time.Now().UnixNano()), "https://patch.example/v1", ptrString("old-key"), 3)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM public.supplier_test_config_group WHERE id=$1`, group.ID)
	})

	t.Run("只传 base_url：成功且组名不变", func(t *testing.T) {
		baseURL := "https://patch-new.example/v2"
		dto, err := service.Update(ctx, supplierID, group.ID, supplypg.TestConfigGroupPatch{BaseURL: &baseURL}, 3)
		if err != nil {
			t.Fatalf("只传 base_url 不应报错：%v", err)
		}
		if dto.BaseURL != baseURL {
			t.Fatalf("URL 应更新为 %q，实际 %q", baseURL, dto.BaseURL)
		}
		if dto.Name != group.Name {
			t.Fatalf("未传 name 时组名不应变化：期望 %q，实际 %q", group.Name, dto.Name)
		}
	})

	t.Run("只传 api_key：成功且不回显 Key", func(t *testing.T) {
		key := "new-key"
		dto, err := service.Update(ctx, supplierID, group.ID, supplypg.TestConfigGroupPatch{APIKey: &key}, 3)
		if err != nil {
			t.Fatalf("只传 api_key 不应报错：%v", err)
		}
		if !dto.HasAPIKey {
			t.Fatal("传了 api_key 后 has_api_key 应为 true")
		}
	})

	t.Run("只传 enabled=false：停用成功", func(t *testing.T) {
		disabled := false
		dto, err := service.Update(ctx, supplierID, group.ID, supplypg.TestConfigGroupPatch{Enabled: &disabled}, 3)
		if err != nil {
			t.Fatalf("只传 enabled 不应报错：%v", err)
		}
		if dto.Enabled {
			t.Fatal("期望已停用")
		}
	})

	t.Run("什么都不传：空操作，返回当前状态", func(t *testing.T) {
		dto, err := service.Update(ctx, supplierID, group.ID, supplypg.TestConfigGroupPatch{}, 3)
		if err != nil {
			t.Fatalf("空 patch 不应报错：%v", err)
		}
		if dto.ID != strconv.FormatInt(group.ID, 10) {
			t.Fatalf("应返回该组本身，实际 %q", dto.ID)
		}
	})

	t.Run("传了空 name：仍然要报错（不能把组名清空）", func(t *testing.T) {
		empty := "  "
		if _, err := service.Update(ctx, supplierID, group.ID, supplypg.TestConfigGroupPatch{Name: &empty}, 3); err == nil {
			t.Fatal("空组名应被拒绝")
		}
	})
}
