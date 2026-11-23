package collector

import (
	"strings"
	"testing"
)

// Oracle 的 MERGE 中，主键列的大小写必须与字段映射的目标列一致，
// 否则 USING 子查询别名与 ON 条件不匹配，Oracle 会报
// ORA-00904: "S"."INSURANCENO": invalid identifier
func TestResolveKeyColumnsCaseInsensitive(t *testing.T) {
	cols := []string{"insuranceNo", "name", "price"}
	got, err := resolveKeyColumns("oracle", cols, []string{"INSURANCENO"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "insuranceNo" {
		t.Fatalf("want [insuranceNo], got %v", got)
	}
}

func TestResolveKeyColumnsMissingForOracle(t *testing.T) {
	_, err := resolveKeyColumns("oracle", []string{"name", "price"}, []string{"INSURANCENO"})
	if err == nil {
		t.Fatal("expected error when key column is absent from mapping")
	}
	if !strings.Contains(err.Error(), "INSURANCENO") {
		t.Fatalf("error should name the offending column, got %v", err)
	}
	if !strings.Contains(err.Error(), "MERGE") {
		t.Fatalf("error should explain the Oracle MERGE requirement, got %v", err)
	}
}

// 其他数据库的 upsert 不要求主键列出现在插入列里，找不到时保持原样
func TestResolveKeyColumnsMissingForMySQL(t *testing.T) {
	got, err := resolveKeyColumns("mysql", []string{"name", "price"}, []string{"id"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "id" {
		t.Fatalf("want [id], got %v", got)
	}
}

func TestResolveKeyColumnsTrimsAndSkipsBlank(t *testing.T) {
	got, err := resolveKeyColumns("oracle", []string{"ID", "NAME"}, []string{" id ", "", "  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "ID" {
		t.Fatalf("want [ID], got %v", got)
	}
}

func TestOracleMergeSQLKeepsColumnCasingConsistent(t *testing.T) {
	w := &SinkWriter{
		typ: "oracle", table: "DRUG", mode: "upsert",
		cols: []string{"insuranceNo", "name"}, keys: []string{"insuranceNo"},
	}
	args, vals := w.oracleMergeSQL(map[string]any{"insuranceNo": "A1", "name": "药"})
	if len(vals) != 2 {
		t.Fatalf("want 2 bind args, got %d", len(vals))
	}
	// USING 子查询里的别名与 ON / INSERT 里引用的主键列必须完全一致
	if !strings.Contains(args, `:1 AS "insuranceNo"`) {
		t.Fatalf("alias casing lost: %s", args)
	}
	if !strings.Contains(args, `t."insuranceNo"=s."insuranceNo"`) {
		t.Fatalf("on condition casing mismatch: %s", args)
	}
	if strings.Contains(args, `"INSURANCENO"`) {
		t.Fatalf("unexpected upper-case identifier: %s", args)
	}
}

func TestOracleMergeSQLSkipsKeyInUpdateSet(t *testing.T) {
	w := &SinkWriter{
		typ: "oracle", table: "DRUG", mode: "upsert",
		cols: []string{"id", "name"}, keys: []string{"id"},
	}
	q, _ := w.oracleMergeSQL(nil)
	if strings.Contains(q, `t."id"=s."id", `) || strings.Contains(q, `UPDATE SET t."id"`) {
		t.Fatalf("primary key must not be in the UPDATE SET list: %s", q)
	}
	if !strings.Contains(q, `UPDATE SET t."name"=s."name"`) {
		t.Fatalf("missing non-key update: %s", q)
	}
}

func TestPreviewSQLTruncatesOnRuneBoundary(t *testing.T) {
	q := "INSERT INTO 药品价格表 (编号) VALUES ('中文值')"
	got := previewSQL(q, 14)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
	// 截断结果必须是合法 UTF-8（不能把中文切一半）
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("truncated preview broke UTF-8: %q", got)
	}
}

func TestPreviewSQLCollapsesWhitespace(t *testing.T) {
	got := previewSQL("MERGE INTO\n  DRUG t\n USING (SELECT 1 FROM dual) s", 4000)
	if strings.Contains(got, "\n") || strings.Contains(got, "  ") {
		t.Fatalf("expected single-line SQL, got %q", got)
	}
}
