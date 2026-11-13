package collector

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"collect/internal/model"

	_ "github.com/glebarez/go-sqlite"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	go_ora "github.com/sijms/go-ora/v2"
)

// OpenSQL 根据数据源配置打开数据库连接
func OpenSQL(ds model.DataSource) (*sql.DB, error) {
	var driver, dsn string
	switch strings.ToLower(ds.Type) {
	case "mysql":
		driver = "mysql"
		dsn = strings.TrimSpace(ds.DSN)
		if dsn == "" {
			port := ds.Port
			if port == 0 {
				port = 3306
			}
			params := ds.Params
			if params == "" {
				params = "charset=utf8mb4&parseTime=true&loc=Local"
			}
			dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s", ds.User, ds.Password, ds.Host, port, ds.Database, strings.TrimPrefix(params, "?"))
		}
	case "oracle":
		driver = "oracle"
		dsn = strings.TrimSpace(ds.DSN)
		if dsn == "" {
			port := ds.Port
			if port == 0 {
				port = 1521
			}
			var opts map[string]string
			if ds.Params != "" {
				opts = map[string]string{}
				for _, kv := range strings.Split(strings.TrimPrefix(ds.Params, "?"), "&") {
					if k, v, ok := strings.Cut(kv, "="); ok {
						opts[k] = v
					}
				}
			}
			dsn = go_ora.BuildUrl(ds.Host, port, ds.Database, ds.User, ds.Password, opts)
		}
	case "postgres", "postgresql":
		driver = "pgx"
		dsn = strings.TrimSpace(ds.DSN)
		if dsn == "" {
			port := ds.Port
			if port == 0 {
				port = 5432
			}
			dsn = fmt.Sprintf("postgres://%s:%s@%s:%d/%s", url.QueryEscape(ds.User), url.QueryEscape(ds.Password), ds.Host, port, ds.Database)
			if ds.Params != "" {
				dsn += "?" + strings.TrimPrefix(ds.Params, "?")
			} else {
				dsn += "?sslmode=disable"
			}
		}
	case "sqlite", "sqlite3":
		driver = "sqlite"
		dsn = strings.TrimSpace(ds.DSN)
		if dsn == "" {
			dsn = ds.Database
		}
		if dsn == "" {
			return nil, errors.New("SQLite 需要填写数据库文件路径")
		}
	default:
		return nil, fmt.Errorf("不支持的数据库类型: %s", ds.Type)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)
	return db, nil
}

func quoteIdent(typ, name string) string {
	q := `"`
	if typ == "mysql" {
		q = "`"
	}
	parts := strings.Split(name, ".")
	for i, p := range parts {
		parts[i] = q + strings.ReplaceAll(p, q, q+q) + q
	}
	return strings.Join(parts, ".")
}

// SinkWriter 批量写入目标数据库
type SinkWriter struct {
	db    *sql.DB
	typ   string
	table string
	cols  []string
	mode  string
	keys  []string
	batch int
	buf   []map[string]any
}

