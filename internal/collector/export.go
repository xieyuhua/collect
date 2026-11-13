package collector

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"collect/internal/model"

	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

// RunRows 读取某次运行采集的数据行
func RunRows(db *gorm.DB, runID uint, headers []string) ([]map[string]any, []string, error) {
	return RunRowsSource(db, runID, "", headers)
}

// RunRowsSource 按数据来源读取数据行；source 为空表示全部
// source = "__unlabeled" 表示升级前没有来源标识的历史数据
func RunRowsSource(db *gorm.DB, runID uint, source string, headers []string) ([]map[string]any, []string, error) {
	q := db.Where("run_id = ?", runID)
	switch source {
	case "":
	case "__unlabeled":
		q = q.Where("source = '' OR source IS NULL")
	default:
		q = q.Where("source = ?", source)
	}
	var rows []model.DatasetRow
	if err := q.Order("seq asc").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	meta := map[string]bool{}
	for _, r := range rows {
		var m map[string]any
		if json.Unmarshal([]byte(r.Data), &m) != nil {
			continue
		}
		out = append(out, m)
		for k := range m {
			meta[k] = true
		}
	}
	return out, orderHeaders(headers, meta), nil
}

// orderHeaders 按给定顺序保留实际存在的列，其余列按字母序追加
func orderHeaders(preferred []string, meta map[string]bool) []string {
	out := make([]string, 0, len(meta))
	seen := map[string]bool{}
	for _, h := range preferred {
		if meta[h] && !seen[h] {
			out = append(out, h)
			seen[h] = true
		}
	}
	var rest []string
	for k := range meta {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func rowValues(row map[string]any, headers []string) []any {
	vals := make([]any, len(headers))
	for i, h := range headers {
		v := row[h]
		switch x := v.(type) {
		case map[string]any, []any:
			b, _ := json.Marshal(x)
			vals[i] = string(b)
		default:
			vals[i] = x
		}
	}
	return vals
}

func ExportRunXLSX(db *gorm.DB, runID uint, source string, headers []string, path string) error {
	rows, heads, err := RunRowsSource(db, runID, source, headers)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f := excelize.NewFile()
	defer f.Close()
	sw, err := f.NewStreamWriter("Sheet1")
	if err != nil {
		return err
	}
	headRow := make([]any, len(heads))
	for i, h := range heads {
		headRow[i] = h
	}
	style, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"3A6FF7"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err := sw.SetRow("A1", headRow, excelize.RowOpts{StyleID: style}); err != nil {
		return err
	}
	for i, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := sw.SetRow(cell, rowValues(row, heads)); err != nil {
			return err
		}
	}
	if err := sw.Flush(); err != nil {
		return err
	}
	f.SetActiveSheet(0)
	return f.SaveAs(path)
}

func ExportRunCSV(db *gorm.DB, runID uint, source string, headers []string, path string) error {
	rows, heads, err := RunRowsSource(db, runID, source, headers)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, _ = f.WriteString("\xEF\xBB\xBF") // BOM，Excel 打开不乱码
	w := csv.NewWriter(f)
	if err := w.Write(heads); err != nil {
		return err
	}
	for _, row := range rows {
		vals := rowValues(row, heads)
		rec := make([]string, len(vals))
		for i, v := range vals {
			if v == nil {
				rec[i] = ""
			} else {
				rec[i] = fmt.Sprint(v)
			}
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return w.Error()
}

func SanitizeFileName(s string) string {
	r := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return r.Replace(s)
}
