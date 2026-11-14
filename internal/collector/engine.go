package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"collect/internal/applog"
	"collect/internal/browser"
	"collect/internal/config"
	"collect/internal/jsonx"
	"collect/internal/model"
	"collect/internal/notify"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"gorm.io/gorm"
)

type Engine struct {
	cfg *config.Config
	db  *gorm.DB
	log *applog.Logger
	bm  *browser.Manager
	nt  *notify.Notifier

	mu      sync.Mutex
	running map[uint]*runControl
}

type runControl struct {
	runID  uint
	cancel context.CancelFunc
}

func NewEngine(cfg *config.Config, db *gorm.DB, log *applog.Logger, bm *browser.Manager, nt *notify.Notifier) *Engine {
	return &Engine{cfg: cfg, db: db, log: log, bm: bm, nt: nt, running: map[uint]*runControl{}}
}

func (e *Engine) IsRunning(taskID uint) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running[taskID] != nil
}

func (e *Engine) acquire(taskID uint) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running[taskID] != nil {
		return false
	}
	e.running[taskID] = &runControl{}
	return true
}

func (e *Engine) attach(taskID, runID uint, cancel context.CancelFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ctrl := e.running[taskID]; ctrl != nil {
		ctrl.runID = runID
		ctrl.cancel = cancel
	}
}

