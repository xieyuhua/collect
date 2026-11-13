package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// KV 通用键值对（header / 变量）
type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Desc  string `json:"desc,omitempty"`
}

// ExtractRule 从响应中提取变量
type ExtractRule struct {
	Var    string `json:"var"`
	Path   string `json:"path"`
	Prefix string `json:"prefix,omitempty"`
	Header string `json:"header,omitempty"` // 从响应头提取
	Regex  string `json:"regex,omitempty"`
}

// HTTPStep 一次 HTTP 请求步骤
type HTTPStep struct {
	Name        string        `json:"name"`
	Method      string        `json:"method"`
	URL         string        `json:"url"`
	Headers     []KV          `json:"headers"`
	Body        string        `json:"body"`
	ContentType string        `json:"content_type"`
	Extract     []ExtractRule `json:"extract"`
	Paginate    bool          `json:"paginate"`
	Capture     bool          `json:"capture"` // 从该响应提取数据记录
	RecordsPath string        `json:"records_path"`
	TotalPath   string        `json:"total_path"`
	DelayMs     int           `json:"delay_ms"`
	// Sink 该接口单独的数据落库配置
	// nil   -> 继承任务级落库设置（旧任务兼容）
	// Type 为 none -> 只保留预览数据，不写数据库/表格
	Sink *SinkConfig `json:"sink,omitempty"`
	// Mapping 该接口单独的字段映射；为空时继承任务级字段映射
	Mapping []FieldMapping `json:"mapping,omitempty"`
	// Pagination 该接口单独的分页配置；nil 时继承任务级分页配置
	Pagination *Pagination `json:"pagination,omitempty"`
}

