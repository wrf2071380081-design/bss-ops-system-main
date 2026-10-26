package api

import (
	"testing"

	"model_bss/internal/infra/supplypg"
)

func ptr(s string) *string { return &s }

func TestPermissionsForStates(t *testing.T) {
	cases := map[string]struct{ edit, submit, withdraw bool }{
		submissionDraft:       {true, true, true},
		submissionPendingTest: {true, true, true},
		submissionPending:     {false, false, true},
		submissionRejected:    {true, true, false},
		submissionWithdrawn:   {true, true, false},
		submissionApproved:    {false, false, false},
	}
	for status, want := range cases {
		got := permissionsFor(status)
		if got.CanEdit != want.edit || got.CanSubmit != want.submit || got.CanWithdraw != want.withdraw {
			t.Fatalf("%s: got %+v want %+v", status, got, want)
		}
	}
}

func TestDeriveRowStatus(t *testing.T) {
	cases := []struct {
		name      string
		row       supplypg.SubmissionRowRecord
		submitted bool
		want      string
	}{
		{"未测-未提交", supplypg.SubmissionRowRecord{}, false, rowUntested},
		{"未测-已提交", supplypg.SubmissionRowRecord{}, true, rowSupplement},
		{"运行中", supplypg.SubmissionRowRecord{ConnectivityStatus: ptr("RUNNING")}, true, rowRunning},
		{"通过", supplypg.SubmissionRowRecord{ConnectivityStatus: ptr("SUCCEEDED"), ConnectivityVerdict: ptr("pass")}, true, rowPassed},
		{"通过带告警", supplypg.SubmissionRowRecord{ConnectivityStatus: ptr("SUCCEEDED"), ConnectivityVerdict: ptr("pass_with_warnings")}, false, rowPassed},
		{"未通过-未提交", supplypg.SubmissionRowRecord{ConnectivityStatus: ptr("SUCCEEDED"), ConnectivityVerdict: ptr("fail")}, false, rowFailed},
		{"未通过-已提交", supplypg.SubmissionRowRecord{ConnectivityStatus: ptr("SUCCEEDED"), ConnectivityVerdict: ptr("fail")}, true, rowSupplement},
		{"失效", supplypg.SubmissionRowRecord{
			ConnectivityStatus: ptr("SUCCEEDED"), ConnectivityVerdict: ptr("pass"), ConnectivityStale: true,
		}, true, rowStale},
	}
	for _, tc := range cases {
		if got := deriveRowStatus(tc.row, tc.submitted); got != tc.want {
			t.Fatalf("%s: got %s want %s", tc.name, got, tc.want)
		}
	}
}

// 折中口径的判定核心：至少一行 pass 且未 stale 才算有通过行。
func TestVerdictOKOnlyAcceptsPass(t *testing.T) {
	for _, verdict := range []string{"pass", "pass_with_warnings"} {
		if !verdictOK(ptr(verdict)) {
			t.Fatalf("%s 应视为通过", verdict)
		}
	}
	for _, verdict := range []string{"fail", "aborted", ""} {
		if verdictOK(ptr(verdict)) {
			t.Fatalf("%s 不应视为通过", verdict)
		}
	}
	if verdictOK(nil) {
		t.Fatal("无 verdict 不应视为通过")
	}
}
