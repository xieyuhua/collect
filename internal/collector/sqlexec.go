package collector

import (
	"context"
	"errors"
	"strings"
	"time"

	"collect/internal/model"
)

// SQLResult 调试执行 SQL 的返回结果
type SQLResult struct {
	Statement string  `json:"statement"` // query|exec
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Affected  int64    `json:"affected"`
	ElapsedMs int64    `json:"elapsed_ms"`
	Truncated bool     `json:"truncated"`
}

// ExecSQL 在指定数据源上执行一条 SQL（只用于调试/校验，不做变量替换）
func ExecSQL(ds model.DataSource, sqlText string, limit int) (*SQLResult, error) {
	sqlText = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sqlText), ";"))
	if sqlText == "" {
		return nil, errors.New("SQL 不能为空")
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	db, err := OpenSQL(ds)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := time.Now()
	res := &SQLResult{Columns: []string{}, Rows: [][]any{}}
	if isQuerySQL(sqlText) {
		res.Statement = "query"
		rows, err := db.QueryContext(ctx, sqlText)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		res.Columns = cols
		for rows.Next() {
			if len(res.Rows) >= limit {
				res.Truncated = true
				break
			}
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return nil, err
			}
			res.Rows = append(res.Rows, normalizeSQLRow(vals))
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	} else {
		res.Statement = "exec"
		r, err := db.ExecContext(ctx, sqlText)
		if err != nil {
			return nil, err
		}
		if n, err := r.RowsAffected(); err == nil {
			res.Affected = n
		}
	}
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res, nil
}

func isQuerySQL(q string) bool {
	s := strings.ToLower(strings.TrimSpace(q))
	for _, p := range []string{"select", "with", "show", "desc", "describe", "explain", "pragma", "values"} {
		if s == p || strings.HasPrefix(s, p+" ") || strings.HasPrefix(s, p+"\n") || strings.HasPrefix(s, p+"\t") || strings.HasPrefix(s, p+"(") {
			return true
		}
	}
	return false
}

func normalizeSQLRow(vals []any) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		switch x := v.(type) {
		case []byte:
			out[i] = string(x)
		case time.Time:
			out[i] = x.Format("2006-01-02 15:04:05")
		default:
			out[i] = x
		}
	}
	return out
}
