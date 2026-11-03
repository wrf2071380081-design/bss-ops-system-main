package api

import (
	"strings"
	"testing"

	"model_bss/internal/infra/supplypg"
	tpdomain "model_bss/internal/domain/testplatform"
)

func fingerprintForTest(modelID int64, name, baseURL, apiKey string) string {
	return tpdomain.TargetFingerprint(modelID, name, baseURL, apiKey)
}

// 000052 起提交门槛按**配置组**校验（原型：每组至少一个连通成功 + 整批至少一次有效压测）。
//
// 与 000046 的三个关键差异（都用用例钉住，防止被改回去）：
//  1. 只校验**本次申报用到的组**；供应商建了但没用的组不能把人拦住；
//  2. 连通要看 **verdict 通过**（不是"有结果就行"）——这是变严，方向是通道至少要能连上；
//  3. 每行必须挂组。
func TestCheckSubmissionCoverage(t *testing.T) {
	status := func(value string) *string { return &value }
	verdict := func(value string) *string { return &value }

	// 三个组：1=华东通道、2=华南通道（都会被用到），3=备援通道（建了但本次不用）。
	groups := map[int64]supplypg.TestConfigGroupCredential{
		1: {TestConfigGroup: supplypg.TestConfigGroup{ID: 1, Name: "华东通道", Enabled: true, SortOrder: 0}},
		2: {TestConfigGroup: supplypg.TestConfigGroup{ID: 2, Name: "华南通道", Enabled: true, SortOrder: 1}},
		3: {TestConfigGroup: supplypg.TestConfigGroup{ID: 3, Name: "备援通道", Enabled: true, SortOrder: 2}},
	}
	groupPtr := func(id int64) *int64 { return &id }

	row := func(groupID int64, connectivity, stress bool, verdictValue string) supplypg.SubmissionRowRecord {
		item := supplypg.SubmissionRowRecord{
			TestConfigGroupID:   groupPtr(groupID),
			ConnectivityStatus:  status("SUCCEEDED"),
			ConnectivityVerdict: verdict(verdictValue),
		}
		if !connectivity {
			item.ConnectivityStatus = nil
			item.ConnectivityVerdict = nil
		}
		if stress {
			item.ParticipateTest = true
			item.StressStatus = status("SUCCEEDED")
			item.StressVerdict = verdict("")
			item.RPS = verdict("30")
			item.TPS = verdict("20000")
		}
		return item
	}

	cases := map[string]struct {
		rows    []supplypg.SubmissionRowRecord
		wantErr string
	}{
		"每组都有连通成功 + 一个压测：放行": {
			rows: []supplypg.SubmissionRowRecord{row(1, true, true, "pass"), row(2, true, false, "pass")},
		},
		"某组全部连通失败：拦，且文案带组名与序号": {
			rows:    []supplypg.SubmissionRowRecord{row(1, true, true, "pass"), row(2, true, false, "fail")},
			wantErr: "第 2 组「华南通道」还没有连通成功的模型",
		},
		"某组一行都没测：同样按组拦": {
			rows:    []supplypg.SubmissionRowRecord{row(1, true, true, "pass"), row(2, false, false, "")},
			wantErr: "第 2 组「华南通道」还没有连通成功的模型",
		},
		"未被使用的组不参与校验（建了 3 组只用 1 组也要能提交）": {
			rows: []supplypg.SubmissionRowRecord{row(1, true, true, "pass")},
		},
		"同一组里只要有一个连通成功就够（其余失败不拦）": {
			rows: []supplypg.SubmissionRowRecord{
				row(1, true, true, "fail"), row(1, true, false, "pass"),
			},
		},
		"没勾压力模型：拦": {
			rows:    []supplypg.SubmissionRowRecord{row(1, true, false, "pass")},
			wantErr: "请至少勾选一个压力测试模型",
		},
		"勾了压测但没结果：拦": {
			rows: func() []supplypg.SubmissionRowRecord {
				item := row(1, true, true, "pass")
				item.StressStatus = status("RUNNING")
				return []supplypg.SubmissionRowRecord{item}
			}(),
			wantErr: "没有压力测试结果",
		},
		"连通结果已失效（换组/改地址）：不算覆盖": {
			rows: func() []supplypg.SubmissionRowRecord {
				item := row(1, true, true, "pass")
				item.ConnectivityStale = true
				return []supplypg.SubmissionRowRecord{item}
			}(),
			wantErr: "第 1 组「华东通道」还没有连通成功的模型",
		},
		"行没挂组：拦并提示选组": {
			rows: func() []supplypg.SubmissionRowRecord {
				item := row(1, true, true, "pass")
				item.TestConfigGroupID = nil
				return []supplypg.SubmissionRowRecord{item}
			}(),
			wantErr: "每个模型都必须选择准入配置组",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkSubmissionCoverage(tc.rows, groups)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("期望允许提交，实际 %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("期望被拒绝，实际通过")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("错误文案不符：期望包含 %q，实际 %q", tc.wantErr, err.Error())
			}
		})
	}
}

