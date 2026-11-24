package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/pkg/perm"
)

func TestAgentCatalogScheduleProxyUsesEmployeeAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token := strings.Repeat("a", 64)
	calls := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("bss_employee_session")
		if err != nil || cookie.Value != token || r.URL.RequestURI() != "/employee/schedules?view=catalog" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls <- r.Method
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"canManage":false,"plans":[]}`))
	}))
	defer server.Close()

	router := gin.New()
	router.Use(func(c *gin.Context) {
		permissions := []string{perm.M1View}
		if c.GetHeader("Fixture-Edit") == "1" {
			permissions = append(permissions, perm.M1Edit)
		}
		c.Set("auth.operator", &middleware.Operator{PortalType: "INTERNAL", OperatorType: "STAFF"})
		c.Set("auth.perms", permissions)
	})
	registerAgentCatalog(router.Group("/api/internal"), server.URL)
	for _, item := range []struct {
		method string
		edit   bool
		status int
	}{
		{http.MethodGet, false, http.StatusOK},
		{http.MethodPost, false, http.StatusOK},
		{http.MethodPost, true, http.StatusOK},
	} {
		req := httptest.NewRequest(item.method, "/api/internal/agent-catalog/schedules?view=other", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if item.edit {
			req.Header.Set("Fixture-Edit", "1")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != item.status {
			t.Fatalf("%s returned %d, expected %d", item.method, rec.Code, item.status)
		}
		if item.status == http.StatusOK && !strings.Contains(rec.Body.String(), `"code":0`) {
			t.Fatalf("missing BSS success envelope: %s", rec.Body.String())
		}
	}
	close(calls)
	var forwarded []string
	for method := range calls {
		forwarded = append(forwarded, method)
	}
	if strings.Join(forwarded, ",") != "GET,POST,POST" {
		t.Fatalf("unexpected forwarded requests: %v", forwarded)
	}
}
