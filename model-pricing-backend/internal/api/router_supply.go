package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
	"net/http"
	"time"
)

type SupplyModelWriter interface {
	Create(context.Context, supply.WriteMeta, supply.ModelData) (*supply.StandardModel, error)
	Update(context.Context, supply.WriteMeta, string, uint64, supply.ModelData) (*supply.StandardModel, error)
	Verify(context.Context, supply.WriteMeta, string, uint64, string) (*supply.StandardModel, error)
}
type SupplyManualModelWriter interface {
	SaveManual(context.Context, supply.WriteMeta, string, uint64, supply.ModelData) (*supply.StandardModel, error)
}
type SupplyNoticeWriter interface {
	MarkRead(context.Context, supply.WriteMeta, string) (*supply.NoticeReadReceipt, error)
}
type SupplyPlanExecutor interface {
	Execute(context.Context, supply.WriteMeta, string, uint64, bool, string) (*supply.PlanExecutionResult, error)
}
type SupplyResourceAdmitter interface {
	Admit(context.Context, supply.WriteMeta, supply.ResourceAdmissionCommand) (*supply.Resource, error)
}
type SupplyResourceConfigBinder interface {
	BindFormalConfig(context.Context, supply.WriteMeta, supply.ResourceConfigBindingCommand) (*supply.ResourceConfigBindingChange, error)
}
type SupplyResourceReportReader interface {
	List(context.Context, string, string, int, int) (*supply.ResourceReportPage, error)
	Get(context.Context, string, string, string) (*supply.ResourceReportDetail, error)
	OpenFile(context.Context, string, string, string, string) (*supply.ResourceReportFileContent, error)
}
type SupplyFormalConfigWriter interface {
	Create(context.Context, supply.WriteMeta, string, supply.FormalConfigInput) (*supply.FormalConfigView, error)
	Update(context.Context, supply.WriteMeta, string, string, uint64, supply.FormalConfigInput) (*supply.FormalConfigView, error)
	Disable(context.Context, supply.WriteMeta, string, string, uint64) (*supply.FormalConfigView, error)
}
type SupplyFormalConfigReader interface {
	List(context.Context, string) ([]*supply.FormalConfigView, error)
}
type SupplyF12Reader interface {
	ListContracts(context.Context, supply.F12ContractQuery) (*supply.F12ContractPage, error)
	ListCredits(context.Context, supply.F12CreditQuery) (*supply.F12CreditPage, error)
}
type SupplyF12ReadinessReader interface {
	Get(context.Context, supply.F12ReadinessRequest) (*supply.F12ReadinessSnapshot, error)
}
type SupplyF12Writer interface {
	CreateContract(context.Context, supply.WriteMeta, supply.F12ContractWriteCommand) (*supply.F12ContractVersion, error)
	ReviseContract(context.Context, supply.WriteMeta, string, uint64, supply.F12ContractWriteCommand) (*supply.F12ContractVersion, error)
	ArchiveContract(context.Context, supply.WriteMeta, string, uint64, supply.F12ContractArchiveCommand) (*supply.F12ContractVersion, error)
	AddCreditVersion(context.Context, supply.WriteMeta, uint64, supply.F12CreditWriteCommand) (*supply.F12CreditVersion, error)
}
type SupplyF12AttachmentService interface {
	Upload(context.Context, supply.WriteMeta, supply.F12AttachmentUploadCommand) (*supply.F12AttachmentMetadata, error)
	Download(context.Context, string, string, string, string, string) (*supply.F12AttachmentContent, error)
}
type SupplySupplierMergePreviewReader interface {
	Preview(context.Context, string, string) (*supply.SupplierMergePreview, error)
}
type SupplySupplierWriter interface {
	Create(context.Context, supply.WriteMeta, supply.SupplierData) (*supply.Supplier, error)
	Update(context.Context, supply.WriteMeta, string, uint64, supply.SupplierData) (*supply.Supplier, error)
}
type SupplyDeps struct {
	PlanWriter                 SupplyPlanWriter
	PricePlanWriter            SupplyPricePlanWriter
	PlanExecutor               SupplyPlanExecutor
	ResourceAdmitter           SupplyResourceAdmitter
	ResourceStages             SupplyResourceStages
	ResourceConfigBinder       SupplyResourceConfigBinder
	ApprovedPriceWriter        SupplyApprovedPriceWriter
	ResourceWorkflow           SupplyResourceWorkflowReader
	ResourceEvidence           SupplyResourceEvidenceWriter
	ResourceReports            SupplyResourceReportReader
	FormalConfigs              SupplyFormalConfigWriter
	FormalConfigReader         SupplyFormalConfigReader
	F12Reader                  SupplyF12Reader
	F12Readiness               SupplyF12ReadinessReader
	F12Writer                  SupplyF12Writer
	F12Attachments             SupplyF12AttachmentService
	SupplierWriter             SupplySupplierWriter
	SupplierReader             *supply.SupplierReader
	SupplierTestConfigs        *supply.SupplierTestConfigService
	SupplierQualificationFiles *supply.SupplierQualificationService
	SupplierMergePreviewReader SupplySupplierMergePreviewReader
	Reader                     *supply.ModelReader
	Writer                     SupplyModelWriter
	IdemStore                  middleware.IdempotencyStore
	Resources                  *supply.ResourceReader
	Notices                    *supply.NoticeReader
	NoticeWriter               SupplyNoticeWriter
	Plans                      *supply.PlanReader
	SourceOperations           *supply.SourceOperationReader
}
type SupplyPlanWriter interface {
	Create(context.Context, supply.WriteMeta, string, uint64, string, time.Time) (*supply.ResourcePlan, error)
	Revise(context.Context, supply.WriteMeta, string, uint64, time.Time) (*supply.ResourcePlan, error)
	Cancel(context.Context, supply.WriteMeta, string, uint64) (*supply.ResourcePlan, error)
}
type SupplyPricePlanWriter interface {
	CreatePriceChange(context.Context, supply.WriteMeta, string, uint64, string, string, string, time.Time) (*supply.ResourcePlan, error)
	RevisePriceChange(context.Context, supply.WriteMeta, string, uint64, string, string, string, time.Time) (*supply.ResourcePlan, error)
}