// Stop sends a cancellation signal to a running task and reports whether the run was active.
func (e *Engine) Stop(runID uint) bool {
	e.mu.Lock()
	var cancel context.CancelFunc
	for _, ctrl := range e.running {
		if ctrl != nil && ctrl.runID == runID {
			cancel = ctrl.cancel
			break
		}
	}
	e.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (e *Engine) release(taskID uint) {
	e.mu.Lock()
	delete(e.running, taskID)
	e.mu.Unlock()
}

// Start 异步执行任务，立即返回运行记录
func (e *Engine) Start(taskID uint, trigger string) (*model.RunLog, error) {
	var task model.Task
	if err := e.db.First(&task, taskID).Error; err != nil {
		return nil, errors.New("任务不存在")
	}
	if !e.acquire(taskID) {
		return nil, errors.New("任务正在运行中，请稍后再试")
	}
	run := model.RunLog{
		TaskID: task.ID, TaskName: task.Name, Mode: task.Mode, Trigger: trigger,
		Status: "running", StartedAt: time.Now(), CreatedAt: time.Now(),
	}
	if err := e.db.Create(&run).Error; err != nil {
		e.release(taskID)
		return nil, err
	}
	e.db.Model(&model.Task{}).Where("id = ?", task.ID).
		Updates(map[string]any{"last_run_at": run.StartedAt, "last_status": "running", "last_message": ""})
	ctx, cancel := context.WithCancel(context.Background())
	e.attach(task.ID, run.ID, cancel)
	go func() {
		defer cancel()
		e.execute(ctx, &task, &run)
	}()
	return &run, nil
}

func (e *Engine) execute(ctx context.Context, task *model.Task, run *model.RunLog) {
	defer e.release(task.ID)
	c := &runCtx{ctx: ctx, e: e, task: task, run: run, vars: NewVars(), seq: 0}
	for _, kv := range task.VarsList() {
		if strings.TrimSpace(kv.Key) != "" {
			c.vars.Set(strings.TrimSpace(kv.Key), kv.Value)
		}
	}
	timeout := time.Duration(task.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(e.cfg.TimeoutSec) * time.Second
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	exec, err := NewExecutor(timeout, e.cfg.Proxy, true)
	if err != nil {
		c.err = err
		c.finish()
		return
	}
	c.exec = exec
	pipeline, err := NewPipeline(e.db, task, run, e.log, c.vars)
	if err != nil {
		c.err = err
		c.finish()
		return
	}
	c.pipeline = pipeline
	func() {
		defer func() {
			if r := recover(); r != nil {
				c.err = fmt.Errorf("执行异常: %v", r)
			}
		}()
		c.err = c.runTask()
	}()
	c.finish()
}

type runCtx struct {
	ctx      context.Context
	e        *Engine
	task     *model.Task
	run      *model.RunLog
	vars     *Vars
	exec     *Executor
	pipeline *Pipeline
	seqMu    sync.Mutex
	seq      int
	// 序号按「真实开始时间」预分配：UI 步骤开始时分配，网络请求发起时分配，
	// 避免日志写入顺序（UI 步骤同步写、接口异步补写）导致运行详情顺序错乱
	orderMu sync.Mutex
	reqSeq  map[string]int
	stepSeq map[int]int
	err     error
}

func (c *runCtx) nextSeq() int {
	c.seqMu.Lock()
	defer c.seqMu.Unlock()
	c.seq++
	return c.seq
}

// reserveReqSeq 在网络请求发起时为该请求预分配步骤序号
func (c *runCtx) reserveReqSeq(id string) {
	if id == "" {
		return
	}
	c.orderMu.Lock()
	if c.reqSeq == nil {
		c.reqSeq = make(map[string]int, 64)
	}
	_, exists := c.reqSeq[id]
	tooMany := len(c.reqSeq) > 3000
	c.orderMu.Unlock()
	if exists || tooMany {
		return
	}
	seq := c.nextSeq()
	c.orderMu.Lock()
	if _, ok := c.reqSeq[id]; !ok {
		c.reqSeq[id] = seq
	}
	c.orderMu.Unlock()
}

// takeReqSeq 取出（并释放）预分配的序号
func (c *runCtx) takeReqSeq(id string) (int, bool) {
	c.orderMu.Lock()
	defer c.orderMu.Unlock()
	seq, ok := c.reqSeq[id]
	if ok {
		delete(c.reqSeq, id)
	}
	return seq, ok
}

// reserveStepSeq 在 UI 步骤开始执行前预分配步骤序号
func (c *runCtx) reserveStepSeq(i int) int {
	c.orderMu.Lock()
	defer c.orderMu.Unlock()
	if c.stepSeq == nil {
		c.stepSeq = make(map[int]int, 32)
	}
	if seq, ok := c.stepSeq[i]; ok {
		return seq
	}
	c.seqMu.Lock()
	c.seq++
	seq := c.seq
	c.seqMu.Unlock()
	c.stepSeq[i] = seq
	return seq
}

// takeStepSeq 取出（并释放）UI 步骤预分配的序号
func (c *runCtx) takeStepSeq(i int) (int, bool) {
	c.orderMu.Lock()
	defer c.orderMu.Unlock()
	seq, ok := c.stepSeq[i]
	if ok {
		delete(c.stepSeq, i)
	}
	return seq, ok
}

func (c *runCtx) truncate(s string) string {
	max := c.e.cfg.MaxBodyKB
	if max <= 0 {
		max = 256
	}
	limit := max * 1024
	if len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("\n...[截断，原始大小 %d 字节]", len(s))
}

func (c *runCtx) saveStep(s *model.StepLog) {
	if err := c.e.db.Create(s).Error; err != nil {
		c.e.log.Error("engine", "写入步骤日志失败: %v", err)
	}
}

func (c *runCtx) runTask() error {
	switch c.task.Mode {
	case "ui":
		return c.runUI()
	case "hybrid":
		return c.runHybrid()
	default:
		return c.runAPI()
	}
}

func (c *runCtx) finish() {
	if c.pipeline != nil {
		if err := c.pipeline.Close(); err != nil && c.err == nil {
			c.err = err
		}
	}
	now := time.Now()
	c.run.FinishedAt = &now
	c.run.DurationMs = now.Sub(c.run.StartedAt).Milliseconds()
	stopped := errors.Is(c.ctx.Err(), context.Canceled)
	if stopped {
		c.run.Status = "stopped"
		c.run.Error = "用户手动结束"
	} else if c.err != nil {
		c.run.Error = c.err.Error()
		if c.run.Inserted > 0 {
			c.run.Status = "partial"
		} else {
			c.run.Status = "failed"
		}
	} else {
		c.run.Status = "success"
	}
	summary := fmt.Sprintf("页数 %d / 抓取 %d / 新增 %d / 重复 %d", c.run.Pages, c.run.Fetched, c.run.Inserted, c.run.Duplicated)
	if c.run.Message != "" {
		summary = c.run.Message + " | " + summary
	}
	c.run.Message = summary
	if err := c.e.db.Save(c.run).Error; err != nil {
		c.e.log.Error("engine", "保存运行记录失败: %v", err)
	}
	c.e.db.Model(&model.Task{}).Where("id = ?", c.task.ID).Updates(map[string]any{
		"last_run_at": now, "last_status": c.run.Status, "last_message": summary,
	})
	switch c.run.Status {
	case "success":
		c.e.log.Info("engine", "任务 %s 执行成功: %s", c.task.Name, summary)
	case "stopped":
		c.e.log.Info("engine", "任务 %s 已手动结束: %s", c.task.Name, summary)
	default:
		c.e.log.Error("engine", "任务 %s 执行%s: %s %s", c.task.Name, map[string]string{"failed": "失败", "partial": "部分成功"}[c.run.Status], summary, c.run.Error)
	}
	c.e.nt.NotifyRun(c.task, c.run)
	if c.run.Notified {
		c.e.db.Model(c.run).Update("notified", true)
	}
}

// ---------- HTTP 采集 ----------

func (c *runCtx) runAPI() error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if n := len(c.task.UIStepList()) + len(c.task.LoginStepList()); n > 0 {
		c.e.log.Warn("engine", "当前是「纯接口」模式，不启动浏览器：已忽略 %d 条 UI / 登录动作；需要页面操作请把采集模式改成「混合」或「纯 UI」", n)
	}
	steps := c.task.HTTPStepList()
	if len(steps) == 0 {
		return errors.New("未配置接口步骤，请先在任务中编辑接口")
	}
	pgDefault := c.task.PaginationCfg()
	applied := false
	for i := range steps {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		step := steps[i]
		if step.DelayMs > 0 {
			if err := c.sleep(time.Duration(step.DelayMs) * time.Millisecond); err != nil {
				return err
			}
		}
		pg := pgDefault
		if step.Pagination != nil {
			pg = *step.Pagination
		}
		batch := SinkBatch{
			Source: stepSourceKey(i), Label: stepName(&step),
			Sink: step.Sink, Mapping: step.Mapping,
		}
		usePg := pg.Enabled && (step.Paginate || (!applied && i == len(steps)-1))
		if usePg {
			if err := c.runPaginated(&step, pg, batch); err != nil {
				return err
			}
			applied = true
			continue
		}
		res, err := c.doHTTP(&step, "")
		if err != nil {
			return err
		}
		if step.Capture {
			recs := extractRecords(res.JSON, step.RecordsPath, pg.RecordsPath)
			if len(recs) == 0 {
				c.e.log.Warn("engine", "步骤 %s 未解析到数据，请检查记录路径", step.Name)
			}
			if err := c.pipeline.Add(batch, recs); err != nil {
				return err
			}
			if c.run.Pages == 0 {
				c.run.Pages = 1
			}
		}
	}
	return nil
}

// stepSourceKey 接口步骤的稳定数据来源标识（运行详情按此分组预览）
func stepSourceKey(i int) string {
	return fmt.Sprintf("step:%d", i+1)
}

func stepName(s *model.HTTPStep) string {
	if strings.TrimSpace(s.Name) != "" {
		return s.Name
	}
	return s.Method + " " + shortURL(s.URL)
}

func shortURL(u string) string {
	if len(u) > 80 {
		return u[:80] + "..."
	}
	return u
}

func (c *runCtx) doHTTP(step *model.HTTPStep, name string) (*HTTPResult, error) {
	if name == "" {
		name = stepName(step)
	}
	urlStr := c.vars.Render(step.URL)
	if !strings.HasPrefix(urlStr, "http") {
		return nil, fmt.Errorf("步骤 %s 的 URL 不合法: %s", name, urlStr)
	}
	headers := make([]model.KV, 0, len(step.Headers))
	for _, h := range step.Headers {
		if strings.TrimSpace(h.Key) == "" {
			continue
		}
		headers = append(headers, model.KV{Key: h.Key, Value: c.vars.Render(h.Value)})
	}
	body := c.vars.RenderBody(step.Body)
	start := time.Now()
	s := &model.StepLog{
		RunID: c.run.ID, Seq: c.nextSeq(), Name: name, Kind: "http", Status: "running",
		Method: strings.ToUpper(step.Method), URL: urlStr, ReqHeaders: mustJSON(headers),
		ReqBody: c.truncate(body), CreatedAt: start,
	}
	res, err := c.exec.DoContext(c.ctx, step.Method, urlStr, headers, body, step.ContentType)
	s.DurationMs = time.Since(start).Milliseconds()
	if res != nil {
		s.RespStatus = res.Status
		s.RespHeaders = mustJSON(res.Headers)
		s.RespBody = c.truncate(res.Text)
	}
	if err != nil {
		if c.ctx.Err() != nil {
			s.Status = "stopped"
			s.Error = "用户手动结束"
		} else {
			s.Status = "failed"
			s.Error = err.Error()
		}
		c.saveStep(s)
		return res, err
	}
	s.Status = "success"
	c.saveStep(s)
	for _, rule := range step.Extract {
		c.applyExtract(rule, res)
	}
	if !c.vars.Has("token") {
		autoToken(res.JSON, c.vars)
	}
	return res, nil
}

func (c *runCtx) applyExtract(rule model.ExtractRule, res *HTTPResult) {
	name := strings.TrimSpace(rule.Var)
	if name == "" {
		return
	}
	val := ""
	switch {
	case rule.Header != "":
		for k, v := range res.Headers {
			if strings.EqualFold(k, rule.Header) {
				val = v
				break
			}
		}
	case rule.Regex != "":
		re, err := regexp.Compile(rule.Regex)
		if err == nil {
			m := re.FindStringSubmatch(res.Text)
			if len(m) > 1 {
				val = m[1]
			} else if len(m) == 1 {
				val = m[0]
			}
		}
	default:
		if res.JSON != nil {
			if v, ok := jsonx.Lookup(res.JSON, rule.Path); ok {
				val = jsonx.Stringify(v)
			}
		}
	}
	if val == "" && rule.Path != "" {
		if v, ok := jsonx.Lookup(res.JSON, rule.Path); ok {
			val = jsonx.Stringify(v)
		}
	}
	if val == "" {
		c.e.log.Warn("engine", "变量 %s 提取失败(path=%s)", name, rule.Path)
		return
	}
	if rule.Prefix != "" {
		val = rule.Prefix + val
	}
	c.vars.Set(name, val)
	c.e.log.Info("engine", "提取变量 %s = %s", name, mask(val))
}

func mask(s string) string {
	if len(s) > 24 {
		return s[:10] + "..." + s[len(s)-6:]
	}
	return s
}

func autoToken(v any, vars *Vars) {
	if v == nil {
		return
	}
	var found string
	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		if depth > 6 || found != "" {
			return
		}
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if s, ok := val.(string); ok {
					lk := strings.ToLower(k)
					if len(s) >= 12 && (strings.Contains(lk, "token") || strings.Contains(lk, "jwt") || lk == "authorization") {
						found = s
						return
					}
				}
				walk(val, depth+1)
			}
		case []any:
			for _, val := range x {
				walk(val, depth+1)
			}
		}
	}
	walk(v, 0)
	if found != "" {
		vars.Set("token", found)
	}
}