// UIStep 浏览器动作
type UIStep struct {
	Action   string  `json:"action"` // open|click|input|select|press|wait|scroll|eval|wait_for
	Selector string  `json:"selector"`
	Value    string  `json:"value"`
	Key      string  `json:"key"`
	Ms       int     `json:"ms"`
	Text     string  `json:"text"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Desc     string  `json:"desc"`
	// Frame 该动作所在的 iframe 选择器（CSS）。留空 = 主页面。
	// 支持用 ">>" 逐层进入嵌套 iframe，例如 "iframe#main >> iframe.inner"
	Frame string `json:"frame,omitempty"`
	// OpenMode 仅「打开页面」使用：空/nav = 整页打开（会重新加载页面）；
	// spa = 同源站内路由跳转（不刷新页面，保留 SPA 内存中的登录态）
	OpenMode string `json:"open_mode,omitempty"`
}

// CaptureConfig UI 模式下的数据抓取配置
type CaptureConfig struct {
	Name        string `json:"name,omitempty"` // 规则名称，多规则时用于区分数据来源
	Mode        string `json:"mode"`           // network | dom
	URLContains string `json:"url_contains"`
	Method      string `json:"method"`
	RecordsPath string `json:"records_path"`
	WaitMs      int    `json:"wait_ms"`
	RowSelector string `json:"row_selector"`
	// Frame DOM 模式下表格所在的 iframe 选择器；留空 = 主页面
	Frame string `json:"frame,omitempty"`
	// Mapping 该规则自己的字段映射；为空时继承任务级字段映射
	Mapping []FieldMapping `json:"mapping,omitempty"`
	// Sink 该规则自己的落库配置；nil 时继承任务级落库配置
	Sink *SinkConfig `json:"sink,omitempty"`
	// Pagination 该规则的接口分页配置（纯 UI 模式下按接口翻页抓取）；
	// nil 或 Enabled=false 时只采集页面回放期间自然产生的请求
	Pagination *Pagination `json:"pagination,omitempty"`
	// Rules 多条抓取规则（可同时抓多个接口 / 多个 DOM 表格）；为空时使用本级配置
	Rules []CaptureConfig `json:"rules,omitempty"`
	// DOM 模式下 fields 的 source 为单元格 CSS 选择器，@attr 表示取属性
}

// Pagination 分页配置
type Pagination struct {
	Enabled       bool   `json:"enabled"`
	// Mode 翻页方式：http=复用捕获接口逐页请求（默认，兼容旧配置）；ui=真实点击「下一页」按钮翻页
	// 纯 UI 模式建议用 ui，风控严格的站点几乎等同于真人操作
	Mode          string `json:"mode,omitempty"`
	Type          string `json:"type"` // page|offset|cursor
	PageParam     string `json:"page_param"`
	SizeParam     string `json:"size_param"`
	PageSize      int    `json:"page_size"`
	StartPage     int    `json:"start_page"`
	MaxPages      int    `json:"max_pages"`
	OffsetParam   string `json:"offset_param"`
	CursorParam   string `json:"cursor_param"`
	CursorPath    string `json:"cursor_path"`
	RecordsPath   string `json:"records_path"`
	TotalPath     string `json:"total_path"`
	StopWhenEmpty bool   `json:"stop_when_empty"`
	DelayMs       int    `json:"delay_ms"`
	PageInBody    bool   `json:"page_in_body"` // 分页参数放在 body(JSON)
	// UI 点击翻页（Mode=ui 时生效）
	NextSelector string `json:"next_selector,omitempty"` // 「下一页」按钮选择器，如 .pager .next / a[rel=next]
	NextText     string `json:"next_text,omitempty"`     // 选择器兜底：按按钮文字查找，如 下一页 / Next
	WaitMs       int    `json:"wait_ms,omitempty"`       // 每次点击后等待新数据加载的最长时间
	// Frame 「下一页」按钮 / 表格所在的 iframe 选择器；留空 = 主页面
	Frame string `json:"frame,omitempty"`
}

// FieldMapping 字段映射
type FieldMapping struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Type    string `json:"type"` // string|int|float|bool|datetime|json
	Format  string `json:"format,omitempty"`
	Default string `json:"default,omitempty"`
	Desc    string `json:"desc,omitempty"`
}

// SinkConfig 数据落地
type SinkConfig struct {
	Type         string   `json:"type"` // table|db|both|none
	DataSourceID uint     `json:"data_source_id"`
	Table        string   `json:"table"`
	WriteMode    string   `json:"write_mode"` // insert|upsert|sql
	KeyColumns   []string `json:"key_columns"`
	BatchSize    int      `json:"batch_size"`
	AutoCreate   bool     `json:"auto_create"`
	ExportFormat string   `json:"export_format"` // xlsx|csv
	// SQLText 自定义 SQL 模板（write_mode = sql 时生效）
	// 支持 {{字段}} 参数绑定、{{变量|修饰}} 运行期变量内联、?/:1/$1 原生占位符
	SQLText string `json:"sql_text,omitempty"`
}

// DedupeConfig 主键去重
type DedupeConfig struct {
	Enabled bool     `json:"enabled"`
	Keys    []string `json:"keys"`
}

type Task struct {
	ID           uint       `gorm:"primarykey" json:"id"`
	Name         string     `json:"name"`
	Mode         string     `json:"mode"` // api|ui|hybrid
	Remark       string     `json:"remark"`
	Enabled      bool       `json:"enabled"`
	Notify       bool       `json:"notify"`
	NotifyOn     string     `json:"notify_on"` // failure|always|success
	ScheduleType string     `json:"schedule_type"`
	CronExpr     string     `json:"cron"`
	IntervalSec  int        `json:"interval_sec"`
	TimeoutSec   int        `json:"timeout_sec"`
	// SessionMode 任务级会话模式：空=跟随系统设置，isolated=每次全新无痕，shared=复用共享会话
	SessionMode  string     `json:"session_mode"`
	Vars         string     `gorm:"type:text" json:"-"`
	HTTPSteps    string     `gorm:"type:text" json:"-"`
	UISteps      string     `gorm:"type:text" json:"-"`
	LoginSteps   string     `gorm:"type:text" json:"-"`
	Capture      string     `gorm:"type:text" json:"-"`
	Pagination   string     `gorm:"type:text" json:"-"`
	Mapping      string     `gorm:"type:text" json:"-"`
	Sink         string     `gorm:"type:text" json:"-"`
	Dedupe       string     `gorm:"type:text" json:"-"`
	LastRunAt    *time.Time `json:"last_run_at"`
	LastStatus   string     `json:"last_status"`
	LastMessage  string     `gorm:"type:text" json:"last_message"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (t *Task) VarsList() []KV { var v []KV; _ = json.Unmarshal([]byte(t.Vars), &v); return v }
func (t *Task) HTTPStepList() []HTTPStep {
	var v []HTTPStep
	_ = json.Unmarshal([]byte(t.HTTPSteps), &v)
	return v
}
func (t *Task) UIStepList() []UIStep {
	var v []UIStep
	_ = json.Unmarshal([]byte(t.UISteps), &v)
	return v
}
func (t *Task) LoginStepList() []UIStep {
	var v []UIStep
	_ = json.Unmarshal([]byte(t.LoginSteps), &v)
	return v
}
func (t *Task) CaptureCfg() CaptureConfig {
	var v CaptureConfig
	_ = json.Unmarshal([]byte(t.Capture), &v)
	return v
}

// CaptureRules 返回生效的抓取规则列表（兼容旧任务：无 rules 时把自身当作一条规则）
func (t *Task) CaptureRules() []CaptureConfig {
	cfg := t.CaptureCfg()
	if len(cfg.Rules) > 0 {
		out := make([]CaptureConfig, 0, len(cfg.Rules))
		for i, r := range cfg.Rules {
			rc := r
			if strings.TrimSpace(rc.Mode) == "" {
				rc.Mode = "network"
			}
			if strings.TrimSpace(rc.Name) == "" {
				rc.Name = fmt.Sprintf("规则%d", i+1)
			}
			out = append(out, rc)
		}
		return out
	}
	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = "network"
	}
	if strings.TrimSpace(cfg.Mode) == "network" && strings.TrimSpace(cfg.URLContains) == "" && strings.TrimSpace(cfg.RecordsPath) == "" {
		return nil
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "默认规则"
	}
	return []CaptureConfig{cfg}
}
func (t *Task) PaginationCfg() Pagination {
	var v Pagination
	_ = json.Unmarshal([]byte(t.Pagination), &v)
	return v
}
func (t *Task) MappingList() []FieldMapping {
	var v []FieldMapping
	_ = json.Unmarshal([]byte(t.Mapping), &v)
	return v
}
func (t *Task) SinkCfg() SinkConfig {
	var v SinkConfig
	_ = json.Unmarshal([]byte(t.Sink), &v)
	return v
}
func (t *Task) DedupeCfg() DedupeConfig {
	var v DedupeConfig
	_ = json.Unmarshal([]byte(t.Dedupe), &v)
	return v
}

