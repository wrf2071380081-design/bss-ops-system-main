package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
	"time"
)

type SupplyModelWriter interface {
	Create(context.Context, supply.WriteMeta, supply.ModelData) (*supply.StandardModel, error)
	Update(context.Context, supply.WriteMeta, string, uint64, supply.ModelData) (*supply.StandardModel, error)
	Verify(context.Context, supply.WriteMeta, string, uint64, string) (*supply.StandardModel, error)
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
type SupplySupplierWriter interface {
	Create(context.Context, supply.WriteMeta, supply.SupplierData) (*supply.Supplier, error)
	Update(context.Context, supply.WriteMeta, string, uint64, supply.SupplierData) (*supply.Supplier, error)
}
type SupplyDeps struct {
	PlanWriter           SupplyPlanWriter
	PlanExecutor         SupplyPlanExecutor
	ResourceAdmitter     SupplyResourceAdmitter
	ResourceConfigBinder SupplyResourceConfigBinder
	ResourceReports      SupplyResourceReportReader
	FormalConfigs        SupplyFormalConfigWriter
	FormalConfigReader   SupplyFormalConfigReader
	F12Reader            SupplyF12Reader
	SupplierWriter       SupplySupplierWriter
	SupplierReader       *supply.SupplierReader
	Reader               *supply.ModelReader
	Writer               SupplyModelWriter
	IdemStore            middleware.IdempotencyStore
	Resources            *supply.ResourceReader
	Notices              *supply.NoticeReader
	NoticeWriter         SupplyNoticeWriter
	Plans                *supply.PlanReader
	SourceOperations     *supply.SourceOperationReader
}
type SupplyPlanWriter interface {
	Create(context.Context, supply.WriteMeta, string, uint64, string, time.Time) (*supply.ResourcePlan, error)
	Revise(context.Context, supply.WriteMeta, string, uint64, time.Time) (*supply.ResourcePlan, error)
	Cancel(context.Context, supply.WriteMeta, string, uint64) (*supply.ResourcePlan, error)
}

// RegisterSupplyRoutes is A's integration entry point. The public assembler passes an
// /api/internal group with AuthN and RequestID; this function also enforces internal staff identity.
// It deliberately does not attach legacy role permissions or owner read scopes.
func RegisterSupplyRoutes(internal *gin.RouterGroup, deps SupplyDeps) {
	group := internal.Group("/standard-models")
	group.Use(requireSupplyInternalStaff)
	h := &supplyModelHandler{reader: deps.Reader, writer: deps.Writer, operations: deps.SourceOperations}
	group.GET("", h.list)
	group.GET("/filter-options", h.options)
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
	suppliers := internal.Group("/suppliers")
	suppliers.Use(requireSupplyInternalStaff)
	fch := &supplyFormalConfigHandler{writer: deps.FormalConfigs, reader: deps.FormalConfigReader}
	suppliers.GET("/:id/formal-configs", fch.list)
	suppliers.POST("/:id/formal-configs", idem, fch.create)
	suppliers.PUT("/:id/formal-configs/:configId", idem, fch.update)
	suppliers.DELETE("/:id/formal-configs/:configId", idem, fch.disable)
	supplierEntities := internal.Group("/supplier-entities")
	supplierEntities.Use(requireSupplyInternalStaff)
	seh := &supplySupplierHandler{writer: deps.SupplierWriter, reader: deps.SupplierReader}
	supplierEntities.GET("", seh.list)
	supplierEntities.GET("/:id", seh.get)
	supplierEntities.POST("", idem, seh.create)
	supplierEntities.PUT("/:id", idem, seh.update)
	contracts := internal.Group("/contracts")
	contracts.Use(requireSupplyInternalStaff)
	f12h := &supplyF12ReadHandler{reader: deps.F12Reader}
	contracts.GET("", f12h.contracts)
	credits := internal.Group("/credits")
	credits.Use(requireSupplyInternalStaff)
	credits.GET("", f12h.credits)
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