// 已停用的组：**允许提交**，但失败文案要标注「该组已停用」，让供应商知道可以改挂。
//
// 定调理由：组被停用是供应商自己的配置动作，在途申报不该被卡死；
// 但按组校验照常进行（连通还是要有一个成功）。
func TestCheckSubmissionCoverageDisabledGroup(t *testing.T) {
	status := func(value string) *string { return &value }
	groups := map[int64]supplypg.TestConfigGroupCredential{
		9: {TestConfigGroup: supplypg.TestConfigGroup{ID: 9, Name: "旧通道", Enabled: false, SortOrder: 0}},
	}
	groupID := int64(9)

	t.Run("停用组里连通通过：仍然放行", func(t *testing.T) {
		rows := []supplypg.SubmissionRowRecord{{
			TestConfigGroupID:   &groupID,
			ConnectivityStatus:  status("SUCCEEDED"),
			ConnectivityVerdict: status("pass"),
			ParticipateTest:     true,
			StressStatus:        status("SUCCEEDED"),
		}}
		if err := checkSubmissionCoverage(rows, groups); err != nil {
			t.Fatalf("停用组不应阻止提交，实际 %v", err)
		}
	})

	t.Run("停用组里没连通：拦，文案标注已停用", func(t *testing.T) {
		rows := []supplypg.SubmissionRowRecord{{
			TestConfigGroupID:   &groupID,
			ConnectivityStatus:  status("SUCCEEDED"),
			ConnectivityVerdict: status("fail"),
			ParticipateTest:     true,
			StressStatus:        status("SUCCEEDED"),
		}}
		err := checkSubmissionCoverage(rows, groups)
		if err == nil || !strings.Contains(err.Error(), "该组已停用") {
			t.Fatalf("期望提示该组已停用，实际 %v", err)
		}
	})
}

// 换组必须让旧结果失效（"换组不能偷偷沿用旧结果"的唯一保障）。
//
// 这里不真跑测试：用两组不同 URL/Key 各算一次指纹，断言 ApplyConfigGroup
// 在组切换后把 ConnectivityStale/StressStale 置为 true。
func TestSubmissionRowApplyConfigGroupInvalidatesOnGroupSwitch(t *testing.T) {
	groupA := supplypg.TestConfigGroupCredential{
		TestConfigGroup: supplypg.TestConfigGroup{ID: 1, Name: "A 组", BaseURL: "https://a.example/v1", Enabled: true},
		APIKey:          "key-a",
	}
	groupB := supplypg.TestConfigGroupCredential{
		TestConfigGroup: supplypg.TestConfigGroup{ID: 2, Name: "B 组", BaseURL: "https://b.example/v1", Enabled: true},
		APIKey:          "key-b",
	}
	row := supplypg.SubmissionRowRecord{
		StandardModelID:            7,
		SupplierModelName:          "vendor-model",
		TestBaseURL:                groupA.BaseURL,
		ConnectivityJobFingerprint: fingerprintForTest(7, "vendor-model", groupA.BaseURL, groupA.APIKey),
		StressJobFingerprint:       fingerprintForTest(7, "vendor-model", groupA.BaseURL, groupA.APIKey),
	}

	t.Run("仍在 A 组：结果不变红", func(t *testing.T) {
		checked := row
		checked.ApplyConfigGroup(&groupA)
		if checked.ConnectivityStale || checked.StressStale {
			t.Fatalf("同一组不应判失效：conn=%v stress=%v", checked.ConnectivityStale, checked.StressStale)
		}
		if checked.TestBaseURL != groupA.BaseURL {
			t.Fatalf("展示地址应为 A 组：%q", checked.TestBaseURL)
		}
	})

	t.Run("换到 B 组：连通与压测都判失效", func(t *testing.T) {
		checked := row
		checked.ApplyConfigGroup(&groupB)
		if !checked.ConnectivityStale || !checked.StressStale {
			t.Fatalf("换组后应判失效：conn=%v stress=%v", checked.ConnectivityStale, checked.StressStale)
		}
		if checked.TestBaseURL != groupB.BaseURL {
			t.Fatalf("展示地址应切到 B 组：%q", checked.TestBaseURL)
		}
	})

	t.Run("组拿不到时不动（历史数据不判红）", func(t *testing.T) {
		checked := row
		checked.ApplyConfigGroup(nil)
		if checked.ConnectivityStale || checked.StressStale {
			t.Fatalf("无组信息时不应改判")
		}
	})
}

