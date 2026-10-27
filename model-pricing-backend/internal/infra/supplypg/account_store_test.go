package supplypg

import "testing"

func TestCurrentRegionLockedCoversThreeConditions(t *testing.T) {
	cases := []struct {
		name string
		rec  *AccountRecord
		want bool
	}{
		{"nil", nil, false},
		{"fresh", &AccountRecord{}, false},
		{"ever-admitted", &AccountRecord{EverAdmitted: true}, true},
		{"has-resource", &AccountRecord{ResourceCount: 1}, true},
		{"has-submission", &AccountRecord{SubmissionCount: 1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CurrentRegionLocked(tc.rec); got != tc.want {
				t.Fatalf("want %v got %v", tc.want, got)
			}
		})
	}
}

func TestNewAccountStoreRejectsNilDBAndBadSchema(t *testing.T) {
	// 与其它 supplypg 仓储一致：schema 必须是合法标识符，防止拼 SQL 时被注入。
	if _, err := NewAccountStore(nil, "public"); err == nil {
		t.Fatal("nil db should be rejected")
	}
	if modelSchemaName.MatchString("public; drop table supplier") {
		t.Fatal("schema guard should reject injection attempt")
	}
	if !modelSchemaName.MatchString("public") {
		t.Fatal("schema guard should accept public")
	}
}

func TestFirstNonEmptyFallsBackToDash(t *testing.T) {
	// 操作人快照绝不能写空串：前端契约要求 operator 非空，
	// 一条空串会让整个操作记录列表判为不合法（2026-10-27 实测）。
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"陈经理", "13800000001"}, "陈经理"},
		{[]string{"", "   ", "13800000001"}, "13800000001"},
		{[]string{"  陈经理  "}, "陈经理"},
		{[]string{}, "—"},
		{[]string{"", " "}, "—"},
	}
	for _, tc := range cases {
		if got := firstNonEmpty(tc.in...); got != tc.want {
			t.Fatalf("firstNonEmpty(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