// ToMap 输出给前端
func (t *Task) ToMap() map[string]any {
	var vars []KV
	var steps []HTTPStep
	var ui []UIStep
	var login []UIStep
	var cap CaptureConfig
	var pg Pagination
	var mapping []FieldMapping
	var sink SinkConfig
	var dedupe DedupeConfig
	_ = json.Unmarshal([]byte(t.Vars), &vars)
	_ = json.Unmarshal([]byte(t.HTTPSteps), &steps)
	_ = json.Unmarshal([]byte(t.UISteps), &ui)
	_ = json.Unmarshal([]byte(t.LoginSteps), &login)
	_ = json.Unmarshal([]byte(t.Capture), &cap)
	_ = json.Unmarshal([]byte(t.Pagination), &pg)
	_ = json.Unmarshal([]byte(t.Mapping), &mapping)
	_ = json.Unmarshal([]byte(t.Sink), &sink)
	_ = json.Unmarshal([]byte(t.Dedupe), &dedupe)
	if vars == nil {
		vars = []KV{}
	}
	if steps == nil {
		steps = []HTTPStep{}
	}
	if ui == nil {
		ui = []UIStep{}
	}
	if login == nil {
		login = []UIStep{}
	}
	if mapping == nil {
		mapping = []FieldMapping{}
	}
	if dedupe.Keys == nil {
		dedupe.Keys = []string{}
	}
	if sink.KeyColumns == nil {
		sink.KeyColumns = []string{}
	}
	return map[string]any{
		"id": t.ID, "name": t.Name, "mode": t.Mode, "remark": t.Remark, "enabled": t.Enabled,
		"notify": t.Notify, "notify_on": t.NotifyOn, "schedule_type": t.ScheduleType,
		"cron": t.CronExpr, "interval_sec": t.IntervalSec, "timeout_sec": t.TimeoutSec,
		"session_mode": t.SessionMode,
		"vars": vars, "http_steps": steps, "ui_steps": ui, "login_steps": login,
		"capture": cap, "pagination": pg, "mapping": mapping, "sink": sink, "dedupe": dedupe,
		"last_run_at": t.LastRunAt, "last_status": t.LastStatus, "last_message": t.LastMessage,
		"created_at": t.CreatedAt, "updated_at": t.UpdatedAt,
	}
}