func extractRecords(v any, paths ...string) []map[string]any {
	if v == nil {
		return nil
	}
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if got, ok := jsonx.Lookup(v, p); ok {
			if recs := toRecords(got); len(recs) > 0 {
				return recs
			}
		}
	}
	if recs := toRecords(v); len(recs) > 0 {
		return recs
	}
	for _, hit := range jsonx.Arrays(v) {
		if got, ok := jsonx.Lookup(v, hit.Path); ok {
			if recs := toRecords(got); len(recs) > 0 {
				return recs
			}
		}
	}
	return nil
}

func toRecords(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func readTotal(v any, path string) int64 {
	if path == "" {
		return -1
	}
	got, ok := jsonx.Lookup(v, path)
	if !ok {
		return -1
	}
	switch x := got.(type) {
	case float64:
		return int64(x)
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
			return n
		}
	}
	return -1
}

func (c *runCtx) runPaginated(step *model.HTTPStep, pg model.Pagination, batch SinkBatch) error {
	if pg.Type == "" {
		pg.Type = "page"
	}
	if pg.StartPage <= 0 {
		pg.StartPage = 1
	}
	if pg.PageSize <= 0 {
		pg.PageSize = 20
	}
	if pg.MaxPages <= 0 {
		pg.MaxPages = 50
	}
	page := pg.StartPage
	total := int64(-1)
	fetched := 0
	cursor := ""
	for i := 0; i < pg.MaxPages; i++ {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		c.vars.Set("page", strconv.Itoa(page))
		c.vars.Set("pageSize", strconv.Itoa(pg.PageSize))
		c.vars.Set("offset", strconv.Itoa((page-pg.StartPage)*pg.PageSize))
		c.vars.Set("cursor", cursor)
		st := preparePaged(step, pg)
		name := fmt.Sprintf("%s 第%d页", stepName(step), i+1)
		res, err := c.doHTTP(st, name)
		if err != nil {
			return err
		}
		recs := extractRecords(res.JSON, st.RecordsPath, pg.RecordsPath)
		if total < 0 {
			total = readTotal(res.JSON, firstNonEmpty(st.TotalPath, pg.TotalPath))
		}
		if len(recs) == 0 {
			c.e.log.Info("engine", "第 %d 页无数据，结束分页", i+1)
			break
		}
		if err := c.pipeline.Add(batch, recs); err != nil {
			return err
		}
		fetched += len(recs)
		c.run.Pages++
		c.e.log.Info("engine", "第 %d 页抓取 %d 条", i+1, len(recs))
		if pg.Type == "cursor" {
			cursor = jsonx.LookupString(res.JSON, pg.CursorPath)
			if cursor == "" {
				break
			}
		} else {
			page++
		}
		if total >= 0 && int64(fetched) >= total {
			break
		}
		if pg.StopWhenEmpty && total < 0 && len(recs) < pg.PageSize {
			break
		}
		if pg.DelayMs > 0 {
			if err := c.sleep(time.Duration(pg.DelayMs) * time.Millisecond); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *runCtx) sleep(d time.Duration) error {
	if d <= 0 {
		return c.ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-timer.C:
		return nil
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func preparePaged(step *model.HTTPStep, pg model.Pagination) *model.HTTPStep {
	st := *step
	st.Headers = append([]model.KV(nil), step.Headers...)
	st.Extract = append([]model.ExtractRule(nil), step.Extract...)
	if pg.Type == "cursor" {
		if pg.CursorParam != "" {
			if strings.TrimSpace(st.Body) != "" && json.Valid([]byte(st.Body)) {
				st.Body = injectBodyParam(st.Body, pg.CursorParam, "{{cursor}}")
			} else {
				st.URL = injectQueryParam(st.URL, pg.CursorParam, "{{cursor}}")
			}
		}
		return &st
	}
	param := pg.PageParam
	if param == "" {
		param = "page"
	}
	if pg.PageInBody {
		st.Body = injectBodyParam(st.Body, param, "{{page}}")
		if pg.SizeParam != "" {
			st.Body = injectBodyParam(st.Body, pg.SizeParam, "{{pageSize}}")
		}
	} else {
		st.URL = injectQueryParam(st.URL, param, "{{page}}")
		if pg.SizeParam != "" {
			st.URL = injectQueryParam(st.URL, pg.SizeParam, "{{pageSize}}")
		}
	}
	return &st
}

func injectQueryParam(raw, key, val string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	// 已存在的分页参数（如录制时的 page=1）直接改写为占位符
	q.Set(key, val)
	// 允许占位符 {{var}} 出现在 URL 中（避免被编码为 %7B%7B）
	u.RawQuery = strings.NewReplacer("%7B%7B", "{{", "%7D%7D", "}}").Replace(q.Encode())
	return u.String()
}

func injectBodyParam(body, key, val string) string {
	if strings.TrimSpace(body) == "" {
		return body
	}
	var m map[string]any
	if json.Unmarshal([]byte(body), &m) != nil {
		return body
	}
	// 已存在的字段同样改写为占位符
	m[key] = val
	b, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return string(b)
}

// ---------- UI 采集 ----------

func (c *runCtx) runUI() error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	steps := c.task.UIStepList()
	if len(steps) == 0 {
		return errors.New("未配置 UI 操作步骤")
	}
	if n := len(c.task.LoginStepList()); n > 0 {
		c.e.log.Warn("engine", "当前是「纯 UI」模式，只执行 UI 步骤：已忽略 %d 条登录步骤；登录动作请直接放进 UI 步骤，或改用「混合」模式", n)
	}
	// 开跑前先检查一遍步骤参数，缺必填项直接给出「第几步、缺什么」，
	// 避免跑到一半才因为空参数失败，让人误以为「步骤没执行」。
	if err := validateUISteps("UI 步骤", steps); err != nil {
		return err
	}
	rules := c.task.CaptureRules()
	needNet := false
	for _, r := range rules {
		if r.Mode != "dom" {
			needNet = true
		}
	}
	b, cleanup, err := c.e.bm.LaunchTask(c.e.cfg.Browser.Headless, true, c.task.SessionMode)
	if err != nil {
		return err
	}
	defer cleanup()
	page, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		return err
	}
	c.e.bm.ApplyFingerprint(page)
	rep := browser.NewReplayer(page, c.e.cfg.MaxBodyKB, c.e.log).WithContext(c.ctx)
	rep.OnStep = c.uiStepLogger()
	// 记录最后一次「打开页面」的时间点：点击翻页抓取第 1 页时只统计这之后
	// 产生的接口请求，避免把上个页面（如登录后跳转的门店页）的数据混进来。
	var lastNavAt time.Time
	{
		startSeq := c.uiStepStarter()
		rep.OnStepStart = func(i int, s model.UIStep) {
			startSeq(i, s)
			if strings.EqualFold(strings.TrimSpace(s.Action), "open") {
				lastNavAt = time.Now()
			}
		}
	}
	rep.Rec.OnRequest = func(r browser.CapRequest) { c.reserveReqSeq(r.ID) }
	if err := rep.Start(); err != nil {
		return err
	}
	// 回放期间边跑边记录接口日志，保证「UI 操作 → 它触发的接口」按时间交错
	tracker := newCapTracker()
	stopWatch := make(chan struct{})
	var wg sync.WaitGroup
	if needNet {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.watchCaptured(rep, rules, tracker, stopWatch)
		}()
	}
	// 注意：录制器与请求监听要一直保持到所有抓取规则处理完，
	// 因为「点击下一页」翻页时每一页新产生的请求都要继续捕获并写入步骤日志。
	defer func() {
		// 兜底：把还没记录完的请求补写日志
		if needNet {
			c.flushCaptured(rep.Rec.Requests(), rules, tracker, true)
		}
		close(stopWatch)
		wg.Wait()
		rep.Stop()
	}()
	repErr := rep.Run(steps)

	capCfg := c.task.CaptureCfg()
	if capCfg.WaitMs > 0 {
		if err := c.sleep(time.Duration(capCfg.WaitMs) * time.Millisecond); err != nil {
			return err
		}
	}
	if needNet {
		c.waitCaptured(rep, rules, 20*time.Second)
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}

	if len(rules) == 0 {
		return fmt.Errorf("未配置抓取规则，请在「抓取配置」中添加要采集的接口或 DOM 表格")
	}

	// 纯 UI 模式下如果要按接口分页抓取，需要沿用回放得到的登录会话。
	// 这里在页面关闭前采集 Cookie / storage，供后续 HTTP 翻页请求使用。
	var cookies []*http.Cookie
	if needNet {
		harvest, cks, herr := browser.Harvest(page)
		if herr != nil {
			c.e.log.Warn("engine", "读取页面会话失败（分页抓取可能缺少登录态）: %v", herr)
		}
		cookies = cks
		for k, v := range harvest {
			c.vars.Set(k, v)
			if i := strings.Index(k, "."); i > 0 && i < len(k)-1 {
				c.vars.Set(k[i+1:], v)
			}
		}
		if len(cookies) > 0 || len(harvest) > 0 {
			c.e.log.Info("engine", "已采集页面会话：Cookie %d 个，存储项 %d 个", len(cookies), len(harvest))
		}
	}

	var msgs []string
	for i, rule := range rules {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		pg := model.Pagination{}
		hasPg := rule.Pagination != nil && rule.Pagination.Enabled
		if hasPg {
			pg = *rule.Pagination
			pg.Enabled = true
			if strings.TrimSpace(pg.RecordsPath) == "" {
				pg.RecordsPath = rule.RecordsPath
			}
		}
		// 点击翻页：在页面里真实点「下一页」，不走接口翻页
		uiClick := hasPg && strings.EqualFold(strings.TrimSpace(pg.Mode), "ui")

		if rule.Mode == "dom" {
			batch := SinkBatch{
				Source:  fmt.Sprintf("dom:%d", i+1),
				Label:   "DOM · " + rule.Name,
				Mapping: mappingOr(rule.Mapping, c.task.MappingList()),
				Sink:    rule.Sink,
			}
			if uiClick {
				pages, rows, perr := c.uiPaginate(rep, page, rule, pg, batch, lastNavAt)
				if perr != nil {
					return fmt.Errorf("抓取规则「%s」点击翻页失败: %w", rule.Name, perr)
				}
				if pages == 0 {
					msgs = append(msgs, rule.Name+" 未取到 DOM 数据")
					continue
				}
				msgs = append(msgs, fmt.Sprintf("%s 点击翻页 %d 页 / %d 行", rule.Name, pages, rows))
				continue
			}
			rows, derr := browser.ExtractDOM(page, rule, mappingOr(rule.Mapping, c.task.MappingList()))
			if derr != nil {
				return fmt.Errorf("DOM 数据提取失败（%s）: %w", rule.Name, derr)
			}
			batch.Mapping = rule.Mapping
			if err := c.pipeline.Add(batch, rows); err != nil {
				return err
			}
			msgs = append(msgs, fmt.Sprintf("%s %d 行", rule.Name, len(rows)))
			continue
		}
		reqs := matchRuleRequests(rep.Rec.Requests(), rule)

		// 规则开启分页
		if hasPg {
			batch := SinkBatch{
				Source:  fmt.Sprintf("cap:%d", i+1),
				Label:   "接口 · " + rule.Name,
				Mapping: mappingOr(rule.Mapping, c.task.MappingList()),
				Sink:    rule.Sink,
			}
			if uiClick {
				pages, total, perr := c.uiPaginate(rep, page, rule, pg, batch, lastNavAt)
				if perr != nil {
					return fmt.Errorf("抓取规则「%s」点击翻页失败: %w", rule.Name, perr)
				}
				if pages == 0 {
					c.e.log.Warn("engine", "抓取规则「%s」未捕获到目标接口（%s），无法点击翻页，请检查配置或页面是否加载完成", rule.Name, rule.URLContains)
					msgs = append(msgs, rule.Name+" 未捕获到接口")
					continue
				}
				msgs = append(msgs, fmt.Sprintf("%s 点击翻页 %d 页 / %d 条", rule.Name, pages, total))
				continue
			}
			// 接口翻页：用回放捕获到的接口作为模板，带 Cookie 逐页请求
			if len(reqs) == 0 {
				c.e.log.Warn("engine", "抓取规则「%s」未捕获到目标接口（%s），无法分页抓取，请检查配置或页面是否加载完成", rule.Name, rule.URLContains)
				msgs = append(msgs, rule.Name+" 未捕获到接口")
				continue
			}
			tmpl := stepFromCapture(reqs[0], rule)
			if len(cookies) > 0 {
				AddCookiesToJar(c.exec.Jar(), cookies, tmpl.URL)
			}
			before := c.run.Pages
			if err := c.runPaginated(tmpl, pg, batch); err != nil {
				return fmt.Errorf("抓取规则「%s」分页抓取失败: %w", rule.Name, err)
			}
			msgs = append(msgs, fmt.Sprintf("%s 分页 %d 页", rule.Name, c.run.Pages-before))
			continue
		}

		if len(reqs) == 0 {
			c.e.log.Warn("engine", "抓取规则「%s」未捕获到目标接口（%s），请检查配置或页面是否加载完成", rule.Name, rule.URLContains)
			msgs = append(msgs, rule.Name+" 未捕获到接口")
			continue
		}
		total := 0
		for _, r := range reqs {
			var parsed any
			if json.Unmarshal([]byte(r.RespBody), &parsed) != nil {
				continue
			}
			recs := extractRecords(parsed, rule.RecordsPath)
			if len(recs) == 0 {
				continue
			}
			batch := SinkBatch{
				Source:  fmt.Sprintf("cap:%d", i+1),
				Label:   "接口 · " + rule.Name,
				Mapping: rule.Mapping,
				Sink:    rule.Sink,
			}
			if err := c.pipeline.Add(batch, recs); err != nil {
				return err
			}
			total += len(recs)
			c.run.Pages++
		}
		msgs = append(msgs, fmt.Sprintf("%s 接口 %d 个 / %d 条", rule.Name, len(reqs), total))
	}
	c.run.Message = strings.Join(msgs, "；")
	if len(msgs) > 0 && allMissed(msgs) {
		return fmt.Errorf("未捕获到任何目标接口，请检查抓取配置或页面是否加载完成")
	}
	if repErr != nil {
		return repErr
	}
	return nil
}

// uiPaginate 纯 UI 模式下的「点击翻页」抓取：
// 第 1 页用回放阶段已经加载的数据，之后每轮真实点击「下一页」按钮，
// 等待页面自己把下一页数据加载出来，再把这一页新产生的接口响应（或 DOM 表格）
// 按规则解析、去重、落库。整个过程都是在浏览器里发生的真实交互，
// 不构造额外的接口请求，风控层面最接近真人操作。
func (c *runCtx) uiPaginate(rep *browser.Replayer, page *rod.Page, rule model.CaptureConfig, pg model.Pagination, batch SinkBatch, since time.Time) (int, int, error) {
	maxPages := pg.MaxPages
	if maxPages <= 0 {
		maxPages = 100
	}
	waitMs := pg.WaitMs
	if waitMs <= 0 {
		waitMs = 3000
	}
	recordsPath := pg.RecordsPath
	if strings.TrimSpace(recordsPath) == "" {
		recordsPath = rule.RecordsPath
	}
	isDOM := rule.Mode == "dom"
	// 「下一页」按钮 / 表格所在 iframe：翻页配置优先，其次用规则自己的 iframe
	frameSel := strings.TrimSpace(pg.Frame)
	if frameSel == "" && isDOM {
		frameSel = strings.TrimSpace(rule.Frame)
	}
	// 老任务没填「表格所在 iframe」时，自动识别一次：主页面找不到表格行、
	// iframe 里有，就把 frameSel 固定下来，后续翻页点击 / DOM 指纹 / 取数都用它。
	if isDOM && frameSel == "" && strings.TrimSpace(rule.RowSelector) != "" &&
		!browser.PageHasSelector(page, rule.RowSelector, 900*time.Millisecond) {
		if fsel, e := browser.DetectFrameForSelector(page, rule.RowSelector, 1200*time.Millisecond); e == nil && fsel != "" {
			frameSel = fsel
			c.e.log.Info("engine", "抓取规则「%s」的表格在 iframe %s 内，已自动识别", rule.Name, fsel)
		}
	}

	collectDOM := func() (int, error) {
		r := rule
		if strings.TrimSpace(r.Frame) == "" && frameSel != "" {
			r.Frame = frameSel
		}
		rows, err := browser.ExtractDOM(page, r, mappingOr(rule.Mapping, c.task.MappingList()))
		if err != nil {
			return 0, err
		}
		if len(rows) == 0 {
			return 0, nil
		}
		if err := c.pipeline.Add(batch, rows); err != nil {
			return 0, err
		}
		return len(rows), nil
	}
	collectNet := func(reqs []browser.CapRequest) (int, error) {
		total := 0
		for _, r := range reqs {
			if strings.TrimSpace(r.RespBody) == "" || !capReady(r) {
				continue
			}
			var parsed any
			if json.Unmarshal([]byte(r.RespBody), &parsed) != nil {
				continue
			}
			recs := extractRecords(parsed, recordsPath)
			if len(recs) == 0 {
				continue
			}
			if err := c.pipeline.Add(batch, recs); err != nil {
				return total, err
			}
			total += len(recs)
		}
		return total, nil
	}
	// domSig 取当前页面表格内容指纹，用来判断「下一页」是否真的换了数据
	domSig := func() string {
		lock := browser.EvalLock(page)
		lock.Lock()
		defer lock.Unlock()
		dp, err := browser.PageForFrame(page, frameSel, 10*time.Second)
		if err != nil {
			return ""
		}
		res, err := dp.Eval(`(sel) => {
			try {
				const els = sel ? Array.from(document.querySelectorAll(sel)) : [];
				if (els.length) {
					return els.length + '|' + els.slice(0, 3).map(e => (e.innerText || '').trim()).join('#');
				}
			} catch (e) {}
			return (document.body.innerText || '').replace(/\s+/g, ' ').slice(0, 240);
		}`, rule.RowSelector)
		if err != nil || res == nil {
			return ""
		}
		return res.Value.Str()
	}

	// ---- 第 1 页：回放阶段自然加载的数据 ----
	pages, rows := 0, 0
	if isDOM {
		n, err := collectDOM()
		if err != nil {
			return 0, 0, err
		}
		rows = n
	} else {
		n, err := collectNet(firstPageNet(rep.Rec.Requests(), rule, since))
		if err != nil {
			return 0, 0, err
		}
		rows = n
		if n == 0 {
			// 第 1 页就没数据，说明规则没匹配上，直接返回 0 让上层提示
			return 0, 0, nil
		}
	}
	pages = 1
	c.run.Pages++

	// ---- 逐页点击「下一页」----
	emptyStreak := 0
	for pages < maxPages {
		if err := c.ctx.Err(); err != nil {
			return pages, rows, err
		}
		if pg.DelayMs > 0 {
			if err := c.sleep(time.Duration(pg.DelayMs) * time.Millisecond); err != nil {
				return pages, rows, err
			}
		}
		beforeReqs := len(rep.Rec.Requests())
		sigBefore := ""
		if isDOM {
			sigBefore = domSig()
		}
		clicked, cerr := rep.ClickNext(pg.NextSelector, pg.NextText, frameSel)
		if cerr != nil {
			return pages, rows, cerr
		}
		if !clicked {
			c.e.log.Info("engine", "抓取规则「%s」翻页结束：下一页按钮不可用（已抓 %d 页）", rule.Name, pages)
			break
		}
		start := time.Now()
		deadline := time.Now().Add(time.Duration(waitMs) * time.Millisecond)
		var fresh []browser.CapRequest
		for {
			if err := c.sleep(200 * time.Millisecond); err != nil {
				return pages, rows, err
			}
			if isDOM {
				if domSig() != sigBefore {
					break
				}
			} else if all := rep.Rec.Requests(); len(all) > beforeReqs {
				cand := matchRuleRequests(all[beforeReqs:], rule)
				ready := 0
				for _, r := range cand {
					if capReady(r) && strings.TrimSpace(r.RespBody) != "" {
						ready++
					}
				}
				if ready > 0 {
					// 同一页可能有多个接口，稍等让它们到齐
					if err := c.sleep(400 * time.Millisecond); err != nil {
						return pages, rows, err
					}
					break
				}
			}
			if time.Now().After(deadline) {
				break
			}
		}
		if !isDOM {
			all := rep.Rec.Requests()
			if len(all) > beforeReqs {
				fresh = matchRuleRequests(all[beforeReqs:], rule)
			}
		}

		// 点击「下一页」后页面完全没有反应（没有任何新请求、DOM 也没变化）：
		// 一般是按钮虽然还能点但实际已到末页，或者点击没落到按钮上。
		// 这种情况不算一页，也不再重复采集当前页数据，直接结束。
		if isDOM {
			if sigBefore != "" && domSig() == sigBefore {
				c.e.log.Info("engine", "抓取规则「%s」翻页结束：点击「下一页」后页面没有变化（已抓 %d 页）", rule.Name, pages)
				break
			}
		} else if len(rep.Rec.Requests()) == beforeReqs {
			c.e.log.Info("engine", "抓取规则「%s」翻页结束：点击「下一页」后没有新的接口请求（已抓 %d 页）", rule.Name, pages)
			break
		}

		n := 0
		var derr error
		if isDOM {
			n, derr = collectDOM()
		} else {
			n, derr = collectNet(fresh)
		}
		if derr != nil {
			return pages, rows, derr
		}
		pages++
		rows += n
		c.run.Pages++
		status := "success"
		if n == 0 {
			status = "warn"
		}
		c.saveStep(&model.StepLog{
			RunID: c.run.ID, Seq: c.nextSeq(),
			Name:       fmt.Sprintf("UI 翻页 第 %d 页（%d 条）", pages, n),
			Kind:       "ui",
			Status:     status,
			URL:        strings.TrimSpace(pg.NextSelector + " " + pg.NextText),
			DurationMs: time.Since(start).Milliseconds(),
			CreatedAt:  start,
		})
		if n == 0 {
			emptyStreak++
			// 勾了「空页自动停止」立即停；没勾也最多再点一次，
			// 避免 SPA 里按钮一直可点但内容不变时白点到最大页数。
			if pg.StopWhenEmpty || emptyStreak >= 2 {
				c.e.log.Info("engine", "抓取规则「%s」翻页结束：第 %d 页没有新数据", rule.Name, pages)
				break
			}
		} else {
			emptyStreak = 0
		}
	}
	return pages, rows, nil
}

func mappingOr(m []model.FieldMapping, def []model.FieldMapping) []model.FieldMapping {
	if len(m) > 0 {
		return m
	}
	return def
}

// uiActionName 把动作 key 转成界面里的中文名，报错时更好定位
func uiActionName(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open":
		return "打开页面"
	case "click":
		return "点击"
	case "input":
		return "输入"
	case "select":
		return "下拉选择"
	case "press":
		return "按键"
	case "wait":
		return "等待"
	case "wait_for":
		return "等待元素"
	case "scroll":
		return "滚动"
	case "eval":
		return "执行 JS"
	}
	return strings.TrimSpace(action)
}

