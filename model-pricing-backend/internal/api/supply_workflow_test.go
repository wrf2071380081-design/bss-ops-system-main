package api

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"testing"
)

type workflowReaderFake struct {
	actor, id string
	result    *ResourceWorkflowActions
}

func (f *workflowReaderFake) GetResourceWorkflow(_ context.Context, actor, id string) (*ResourceWorkflowActions, error) {
	f.actor = actor
	f.id = id
	return f.result, nil
}
func TestResourceWorkflowNeverAdvertisesMissingWriteProviders(t *testing.T) {
	f := &workflowReaderFake{result: &ResourceWorkflowActions{CanBind: true, CanAdmit: true, ApprovedPrices: []ApprovedPriceOption{{SourceRowID: "71", SourceRevision: "3", Coefficient: "0.5"}}}}
	r := &configuredWorkflowReader{reader: f, bind: true}
	got, err := r.GetResourceWorkflow(context.Background(), "7", "11")
	if err != nil || !got.CanBind || got.CanAdmit || len(got.ApprovedPrices) != 0 {
		t.Fatal("incomplete writes were advertised")
	}
	if !f.result.CanAdmit || len(f.result.ApprovedPrices) != 1 {
		t.Fatal("provider snapshot mutated")
	}
	if _, err := (&configuredWorkflowReader{}).GetResourceWorkflow(context.Background(), "7", "11"); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal("missing facts must not become allowed")
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	RegisterSupplyRoutes(engine.Group("/api/internal"), SupplyDeps{ResourceWorkflow: r})
	rec := supplyHTTPCall(engine, "GET", "/api/internal/resources/11/workflow-actions", "", "", "trace")
	if rec.Code != 200 || f.actor != "7" || f.id != "11" {
		t.Fatal("viewer must not override authenticated operator")
	}
}
