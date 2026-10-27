package api

import (
	"strings"
	"testing"

	"model_bss/internal/infra/supplypg"
)

// 2026-10-27 与原型对齐后的提交门禁：
//   - 所有模型都要有连通性测试结果（不看 verdict，结果不达标也算有效）
//   - 至少勾选一个压力测试模型，且勾选的行要有压力结果
//
// 这里只测纯规则；「按主体凭据重算失效」由 supplypg 的 ApplyCredential 负责，
// 见 TestSubmissionRowApplyCredential。
func TestCheckSubmissionCoverage(t *testing.T) {
	status := func(value string) *string { return &value }
	verdict := func(value string) *string { return &value }
	conn := func() supplypg.SubmissionRowRecord {
		return supplypg.SubmissionRowRecord{ConnectivityStatus: status("SUCCEEDED"), ConnectivityVerdict: verdict("pass")}
	}
	stress := func() supplypg.SubmissionRowRecord {
		row := conn()
		row.ParticipateTest = true
		row.StressStatus = status("SUCCEEDED")
		row.StressVerdict = verdict("")
		row.RPS = verdict("30")
		row.TPS = verdict("20000")
		return row
	}

	cases := map[string]struct {
		rows    []supplypg.SubmissionRowRecord
		wantErr string
	}{
		"全覆盖+一个压力：允许提交": {
			rows: []supplypg.SubmissionRowRecord{stress(), conn()},
		},
		"连通未通过也允许提交（原型：不达标属于有效结果）": {
			rows: func() []supplypg.SubmissionRowRecord {
				row := stress()
				row.ConnectivityVerdict = verdict("fail")
				return []supplypg.SubmissionRowRecord{row}
			}(),
		},
		"有行没跑连通：拒绝并带计数": {
			rows:    []supplypg.SubmissionRowRecord{stress(), {ParticipateTest: false}},
			wantErr: "还有 1 个模型没有连通性测试结果",
		},
		"连通还在跑：算没有结果": {
			rows: func() []supplypg.SubmissionRowRecord {
				row := stress()
				row.ConnectivityStatus = status("RUNNING")
				return []supplypg.SubmissionRowRecord{row}
			}(),
			wantErr: "还有 1 个模型没有连通性测试结果",
		},
		"没勾压力模型：拒绝": {
			rows:    []supplypg.SubmissionRowRecord{conn()},
			wantErr: "请至少勾选一个压力测试模型",
		},
		"勾了但没压力结果：拒绝": {
			rows: func() []supplypg.SubmissionRowRecord {
				row := conn()
				row.ParticipateTest = true
				return []supplypg.SubmissionRowRecord{row}
			}(),
			wantErr: "还有 1 个没有压力测试结果",
		},
		"结果已失效：不算覆盖": {
			rows: func() []supplypg.SubmissionRowRecord {
				row := stress()
				row.ConnectivityStale = true
				return []supplypg.SubmissionRowRecord{row}
			}(),
			wantErr: "没有连通性测试结果",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkSubmissionCoverage(tc.rows)
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

// 主体凭据变更后，行结果必须被判失效；凭据没变则保持原样。
func TestSubmissionRowApplyCredential(t *testing.T) {
	cred := func(url, key string) *supplypg.SupplierTestCredential {
		return &supplypg.SupplierTestCredential{SupplierID: 1, BaseURL: url, APIKey: key}
	}
	row := supplypg.SubmissionRowRecord{
		StandardModelID:   7,
		SupplierModelName: "vendor-model",
		TestBaseURL:       "https://old.example/v1",
		// 指纹由同一份 (7, vendor-model, https://old.example/v1, k) 算出，
		// 这里留空即可——只要和下面的「新凭据」不同就会判失效。
		ConnectivityJobFingerprint: "fingerprint-from-old-credential",
		StressJobFingerprint:       "",
	}

	t.Run("凭据未变：仍按行快照判定", func(t *testing.T) {
		unchanged := row
		unchanged.ApplyCredential(nil)
		if unchanged.ConnectivityStale {
			t.Fatalf("凭据未登记时不应改判")
		}
	})

	t.Run("凭据变了：连通结果失效", func(t *testing.T) {
		changed := row
		changed.ApplyCredential(cred("https://new.example/v1", "new-key"))
		if !changed.ConnectivityStale {
			t.Fatalf("改了主体凭据，旧连通结果应判失效")
		}
		if changed.TestBaseURL != "https://new.example/v1" {
			t.Fatalf("展示用地址应跟随主体凭据，实际 %q", changed.TestBaseURL)
		}
	})

	t.Run("老任务没记指纹：不做无依据误判", func(t *testing.T) {
		legacy := row
		legacy.ConnectivityJobFingerprint = ""
		legacy.ApplyCredential(cred("https://new.example/v1", "new-key"))
		if legacy.ConnectivityStale {
			t.Fatalf("任务侧没有指纹时不应判失效")
		}
	})
}