// uiStepIssue 检查一个页面动作是否缺必填参数，返回可读提示（空表示没问题）
func uiStepIssue(s model.UIStep) string {
	switch strings.ToLower(strings.TrimSpace(s.Action)) {
	case "open":
		if strings.TrimSpace(s.Value) == "" && strings.TrimSpace(s.Selector) == "" {
			return "缺少网址（请在「值 / 网址」里填要打开的 URL）"
		}
	case "click":
		if strings.TrimSpace(s.Selector) == "" && strings.TrimSpace(s.Text) == "" && s.X <= 0 && s.Y <= 0 {
			return "缺少选择器（或按钮文本 / 坐标）"
		}
	case "input", "select":
		if strings.TrimSpace(s.Selector) == "" {
			return "缺少选择器（要操作哪个输入框 / 下拉框）"
		}
	case "wait_for":
		if strings.TrimSpace(s.Selector) == "" {
			return "缺少选择器（要等哪个元素出现）"
		}
	case "press":
		if strings.TrimSpace(s.Key) == "" {
			return "缺少按键名（如 Enter / Tab）"
		}
	case "eval":
		if strings.TrimSpace(s.Value) == "" {
			return "缺少 JS 代码（填在「值 / 网址 / JS」里）"
		}
	case "":
		return "没有选择动作类型"
	}
	return ""
}