type RunLog struct {
	ID         uint       `gorm:"primarykey" json:"id"`
	TaskID     uint       `gorm:"index" json:"task_id"`
	TaskName   string     `json:"task_name"`
	Mode       string     `json:"mode"`
	Trigger    string     `json:"trigger"` // manual|cron
	Status     string     `json:"status"`  // running|success|failed|partial
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	DurationMs int64      `json:"duration_ms"`
	Pages      int        `json:"pages"`
	Fetched    int        `json:"fetched"`
	Inserted   int        `json:"inserted"`
	Duplicated int        `json:"duplicated"`
	Failed     int        `json:"failed"`
	Error      string     `gorm:"type:text" json:"error"`
	Message    string     `gorm:"type:text" json:"message"`
	ExportPath string     `json:"export_path"`
	Notified   bool       `json:"notified"`
	CreatedAt  time.Time  `json:"created_at"`
}

type StepLog struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	RunID       uint      `gorm:"index" json:"run_id"`
	Seq         int       `json:"seq"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"` // http|ui|sink|info
	Status      string    `json:"status"`
	DurationMs  int64     `json:"duration_ms"`
	Method      string    `json:"method"`
	URL         string    `gorm:"type:text" json:"url"`
	ReqHeaders  string    `gorm:"type:text" json:"req_headers"`
	ReqBody     string    `gorm:"type:text" json:"req_body"`
	RespStatus  int       `json:"resp_status"`
	RespHeaders string    `gorm:"type:text" json:"resp_headers"`
	RespBody    string    `gorm:"type:text" json:"resp_body"`
	Error       string    `gorm:"type:text" json:"error"`
	CreatedAt   time.Time `json:"created_at"`
}

type DatasetRow struct {
	ID     uint `gorm:"primarykey" json:"id"`
	RunID  uint `gorm:"index" json:"run_id"`
	TaskID uint `gorm:"index" json:"task_id"`
	Seq    int  `json:"seq"`
	// Source 数据来源标识（接口步骤 / 捕获接口 / DOM），用于运行详情按接口分开预览
	Source      string    `gorm:"index;size:191" json:"source"`
	SourceLabel string    `json:"source_label"`
	Data        string    `gorm:"type:text" json:"data"`
	CreatedAt   time.Time `json:"created_at"`
}

type DataSource struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // mysql|oracle|sqlite|postgres
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	User      string    `json:"user"`
	Password  string    `json:"password"`
	Database  string    `json:"database"`
	Params    string    `json:"params"`
	DSN       string    `json:"dsn"` // 直接填写时优先使用
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Notification struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	TaskID    uint      `json:"task_id"`
	RunID     uint      `json:"run_id"`
	Level     string    `json:"level"` // success|error|info
	Title     string    `json:"title"`
	Content   string    `gorm:"type:text" json:"content"`
	Channel   string    `json:"channel"`
	Status    string    `json:"status"` // sent|failed|skipped
	Error     string    `json:"error"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

type DedupeKey struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	TaskID    uint      `gorm:"uniqueIndex:idx_task_hash" json:"task_id"`
	Hash      string    `gorm:"uniqueIndex:idx_task_hash;size:64" json:"hash"`
	KeyText   string    `gorm:"type:text" json:"key_text"`
	CreatedAt time.Time `json:"created_at"`
}

type Setting struct {
	Key       string    `gorm:"primarykey" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ---------- 定时作业（GET/POST / Shell / SQL） ----------

