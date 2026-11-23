package collector

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"collect/internal/applog"
	"collect/internal/config"
	"collect/internal/jsonx"
	"collect/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Pipeline 负责字段映射、主键去重、本地数据留存与入库。
//
// 每个数据来源（接口步骤 / 页面捕获到的接口 / DOM 提取）都有独立的
// sinkRoute：各自独立的写入器、独立的去重范围，因此不同接口的数据
// 可以落到不同的数据库、不同的表，也可以配置为「只预览、不落库」。
type Pipeline struct {
	db   *gorm.DB
	task *model.Task
	run  *model.RunLog
	log  *applog.Logger
	vars *Vars

	mapping     []model.FieldMapping
	dedupe      model.DedupeConfig
	defaultSink model.SinkConfig

	routes  map[string]*sinkRoute
	order   []string
	dsCache map[uint]model.DataSource
	seen    map[string]bool
	dataset []model.DatasetRow
	seq     int
}

// sinkRoute 一个数据来源对应的落库通道
type sinkRoute struct {
	key       string // 稳定的来源标识，如 step:2
	label     string // 展示名，如 门店列表
	sink      model.SinkConfig
	mapping   []model.FieldMapping // 本来源的字段映射
	targets   []string             // 本来源的字段顺序
	writer    RowWriter
	pending   []map[string]any
	batchSize int
	rows      int
	writeErr  error // 本次运行中该来源的首次写库错误，供 Close 生成落库步骤时使用
}

// RowWriter 目标库写入器（按表批量写入 / 自定义 SQL 写入）
type RowWriter interface {
	Write(rows []map[string]any) error
	Close() error
	// LastSQL 最近一次实际执行的 SQL，用于运行详情与排错
	LastSQL() string
}

func NewPipeline(db *gorm.DB, task *model.Task, run *model.RunLog, log *applog.Logger, vars *Vars) (*Pipeline, error) {
	p := &Pipeline{
		db: db, task: task, run: run, log: log, vars: vars,
		mapping: task.MappingList(), dedupe: task.DedupeCfg(), defaultSink: task.SinkCfg(),
		routes: map[string]*sinkRoute{}, dsCache: map[uint]model.DataSource{},
		seen: map[string]bool{},
	}
	normalizeSink(&p.defaultSink, "table")
	// 任务级落库配置仍在创建时校验，保证旧任务行为与提示不变
	if p.defaultSink.Type == "db" || p.defaultSink.Type == "both" {
		if p.defaultSink.DataSourceID == 0 {
			return nil, errors.New("入库方式包含数据库，但未选择数据源")
		}
		var ds model.DataSource
		if err := db.First(&ds, p.defaultSink.DataSourceID).Error; err != nil {
			return nil, errors.New("数据源不存在")
		}
		p.dsCache[ds.ID] = ds
		if !strings.EqualFold(p.defaultSink.WriteMode, "sql") && len(p.mappingTargets()) == 0 {
			return nil, errors.New("请先配置字段映射，再进行数据库入库")
		}
	}
	return p, nil
}

func normalizeSink(s *model.SinkConfig, defType string) {
	if s == nil {
		return
	}
	s.Type = strings.ToLower(strings.TrimSpace(s.Type))
	if s.Type == "" {
		s.Type = defType
	}
	s.WriteMode = strings.ToLower(strings.TrimSpace(s.WriteMode))
	if s.WriteMode == "" {
		s.WriteMode = "insert"
	}
	if s.BatchSize <= 0 {
		s.BatchSize = 200
	}
	if s.KeyColumns == nil {
		s.KeyColumns = []string{}
	}
	if s.ExportFormat == "" {
		s.ExportFormat = "xlsx"
	}
}

func (p *Pipeline) mappingTargets() []string {
	return targetsOf(p.mapping)
}