// validateUISteps 在开跑前校验整组页面动作，报错信息带序号与动作名
func validateUISteps(label string, steps []model.UIStep) error {
	for i, s := range steps {
		if issue := uiStepIssue(s); issue != "" {
			return fmt.Errorf("%s 第 %d 步（%s）：%s", label, i+1, uiActionName(s.Action), issue)
		}
	}
	return nil
}

// stepFromCapture 把浏览器里捕获到的一次请求转换成可复用的接口步骤，
// 纯 UI 模式翻页抓取时以它为模板（保留原始请求头 / 方法 / 请求体）。
func stepFromCapture(r browser.CapRequest, rule model.CaptureConfig) *model.HTTPStep {
	headers := make([]model.KV, 0, len(r.ReqHeaders))
	contentType := ""
	for k, v := range r.ReqHeaders {
		lk := strings.ToLower(strings.TrimSpace(k))
		switch lk {
		case "", "host", "content-length", "connection", "cookie", "accept-encoding":
			continue
		}
		if strings.HasPrefix(lk, ":") {
			continue
		}
		if lk == "content-type" {
			contentType = v
		}
		headers = append(headers, model.KV{Key: k, Value: v})
	}
	sort.Slice(headers, func(i, j int) bool {
		return strings.ToLower(headers[i].Key) < strings.ToLower(headers[j].Key)
	})
	name := "分页抓取 · " + rule.Name
	if strings.TrimSpace(rule.Name) == "" {
		name = "分页抓取"
	}
	return &model.HTTPStep{
		Name:        name,
		Method:      r.Method,
		URL:         r.URL,
		Headers:     headers,
		Body:        r.ReqBody,
		ContentType: contentType,
		RecordsPath: rule.RecordsPath,
	}
}

