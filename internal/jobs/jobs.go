// Package jobs 执行「定时作业」：按顺序运行 HTTP 请求 / Shell 命令 / SQL 语句。
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"collect/internal/applog"
	"collect/internal/collector"
	"collect/internal/config"
	"collect/internal/model"
	"collect/internal/notify"

	"gorm.io/gorm"
)

// Runner 作业执行器（与采集引擎相互独立）
type Runner struct {
	cfg *config.Config
	db  *gorm.DB
	log *applog.Logger
	nt  *notify.Notifier

	mu      sync.Mutex
	running map[uint]bool
}

func NewRunner(cfg *config.Config, db *gorm.DB, log *applog.Logger, nt *notify.Notifier) *Runner {
	return &Runner{cfg: cfg, db: db, log: log, nt: nt, running: map[uint]bool{}}
}

func (r *Runner) IsRunning(jobID uint) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[jobID]
}

func (r *Runner) acquire(jobID uint) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[jobID] {
		return false
	}
	r.running[jobID] = true
	return true
}

func (r *Runner) release(jobID uint) {
	r.mu.Lock()
	delete(r.running, jobID)
	r.mu.Unlock()
}

// Start 异步执行作业，立即返回执行记录
func (r *Runner) Start(jobID uint, trigger string) (*model.JobRun, error) {
	var job model.Job
	if err := r.db.First(&job, jobID).Error; err != nil {
		return nil, errors.New("作业不存在")
	}
	if !r.acquire(jobID) {
		return nil, errors.New("作业正在执行中，请稍后再试")
	}
	run := model.JobRun{
		JobID: job.ID, JobName: job.Name, Trigger: trigger, Status: "running",
		StartedAt: time.Now(), CreatedAt: time.Now(),
	}
	if err := r.db.Create(&run).Error; err != nil {
		r.release(jobID)
		return nil, err
	}
	r.db.Model(&model.Job{}).Where("id = ?", job.ID).
		Updates(map[string]any{"last_run_at": run.StartedAt, "last_status": "running", "last_message": ""})
	go r.execute(&job, &run)
	return &run, nil
}

func (r *Runner) execute(job *model.Job, run *model.JobRun) {
	defer r.release(job.ID)
	steps := job.StepList()
	rc := newRunContext()
	var errs []string
	run.StepsTotal = 0
	for i := range steps {
		if steps[i].IsEnabled() {
			run.StepsTotal++
		}
	}
	seq := 0
	for i := range steps {
		step := steps[i]
		if !step.IsEnabled() {
			continue
		}
		seq++
		name := strings.TrimSpace(step.Name)
		if name == "" {
			name = fmt.Sprintf("%s-%d", kindName(step.Type), seq)
		}
		lg, err := r.runStep(job, step, seq, name, rc)
		lg.JobRunID = run.ID
		if cerr := r.db.Create(lg).Error; cerr != nil {
			r.log.Error("jobs", "写入作业步骤日志失败: %v", cerr)
		}
		if err != nil {
			run.StepsFail++
			errs = append(errs, name+": "+err.Error())
			r.log.Error("jobs", "作业「%s」步骤 %s 失败: %v", job.Name, name, err)
			if !step.ContinueOnError {
				break
			}
			continue
		}
		run.StepsOK++
	}
	now := time.Now()
	run.FinishedAt = &now
	run.DurationMs = now.Sub(run.StartedAt).Milliseconds()
	switch {
	case run.StepsFail == 0:
		run.Status = "success"
	case run.StepsOK > 0:
		run.Status = "partial"
	default:
		run.Status = "failed"
	}
	if len(errs) > 0 {
		run.Error = strings.Join(errs, "\n")
	}
	run.Message = fmt.Sprintf("步骤 %d / 成功 %d / 失败 %d", run.StepsTotal, run.StepsOK, run.StepsFail)
	if err := r.db.Save(run).Error; err != nil {
		r.log.Error("jobs", "保存作业执行记录失败: %v", err)
	}
	r.db.Model(&model.Job{}).Where("id = ?", job.ID).Updates(map[string]any{
		"last_run_at": now, "last_status": run.Status, "last_message": run.Message,
	})
	if run.Status == "success" {
		r.log.Info("jobs", "作业「%s」执行成功: %s", job.Name, run.Message)
	} else {
		r.log.Error("jobs", "作业「%s」执行%s: %s", job.Name, map[string]string{"failed": "失败", "partial": "部分成功"}[run.Status], run.Message)
	}
	if r.nt != nil {
		r.nt.NotifyJob(job, run)
		if run.Notified {
			r.db.Model(run).Update("notified", true)
		}
	}
}

// runContext 保存一次作业执行期间跨步骤共享的状态：Cookie 会话与提取的变量
type runContext struct {
	mu   sync.Mutex
	vars map[string]string
	jar  http.CookieJar
}

