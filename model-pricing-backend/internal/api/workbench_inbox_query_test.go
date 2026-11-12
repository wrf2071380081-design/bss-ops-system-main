package api

import "testing"

// 多选筛选的参数解析：既要支持 ?action=a,b，也要支持 ?action=a&action=b。
func TestSplitQueryValues(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, nil},
		{"single", []string{"APPROVE"}, []string{"APPROVE"}},
		{"comma", []string{"APPROVE,REJECT"}, []string{"APPROVE", "REJECT"}},
		{"repeated", []string{"APPROVE", "REJECT"}, []string{"APPROVE", "REJECT"}},
		{"mixed_with_spaces", []string{"APPROVE, REJECT", "UPDATE"}, []string{"APPROVE", "REJECT", "UPDATE"}},
		{"trailing_comma", []string{"APPROVE,"}, []string{"APPROVE"}},
		{"only_commas", []string{",,,"}, nil},
	}
	for _, c := range cases {
		got := splitQueryValues(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: got %v want %v", c.name, got, c.want)
			}
		}
	}
}

// 人员 id 多选：非法值跳过，不能把整条筛选变成 0 条。
func TestParseQueryIDs(t *testing.T) {
	got := parseQueryIDs([]string{"6,1", "52", "abc", "0", "-3"})
	want := []int64{6, 1, 52}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if parseQueryIDs([]string{"", "x"}) != nil {
		t.Fatalf("expect nil when no valid id")
	}
}
