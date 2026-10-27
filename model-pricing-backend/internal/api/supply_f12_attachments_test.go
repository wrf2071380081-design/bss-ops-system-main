package api

import (
	"context"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
)

type f12AttachmentHTTPServiceFake struct {
	command supply.F12AttachmentUploadCommand
	actorID string
	calls   int
}

func (f *f12AttachmentHTTPServiceFake) Upload(_ context.Context, meta supply.WriteMeta, command supply.F12AttachmentUploadCommand) (*supply.F12AttachmentMetadata, error) {
	f.calls++
	f.actorID, f.command = meta.ActorID, command
	return &supply.F12AttachmentMetadata{
		ID: "f12:v1:" + strings.Repeat("a", 32), SubjectType: command.SubjectType, SubjectID: command.SubjectID,
		Purpose: command.Purpose, Name: command.FileName, ContentType: "application/pdf", UploadedBy: meta.ActorID, Size: len(command.Content),
	}, nil
}

func (*f12AttachmentHTTPServiceFake) Download(context.Context, string, string, string, string, string) (*supply.F12AttachmentContent, error) {
	return nil, supply.ErrNotFound
}

func TestF12AttachmentOrdinaryMaintenanceOmitsFlowFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(infra.RequestID())
	router.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	service := &f12AttachmentHTTPServiceFake{}
	RegisterSupplyRoutes(router.Group("/api/internal"), SupplyDeps{IdemStore: newSupplyHTTPIdemStore(), F12Attachments: service})
	body := `{"subject_type":"SUPPLIER","subject_id":"10","purpose":"CONTRACT","file_name":"contract.pdf","content_base64":"Zm9v","reason":"普通合同维护"}`
	rec := supplyHTTPCall(router, "POST", "/api/internal/f12/attachments", body, "f12-ordinary-no-flow", "trace")
	if rec.Code != 200 || service.calls != 1 || service.command.SourceFlowID != "" || service.command.ExpectedFlowRevision != 0 {
		t.Fatalf("ordinary upload must not require a workflow: status=%d body=%s calls=%d", rec.Code, rec.Body, service.calls)
	}
}

func TestF12AttachmentUploadForwardsExpectedFlowCAS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(infra.RequestID())
	router.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	service := &f12AttachmentHTTPServiceFake{}
	RegisterSupplyRoutes(router.Group("/api/internal"), SupplyDeps{
		IdemStore: newSupplyHTTPIdemStore(), F12Attachments: service,
	})
	body := `{"subject_type":"CUSTOMER","subject_id":"10","purpose":"CONTRACT","source_flow_id":"41","source_module_key":"CONTRACT","expected_flow_revision":"12","file_name":"contract.pdf","content_base64":"Zm9v","reason":"首次办理合同"}`
	rec := supplyHTTPCall(router, "POST", "/api/internal/f12/attachments", body, "f12-attachment-flow-cas", "trace")
	if rec.Code != 200 || service.calls != 1 || service.actorID != "7" || service.command.SubjectType != "CUSTOMER" || service.command.SubjectID != "10" ||
		service.command.SourceFlowID != "41" || service.command.SourceModuleKey != "CONTRACT" || service.command.ExpectedFlowRevision != 12 || string(service.command.Content) != "foo" {
		t.Fatalf("flow attachment context was not forwarded: status=%d body=%s calls=%d actor=%s command=%+v", rec.Code, rec.Body, service.calls, service.actorID, service.command)
	}
}
