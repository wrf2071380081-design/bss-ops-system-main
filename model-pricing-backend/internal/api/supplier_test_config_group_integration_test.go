package api

import (
	"context"
	"fmt"
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