func allMissed(msgs []string) bool {
	for _, m := range msgs {
		if !strings.Contains(m, "未捕获到") {
			return false
		}
	}
	return len(msgs) > 0
}

func (c *runCtx) uiStepLogger() func(i int, s model.UIStep, d time.Duration, err error) {
	return func(i int, s model.UIStep, d time.Duration, err error) {
		name := fmt.Sprintf("UI-%d %s", i, uiStepLabel(s))
		seq, ok := c.takeStepSeq(i)
		if !ok {
			seq = c.nextSeq()
		}
		st := &model.StepLog{
			RunID: c.run.ID, Seq: seq, Name: name, Kind: "ui", Status: "success",
			DurationMs: d.Milliseconds(), CreatedAt: time.Now().Add(-d),
		}
		if s.Action == "open" {
			st.URL = s.Value
		} else {
			st.URL = s.Selector
		}
		if err != nil {
			if c.ctx.Err() != nil {
				st.Status = "stopped"
				st.Error = "用户手动结束"
			} else {
				st.Status = "failed"
				st.Error = err.Error()
			}
		}
		c.saveStep(st)
	}
}

// uiStepStarter 在 UI 步骤开始执行时预分配日志序号
func (c *runCtx) uiStepStarter() func(i int, s model.UIStep) {
	return func(i int, s model.UIStep) {
		c.reserveStepSeq(i)
	}
}

