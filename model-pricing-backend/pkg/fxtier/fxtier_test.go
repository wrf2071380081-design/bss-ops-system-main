package fxtier

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTiers_Exact8: 8 档固定集合与顺序锚点。
func TestTiers_Exact8(t *testing.T) {
	got := Tiers()
	require.Equal(t, []string{"6.5", "6.7", "6.75", "6.8", "6.85", "6.9", "6.95", "7.0"}, got)
}

// TestTiers_ReturnsCopy: 调用方改动返回值不得污染内部状态。
func TestTiers_ReturnsCopy(t *testing.T) {
	got := Tiers()
	got[0] = "9.9"
	require.Equal(t, "6.5", Tiers()[0])
}

// TestNormalize_NumericEquivalence: 数值等价命中，回显规范形式。
func TestNormalize_NumericEquivalence(t *testing.T) {
	cases := map[string]string{
		"6.8":     "6.8",
		"6.80":    "6.8",
		"6.800":   "6.8",
		"6.5":     "6.5",
		"6.50":    "6.5",
		"6.750":   "6.75",
		"7.0":     "7.0",
		"7.000":   "7.0",
		" 7.000 ": "7.0",
	}
	for raw, want := range cases {
		got, ok := Normalize(raw)
		require.Truef(t, ok, "Normalize(%q) 应命中", raw)
		require.Equalf(t, want, got, "Normalize(%q)", raw)
	}
}

// TestNormalize_RejectOutOfRange: 非 8 档一律拒绝（含 7.1 / 非数值）。
func TestNormalize_RejectOutOfRange(t *testing.T) {
	for _, raw := range []string{"7.1", "6.4", "6.6", "abc", "", "  ", "8"} {
		_, ok := Normalize(raw)
		require.Falsef(t, ok, "Normalize(%q) 应拒绝", raw)
	}
}

// TestIsValid: 白名单判定与 Normalize 一致。
func TestIsValid(t *testing.T) {
	require.True(t, IsValid("7.0"))
	require.True(t, IsValid("6.80"))
	require.False(t, IsValid("7.1"))
	require.False(t, IsValid(""))
}

// TestCanonical: nil 保持 nil；命中规范化；未命中历史值原样返回。
func TestCanonical(t *testing.T) {
	require.Nil(t, Canonical(nil))
	require.Equal(t, "6.8", *Canonical(strPtr("6.800")))
	require.Equal(t, "7.0", *Canonical(strPtr("7.000")))
	require.Equal(t, "6.86", *Canonical(strPtr("6.86")))
}

func strPtr(s string) *string { return &s }
