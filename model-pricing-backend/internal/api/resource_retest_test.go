package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	tp "model_bss/internal/domain/testplatform"
)

type retestStarterProbe struct {
	called bool
	cmd    tp.CreateCommand
}

func (s *retestStarterProbe) Start(_ context.Context, c tp.CreateCommand) (*tp.StartResult, error) {
	s.called = true
	s.cmd = c
	return &tp.StartResult{LocalJobID: "31", ExternalID: "external31", Status: "queued"}, nil
}

func TestInternalRetestRejectsClientCredentialOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	starter := &retestStarterProbe{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth.operator", &middleware.Operator{PortalType: "INTERNAL", OperatorType: "STAFF", StaffID: 7})
		c.Next()
	})
	router.POST("/resources/:id/tests", (&submissionTestHandler{starter: starter}).start)
	req := httptest.NewRequest("POST", "/resources/11/tests", strings.NewReader(`{"category":"baseline","model":"supplier-model","base_url":"https://supplier.invalid","api_key":"supplier-sentinel"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 400 || starter.called {
		t.Fatalf("client must not override internal platform target: HTTP %d, called=%v", rec.Code, starter.called)
	}
}

type internalTargetProbe struct {
	actor       string
	unavailable bool
}

func (p *internalTargetProbe) TestTarget(_ context.Context, actor, resource string) (*ResourceTestTarget, error) {
	p.actor = actor
	if actor != "7" {
		return nil, supply.ErrForbidden
	}
	if p.unavailable {
		return &ResourceTestTarget{Model: "platform-model"}, nil
	}
	return &ResourceTestTarget{Model: "platform-model", BaseURL: "https://platform.invalid/v1", SealedAPIKey: "platform-sentinel", TargetFingerprint: "platform-fingerprint"}, nil
}
func TestInternalRetestUsesServerPlatformTargetAndCurrentOperator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	starter := &retestStarterProbe{}
	targets := &internalTargetProbe{}
	for _, staff := range []int64{7, 8} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("auth.operator", &middleware.Operator{PortalType: "INTERNAL", OperatorType: "STAFF", StaffID: staff})
			c.Next()
		})
		router.POST("/resources/:id/tests", (&submissionTestHandler{starter: starter, targets: targets}).start)
		req := httptest.NewRequest("POST", "/resources/11/tests", strings.NewReader(`{"category":"stress"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if staff == 7 {
			if rec.Code != 200 || starter.cmd.Target.BaseURL != "https://platform.invalid/v1" || starter.cmd.Target.APIKey != "platform-sentinel" || starter.cmd.Source != "INTERNAL" || strings.Contains(rec.Body.String(), "platform-sentinel") {
				t.Fatal("server platform target not safely used", rec.Code)
			}
		} else if rec.Code != 403 {
			t.Fatal("another staff must be forbidden", rec.Code)
		}
	}
	if targets.actor != "8" {
		t.Fatal("authenticated actor must reach authority provider")
	}
}
func TestInternalRetestOptionsAndMissingConfigDoNotStart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	starter := &retestStarterProbe{}
	h := &submissionTestHandler{starter: starter, targets: &internalTargetProbe{unavailable: true}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth.operator", &middleware.Operator{PortalType: "INTERNAL", OperatorType: "STAFF", StaffID: 7})
		c.Next()
	})
	router.GET("/resources/:id/tests/options", h.options)
	router.POST("/resources/:id/tests", h.start)
	opt := httptest.NewRecorder()
	router.ServeHTTP(opt, httptest.NewRequest("GET", "/resources/11/tests/options", nil))
	if opt.Code != 200 || !strings.Contains(opt.Body.String(), `"can_start":false`) {
		t.Fatal("missing config option must be explicit", opt.Code)
	}
	req := httptest.NewRequest("POST", "/resources/11/tests", strings.NewReader(`{"category":"baseline"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 503 || starter.called {
		t.Fatal("missing platform config must not start")
	}
}
