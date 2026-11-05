package api

import "testing"

// approvalResultOf 把逐模型决定折成「通过 / 部分通过 / 不通过」。
// 原型列表直接用这句话区分「整单通过」和「部分通过」——status 只能表达 APPROVED/PROCESSING，
// 如果不派生，用户会以为被驳回的模型也一起接入。
func TestApprovalResultOf(t *testing.T) {
	cases := []struct {
		name   string
		status string
		rows   []nativeFlowRow
		want   string
	}{
		{"未决策", "PENDING_APPROVAL", []nativeFlowRow{{Decision: ""}, {Decision: ""}}, ""},
		{"全部通过", "PROCESSING", []nativeFlowRow{{Decision: "APPROVED"}, {Decision: "APPROVED"}}, "通过"},
		{"部分通过", "PROCESSING", []nativeFlowRow{{Decision: "APPROVED"}, {Decision: "REJECTED"}}, "部分通过"},
		{"全部不通过", "PROCESSING", []nativeFlowRow{{Decision: "REJECTED"}, {Decision: "REJECTED"}}, "不通过"},
		{"整单驳回（行上无决定）", "REJECTED", []nativeFlowRow{{Decision: "PENDING"}}, "不通过"},
		{"无行", "PENDING_APPROVAL", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := approvalResultOf(tc.status, tc.rows); got != tc.want {
				t.Fatalf("approvalResultOf = %q, want %q", got, tc.want)
			}
		})
	}
}
