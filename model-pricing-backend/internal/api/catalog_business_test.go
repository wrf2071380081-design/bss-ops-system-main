package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/auth"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type catalogTestSessions struct{ snapshot *auth.Snapshot }

func (s catalogTestSessions) FindSessionByTokenHash(context.Context, string) (*auth.SessionView, error) {
	return &auth.SessionView{Snapshot: s.snapshot, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (s catalogTestSessions) RefreshSessionExpiry(context.Context, int64, time.Time) error {
	return nil
}
func TestCatalogWorkerAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, service, employee, body string
		snapshot                      *auth.Snapshot
		want                          int
	}{
		{name: "missing service token", body: `{"operation":"status","vendor":"deepseek"}`, want: 401},
		{name: "supplier denied", service: strings.Repeat("s", 40), employee: "token", body: `{"operation":"status","vendor":"deepseek"}`, snapshot: &auth.Snapshot{PortalType: "SUPPLIER", OperatorType: "SUPPLIER"}, want: 403},
		{name: "forged staff", service: strings.Repeat("s", 40), employee: "token", body: `{"operation":"status","vendor":"deepseek","staffId":"9"}`, snapshot: &auth.Snapshot{PortalType: "INTERNAL", OperatorType: "STAFF", StaffID: 3, Perms: []string{"M1:V"}}, want: 403},
		{name: "employee credential required", service: strings.Repeat("s", 40), body: `{"operation":"status","vendor":"deepseek","staffId":"3"}`, want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			registerCatalogBusiness(r, &CatalogBusinessStore{}, catalogTestSessions{tc.snapshot}, strings.Repeat("s", 40))
			req := httptest.NewRequest("POST", "/api/agent/catalog/business", strings.NewReader(strings.Replace(tc.body, "{", `{"schemaVersion":"catalog-business.v1",`, 1)))
			if tc.service != "" {
				req.Header.Set("Authorization", "Bearer "+tc.service)
			}
			req.Header.Set("X-BSS-Employee-Token", tc.employee)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status %d", w.Code)
			}
		})
	}
}

func TestCatalogVisibility(t *testing.T) {
	for _, tc := range []struct {
		scope        string
		owner, actor int64
		want         bool
	}{
		{"platform", 3, 4, true}, {"personal", 3, 4, false}, {"personal", 3, 3, true},
		{"manual", 3, 4, false}, {"manual", 3, 3, true}, {"unknown", 3, 3, false},
	} {
		if got := catalogVisible(tc.scope, tc.owner, tc.actor); got != tc.want {
			t.Errorf("%s owner=%d actor=%d: %v", tc.scope, tc.owner, tc.actor, got)
		}
	}
}

func TestCatalogOrigin(t *testing.T) {
	for _, tc := range []struct {
		key, scope string
		owner      int64
		valid      bool
	}{
		{"manual:3:request", "manual", 3, true}, {"personal:4:request", "personal", 4, true},
		{"daily:2026-11-24", "platform", 0, true}, {"manual:0:x", "", 0, false},
		{"personal:abc:x", "", 0, false}, {"unknown:3:x", "", 0, false},
	} {
		scope, owner, err := catalogOrigin(tc.key)
		if (err == nil) != tc.valid || (tc.valid && (scope != tc.scope || owner != tc.owner)) {
			t.Errorf("%s: %s %d %v", tc.key, scope, owner, err)
		}
	}
}