func NewSinkWriter(ds model.DataSource, table string, cols []string, mode string, keys []string, batch int, autoCreate bool) (*SinkWriter, error) {
	if table == "" {
		return nil, errors.New("未配置目标表名")
	}
	if len(cols) == 0 {
		return nil, errors.New("字段映射为空，无法入库")
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
	typ := strings.ToLower(ds.Type)
	if autoCreate {
		if err := ensureTable(db, typ, table, cols, keys); err != nil {
			db.Close()
			return nil, fmt.Errorf("自动建表失败: %w", err)
		}
	}
	if batch <= 0 {
		batch = 200
	}
	return &SinkWriter{db: db, typ: typ, table: table, cols: cols, mode: mode, keys: keys, batch: batch}, nil
}

func ensureTable(db *sql.DB, typ, table string, cols []string, keys []string) error {
	if len(cols) == 0 {
		return errors.New("没有字段")
	}
	keySet := map[string]bool{}
	for _, k := range keys {
		keySet[strings.ToLower(strings.TrimSpace(k))] = true
	}
	var keyCols []string
	var defs []string
	for _, c := range cols {
		isKey := keySet[strings.ToLower(c)]
		if isKey {
			keyCols = append(keyCols, c)
		}
		if typ == "oracle" {
			if isKey {
				defs = append(defs, quoteIdent(typ, c)+" VARCHAR2(255) NOT NULL")
			} else {
				defs = append(defs, quoteIdent(typ, c)+" VARCHAR2(4000)")
			}
			continue
		}
		if typ == "mysql" {
			if isKey {
				defs = append(defs, quoteIdent(typ, c)+" VARCHAR(191) NOT NULL")
			} else {
				defs = append(defs, quoteIdent(typ, c)+" TEXT")
			}
			continue
		}
		defs = append(defs, quoteIdent(typ, c)+" TEXT")
	}
	if len(keyCols) > 0 {
		var ks []string
		for _, k := range keyCols {
			ks = append(ks, quoteIdent(typ, k))
		}
		defs = append(defs, "PRIMARY KEY ("+strings.Join(ks, ", ")+")")
	}
	var stmt string
	if typ == "oracle" {
		stmt = fmt.Sprintf("CREATE TABLE %s (%s)", quoteIdent(typ, table), strings.Join(defs, ", "))
	} else {
		stmt = fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", quoteIdent(typ, table), strings.Join(defs, ", "))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		if typ == "oracle" && strings.Contains(err.Error(), "ORA-00955") {
			return nil
		}
		return err
	}
	return nil
}

func (w *SinkWriter) Write(rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	w.buf = append(w.buf, rows...)
	for len(w.buf) >= w.batch {
		n := w.batch
		chunk := w.buf[:n]
		w.buf = w.buf[n:]
		if err := w.flush(chunk); err != nil {
			return err
		}
	}
	return nil
}

func (w *SinkWriter) Flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	chunk := w.buf
	w.buf = nil
	return w.flush(chunk)
}

func (w *SinkWriter) Close() error {
	err := w.Flush()
	if cerr := w.db.Close(); err == nil {
		err = cerr
	}
	return err
}

func (w *SinkWriter) flush(rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if w.typ == "oracle" {
		for _, row := range rows {
			if err := w.insertOracle(ctx, row); err != nil {
				return err
			}
		}
		return nil
	}
	query, args := w.buildInsert(rows)
	if query == "" {
		return errors.New("没有可写入的数据")
	}
	_, err := w.db.ExecContext(ctx, query, args...)
	if err != nil && w.mode == "upsert" && (strings.Contains(err.Error(), "ON CONFLICT") || strings.Contains(err.Error(), "unique constraint") || strings.Contains(err.Error(), "UNIQUE constraint")) {
		return fmt.Errorf("写入失败：目标表 %s 缺少与 %v 匹配的主键/唯一约束，请改用 insert 模式或先为该表建立唯一索引（%w）", w.table, w.keys, err)
	}
	return err
}