func newRunContext() *runContext {
	jar, _ := cookiejar.New(nil)
	return &runContext{vars: map[string]string{}, jar: jar}
}

func (c *runContext) setVar(k, v string) {
	c.mu.Lock()
	c.vars[k] = v
	c.mu.Unlock()
}

func (c *runContext) render(s string) string {
	s = render(s)
	if !strings.Contains(s, "{{") {
		return s
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
		s = strings.ReplaceAll(s, "{{ "+k+" }}", v)
	}
	return s
}

// runStep 执行单个步骤并写入步骤日志
func (r *Runner) runStep(job *model.Job, step model.JobStep, seq int, name string, rc *runContext) (*model.JobStepLog, error) {
	start := time.Now()
	lg := &model.JobStepLog{
		Seq: seq, Name: name, Kind: strings.ToLower(strings.TrimSpace(step.Type)),
		Status: "running", CreatedAt: start,
	}
	var err error
	timeout := step.TimeoutSec
	if timeout <= 0 {
		timeout = job.TimeoutSec
	}
	if timeout <= 0 {
		timeout = r.cfg.TimeoutSec
	}
	if timeout <= 0 {
		timeout = 60
	}
	switch strings.ToLower(strings.TrimSpace(step.Type)) {
	case "http":
		err = r.runHTTP(step, lg, time.Duration(timeout)*time.Second, rc)
	case "shell":
		err = r.runShell(step, lg, time.Duration(timeout)*time.Second, rc)
	case "sql":
		err = r.runSQL(step, lg, timeout, rc)
	default:
		err = fmt.Errorf("不支持的步骤类型: %s", step.Type)
	}
	lg.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		lg.Status = "failed"
		lg.Error = err.Error()
	} else {
		lg.Status = "success"
	}
	return lg, err
}

func (r *Runner) runHTTP(step model.JobStep, lg *model.JobStepLog, timeout time.Duration, rc *runContext) error {
	rawURL := rc.render(step.URL)
	if !strings.HasPrefix(rawURL, "http") {
		return fmt.Errorf("URL 不合法: %s", rawURL)
	}
	headers := make([]model.KV, 0, len(step.Headers))
	for _, h := range step.Headers {
		if strings.TrimSpace(h.Key) == "" {
			continue
		}
		headers = append(headers, model.KV{Key: h.Key, Value: rc.render(h.Value)})
	}
	body := rc.render(step.Body)
	exec, err := collector.NewExecutorWithJar(timeout, r.cfg.Proxy, true, rc.jar)
	if err != nil {
		return err
	}
	method := strings.ToUpper(strings.TrimSpace(step.Method))
	if method == "" {
		method = "GET"
	}
	lg.Method = method
	lg.URL = rawURL
	lg.ReqHeaders = mustJSON(headers)
	lg.ReqBody = truncate(body, r.maxBody())
	res, err := exec.Do(method, rawURL, headers, body, step.ContentType)
	if res != nil {
		lg.RespStatus = res.Status
		lg.RespHeaders = mustJSON(res.Headers)
		lg.RespBody = truncate(res.Text, r.maxBody())
	}
	if res != nil && err == nil {
		if saved := extractVars(step, res, rc); saved != "" {
			lg.Output = "提取变量: " + saved
		}
	}
	return err
}

func (r *Runner) runShell(step model.JobStep, lg *model.JobStepLog, timeout time.Duration, rc *runContext) error {
	cmdText := strings.TrimSpace(step.Command)
	if cmdText == "" {
		return errors.New("Shell 命令为空")
	}
	cmdText = rc.render(cmdText)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", cmdText)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdText)
	}
	if strings.TrimSpace(step.WorkDir) != "" {
		cmd.Dir = rc.render(step.WorkDir)
	}
	env := os.Environ()
	for _, kv := range step.Env {
		if strings.TrimSpace(kv.Key) == "" {
			continue
		}
		env = append(env, kv.Key+"="+rc.render(kv.Value))
	}
	cmd.Env = env
	lg.Method = "SHELL"
	lg.URL = cmdText
	out, err := cmd.CombinedOutput()
	lg.Output = truncate(string(out), r.maxBody())
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("命令超时（%s）", timeout)
	}
	if err != nil {
		return fmt.Errorf("%v%s", err, outputHint(lg.Output))
	}
	return nil
}

func (r *Runner) runSQL(step model.JobStep, lg *model.JobStepLog, timeout int, rc *runContext) error {
	sqlText := strings.TrimSpace(rc.render(step.SQL))
	if sqlText == "" {
		return errors.New("SQL 语句为空")
	}
	if step.DataSourceID == 0 {
		return errors.New("未选择数据库")
	}
	var ds model.DataSource
	if err := r.db.First(&ds, step.DataSourceID).Error; err != nil {
		return errors.New("数据源不存在")
	}
	lg.Method = "SQL"
	lg.URL = ds.Name + " (" + ds.Type + ")"
	lg.ReqBody = truncate(sqlText, r.maxBody())
	res, err := collector.ExecSQL(ds, sqlText, 200)
	if err != nil {
		lg.Output = truncate(err.Error(), r.maxBody())
		return err
	}
	b, _ := json.Marshal(res)
	lg.RespBody = truncate(string(b), r.maxBody())
	if res.Statement == "query" {
		lg.Output = fmt.Sprintf("查询返回 %d 行 / %d 列", len(res.Rows), len(res.Columns))
	} else {
		lg.Output = fmt.Sprintf("执行成功，影响 %d 行", res.Affected)
	}
	return nil
}

