package api

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/response"
)

// 8MB 文件上限；请求体上限按 base64 膨胀留足余量。
const (
	reportUploadMaxBytes        = 8 << 20
	reportUploadRequestMaxBytes = 12 << 20
)

type reportUploadRequest struct {
	FileName      string `json:"file_name"`
	ContentBase64 string `json:"content_base64"`
	TestItem      string `json:"test_item"`
	Scene         string `json:"scene"`
	Reason        string `json:"reason"`
}

// upload 对应原型模型广场「测试与报告」里的「上传专项报告」：
// 外部报告的结论由供应方/测试方给出，平台只登记报告事实，
// 所以不写连通/压力结论，也不参与准入判定（准入仍只看平台测试任务）。
func (h *supplyResourceReportHandler) upload(c *gin.Context) {
	if h == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req reportUploadRequest
	if err := decodeSupplyRequestLimit(c, &req, []string{"file_name", "content_base64"}, reportUploadRequestMaxBytes); err != nil {
		supplyHTTPError(c, err)
		return
	}
	resourceID := strings.TrimSpace(c.Param("id"))
	if _, err := strconv.ParseInt(resourceID, 10, 64); err != nil {
		supplyHTTPError(c, supplyRequestError("id", "invalid resource id"))
		return
	}
	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" || len(fileName) > 200 || strings.ContainsAny(fileName, "/\\") {
		supplyHTTPError(c, supplyRequestError("file_name", "invalid file name"))
		return
	}
	content, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil || len(content) == 0 {
		supplyHTTPError(c, supplyRequestError("content_base64", "invalid file content"))
		return
	}
	if len(content) > reportUploadMaxBytes {
		supplyHTTPError(c, supplyRequestError("content_base64", "file is too large"))
		return
	}
	// 只认已知格式：报告附件本来就是 pdf/xlsx/json/csv/md 这几类。
	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	switch format {
	case "pdf", "xlsx", "csv", "json", "md", "markdown", "zip":
	default:
		supplyHTTPError(c, supplyRequestError("file_name", "unsupported report format"))
		return
	}
	scene := strings.TrimSpace(req.Scene)
	if len(scene) > 500 {
		supplyHTTPError(c, supplyRequestError("scene", "scene is too long"))
		return
	}
	actor := middleware.OperatorFrom(c).StaffID
	tx, err := supplypg.GORMModelTxFromContext(c.Request.Context())
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	// 资源写权限沿用「归属采购人」口径，和 can_edit 一致。
	var buyerID, exists int64
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT count(*), coalesce(max(buyer_id),0) FROM public.resource WHERE id=$1`, resourceID).Scan(&exists, &buyerID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if exists == 0 {
		supplyHTTPError(c, supply.ErrNotFound)
		return
	}
	if buyerID != actor {
		supplyHTTPError(c, supply.ErrForbidden)
		return
	}
	var reportID int64
	if err := tx.QueryRowContext(c.Request.Context(), `INSERT INTO public.test_report
(test_job_id,resource_id,report_file,report_format,scene,tested_by,tested_at,source)
VALUES (NULL,$1,$2,$3,NULLIF($4,''),$5,now(),'UPLOADED') RETURNING id`,
		resourceID, fileName, format, scene, actor).Scan(&reportID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, gin.H{"report_id": strconv.FormatInt(reportID, 10), "source": "UPLOADED"})
}

// uploaded 列外部上传的专项报告。它们没有平台测试任务，因此不进平台测试报告历史
// （那份历史的 ID 语义、准入证据链都建立在 test_job 上），单独列出来。
func (h *supplyResourceReportHandler) uploaded(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c, "page", "size"); err != nil {
		supplyHTTPError(c, err)
		return
	}
	// 只有测试平台读取器持有 DB，用可选接口取数，不动既有读取契约。
	reader, ok := h.reader.(interface {
		ListUploadedReports(context.Context, string) ([]supplypg.UploadedReportView, error)
	})
	if !ok {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	list, err := reader.ListUploadedReports(c.Request.Context(), strings.TrimSpace(c.Param("id")))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, gin.H{"list": list, "total": len(list)})
}
