package api

import (
	"math"
	"math/big"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

var reportDecimal = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
var reportTokenCount = regexp.MustCompile(`^[0-9]+$`)

// reportPercent 是平台直接上报的命中率形态（0-100，最多两位小数）。
var reportPercent = regexp.MustCompile(`^(0|[1-9][0-9]?)(\.[0-9]{1,2})?$|^100(\.0{1,2})?$`)

type supplyResourceReportHandler struct{ reader SupplyResourceReportReader }

func (h *supplyResourceReportHandler) list(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	values, err := supplyQuery(c, "page", "size")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 10
	}
	if size > 100 || page > math.MaxInt/size {
		supplyHTTPError(c, supplyRequestError("page", "pagination is out of range"))
		return
	}
	resourceID := c.Param("id")
	if _, err := planPositiveID(resourceID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	actorID, err := reportActorID(c)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.List(c.Request.Context(), actorID, resourceID, page, size)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if result == nil || result.Page != page || result.Size != size || result.Total < 0 || len(result.List) > size || int64(len(result.List)) > result.Total {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	seen := make(map[string]struct{}, len(result.List))
	for i := range result.List {
		if !validResourceReport(&result.List[i], resourceID) {
			supplyHTTPError(c, supply.ErrReadContract)
			return
		}
		if _, ok := seen[result.List[i].ID]; ok {
			supplyHTTPError(c, supply.ErrReadContract)
			return
		}
		seen[result.List[i].ID] = struct{}{}
	}
	response.Success(c, result)
}

func (h *supplyResourceReportHandler) get(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	resourceID, reportID, actorID, ok := reportRequestIdentity(c)
	if !ok {
		return
	}
	result, err := h.reader.Get(c.Request.Context(), actorID, resourceID, reportID)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if !validResourceReportDetail(result, resourceID, reportID) {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	response.Success(c, result)
}

func (h *supplyResourceReportHandler) file(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	resourceID, reportID, actorID, ok := reportRequestIdentity(c)
	if !ok {
		return
	}
	fileID := c.Param("fileId")
	if _, err := planPositiveID(fileID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	// Check report and file ownership before opening bytes. The attachment
	// provider must repeat the ownership/authorization check in OpenFile.
	detail, err := h.reader.Get(c.Request.Context(), actorID, resourceID, reportID)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if !validResourceReportDetail(detail, resourceID, reportID) {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	allowed := false
	expectedName := ""
	for _, file := range detail.Files {
		if file.ID == fileID {
			allowed = file.CanDownload
			expectedName = file.Name
			break
		}
	}
	if !allowed {
		supplyHTTPError(c, supply.ErrForbidden)
		return
	}
	content, err := h.reader.OpenFile(c.Request.Context(), actorID, resourceID, reportID, fileID)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if content == nil || content.Body == nil || content.Size < 0 || content.Name != expectedName || strings.TrimSpace(content.Name) == "" || strings.ContainsAny(content.Name, `/\\`) {
		if content != nil && content.Body != nil {
			_ = content.Body.Close()
		}
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	defer content.Body.Close()
	contentType := safeReportContentType(content.ContentType)
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": content.Name})
	if disposition == "" {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	c.Header("Content-Disposition", disposition)
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, content.Size, contentType, content.Body, nil)
}

func reportActorID(c *gin.Context) (string, error) {
	op := middleware.OperatorFrom(c)
	if op == nil || op.PortalType != "INTERNAL" || op.OperatorType != "STAFF" || op.StaffID <= 0 {
		return "", supply.ErrForbidden
	}
	return strconv.FormatInt(op.StaffID, 10), nil
}

func reportRequestIdentity(c *gin.Context) (string, string, string, bool) {
	resourceID, reportID := c.Param("id"), c.Param("reportId")
	if _, err := planPositiveID(resourceID); err != nil {
		supplyHTTPError(c, err)
		return "", "", "", false
	}
	if _, err := planPositiveID(reportID); err != nil {
		supplyHTTPError(c, err)
		return "", "", "", false
	}
	actorID, err := reportActorID(c)
	if err != nil {
		supplyHTTPError(c, err)
		return "", "", "", false
	}
	return resourceID, reportID, actorID, true
}

func validResourceReport(report *supply.ResourceReportView, resourceID string) bool {
	if report == nil || report.ResourceID != resourceID {
		return false
	}
	if _, err := planPositiveID(report.ID); err != nil {
		return false
	}
	if report.Status != "RUNNING" && report.Status != "COMPLETED" && report.Status != "FAILED" {
		return false
	}
	if report.CreatedAt.IsZero() || report.TestedAt.IsZero() || strings.TrimSpace(report.TestItem) == "" || (report.Source != "" && report.Source != "SUPPLIER" && report.Source != "INTERNAL") || report.TargetMatches == nil || (report.Status != "RUNNING" && report.FinishedAt == nil) {
		return false
	}
	if report.FinishedAt != nil && report.FinishedAt.Before(report.CreatedAt) {
		return false
	}
	for _, metric := range []*string{report.Metrics.ConnectionMS, report.Metrics.RPS, report.Metrics.TPS} {
		if metric != nil && !reportDecimal.MatchString(*metric) {
			return false
		}
	}
	cache := report.CacheStats
	if cache.ReportID != report.ID {
		return false
	}
	switch cache.Status {
	case "PROVIDED":
		// Q4：平台只上报比率时没有 Token 明细，此时用 hit_percent 表达已上报值。
		if cache.HitTokens == "" && cache.InputTokens == "" {
			if !reportPercent.MatchString(cache.HitPercent) {
				return false
			}
			return true
		}
		if !reportTokenCount.MatchString(cache.HitTokens) || !reportTokenCount.MatchString(cache.InputTokens) {
			return false
		}
		hit, okHit := new(big.Int).SetString(cache.HitTokens, 10)
		input, okInput := new(big.Int).SetString(cache.InputTokens, 10)
		if !okHit || !okInput || input.Sign() <= 0 || hit.Cmp(input) > 0 {
			return false
		}
	case "NOT_PROVIDED", "NOT_APPLICABLE", "NOT_TESTED":
		if cache.HitTokens != "" || cache.InputTokens != "" || cache.HitPercent != "" {
			return false
		}
	default:
		return false
	}
	return true
}

func validResourceReportDetail(detail *supply.ResourceReportDetail, resourceID, reportID string) bool {
	if detail == nil || detail.ID != reportID || detail.Files == nil || !validResourceReport(&detail.ResourceReportView, resourceID) {
		return false
	}
	seen := make(map[string]struct{}, len(detail.Files))
	for _, file := range detail.Files {
		if _, err := planPositiveID(file.ID); err != nil || strings.TrimSpace(file.Name) == "" || strings.ContainsAny(file.Name, `/\\`) {
			return false
		}
		if file.CanDownload && file.UnavailableReason != "" {
			return false
		}
		if _, ok := seen[file.ID]; ok {
			return false
		}
		seen[file.ID] = struct{}{}
	}
	return true
}

func safeReportContentType(raw string) string {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "application/octet-stream"
	}
	switch mediaType {
	case "application/pdf", "text/plain", "text/csv", "image/png", "image/jpeg", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return mediaType
	default:
		return "application/octet-stream"
	}
}
