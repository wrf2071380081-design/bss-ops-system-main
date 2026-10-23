package testplatform

import "testing"

func TestTargetFingerprintStable(t *testing.T) {
	base := TargetFingerprint(15, "demo/deepseek-v4", "https://api.example.com/v1", "v1:abc")
	if base == "" || len(base) != 64 {
		t.Fatalf("fingerprint should be sha256 hex, got %q", base)
	}
	// 大小写与尾部斜杠不算目标变化，避免"假失效"。
	if got := TargetFingerprint(15, "DEMO/DeepSeek-V4 ", "https://API.example.com/v1/", "v1:abc"); got != base {
		t.Fatalf("normalization broken: %s vs %s", got, base)
	}
}

func TestTargetFingerprintChangesWithTarget(t *testing.T) {
	base := TargetFingerprint(15, "demo/deepseek-v4", "https://api.example.com/v1", "v1:abc")
	cases := map[string]string{
		"换标准模型":  TargetFingerprint(14, "demo/deepseek-v4", "https://api.example.com/v1", "v1:abc"),
		"换接口模型名": TargetFingerprint(15, "other-model", "https://api.example.com/v1", "v1:abc"),
		"换测试地址":  TargetFingerprint(15, "demo/deepseek-v4", "https://api.other.com/v1", "v1:abc"),
		"换 Key":  TargetFingerprint(15, "demo/deepseek-v4", "https://api.example.com/v1", "v1:def"),
	}
	for label, got := range cases {
		if got == base {
			t.Fatalf("%s 应改变指纹", label)
		}
	}
}

// 采购系数不参与指纹：改价不应逼供应商重测。
func TestTargetFingerprintIgnoresCoefficient(t *testing.T) {
	a := TargetFingerprint(15, "demo/deepseek-v4", "https://api.example.com/v1", "v1:abc")
	b := TargetFingerprint(15, "demo/deepseek-v4", "https://api.example.com/v1", "v1:abc")
	if a != b {
		t.Fatal("相同目标必须得到相同指纹")
	}
	// 函数签名里没有系数入参——这里以注释形式固化约定，防止将来有人"顺手"加进去。
}
