package supplypg

import (
	"context"
	"testing"
)

// 原型「模型类型」「接入状态」两组筛选项都是写死的枚举：
// 前者四项（文本/图片/视频/音频），后者三档（待接入/已接入/已下架）。
// 这里锁住契约，避免有人又改回「按数据推导」——那样列表一空下拉就空了。
func TestSupplierModelFilterOptionsAreFixedEnums(t *testing.T) {
	store := &SupplierModelStore{}
	options, err := store.FilterOptions(context.Background(), 0)
	if err != nil {
		t.Fatalf("空主体不应报错：%v", err)
	}
	wantTypes := []string{"TEXT", "IMAGE", "VIDEO", "AUDIO"}
	if len(options.ModelTypes) != len(wantTypes) {
		t.Fatalf("模型类型应是固定四项，实际 %v", options.ModelTypes)
	}
	for i, want := range wantTypes {
		if options.ModelTypes[i] != want {
			t.Fatalf("模型类型第 %d 项应为 %s，实际 %s", i+1, want, options.ModelTypes[i])
		}
	}
	wantStates := []string{"PENDING_ACCESS", "ADMITTED", "OFFLINE"}
	if len(options.States) != len(wantStates) {
		t.Fatalf("接入状态应是固定三档，实际 %v", options.States)
	}
	for i, want := range wantStates {
		if options.States[i] != want {
			t.Fatalf("接入状态第 %d 项应为 %s，实际 %s", i+1, want, options.States[i])
		}
	}
	// 公司是开放集合，没有数据时必须是空切片（前端据此判断"无可选公司"）。
	if options.Companies == nil || len(options.Companies) != 0 {
		t.Fatalf("无资源时公司候选值应为空切片，实际 %v", options.Companies)
	}
}

// 待测试与待接入在供应商侧都是「待接入」，不能露出内部阶段词。
func TestSupplierAccessStatusFoldsPendingTest(t *testing.T) {
	cases := map[string]string{
		"PENDING_TEST":   "PENDING_ACCESS",
		"PENDING_ACCESS": "PENDING_ACCESS",
		"ADMITTED":       "ADMITTED",
		"OFFLINE":        "OFFLINE",
		"UNKNOWN":        "PENDING_ACCESS",
	}
	for state, want := range cases {
		if got := SupplierAccessStatus(state); got != want {
			t.Fatalf("%s 应折叠成 %s，实际 %s", state, want, got)
		}
	}
}