func (r *Runner) maxBody() int {
	kb := r.cfg.MaxBodyKB
	if kb <= 0 {
		kb = 256
	}
	return kb * 1024
}

// extractVars 按步骤配置从响应中提取变量，返回 "name=value" 的摘要
func extractVars(step model.JobStep, res *collector.HTTPResult, rc *runContext) string {
	var saved []string
	for _, kv := range step.Extract {
		name := strings.TrimSpace(kv.Key)
		expr := strings.TrimSpace(kv.Value)
		if name == "" || expr == "" {
			continue
		}
		if v, ok := evalExpr(expr, res); ok {
			rc.setVar(name, v)
			saved = append(saved, name+"="+maskValue(name, v))
		}
	}
	return strings.Join(saved, ", ")
}

// evalExpr 从 HTTP 响应中取值，支持：
//   - status                   响应状态码
//   - header:X-Token           响应头
//   - regex:<pattern>          正文正则（取第一个分组）
//   - data.token / data.list.0.id   JSON 路径
func evalExpr(expr string, res *collector.HTTPResult) (string, bool) {
	lower := strings.ToLower(expr)
	if lower == "status" {
		return strconv.Itoa(res.Status), true
	}
	if strings.HasPrefix(lower, "header:") {
		name := strings.TrimSpace(expr[len("header:"):])
		for k, v := range res.Headers {
			if strings.EqualFold(k, name) {
				return v, true
			}
		}
		return "", false
	}
	if strings.HasPrefix(lower, "regex:") {
		pat := strings.TrimSpace(expr[len("regex:"):])
		re, err := regexp.Compile(pat)
		if err != nil {
			return "", false
		}
		m := re.FindStringSubmatch(res.Text)
		if len(m) == 0 {
			return "", false
		}
		if len(m) > 1 {
			return m[1], true
		}
		return m[0], true
	}
	if res.JSON == nil {
		return "", false
	}
	cur := res.JSON
	for _, seg := range jsonPathSegments(expr) {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return "", false
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return "", false
			}
			cur = node[idx]
		default:
			return "", false
		}
	}
	switch v := cur.(type) {
	case nil:
		return "", false
	case string:
		return v, true
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}

// jsonPathSegments 拆分 a.b.0.c / a[0].b 形式的路径
func jsonPathSegments(path string) []string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	path = strings.ReplaceAll(path, "[", ".")
	path = strings.ReplaceAll(path, "]", "")
	out := make([]string, 0, 4)
	for _, s := range strings.Split(path, ".") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// maskValue 日志中对敏感变量打码，其余变量保留可读值（最长 60 字符）
func maskValue(name, v string) string {
	if isSensitiveName(name) {
		if len(v) <= 8 {
			return "***"
		}
		return v[:4] + "***" + v[len(v)-2:]
	}
	if len(v) > 60 {
		return v[:60] + "..."
	}
	return v
}

func isSensitiveName(name string) bool {
	n := strings.ToLower(name)
	for _, kw := range []string{"token", "secret", "password", "passwd", "auth", "cookie", "session", "key", "sign", "ticket"} {
		if strings.Contains(n, kw) {
			return true
		}
	}
	return false
}

func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("\n...[截断，原始大小 %d 字节]", len(s))
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func outputHint(out string) string {
	if strings.TrimSpace(out) == "" {
		return ""
	}
	return "：" + strings.TrimSpace(out)
}

func kindName(t string) string {
	switch strings.ToLower(t) {
	case "http":
		return "HTTP"
	case "shell":
		return "Shell"
	case "sql":
		return "SQL"
	}
	return t
}

// render 替换内置变量：{{now}} {{today}} {{timestamp}} {{ts}} {{uuid}}
func render(s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	now := time.Now()
	vals := map[string]string{
		"now":       now.Format("2006-01-02 15:04:05"),
		"datetime":  now.Format("2006-01-02 15:04:05"),
		"today":     now.Format("2006-01-02"),
		"date":      now.Format("2006-01-02"),
		"timestamp": fmt.Sprintf("%d", now.Unix()),
		"ts":        fmt.Sprintf("%d", now.Unix()),
		"uuid":      randomHex(),
	}
	out := s
	for k, v := range vals {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
		out = strings.ReplaceAll(out, "{{ "+k+" }}", v)
	}
	return out
}

func randomHex() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