func targetsOf(mapping []model.FieldMapping) []string {
	out := make([]string, 0, len(mapping))
	seen := map[string]bool{}
	for _, m := range mapping {
		t := strings.TrimSpace(m.Target)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// Targets 全部来源的字段并集（用于整份运行导出）
func (p *Pipeline) Targets() []string { return p.allTargets() }

func (p *Pipeline) allTargets() []string {
	var out []string
	seen := map[string]bool{}
	add := func(cols []string) {
		for _, c := range cols {
			if c == "" || seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, c)
		}
	}
	add(p.mappingTargets())
	for _, k := range p.order {
		add(p.routes[k].targets)
	}
	return out
}

// SinkBatch 一批待处理数据的来源与落库配置
type SinkBatch struct {
	Source  string               // 稳定来源标识，如 step:2 / cap:1:门店列表
	Label   string               // 展示名
	Sink    *model.SinkConfig    // nil = 继承任务级落库配置
	Mapping []model.FieldMapping // 空 = 继承任务级字段映射
}

func (p *Pipeline) routeFor(b SinkBatch) *sinkRoute {
	key := strings.TrimSpace(b.Source)
	if key == "" {
		key = "default"
	}
	if rt, ok := p.routes[key]; ok {
		if strings.TrimSpace(rt.label) == "" && strings.TrimSpace(b.Label) != "" {
			rt.label = strings.TrimSpace(b.Label)
		}
		return rt
	}
	cfg := p.defaultSink
	if b.Sink != nil {
		cfg = *b.Sink
		normalizeSink(&cfg, "db")
		if cfg.KeyColumns == nil {
			cfg.KeyColumns = []string{}
		}
	}
	mapping := p.mapping
	if len(b.Mapping) > 0 {
		mapping = b.Mapping
	}
	lbl := strings.TrimSpace(b.Label)
	if lbl == "" {
		lbl = key
	}
	rt := &sinkRoute{
		key: key, label: lbl, sink: cfg,
		mapping: mapping, targets: targetsOf(mapping), batchSize: cfg.BatchSize,
	}
	p.routes[key] = rt
	p.order = append(p.order, key)
	return rt
}

// Add 处理一批原始记录
func (p *Pipeline) Add(b SinkBatch, records []map[string]any) error {
	if len(records) == 0 {
		return nil
	}
	rt := p.routeFor(b)
	for _, rec := range records {
		row := mapRowWith(rt.mapping, rec)
		if len(row) == 0 {
			continue
		}
		p.run.Fetched++
		if len(rt.targets) == 0 {
			p.ensureTargets(rt, row)
		}
		dup := false
		if p.dedupe.Enabled && len(p.dedupe.Keys) > 0 {
			var err error
			dup, err = p.isDuplicate(rt, rec, row)
			if err != nil {
				return fmt.Errorf("去重校验失败: %w", err)
			}
		}
		if dup {
			p.run.Duplicated++
			continue
		}
		if err := p.ensureWriter(rt, row); err != nil {
			return err
		}
		p.run.Inserted++
		if err := p.appendRow(rt, row); err != nil {
			return err
		}
	}
	return nil
}

func mapRowWith(mapping []model.FieldMapping, rec map[string]any) map[string]any {
	row := make(map[string]any, len(mapping))
	if len(mapping) == 0 {
		return jsonx.Flatten(rec)
	}
	for _, m := range mapping {
		if strings.TrimSpace(m.Target) == "" {
			continue
		}
		v, ok := jsonx.Lookup(rec, m.Source)
		if !ok || v == nil {
			if m.Default != "" {
				v = m.Default
			} else {
				v = nil
			}
		}
		row[m.Target] = convertValue(v, m)
	}
	return row
}

func convertValue(v any, m model.FieldMapping) any {
	switch strings.ToLower(m.Type) {
	case "int", "integer", "long":
		switch x := v.(type) {
		case float64:
			return int64(x)
		case int64:
			return x
		case int:
			return int64(x)
		case json.Number:
			if i, err := x.Int64(); err == nil {
				return i
			}
		}
		if s := strings.TrimSpace(jsonx.Stringify(v)); s != "" {
			if i, err := parseInt(s); err == nil {
				return i
			}
		}
		return nil
	case "float", "number", "double", "decimal":
		switch x := v.(type) {
		case float64:
			return x
		case int64:
			return float64(x)
		}
		if s := strings.TrimSpace(jsonx.Stringify(v)); s != "" {
			if f, err := parseFloat(s); err == nil {
				return f
			}
		}
		return nil
	case "bool", "boolean":
		s := strings.ToLower(strings.TrimSpace(jsonx.Stringify(v)))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	case "json":
		if v == nil {
			return nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return jsonx.Stringify(v)
		}
		return string(b)
	case "datetime", "date", "time":
		s := strings.TrimSpace(jsonx.Stringify(v))
		if s == "" {
			return nil
		}
		if f, err := parseFloat(s); err == nil && f > 1e9 {
			sec := int64(f)
			if f > 1e12 {
				sec = int64(f / 1000)
			}
			return time.Unix(sec, 0).Format("2006-01-02 15:04:05")
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05", "2006-01-02", "2006/01/02 15:04:05", "2006/01/02"} {
			if t, err := time.Parse(layout, s); err == nil {
				if layout == "2006-01-02" {
					return t.Format("2006-01-02")
				}
				return t.Format("2006-01-02 15:04:05")
			}
		}
		return s
	default:
		if v == nil {
			return nil
		}
		switch v.(type) {
		case map[string]any, []any:
			b, _ := json.Marshal(v)
			return string(b)
		}
		return jsonx.Stringify(v)
	}
}

func (p *Pipeline) ensureTargets(rt *sinkRoute, row map[string]any) {
	if len(rt.targets) > 0 {
		return
	}
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// id 类字段优先
	for i, k := range keys {
		if strings.EqualFold(k, "id") && i > 0 {
			keys[0], keys[i] = keys[i], keys[0]
			break
		}
	}
	rt.targets = keys
}

func (p *Pipeline) datasource(id uint) (model.DataSource, error) {
	if ds, ok := p.dsCache[id]; ok {
		return ds, nil
	}
	var ds model.DataSource
	if err := p.db.First(&ds, id).Error; err != nil {
		return ds, errors.New("数据源不存在")
	}
	p.dsCache[id] = ds
	return ds, nil
}

// ensureWriter 惰性创建该来源的数据库写入器（仅 db/both 需要）
func (p *Pipeline) ensureWriter(rt *sinkRoute, row map[string]any) error {
	if rt.writer != nil {
		return nil
	}
	if len(rt.targets) == 0 {
		p.ensureTargets(rt, row)
	}
	switch rt.sink.Type {
	case "db", "both":
	default:
		// table / none：只留存预览数据，表格在 Close 时统一导出
		return nil
	}
	if rt.sink.DataSourceID == 0 {
		return fmt.Errorf("接口「%s」选择了写入数据库，但未选择数据源", rt.label)
	}
	ds, err := p.datasource(rt.sink.DataSourceID)
	if err != nil {
		return fmt.Errorf("接口「%s」%w", rt.label, err)
	}
	if strings.EqualFold(rt.sink.WriteMode, "sql") {
		w, err := NewSQLSink(ds, rt.sink.SQLText, rt.targets, rt.batchSize, p.vars)
		if err != nil {
			return fmt.Errorf("接口「%s」创建 SQL 落库失败: %w", rt.label, err)
		}
		rt.writer = w
		return nil
	}
	if len(rt.targets) == 0 {
		return fmt.Errorf("接口「%s」请先配置字段映射，再进行数据库入库", rt.label)
	}
	w, err := NewSinkWriter(ds, rt.sink.Table, rt.targets, rt.sink.WriteMode, rt.sink.KeyColumns, rt.batchSize, rt.sink.AutoCreate)
	if err != nil {
		return fmt.Errorf("接口「%s」连接目标库失败: %w", rt.label, err)
	}
	rt.writer = w
	return nil
}

// dedupeScope 去重范围：写入数据库的按目标表区分，其余按来源区分，
// 避免不同接口的主键值互相冲突。
func (p *Pipeline) dedupeScope(rt *sinkRoute) string {
	if rt.sink.Type == "db" || rt.sink.Type == "both" {
		if !strings.EqualFold(rt.sink.WriteMode, "sql") {
			if t := strings.TrimSpace(rt.sink.Table); t != "" {
				return "tbl:" + strings.ToLower(t)
			}
		}
	}
	return "src:" + rt.key
}

func hashKey(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (p *Pipeline) isDuplicate(rt *sinkRoute, rec, row map[string]any) (bool, error) {
	var parts []string
	for _, k := range p.dedupe.Keys {
		v, ok := jsonx.Lookup(rec, k)
		if ok {
			parts = append(parts, jsonx.Stringify(v))
			continue
		}
		if rv, ok2 := row[k]; ok2 {
			parts = append(parts, jsonx.Stringify(rv))
		}
	}
	keyText := strings.Join(parts, "|")
	if strings.Trim(keyText, "|") == "" {
		b, _ := json.Marshal(row)
		keyText = string(b)
	}
	legacySeed := fmt.Sprintf("%d|%s", p.task.ID, keyText)
	seed := legacySeed
	if scope := p.dedupeScope(rt); scope != "" {
		seed = fmt.Sprintf("%d|%s|%s", p.task.ID, scope, keyText)
	}
	h := hashKey(seed)
	if p.seen[h] {
		return true, nil
	}
	// 兼容升级前的去重记录（旧版本按 任务+主键 记录），避免整表重复入库
	if seed != legacySeed {
		var n int64
		if err := p.db.Model(&model.DedupeKey{}).
			Where("task_id = ? AND hash = ?", p.task.ID, hashKey(legacySeed)).
			Count(&n).Error; err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	p.seen[h] = true
	dk := model.DedupeKey{TaskID: p.task.ID, Hash: h, KeyText: keyText, CreatedAt: time.Now()}
	res := p.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&dk)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 0, nil
}

func (p *Pipeline) appendRow(rt *sinkRoute, row map[string]any) error {
	p.seq++
	b, err := json.Marshal(row)
	if err != nil {
		return err
	}
	dr := model.DatasetRow{
		RunID: p.run.ID, TaskID: p.task.ID, Seq: p.seq, Data: string(b), CreatedAt: time.Now(),
	}
	if rt.key != "default" {
		dr.Source = rt.key
		dr.SourceLabel = rt.label
	}
	p.dataset = append(p.dataset, dr)
	rt.rows++
	if len(p.dataset) >= 200 {
		if err := p.flushDataset(); err != nil {
			return err
		}
	}
	if rt.writer != nil {
		rt.pending = append(rt.pending, row)
		if len(rt.pending) >= rt.batchSize {
			if err := p.flushRoute(rt); err != nil {
				return err
			}
		}
	}
	return nil
}

// flushRoute 把某个来源累积的记录交给它自己的写入器
func (p *Pipeline) flushRoute(rt *sinkRoute) error {
	if rt.writer == nil || len(rt.pending) == 0 {
		return nil
	}
	rows := rt.pending
	rt.pending = nil
	if err := rt.writer.Write(rows); err != nil {
		err = fmt.Errorf("接口「%s」写入数据库失败: %w", rt.label, err)
		if rt.writeErr == nil {
			rt.writeErr = err
		}
		return err
	}
	return nil
}

// sinkSQL 取落库器实际执行的 SQL（取不到时回退到配置里的模板 / 目标表）
func (p *Pipeline) sinkSQL(rt *sinkRoute) string {
	if rt.writer != nil {
		if q := strings.TrimSpace(rt.writer.LastSQL()); q != "" {
			return q
		}
	}
	if strings.EqualFold(rt.sink.WriteMode, "sql") {
		return strings.TrimSpace(rt.sink.SQLText)
	}
	return ""
}

func (p *Pipeline) flushDataset() error {
	if len(p.dataset) == 0 {
		return nil
	}
	rows := p.dataset
	p.dataset = nil
	return p.db.CreateInBatches(rows, 200).Error
}

// Close 落盘并生成表格文件
func (p *Pipeline) Close() error {
	var firstErr error
	for _, key := range p.order {
		rt := p.routes[key]
		if rt.writer == nil {
			continue
		}
		start := time.Now()
		// flush 阶段的错误已经记在 rt.writeErr 上：此时缓冲已在失败时清空，
		// 若只看本次 flush 的结果，落库步骤会被误记成成功。
		err := p.flushRoute(rt)
		if err == nil {
			err = rt.writeErr
		}
		if e := rt.writer.Close(); e != nil && err == nil {
			err = fmt.Errorf("数据库写入失败: %w", e)
		}
		status := "success"
		msg := fmt.Sprintf("写入 %d 条", rt.rows)
		if err != nil {
			status = "failed"
			msg = err.Error()
			if firstErr == nil {
				firstErr = err
			}
			if p.log != nil {
				p.log.Error("sink", "接口「%s」写入数据库失败: %s\nSQL: %s", rt.label, err.Error(), p.sinkSQL(rt))
			}
		}
		p.addSinkStep(rt, status, msg, time.Since(start))
	}
	if e := p.flushDataset(); e != nil && firstErr == nil {
		firstErr = e
	}
	if p.run.Inserted > 0 {
		path := config.DataPath("exports", fmt.Sprintf("%d", p.task.ID), fmt.Sprintf("run_%d.xlsx", p.run.ID))
		if e := ExportRunXLSX(p.db, p.run.ID, "", p.allTargets(), path); e == nil {
			p.run.ExportPath = path
		} else if p.log != nil {
			p.log.Warn("pipeline", "生成表格失败: %v", e)
		}
	}
	return firstErr
}

// addSinkStep 在运行详情里记录一条落库步骤（每个接口一条）
func (p *Pipeline) addSinkStep(rt *sinkRoute, status, msg string, dur time.Duration) {
	if p.db == nil || p.run == nil || p.run.ID == 0 {
		return
	}
	var maxSeq int
	p.db.Model(&model.StepLog{}).Where("run_id = ?", p.run.ID).Select("COALESCE(MAX(seq),0)").Scan(&maxSeq)
	dest := strings.TrimSpace(rt.sink.Table)
	if q := p.sinkSQL(rt); q != "" {
		// upsert / insert 的 SQL 才是真正的执行语句；自定义 SQL 模板也在这里展示
		dest = previewSQL(q, 4000)
	}
	kind := "数据库"
	if rt.sink.Type == "both" {
		kind = "表格 + 数据库"
	} else if strings.EqualFold(rt.sink.WriteMode, "sql") {
		kind = "自定义 SQL"
	}
	if ds, ok := p.dsCache[rt.sink.DataSourceID]; ok && strings.TrimSpace(ds.Name) != "" {
		kind = strings.ToUpper(ds.Type) + " · " + ds.Name
	}
	if dest == "" {
		dest = "-"
	}
	name := fmt.Sprintf("落库 · %s → %s", rt.label, kind)
	sl := model.StepLog{
		RunID: p.run.ID, Seq: maxSeq + 1, Name: name, Kind: "sink", Status: status,
		DurationMs: dur.Milliseconds(), URL: dest, RespBody: msg, CreatedAt: time.Now(),
	}
	if status != "success" {
		sl.Error = msg
	}
	if err := p.db.Create(&sl).Error; err != nil && p.log != nil {
		p.log.Warn("pipeline", "写入落库步骤日志失败: %v", err)
	}
}