// uiStepLabel 生成步骤日志里显示的短描述（优先用录制时记录的 desc）
func uiStepLabel(s model.UIStep) string {
	desc := s.Desc
	if desc == "" {
		desc = s.Action + " " + s.Selector + s.Value
	}
	if len(desc) > 40 {
		desc = desc[:40]
	}
	return desc
}

// capTracker 记录已写入日志的请求，避免重复
type capTracker struct {
	mu   sync.Mutex
	seen map[string]bool
	n    int
}

func newCapTracker() *capTracker { return &capTracker{seen: map[string]bool{}} }

// take 首次遇到该请求时返回 true
func (t *capTracker) take(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen[id] {
		return false
	}
	t.seen[id] = true
	t.n++
	return true
}

func (t *capTracker) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.n
}

// watchCaptured 回放期间持续把已完成的接口请求写入步骤日志
func (c *runCtx) watchCaptured(rep *browser.Replayer, rules []model.CaptureConfig, tr *capTracker, stop <-chan struct{}) {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-c.ctx.Done():
			return
		case <-t.C:
			c.flushCaptured(rep.Rec.Requests(), rules, tr, false)
		}
	}
}

// flushCaptured 写入尚未记录的请求日志；final=false 时只写已完成响应
func (c *runCtx) flushCaptured(reqs []browser.CapRequest, rules []model.CaptureConfig, tr *capTracker, final bool) {
	for _, r := range reqs {
		if tr.count() >= 60 {
			return
		}
		isTarget := false
		for _, rule := range rules {
			if rule.Mode != "dom" && matchRule(r, rule) {
				isTarget = true
				break
			}
		}
		jsonReq := strings.Contains(strings.ToLower(r.MimeType), "json") || r.IsJSON
		if !isTarget && (!jsonReq || isStaticAsset(r.URL)) {
			continue
		}
		if !final && !capReady(r) {
			continue
		}
		if !tr.take(r.ID) {
			continue
		}
		seq, reserved := c.takeReqSeq(r.ID)
		if !reserved {
			seq = c.nextSeq()
		}
		createdAt := r.StartedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		st := &model.StepLog{
			RunID: c.run.ID, Seq: seq, Name: "捕获 " + r.Method + " " + shortURL(r.URL),
			Kind: "http", Status: "success", Method: r.Method, URL: r.URL,
			ReqHeaders: mustJSON(r.ReqHeaders), ReqBody: c.truncate(r.ReqBody),
			RespStatus: r.Status, RespHeaders: mustJSON(r.RespHeaders), RespBody: c.truncate(r.RespBody),
			DurationMs: r.DurationMs, CreatedAt: createdAt,
		}
		if r.Failed || r.Status >= 400 {
			st.Status = "failed"
			st.Error = r.Error
		}
		c.saveStep(st)
	}
}

