package api

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

// 申报记录筛选项从 2026-10-27 起是多选（原型是勾选框），
// 前端按逗号拼接，接口同时接受重复参数。这里盯住解析口径。
func TestFilterQueryValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := map[string]struct {
		query string
		key   string
		want  []string
	}{
		"单值":     {query: "status=PENDING_APPROVAL", key: "status", want: []string{"PENDING_APPROVAL"}},
		"逗号多值":   {query: "status=PENDING_APPROVAL,APPROVED", key: "status", want: []string{"PENDING_APPROVAL", "APPROVED"}},
		"重复参数":   {query: "status=PENDING_APPROVAL&status=APPROVED", key: "status", want: []string{"PENDING_APPROVAL", "APPROVED"}},
		"带空格与空项": {query: "region=CN,%20OVERSEAS%20,", key: "region", want: []string{"CN", "OVERSEAS"}},
		"未传":     {query: "", key: "company", want: nil},
		"中文提交人":  {query: "submitter=陈经理,云桥算力", key: "submitter", want: []string{"陈经理", "云桥算力"}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("GET", "/api/supplier/submissions?"+tc.query, nil)
			got := filterQueryValues(ctx, tc.key)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("解析结果不符：期望 %v，实际 %v", tc.want, got)
			}
		})
	}
}
