package testplatform

import (
	"encoding/json"
	"testing"
)

func TestInterpretBaseline(t *testing.T) {
	state := JobState{
		Status:  "succeeded",
		Verdict: "pass",
		Summary: json.RawMessage(`{"ttft_ms":86.4,"availability":1,"cache_hit_tokens":600,"cache_input_tokens":1000}`),
	}
	out := Interpret(CategoryBaseline, state, nil)
	if !out.ConnectivityPassed || out.ConnectionMS == nil || *out.ConnectionMS != "86.4" {
		t.Fatalf("baseline mapping wrong: %+v", out)
	}
	if !out.DataComplete {
		t.Fatalf("baseline with latency should be complete: %+v", out)
	}
	if out.CacheHitTokens == nil || *out.CacheHitTokens != 600 || out.CacheInputTokens == nil || *out.CacheInputTokens != 1000 {
		t.Fatalf("cache tokens wrong: %+v", out)
	}
}

func TestInterpretBaselineFailIsNotComplete(t *testing.T) {
	out := Interpret(CategoryBaseline, JobState{Status: "failed", Verdict: "fail"}, nil)
	if out.ConnectivityPassed || out.DataComplete {
		t.Fatalf("fail must not be treated as complete: %+v", out)
	}
}

func TestInterpretAbortIsNotPass(t *testing.T) {
	out := Interpret(CategoryBaseline, JobState{Status: "aborted", Verdict: "abort"}, nil)
	if out.ConnectivityPassed || out.DataComplete {
		t.Fatalf("abort means L1 gate failed, must not pass: %+v", out)
	}
}

func TestInterpretStress(t *testing.T) {
	state := JobState{
		Status:  "succeeded",
		Verdict: "pass_with_warnings",
		Summary: json.RawMessage(`{"qps":24.5,"tps":22000,"cache_hit_ratio_pct":60}`),
	}
	out := Interpret(CategoryStress, state, nil)
	if !out.PressurePassed || out.RPS == nil || *out.RPS != "24.5" || out.TPS == nil || *out.TPS != "22000" {
		t.Fatalf("stress mapping wrong: %+v", out)
	}
	if !out.DataComplete {
		t.Fatalf("stress with rps/tps should be complete: %+v", out)
	}
	// 平台只给比率时，绝对 token 计数必须留空，不能拿 60% 编成数字。
	if out.CacheHitTokens != nil || out.CacheInputTokens != nil {
		t.Fatalf("ratio must not be converted into token counts: %+v", out)
	}
}

func TestInterpretStressFromReportDocument(t *testing.T) {
	document := []byte(`{"summary":{"steady_qps":"18.2","steady_tps":"9000"}}`)
	out := Interpret(CategoryStress, JobState{Status: "succeeded", Verdict: "pass"}, document)
	if out.RPS == nil || *out.RPS != "18.2" || out.TPS == nil || *out.TPS != "9000" {
		t.Fatalf("nested summary from report document not read: %+v", out)
	}
}

func TestInterpretInconsistentCacheLeftEmpty(t *testing.T) {
	// hit > input 属于脏数据，必须整体丢弃而不是写进去。
	state := JobState{Status: "succeeded", Verdict: "pass",
		Summary: json.RawMessage(`{"cache_hit_tokens":900,"cache_input_tokens":100}`)}
	out := Interpret(CategoryBaseline, state, nil)
	if out.CacheHitTokens != nil || out.CacheInputTokens != nil {
		t.Fatalf("inconsistent counters must be dropped: %+v", out)
	}
}

func TestFormatDecimal(t *testing.T) {
	if got := FormatDecimal(86.4); got == nil || *got != "86.4" {
		t.Fatalf("86.4 -> %v", got)
	}
	if got := FormatDecimal(22000); got == nil || *got != "22000" {
		t.Fatalf("22000 -> %v", got)
	}
	if FormatDecimal(-1) != nil {
		t.Fatal("negative must be nil")
	}
}
