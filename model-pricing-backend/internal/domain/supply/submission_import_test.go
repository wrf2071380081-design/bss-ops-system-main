package supply

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func testCatalog() []ImportModel {
	return []ImportModel{
		{ID: 15, Name: "Claude Sonnet 4", OfficialID: "claude-sonnet-4", Company: "Anthropic", ModelType: "TEXT"},
		{ID: 14, Name: "Qwen3 Max", OfficialID: "Qwen3-Max", Company: "阿里云", ModelType: "TEXT"},
	}
}

// buildImportFile 用 excelize 在内存里造一个导入文件，模拟用户填表。
func buildImportFile(t *testing.T, rows [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	if err := f.SetSheetName(sheet, importSheetData); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}
	for i, title := range templateHeader {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(importSheetData, cell, title)
	}
	for rowIndex, row := range rows {
		for colIndex, value := range row {
			cell, _ := excelize.CoordinatesToCellName(colIndex+1, rowIndex+2)
			_ = f.SetCellValue(importSheetData, cell, value)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = f.Close()
	return buf.Bytes()
}

func TestBuildSubmissionTemplateHasThreeSheets(t *testing.T) {
	data, err := BuildSubmissionTemplate(testCatalog())
	if err != nil {
		t.Fatalf("build template: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open template: %v", err)
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	for _, want := range []string{importSheetData, importSheetModel, importSheetHelp} {
		found := false
		for _, name := range sheets {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("模板缺少工作表 %s，实际 %v", want, sheets)
		}
	}
	// 目录页应包含传入的模型，用户可照着填。
	rows, err := f.GetRows(importSheetModel)
	if err != nil {
		t.Fatalf("read catalog sheet: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("目录页应为表头+2行，实际 %d 行", len(rows))
	}
}

func TestParseSubmissionSheetValidRows(t *testing.T) {
	data := buildImportFile(t, [][]any{
		{"claude-sonnet-4", "my-claude", "0.68", "是"},
		{"Qwen3 Max", "my-qwen", "0.5", "否"},
	})
	rows, err := ParseSubmissionSheet(data, testCatalog())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应解析 2 行，实际 %d", len(rows))
	}
	if !rows[0].OK || rows[0].StandardModelID != 15 || !rows[0].ParticipateTest {
		t.Fatalf("第 1 行解析错误：%+v", rows[0])
	}
	// 官方 ID 大小写不敏感，也支持按名称匹配。
	if !rows[1].OK || rows[1].StandardModelID != 14 || rows[1].ParticipateTest {
		t.Fatalf("第 2 行解析错误：%+v", rows[1])
	}
	if rows[0].Line != 2 || rows[1].Line != 3 {
		t.Fatalf("行号应为 2/3，实际 %d/%d", rows[0].Line, rows[1].Line)
	}
}

func TestParseSubmissionSheetRejectsBadRows(t *testing.T) {
	data := buildImportFile(t, [][]any{
		{"不存在的模型", "x", "0.6", "否"},
		{"claude-sonnet-4", "", "0.6", "否"},
		{"Qwen3 Max", "q", "0", "否"},
		{"Qwen3 Max", "q2", "1.2", "否"},
		{"claude-sonnet-4", "c2", "0.12345", "否"},
		{"claude-sonnet-4", "c3", "0.6", "大概吧"},
		{"claude-sonnet-4", "c4", "0.7", "否"}, // 与第 2 行同模型 → 重复
	})
	rows, err := ParseSubmissionSheet(data, testCatalog())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 7 {
		t.Fatalf("应返回 7 行（含错误行），实际 %d", len(rows))
	}
	for index, row := range rows {
		if row.OK {
			t.Fatalf("第 %d 行应为错误行，实际通过：%+v", index+2, row)
		}
		if len(row.Errors) == 0 {
			t.Fatalf("第 %d 行应带错误说明", index+2)
		}
	}
	// 错误文案要带行号，前端直接展示。
	if got := rows[0].Errors[0]; got[:4] != "第 2 "[:4] {
		t.Fatalf("错误文案应包含行号，实际 %q", got)
	}
}

func TestParseSubmissionSheetRejectsEmpty(t *testing.T) {
	data := buildImportFile(t, nil)
	if _, err := ParseSubmissionSheet(data, testCatalog()); err == nil {
		t.Fatal("空文件应报错")
	}
	if _, err := ParseSubmissionSheet([]byte("not a xlsx"), testCatalog()); err == nil {
		t.Fatal("非 xlsx 应报错")
	}
}
