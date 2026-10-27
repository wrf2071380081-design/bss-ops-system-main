package api

import (
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

func refreshResourceEvidenceHandler(writer SupplyResourceEvidenceWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if writer == nil {
			supplyHTTPError(c, supply.ErrDependencyUnavailable)
			return
		}
		if _, err := planPositiveID(c.Param("id")); err != nil {
			supplyHTTPError(c, err)
			return
		}
		var req struct {
			Revision string `json:"expected_revision"`
			Reason   string `json:"reason"`
		}
		if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "reason"}); err != nil {
			supplyHTTPError(c, err)
			return
		}
		expected, err := planPositiveID(req.Revision)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		meta, err := supplyWriteMeta(c, req.Reason)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		r, err := writer.RefreshEvidence(c.Request.Context(), meta, c.Param("id"), expected)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		if r == nil || r.ID != c.Param("id") {
			supplyHTTPError(c, supply.ErrReadContract)
			return
		}
		receipt := gin.H{"resource_id": r.ID, "revision": uintString(r.Revision), "report_id": r.LatestReportID, "state": r.State}
		middleware.SetIdempotencyResult(c, receipt)
		response.Success(c, receipt)
	}
}
