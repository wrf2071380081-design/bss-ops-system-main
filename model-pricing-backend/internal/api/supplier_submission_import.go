package api

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// 导入文件上限：模板本身很小，2MB 足够容纳 200 行。
const maxImportFileSize = 2 << 20

// importTemplate 下载申报导入模板（xlsx，含标准模型目录与填写说明）。
func (h *supplierSubmissionHandler) importTemplate(c *gin.Context) {
	if _, ok := h.supplierID(c); !ok {
		return
	}
	models, err := h.svc.store.ActiveModelCatalog(c.Request.Context())
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	// 模板里要带「准入配置组」示例：用本主体已存在的组名（没有就留占位）。
	groupNames, err := h.groupNames(c)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	body, err := supply.BuildSubmissionTemplate(models, groupNames)
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="submission_template.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", body)
}

// groupNames 取本主体全部组名（含停用），用于模板示例与导入时的组名校验。
//
// 停用组也放进来：在途申报里的行可能仍挂在停用组上，导入回填时不该因此报错。
func (h *supplierSubmissionHandler) groupNames(c *gin.Context) ([]string, error) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return nil, nil
	}
	ctx := c.Request.Context()
	groups, err := h.svc.groups.List(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		names = append(names, group.Name)
	}
	return names, nil
}

// importParse 解析导入文件并返回逐行校验结果（只解析，不落库）。
//
// 不落库是刻意的：用户先看预览、确认无误后再点「保存草稿」，
// 复用已有的写接口，避免多一套幂等与乐观锁。
func (h *supplierSubmissionHandler) importParse(c *gin.Context) {
	if _, ok := h.supplierID(c); !ok {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请上传 xlsx 文件", 400))
		return
	}
	if file.Size <= 0 || file.Size > maxImportFileSize {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "文件大小须在 2MB 以内", 400))
		return
	}
	if !strings.EqualFold(filepath.Ext(file.Filename), ".xlsx") {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "本期仅支持 .xlsx 文件", 400))
		return
	}
	opened, err := file.Open()
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	defer func() { _ = opened.Close() }()
	data, err := io.ReadAll(io.LimitReader(opened, maxImportFileSize+1))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}

	models, err := h.svc.store.ActiveModelCatalog(c.Request.Context())
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	groupNames, err := h.groupNames(c)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	rows, err := supply.ParseSubmissionSheet(data, models, groupNames)
	if err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, err.Error(), 400))
		return
	}
	okCount := 0
	for _, row := range rows {
		if row.OK {
			okCount++
		}
	}
	response.Success(c, gin.H{
		"total": len(rows), "ok_count": okCount, "error_count": len(rows) - okCount, "rows": rows,
	})
}