// isStaticAsset 判断是否为静态资源（避免把 favicon / js / css 之类的噪音写进运行日志）
func isStaticAsset(rawURL string) bool {
	p := strings.ToLower(rawURL)
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if i := strings.IndexByte(p, '#'); i >= 0 {
		p = p[:i]
	}
	for _, ext := range []string{
		".css", ".js", ".mjs", ".map", ".png", ".jpg", ".jpeg", ".gif", ".svg",
		".ico", ".webp", ".woff", ".woff2", ".ttf", ".eot", ".mp4", ".webm",
	} {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

// capReady 响应是否已经可以落日志（响应体已取回或已等待足够时间）
func capReady(r browser.CapRequest) bool {
	if r.Failed || r.Status >= 400 {
		return true
	}
	if r.Status == 0 {
		return false
	}
	if r.RespBody != "" {
		return true
	}
	return !r.StartedAt.IsZero() && time.Since(r.StartedAt) > 1200*time.Millisecond
}

// matchRule 请求是否命中某条抓取规则
func matchRule(r browser.CapRequest, rule model.CaptureConfig) bool {
	if rule.Method != "" && !strings.EqualFold(rule.Method, r.Method) {
		return false
	}
	if rule.URLContains != "" && !strings.Contains(r.URL, rule.URLContains) {
		return false
	}
	return true
}

// matchRuleRequests 命中规则且响应正常的 JSON 请求
func matchRuleRequests(reqs []browser.CapRequest, rule model.CaptureConfig) []browser.CapRequest {
	var out []browser.CapRequest
	seen := map[string]bool{}
	for _, r := range reqs {
		if !r.IsJSON || r.Status < 200 || r.Status >= 400 {
			continue
		}
		if !matchRule(r, rule) {
			continue
		}
		if seen[r.URL] && rule.URLContains == "" {
			continue
		}
		seen[r.URL] = true
		out = append(out, r)
	}
	return out
}

// firstPageNet 取「当前这一屏」的匹配请求。
// 优先用最后一次「打开页面」的时间点做边界；没有可用边界时退化为
// 「以最后一条匹配请求为锚点的 5 秒窗口」，避免把上一页面一次性加载的
// 数据错误并入点击翻页的第 1 页。
func firstPageNet(reqs []browser.CapRequest, rule model.CaptureConfig, since time.Time) []browser.CapRequest {
	m := matchRuleRequests(reqs, rule)
	if len(m) == 0 {
		return m
	}
	if !since.IsZero() {
		out := make([]browser.CapRequest, 0, len(m))
		for _, r := range m {
			if !r.StartedAt.Before(since) {
				out = append(out, r)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if len(m) <= 1 {
		return m
	}
	anchor := m[len(m)-1].StartedAt
	if anchor.IsZero() {
		return m
	}
	const window = 5 * time.Second
	out := make([]browser.CapRequest, 0, len(m))
	for _, r := range m {
		if r.StartedAt.IsZero() || anchor.Sub(r.StartedAt) <= window {
			out = append(out, r)
		}
	}
	return out
}

func (c *runCtx) waitCaptured(rep *browser.Replayer, rules []model.CaptureConfig, max time.Duration) {
	total := 0
	for _, r := range rules {
		if r.Mode != "dom" {
			total++
		}
	}
	if total == 0 {
		return
	}
	deadline := time.Now().Add(max)
	var firstHit time.Time
	// 已有一条规则命中后再等一会，给其它接口（多接口采集）留出加载时间
	const grace = 6 * time.Second
	for time.Now().Before(deadline) {
		if err := c.ctx.Err(); err != nil {
			return
		}
		reqs := rep.Rec.Requests()
		matched := 0
		for _, rule := range rules {
			if rule.Mode == "dom" {
				continue
			}
			if len(matchRuleRequests(reqs, rule)) > 0 {
				matched++
			}
		}
		if matched >= total {
			_ = c.sleep(900 * time.Millisecond) // 等待响应体写完
			return
		}
		if matched > 0 && firstHit.IsZero() {
			firstHit = time.Now()
		}
		if !firstHit.IsZero() && time.Since(firstHit) > grace {
			_ = c.sleep(400 * time.Millisecond)
			return
		}
		if err := c.sleep(300 * time.Millisecond); err != nil {
			return
		}
	}
}

// ---------- UI + 接口 ----------

func (c *runCtx) runHybrid() error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	loginSteps := c.task.LoginStepList()
	if len(loginSteps) == 0 {
		return errors.New("混合模式需要先录制登录 UI 步骤")
	}
	if n := len(c.task.UIStepList()); n > 0 {
		c.e.log.Warn("engine", "当前是「UI 登录 + 接口」模式，只执行登录步骤：已忽略 %d 条 UI 步骤；需要页面动作请加到「登录步骤」，或把采集模式改成「纯 UI」", n)
	}
	if err := validateUISteps("登录步骤", loginSteps); err != nil {
		return err
	}
	b, cleanup, err := c.e.bm.LaunchTask(c.e.cfg.Browser.Headless, true, c.task.SessionMode)
	if err != nil {
		return err
	}
	page, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		cleanup()
		return err
	}
	c.e.bm.ApplyFingerprint(page)
	rep := browser.NewReplayer(page, c.e.cfg.MaxBodyKB, c.e.log).WithContext(c.ctx)
	rep.OnStep = c.uiStepLogger()
	rep.OnStepStart = c.uiStepStarter()
	rep.Rec.OnRequest = func(r browser.CapRequest) { c.reserveReqSeq(r.ID) }
	if err := rep.Start(); err != nil {
		cleanup()
		return err
	}
	tracker := newCapTracker()
	stopWatch := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.watchCaptured(rep, nil, tracker, stopWatch)
	}()
	loginErr := rep.Run(loginSteps)
	if c.ctx.Err() == nil {
		_ = c.sleep(1500 * time.Millisecond)
	}
	harvest, cookies, _ := browser.Harvest(page)
	close(stopWatch)
	wg.Wait()
	rep.Stop()
	c.flushCaptured(rep.Rec.Requests(), nil, tracker, true)
	cleanup()
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if loginErr != nil {
		return fmt.Errorf("登录流程失败: %w", loginErr)
	}
	// 会话注入
	base := c.task.CaptureCfg().URLContains
	if base == "" {
		for _, s := range loginSteps {
			if s.Action == "open" && strings.HasPrefix(s.Value, "http") {
				base = s.Value
				break
			}
		}
	}
	if base != "" {
		if !strings.HasPrefix(base, "http") {
			base = "https://" + base
		}
		AddCookiesToJar(c.exec.Jar(), cookies, base)
		if u, err := url.Parse(base); err == nil {
			c.e.log.Info("engine", "已注入 Cookie 到 %s（当前生效 %d 个）: %v", u.Host, len(c.exec.Jar().Cookies(u)), cookieNames(c.exec.Jar().Cookies(u)))
		}
	}
	for k, v := range harvest {
		c.vars.Set(k, v)
		if i := strings.Index(k, "."); i > 0 && i < len(k)-1 {
			c.vars.Set(k[i+1:], v)
		}
	}
	c.e.log.Info("engine", "已获取登录会话：Cookie %d 个，存储项 %d 个", len(cookies), len(harvest))
	return c.runAPI()
}

func AddCookiesToJar(jar interface {
	SetCookies(u *url.URL, cookies []*http.Cookie)
}, cookies []*http.Cookie, base string) {
	u, err := url.Parse(base)
	if err != nil {
		return
	}
	for _, ck := range cookies {
		cu := *u
		if ck.Domain != "" {
			cu.Host = strings.TrimPrefix(ck.Domain, ".")
		}
		if ck.Path != "" {
			cu.Path = ck.Path
		}
		jar.SetCookies(&cu, []*http.Cookie{ck})
	}
}

func cookieNames(cks []*http.Cookie) []string {
	out := make([]string, 0, len(cks))
	for _, ck := range cks {
		out = append(out, ck.Name+"@"+ck.Domain+ck.Path)
	}
	return out
}
