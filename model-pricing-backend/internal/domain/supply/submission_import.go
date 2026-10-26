package supply

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ImportModel 是导入时可匹配的标准模型（只含启用中的）。
type ImportModel struct {
	ID         int64
	Name       string
	OfficialID string
	Company    string
	ModelType  string
}

// ImportRow 是一行导入结果：解析出的值 + 校验错误（为空且 OK 才能落库）。
type ImportRow struct {
	Line             int      `json:"line"`
	OK               bool     `json:"ok"`
	Errors           []string `json:"errors"`
	StandardModel    string   `json:"standard_model"`
	InterfaceModelID string   `json:"interface_model_id"`
	Coefficient      string   `json:"coefficient"`
	ParticipateTest  bool     `json:"participate_test"`
	StandardModelID  int64    `json:"standard_model_id,omitempty"`
	StandardModelName string  `json:"standard_model_name,omitempty"`
	OfficialID       string   `json:"official_id,omitempty"`
	Company          string   `json:"company,omitempty"`
	ModelType        string   `json:"model_type,omitempty"`
}

const (
	importSheetData  = "申报导入"
	importSheetModel = "标准模型"
	importSheetHelp  = "填写说明"
	// MaxImportRows 防止一次导入过大的文件。
	MaxImportRows = 200
)

var templateHeader = []string{"对应标准模型", "接口模型 ID", "采购系数", "压力测试"}

// BuildSubmissionTemplate 生成导入模板：数据表 + 标准模型目录 + 填写说明。
func BuildSubmissionTemplate(models []ImportModel) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	first := f.GetSheetName(0)
	if err := f.SetSheetName(first, importSheetData); err != nil {
		return nil, err
	}
	if _, err := f.NewSheet(importSheetModel); err != nil {
		return nil, err
	}
	if _, err := f.NewSheet(importSheetHelp); err != nil {
		return nil, err
	}

	for i, title := range templateHeader {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(importSheetData, cell, title); err != nil {
			return nil, err
		}
	}
	example := []any{"deepseek-chat", "my-deepseek", "0.6800", "否"}
	if len(models) > 0 && strings.TrimSpace(models[0].OfficialID) != "" {
		example[0] = models[0].OfficialID
	}
	for i, value := range example {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		if err := f.SetCellValue(importSheetData, cell, value); err != nil {
			return nil, err
		}
	}
	_ = f.SetColWidth(importSheetData, "A", "D", 24)

	modelHeader := []string{"标准模型名称", "官方 ID", "模型公司", "模型类型"}
	for i, title := range modelHeader {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(importSheetModel, cell, title); err != nil {
			return nil, err
		}
	}
	for rowIndex, model := range models {
		values := []any{model.Name, model.OfficialID, model.Company, model.ModelType}
		for colIndex, value := range values {
			cell, _ := excelize.CoordinatesToCellName(colIndex+1, rowIndex+2)
			if err := f.SetCellValue(importSheetModel, cell, value); err != nil {
				return nil, err
			}
		}
	}
	_ = f.SetColWidth(importSheetModel, "A", "D", 26)

	help := []string{
		"填写说明：",
		"1. 只在「申报导入」表填写，第 1 行是表头，不要删除。",
		"2. 对应标准模型：填官方 ID 或标准模型名称，必须与「标准模型」表一致。",
		"3. 接口模型 ID：供应商自己的模型标识，必填。",
		"4. 采购系数：大于 0 且不超过 1，最多 4 位小数。",
		"5. 压力测试：填 是/否（也接受 Y/N、true/false、1/0）。所有模型都会做连通测试。",
		fmt.Sprintf("6. 单次最多导入 %d 行。", MaxImportRows),
	}
	for i, line := range help {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetCellValue(importSheetHelp, cell, line); err != nil {
			return nil, err
		}
	}
	_ = f.SetColWidth(importSheetHelp, "A", "A", 90)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseSubmissionSheet 解析导入文件并逐行校验。
// 只解析不落库：前端拿预览结果确认后再走保存草稿。
func ParseSubmissionSheet(data []byte, models []ImportModel) ([]ImportRow, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("文件不是有效的 xlsx：%w", err)
	}
	defer func() { _ = f.Close() }()

	sheet := importSheetData
	if !containsString(f.GetSheetList(), sheet) {
		sheet = f.GetSheetName(0)
	}
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, err
	}
	if len(rows) <= 1 {
		return nil, fmt.Errorf("文件里没有数据行")
	}

	byOfficial := make(map[string]ImportModel, len(models))
	byName := make(map[string]ImportModel, len(models))
	for _, model := range models {
		if key := strings.ToLower(strings.TrimSpace(model.OfficialID)); key != "" {
			byOfficial[key] = model
		}
		if key := strings.ToLower(strings.TrimSpace(model.Name)); key != "" {
			byName[key] = model
		}
	}

	result := make([]ImportRow, 0, len(rows)-1)
	seen := make(map[int64]bool, len(rows))
	for index, raw := range rows[1:] {
		line := index + 2 // 数据从第 2 行开始
		if isEmptyRow(raw) {
			continue
		}
		if len(result) >= MaxImportRows {
			return nil, fmt.Errorf("单次最多导入 %d 行", MaxImportRows)
		}
		item := ImportRow{Line: line, OK: true, Errors: []string{}}
		modelCell := cellAt(raw, 0)
		item.StandardModel = modelCell
		item.InterfaceModelID = cellAt(raw, 1)
		item.Coefficient = cellAt(raw, 2)
		participate, participateErr := parseYesNo(cellAt(raw, 3))

		fail := func(msg string) {
			item.OK = false
			item.Errors = append(item.Errors, fmt.Sprintf("第 %d 行：%s", line, msg))
		}

		matched, found := byOfficial[strings.ToLower(modelCell)]
		if !found {
			matched, found = byName[strings.ToLower(modelCell)]
		}
		if !found {
			fail("对应标准模型不存在或未启用")
		} else {
			item.StandardModelID = matched.ID
			item.StandardModelName = matched.Name
			item.OfficialID = matched.OfficialID
			item.Company = matched.Company
			item.ModelType = matched.ModelType
			if seen[matched.ID] {
				fail("同一标准模型在文件中重复出现")
			}
			seen[matched.ID] = true
		}
		if item.InterfaceModelID == "" {
			fail("接口模型 ID 不能为空")
		} else if len([]rune(item.InterfaceModelID)) > 128 {
			fail("接口模型 ID 超过 128 个字符")
		}
		if ok, msg := validCoefficient(item.Coefficient); !ok {
			fail(msg)
		}
		if participateErr != nil {
			fail("压力测试列只能填 是/否")
		}
		item.ParticipateTest = participate
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("文件里没有数据行")
	}
	return result, nil
}

func validCoefficient(raw string) (bool, string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false, "采购系数不能为空"
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false, "采购系数不是数字"
	}
	if number <= 0 || number > 1 {
		return false, "采购系数须大于 0 且不超过 1"
	}
	if dot := strings.IndexByte(value, '.'); dot >= 0 && len(value)-dot-1 > 4 {
		return false, "采购系数最多 4 位小数"
	}
	return true, ""
}

func parseYesNo(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "是", "y", "yes", "true", "1":
		return true, nil
	case "", "否", "n", "no", "false", "0":
		return false, nil
	}
	return false, fmt.Errorf("invalid yes/no")
}

func cellAt(row []string, index int) string {
	if index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func isEmptyRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