// 组序号必须按**卡片位置**编号（2026-11-03 自查发现的两端不一致）。
//
// 页面上卡片是按全部组编号展示的（第 1/2/3 组）。如果后端按"第几个**用到的**组"
// 重新计数，就会出现「第 1 组「华南通道」」——组名对、序号错，用户会去数第一张卡片。
// 这两条把"序号 = 在全部组里的位置"钉死。
func TestCheckSubmissionCoverageGroupOrdinalFollowsCardOrder(t *testing.T) {
	status := func(value string) *string { return &value }
	verdict := func(value string) *string { return &value }
	groups := map[int64]supplypg.TestConfigGroupCredential{
		1: {TestConfigGroup: supplypg.TestConfigGroup{ID: 1, Name: "华东通道", Enabled: true, SortOrder: 0}},
		2: {TestConfigGroup: supplypg.TestConfigGroup{ID: 2, Name: "华南通道", Enabled: true, SortOrder: 1}},
		3: {TestConfigGroup: supplypg.TestConfigGroup{ID: 3, Name: "备援通道", Enabled: true, SortOrder: 2}},
	}
	groupPtr := func(id int64) *int64 { return &id }
	row := func(groupID int64, pass bool, stress bool) supplypg.SubmissionRowRecord {
		verdictValue := "fail"
		if pass {
			verdictValue = "pass"
		}
		item := supplypg.SubmissionRowRecord{
			TestConfigGroupID:   groupPtr(groupID),
			ConnectivityStatus:  status("SUCCEEDED"),
			ConnectivityVerdict: verdict(verdictValue),
		}
		if stress {
			item.ParticipateTest = true
			item.StressStatus = status("SUCCEEDED")
			item.StressVerdict = verdict("")
		}
		return item
	}

	t.Run("只有第 2 组挂模型且不通：报第 2 组（不是第 1 组）", func(t *testing.T) {
		err := checkSubmissionCoverage([]supplypg.SubmissionRowRecord{row(2, false, true)}, groups)
		if err == nil {
			t.Fatal("期望被拒绝")
		}
		if !strings.Contains(err.Error(), "第 2 组「华南通道」") {
			t.Fatalf("序号应按卡片位置（第 2 组），实际 %q", err.Error())
		}
		if strings.Contains(err.Error(), "第 1 组") {
			t.Fatalf("不该按「用到的组」重新计数：%q", err.Error())
		}
	})

	t.Run("第 1 组空、第 2 组通、第 3 组不通：报第 3 组", func(t *testing.T) {
		err := checkSubmissionCoverage([]supplypg.SubmissionRowRecord{
			row(2, true, true), row(3, false, false),
		}, groups)
		if err == nil {
			t.Fatal("期望被拒绝")
		}
		if !strings.Contains(err.Error(), "第 3 组「备援通道」") {
			t.Fatalf("序号应按卡片位置（第 3 组），实际 %q", err.Error())
		}
	})
}
