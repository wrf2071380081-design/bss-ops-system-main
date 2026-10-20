package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

type SupplyModelWriter interface {
	Create(context.Context, supply.WriteMeta, supply.ModelData) (*supply.StandardModel, error)
	Update(context.Context, supply.WriteMeta, string, uint64, supply.ModelData) (*supply.StandardModel, error)
	Verify(context.Context, supply.WriteMeta, string, uint64, string) (*supply.StandardModel, error)
}
type SupplyNoticeWriter interface {
	MarkRead(context.Context, supply.WriteMeta, string) (*supply.NoticeReadReceipt, error)
}
type SupplyDeps struct {
	Reader       *supply.ModelReader
	Writer       SupplyModelWriter
	IdemStore    middleware.IdempotencyStore
	Resources    *supply.ResourceReader
	Notices      *supply.NoticeReader
	NoticeWriter SupplyNoticeWriter
	Plans        *supply.PlanReader
}

// RegisterSupplyRoutes is A's integration entry point. The public assembler passes an
// /api/internal group with AuthN and RequestID; this function also enforces internal staff identity.
// It deliberately does not attach legacy role permissions or owner read scopes.
func RegisterSupplyRoutes(internal *gin.RouterGroup, deps SupplyDeps) {
	group := internal.Group("/standard-models")
	group.Use(requireSupplyInternalStaff)
	h := &supplyModelHandler{reader: deps.Reader, writer: deps.Writer}
	group.GET("", h.list)
	group.GET("/:id/versions", h.versions)
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
	plans.GET("/:id", ph.get)
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
