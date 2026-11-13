package collector

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"collect/internal/model"
)

// SQLSink 用自定义 SQL 模板把每条记录写入目标库。
//
// 模板中支持三种取值方式：
//  1. {{字段名}}     -> 按字段映射后的目标字段取值，生成绑定参数（安全，推荐）
//  2. {{变量|修饰}}  -> 运行期变量（token/page/now/uuid…），内联成 SQL 字面量
//  3. ? / :1 / $1    -> 原生占位符，按字段映射顺序逐列绑定（模板中不含 {{字段}} 时生效）
//
// 每条记录执行一次 SQL，按 batch 条数包在同一事务里提交。
type SQLSink struct {
	db    *sql.DB
	typ   string
	tpl   string
	cols  []string
	batch int
	vars  *Vars
	buf   []map[string]any
	exec  int
}

// NewSQLSink 创建自定义 SQL 落库器
func NewSQLSink(ds model.DataSource, tmpl string, cols []string, batch int, vars *Vars) (*SQLSink, error) {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return nil, errors.New("写入方式为「自定义 SQL」，但 SQL 模板是空的")
	}
	if vars == nil {
		vars = NewVars()
	}
	db, err := OpenSQL(ds)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}
	if batch <= 0 {
		batch = 200
	}
	return &SQLSink{db: db, typ: strings.ToLower(ds.Type), tpl: tmpl, cols: cols, batch: batch, vars: vars}, nil
}

// Executed 已执行的 SQL 条数
func (s *SQLSink) Executed() int { return s.exec }

// Write 追加记录，达到批量条数后提交
func (s *SQLSink) Write(rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	s.buf = append(s.buf, rows...)
	for len(s.buf) >= s.batch {
		chunk := s.buf[:s.batch]
		s.buf = s.buf[s.batch:]
		if err := s.flush(chunk); err != nil {
			return err
		}
	}
	return nil
}

// Flush 提交缓冲区剩余记录
func (s *SQLSink) Flush() error {
	if len(s.buf) == 0 {
		return nil
	}
	chunk := s.buf
	s.buf = nil
	return s.flush(chunk)
}

// Close 提交并释放连接
func (s *SQLSink) Close() error {
	err := s.Flush()
	if cerr := s.db.Close(); err == nil {
		err = cerr
	}
	return err
}

func (s *SQLSink) flush(rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	for _, row := range rows {
		q, args, err := s.render(row)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("执行 SQL 失败: %w（语句: %s）", err, previewSQL(q, 200))
		}
		s.exec++
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

// render 把模板渲染成可执行的 SQL + 绑定参数
func (s *SQLSink) render(row map[string]any) (string, []any, error) {
	var sb strings.Builder
	var args []any
	last := 0
	for _, m := range varRe.FindAllStringSubmatchIndex(s.tpl, -1) {
		sb.WriteString(s.tpl[last:m[0]])
		last = m[1]
		name := s.tpl[m[2]:m[3]]
		filter := ""
		if m[4] >= 0 {
			filter = s.tpl[m[4]:m[5]]
		}
		if v, ok := lookupRowValue(row, name); ok {
			args = append(args, dbValue(v))
			sb.WriteString(s.placeholder(len(args)))
			continue
		}
		if val, ok := s.vars.lookup(name); ok {
			sb.WriteString(inlineLiteral(applyVarFilter(val, filter), insideQuote(s.tpl[:m[0]])))
			continue
		}
		return "", nil, fmt.Errorf("SQL 模板中的 {{%s}} 既不是字段映射的目标字段，也不是可用变量", name)
	}
	sb.WriteString(s.tpl[last:])
	q := sb.String()
	if len(args) == 0 {
		// 没有 {{字段}}，尝试原生占位符按映射顺序绑定
		n := countPlaceholders(q, s.typ)
		if n > 0 {
			if len(s.cols) == 0 {
				return "", nil, errors.New("SQL 使用了原生占位符，但没有配置字段映射，无法确定绑定顺序")
			}
			if n != len(s.cols) {
				return "", nil, fmt.Errorf("SQL 占位符个数（%d）与字段映射列数（%d）不一致，无法绑定", n, len(s.cols))
			}
			for _, c := range s.cols {
				args = append(args, dbValue(row[c]))
			}
		}
	}
	return q, args, nil
}

func (s *SQLSink) placeholder(n int) string {
	switch s.typ {
	case "postgres":
		return fmt.Sprintf("$%d", n)
	case "oracle":
		return fmt.Sprintf(":%d", n)
	default:
		return "?"
	}
}

// lookupRowValue 大小写不敏感地取行数据
func lookupRowValue(row map[string]any, key string) (any, bool) {
	if v, ok := row[key]; ok {
		return v, true
	}
	for k, v := range row {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

// insideQuote 判断模板当前位置是否处于单引号字符串内部
func insideQuote(s string) bool {
	q := false
	for i := 0; i < len(s); i++ {
		if s[i] != '\'' {
			continue
		}
		if q && i+1 < len(s) && s[i+1] == '\'' {
			i++
			continue
		}
		q = !q
	}
	return q
}

// inlineLiteral 把变量值转成 SQL 字面量（数字/布尔/NULL 原样，其余加单引号转义）
func inlineLiteral(v string, quoted bool) string {
	esc := strings.ReplaceAll(v, "'", "''")
	if quoted {
		return esc
	}
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "NULL", "TRUE", "FALSE":
		return strings.TrimSpace(v)
	}
	if v != "" && isNumeric(strings.TrimSpace(v)) {
		return strings.TrimSpace(v)
	}
	return "'" + esc + "'"
}

// applyVarFilter 与 Vars.Render 的修饰符保持一致
func applyVarFilter(val, filter string) string {
	switch strings.ToLower(filter) {
	case "url", "urlencode":
		return url.QueryEscape(val)
	case "b64", "base64":
		return base64.StdEncoding.EncodeToString([]byte(val))
	case "lower":
		return strings.ToLower(val)
	case "upper":
		return strings.ToUpper(val)
	}
	return val
}

// countPlaceholders 统计 SQL 中的原生占位符个数（跳过字符串字面量）
func countPlaceholders(q, typ string) int {
	n := 0
	inStr := false
	for i := 0; i < len(q); i++ {
		c := q[i]
		if c == '\'' {
			if inStr && i+1 < len(q) && q[i+1] == '\'' {
				i++
				continue
			}
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch typ {
		case "postgres":
			if c == '$' && i+1 < len(q) && isDigitByte(q[i+1]) {
				n++
			}
		case "oracle":
			if c == ':' && i+1 < len(q) && isDigitByte(q[i+1]) {
				n++
			}
		default:
			if c == '?' {
				n++
			}
		}
	}
	return n
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

// previewSQL 单行化 SQL 便于日志展示
func previewSQL(q string, max int) string {
	q = strings.Join(strings.Fields(q), " ")
	if len(q) <= max {
		return q
	}
	return q[:max] + "…"
}