// JobStep 定时作业中的一步动作
type JobStep struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // http | shell | sql
	Enabled *bool  `json:"enabled,omitempty"`
	// HTTP
	Method      string `json:"method,omitempty"`
	URL         string `json:"url,omitempty"`
	Headers     []KV   `json:"headers,omitempty"`
	Body        string `json:"body,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	// Extract 从 HTTP 响应提取变量，供后续步骤使用：Key=变量名，Value=取值表达式
	// 支持 JSON 路径 data.token / data.list.0.id、header:X-Token、regex:正则、status
	Extract []KV `json:"extract,omitempty"`
	// Shell
	Command string `json:"command,omitempty"`
	WorkDir string `json:"work_dir,omitempty"`
	Env     []KV   `json:"env,omitempty"`
	// SQL
	DataSourceID uint   `json:"data_source_id,omitempty"`
	SQL          string `json:"sql,omitempty"`
	// 通用
	TimeoutSec      int  `json:"timeout_sec,omitempty"`
	ContinueOnError bool `json:"continue_on_error,omitempty"`
}

// IsEnabled 未显式设置时视为启用
func (s JobStep) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// Job 定时作业：按 Cron / 固定间隔执行一组动作
type Job struct {
	ID           uint       `gorm:"primarykey" json:"id"`
	Name         string     `json:"name"`
	Remark       string     `json:"remark"`
	Enabled      bool       `json:"enabled"`
	Notify       bool       `json:"notify"`
	NotifyOn     string     `json:"notify_on"` // failure|always|success
	ScheduleType string     `json:"schedule_type"`
	CronExpr     string     `json:"cron"`
	IntervalSec  int        `json:"interval_sec"`
	TimeoutSec   int        `json:"timeout_sec"`
	Steps        string     `gorm:"type:text" json:"-"`
	LastRunAt    *time.Time `json:"last_run_at"`
	LastStatus   string     `json:"last_status"`
	LastMessage  string     `gorm:"type:text" json:"last_message"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (j *Job) StepList() []JobStep {
	var v []JobStep
	_ = json.Unmarshal([]byte(j.Steps), &v)
	return v
}

func (j *Job) ToMap() map[string]any {
	steps := j.StepList()
	if steps == nil {
		steps = []JobStep{}
	}
	return map[string]any{
		"id": j.ID, "name": j.Name, "remark": j.Remark, "enabled": j.Enabled,
		"notify": j.Notify, "notify_on": j.NotifyOn, "schedule_type": j.ScheduleType,
		"cron": j.CronExpr, "interval_sec": j.IntervalSec, "timeout_sec": j.TimeoutSec,
		"steps": steps, "last_run_at": j.LastRunAt, "last_status": j.LastStatus,
		"last_message": j.LastMessage, "created_at": j.CreatedAt, "updated_at": j.UpdatedAt,
	}
}

// JobRun 作业执行记录
type JobRun struct {
	ID         uint       `gorm:"primarykey" json:"id"`
	JobID      uint       `gorm:"index" json:"job_id"`
	JobName    string     `json:"job_name"`
	Trigger    string     `json:"trigger"` // manual|cron
	Status     string     `json:"status"`  // running|success|failed|partial
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	DurationMs int64      `json:"duration_ms"`
	StepsTotal int        `json:"steps_total"`
	StepsOK    int        `json:"steps_ok"`
	StepsFail  int        `json:"steps_fail"`
	Error      string     `gorm:"type:text" json:"error"`
	Message    string     `gorm:"type:text" json:"message"`
	Notified   bool       `json:"notified"`
	CreatedAt  time.Time  `json:"created_at"`
}

// JobStepLog 作业步骤日志（请求 / 响应 / 命令输出）
type JobStepLog struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	JobRunID    uint      `gorm:"index" json:"job_run_id"`
	Seq         int       `json:"seq"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"` // http|shell|sql
	Status      string    `json:"status"`
	DurationMs  int64     `json:"duration_ms"`
	Method      string    `json:"method"`
	URL         string    `gorm:"type:text" json:"url"`
	ReqHeaders  string    `gorm:"type:text" json:"req_headers"`
	ReqBody     string    `gorm:"type:text" json:"req_body"`
	RespStatus  int       `json:"resp_status"`
	RespHeaders string    `gorm:"type:text" json:"resp_headers"`
	RespBody    string    `gorm:"type:text" json:"resp_body"`
	Output      string    `gorm:"type:text" json:"output"`
	Error       string    `gorm:"type:text" json:"error"`
	CreatedAt   time.Time `json:"created_at"`
}