// RegisterSupplyRoutes is A's integration entry point. The public assembler passes an
// /api/internal group with AuthN and RequestID; this function also enforces internal staff identity.
// It deliberately does not attach legacy role permissions or owner read scopes.
func RegisterSupplyRoutes(internal *gin.RouterGroup, deps SupplyDeps) {
	group := internal.Group("/standard-models")
	group.Use(requireSupplyInternalStaff)
	manualWriter, _ := deps.Writer.(SupplyManualModelWriter)
	h := &supplyModelHandler{reader: deps.Reader, writer: deps.Writer, manualWriter: manualWriter, operations: deps.SourceOperations}
	group.GET("", h.list)
	group.GET("/filter-options", h.options)
	group.GET("/operations", h.modelOperations)
	group.GET("/:id/operations", h.modelOperations)
	group.GET("/:id/operations/:operationId", h.modelOperation)
	group.GET("/:id/versions", h.versions)
	group.GET("/:id/resource-operations", h.sourceOperations)
	group.GET("/:id", h.get)
	var idem gin.HandlerFunc
	if deps.IdemStore == nil {
		idem = func(c *gin.Context) { supplyHTTPError(c, supply.ErrDependencyUnavailable); c.Abort() }
	} else {
		idem = middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey)
	}
	group.POST("", idem, h.create)
	group.PUT("/:id", idem, h.update)
	group.POST("/manual-completion", idem, h.manualCreate)
	group.PUT("/:id/manual-completion", idem, h.manualUpdate)
	group.POST("/:id/verify", idem, h.verify)
	resources := internal.Group("/resources")
	resources.Use(requireSupplyInternalStaff)
	rh := &supplyResourceHandler{reader: deps.Resources}
	resources.GET("", rh.list)
	resources.GET("/filter-options", rh.options)
	resources.GET("/:id", rh.get)
	rph := &supplyResourceReportHandler{reader: deps.ResourceReports}
	resources.GET("/:id/test-reports", rph.list)
	resources.GET("/:id/test-reports/:reportId", rph.get)
	resources.GET("/:id/test-reports/:reportId/files/:fileId", rph.file)
	rwh := &supplyResourceOperationHandler{admitter: deps.ResourceAdmitter, binder: deps.ResourceConfigBinder}
	resources.POST("/:id/admit", idem, rwh.admit)
	resources.PUT("/:id/formal-config", idem, rwh.bindFormalConfig)
	resources.GET("/:id/workflow-actions", resourceWorkflowHandler(deps.ResourceWorkflow))
	resources.POST("/:id/apply-approved-price", idem, (&supplyApprovedPriceHandler{writer: deps.ApprovedPriceWriter}).apply)
	resources.POST("/:id/refresh-test-evidence", idem, refreshResourceEvidenceHandler(deps.ResourceEvidence))
	suppliers := internal.Group("/suppliers")
	suppliers.Use(requireSupplyInternalStaff)
	suppliers.GET("/:id/operations", h.supplierOperations)
	fch := &supplyFormalConfigHandler{writer: deps.FormalConfigs, reader: deps.FormalConfigReader}
	suppliers.GET("/:id/formal-configs", fch.list)
	suppliers.POST("/:id/formal-configs", idem, fch.create)
	suppliers.PUT("/:id/formal-configs/:configId", idem, fch.update)
	suppliers.DELETE("/:id/formal-configs/:configId", idem, fch.disable)
	supplierEntities := internal.Group("/supplier-entities")
	supplierEntities.Use(requireSupplyInternalStaff)
	seh := &supplySupplierHandler{writer: deps.SupplierWriter, reader: deps.SupplierReader, qualificationFiles: deps.SupplierQualificationFiles}
	testConfigHandler := &supplySupplierTestConfigHandler{service: deps.SupplierTestConfigs}
	supplierEntities.GET("", seh.list)
	supplierEntities.GET("/:id", seh.get)
	supplierEntities.POST("/:id/resource-actions", idem, resourceBatchHandler(deps.ResourceStages))
	supplierEntities.GET("/:id/test-config", testConfigHandler.get)
	supplierEntities.PUT("/:id/test-config", idem, testConfigHandler.save)
	supplierEntities.GET("/:id/qualifications", seh.qualifications)
	supplierEntities.POST("/:id/qualifications", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, supplierQualificationRequestMaxBytes)
		c.Next()
	}, idem, seh.uploadQualification)
	supplierEntities.PUT("/:id/qualifications/:qualificationId", idem, seh.updateQualification)
	supplierEntities.GET("/:id/qualifications/:qualificationId/file", seh.qualificationFile)
	mergePreview := &supplySupplierMergePreviewHandler{reader: deps.SupplierMergePreviewReader}
	supplierEntities.POST("/:id/merge-preview", mergePreview.preview)
	supplierEntities.POST("", idem, seh.create)
	supplierEntities.PUT("/:id", idem, seh.update)
	contracts := internal.Group("/contracts")
	contracts.Use(requireSupplyInternalStaff)
	f12h := &supplyF12ReadHandler{reader: deps.F12Reader}
	f12w := &supplyF12WriteHandler{writer: deps.F12Writer}
	contracts.GET("", f12h.contracts)
	contracts.POST("", idem, f12w.createContract)
	contracts.POST("/:contractKey/versions", idem, f12w.reviseContract)
	contracts.POST("/:contractKey/archive", idem, f12w.archiveContract)
	credits := internal.Group("/credits")
	credits.Use(requireSupplyInternalStaff)
	credits.GET("", f12h.credits)
	credits.POST("", idem, f12w.addCreditVersion)
	f12 := internal.Group("/f12")
	f12.Use(requireSupplyInternalStaff)
	f12.GET("/readiness", (&supplyF12ReadinessHandler{reader: deps.F12Readiness}).get)
	f12Attachments := internal.Group("/f12/attachments")
	f12Attachments.Use(requireSupplyInternalStaff)
	f12ah := &supplyF12AttachmentHandler{service: deps.F12Attachments}
	f12Attachments.POST("", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, f12AttachmentRequestMaxBytes)
		c.Next()
	}, idem, f12ah.upload)
	f12Attachments.GET("/:fileID", f12ah.download)
	notices := internal.Group("/notices")
	notices.Use(requireSupplyInternalStaff)
	nh := &supplyNoticeHandler{reader: deps.Notices, writer: deps.NoticeWriter}
	notices.GET("", nh.listCurrent)
	notices.GET("/read-states", nh.listPeople)
	notices.GET("/:id", nh.get)
	notices.POST("/:id/read", idem, nh.markRead)
	plans := internal.Group("/plans")
	plans.Use(requireSupplyInternalStaff)
	ph := &supplyPlanHandler{reader: deps.Plans}
	plans.GET("", ph.list)
	plans.GET("/:id", ph.get)
	pw := &supplyPlanWriteHandler{writer: deps.PlanWriter}
	plans.POST("", idem, pw.create)
	plans.PUT("/:id", idem, pw.revise)
	plans.POST("/:id/cancel", idem, pw.cancel)
	pricePlans := &supplyPricePlanWriteHandler{writer: deps.PricePlanWriter}
	plans.POST("/price-change", idem, pricePlans.create)
	plans.PUT("/:id/price-change", idem, pricePlans.revise)
	pe := &supplyPlanExecutionHandler{executor: deps.PlanExecutor}
	plans.POST("/:id/execute", idem, pe.execute)
}

func requireSupplyInternalStaff(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		c.Abort()
		return
	}
	if op.PortalType != "INTERNAL" || op.OperatorType != "STAFF" || op.StaffID <= 0 {
		response.Error(c, apperr.ErrForbidden)
		c.Abort()
		return
	}
	c.Next()
}

// registerSupplyRoutes is the entry called by the main branch public assembler.
// The A handlers above require concrete target-schema stores and shared services.
// Keep registration closed until those adapters are supplied through Deps;
// do not wire nil stores or silently fall back to the legacy schema.
func registerSupplyRoutes(g *gin.RouterGroup, protected *gin.RouterGroup, portal string, deps Deps) {
	if portal != "internal" || protected == nil || deps.SupplyRoutes == nil {
		return
	}
	RegisterSupplyRoutes(protected, *deps.SupplyRoutes)
}