func (w *SinkWriter) buildInsert(rows []map[string]any) (string, []any) {
	var placeholders []string
	var args []any
	idx := 0
	for _, row := range rows {
		var ph []string
		for _, c := range w.cols {
			idx++
			switch w.typ {
			case "postgres":
				ph = append(ph, fmt.Sprintf("$%d", idx))
			case "oracle":
				ph = append(ph, fmt.Sprintf(":%d", idx))
			default:
				ph = append(ph, "?")
			}
			args = append(args, dbValue(row[c]))
		}
		placeholders = append(placeholders, "("+strings.Join(ph, ",")+")")
	}
	var cols []string
	for _, c := range w.cols {
		cols = append(cols, quoteIdent(w.typ, c))
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", quoteIdent(w.typ, w.table), strings.Join(cols, ","), strings.Join(placeholders, ","))
	if w.mode == "upsert" {
		q += w.upsertClause()
	}
	return q, args
}

func (w *SinkWriter) upsertClause() string {
	keySet := map[string]bool{}
	for _, k := range w.keys {
		keySet[strings.ToLower(k)] = true
	}
	var updates []string
	for _, c := range w.cols {
		if keySet[strings.ToLower(c)] {
			continue
		}
		switch w.typ {
		case "mysql":
			updates = append(updates, fmt.Sprintf("%s=VALUES(%s)", quoteIdent(w.typ, c), quoteIdent(w.typ, c)))
		case "postgres", "sqlite":
			updates = append(updates, fmt.Sprintf("%s=excluded.%s", quoteIdent(w.typ, c), quoteIdent(w.typ, c)))
		}
	}
	switch w.typ {
	case "mysql":
		if len(updates) == 0 {
			return ""
		}
		return " ON DUPLICATE KEY UPDATE " + strings.Join(updates, ",")
	case "postgres", "sqlite":
		target := ""
		if len(w.keys) > 0 {
			var ks []string
			for _, k := range w.keys {
				ks = append(ks, quoteIdent(w.typ, k))
			}
			target = " (" + strings.Join(ks, ",") + ")"
		}
		if len(updates) == 0 {
			return " ON CONFLICT" + target + " DO NOTHING"
		}
		return " ON CONFLICT" + target + " DO UPDATE SET " + strings.Join(updates, ",")
	}
	return ""
}

func (w *SinkWriter) insertOracle(ctx context.Context, row map[string]any) error {
	if w.mode != "upsert" || len(w.keys) == 0 {
		var ph, cols []string
		var args []any
		for i, c := range w.cols {
			ph = append(ph, fmt.Sprintf(":%d", i+1))
			cols = append(cols, quoteIdent(w.typ, c))
			args = append(args, dbValue(row[c]))
		}
		q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", quoteIdent(w.typ, w.table), strings.Join(cols, ","), strings.Join(ph, ","))
		_, err := w.db.ExecContext(ctx, q, args...)
		return err
	}
	var selects, cols, sets, insCols, insVals []string
	var args []any
	i := 0
	for _, c := range w.cols {
		i++
		selects = append(selects, fmt.Sprintf(":%d AS %s", i, quoteIdent(w.typ, c)))
		cols = append(cols, quoteIdent(w.typ, c))
		insCols = append(insCols, quoteIdent(w.typ, c))
		insVals = append(insVals, "s."+quoteIdent(w.typ, c))
		args = append(args, dbValue(row[c]))
	}
	keySet := map[string]bool{}
	for _, k := range w.keys {
		keySet[strings.ToLower(k)] = true
	}
	for _, c := range w.cols {
		if keySet[strings.ToLower(c)] {
			continue
		}
		sets = append(sets, fmt.Sprintf("t.%s=s.%s", quoteIdent(w.typ, c), quoteIdent(w.typ, c)))
	}
	var on []string
	for _, k := range w.keys {
		on = append(on, fmt.Sprintf("t.%s=s.%s", quoteIdent(w.typ, k), quoteIdent(w.typ, k)))
	}
	q := fmt.Sprintf("MERGE INTO %s t USING (SELECT %s FROM dual) s ON (%s)", quoteIdent(w.typ, w.table), strings.Join(selects, ", "), strings.Join(on, " AND "))
	if len(sets) > 0 {
		q += " WHEN MATCHED THEN UPDATE SET " + strings.Join(sets, ", ")
	}
	q += fmt.Sprintf(" WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s)", strings.Join(insCols, ","), strings.Join(insVals, ","))
	_, err := w.db.ExecContext(ctx, q, args...)
	return err
}

func dbValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return x
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
		return x.String()
	case []byte:
		return string(x)
	case time.Time:
		return x.Format("2006-01-02 15:04:05")
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}
