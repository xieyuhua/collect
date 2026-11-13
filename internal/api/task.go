package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"collect/internal/model"

	"github.com/gin-gonic/gin"
)

// taskPayload 前端提交的任务结构（数组/对象以原始 JSON 传入，直接落库）
type taskPayload struct {
	Name         string          `json:"name"`
	Mode         string          `json:"mode"`
	Remark       string          `json:"remark"`
	Enabled      *bool           `json:"enabled"`
	Notify       *bool           `json:"notify"`
	NotifyOn     string          `json:"notify_on"`
	ScheduleType string          `json:"schedule_type"`
	Cron         string          `json:"cron"`
	IntervalSec  int             `json:"interval_sec"`
	TimeoutSec   int             `json:"timeout_sec"`
	SessionMode  string          `json:"session_mode"`
	Vars         json.RawMessage `json:"vars"`
	HTTPSteps    json.RawMessage `json:"http_steps"`
	UISteps      json.RawMessage `json:"ui_steps"`
	LoginSteps   json.RawMessage `json:"login_steps"`
	Capture      json.RawMessage `json:"capture"`
	Pagination   json.RawMessage `json:"pagination"`
	Mapping      json.RawMessage `json:"mapping"`
	Sink         json.RawMessage `json:"sink"`
	Dedupe       json.RawMessage `json:"dedupe"`
}

func rawJSON(raw json.RawMessage, def string) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return def
	}
	return s
}

func (p *taskPayload) apply(t *model.Task) error {
	t.Name = strings.TrimSpace(p.Name)
	if t.Name == "" {
		return errors.New("请填写任务名称")
	}
	switch p.Mode {
	case "api", "ui", "hybrid":
		t.Mode = p.Mode
	default:
		return errors.New("采集模式必须是 api / ui / hybrid")
	}
	t.Remark = p.Remark
	if p.Enabled != nil {
		t.Enabled = *p.Enabled
	}
	if p.Notify != nil {
		t.Notify = *p.Notify
	}
	if p.NotifyOn == "" {
		p.NotifyOn = "failure"
	}
	t.NotifyOn = p.NotifyOn
	if p.ScheduleType == "" {
		p.ScheduleType = "manual"
	}
	t.ScheduleType = p.ScheduleType
	t.CronExpr = strings.TrimSpace(p.Cron)
	t.IntervalSec = p.IntervalSec
	t.TimeoutSec = p.TimeoutSec
	switch strings.ToLower(strings.TrimSpace(p.SessionMode)) {
	case "", "inherit", "default":
		t.SessionMode = ""
	case "isolated", "incognito":
		t.SessionMode = "isolated"
	case "shared", "share":
		t.SessionMode = "shared"
	default:
		return errors.New("会话模式只能是「跟随系统设置 / 每次全新 / 共享会话」")
	}
	t.Vars = rawJSON(p.Vars, "[]")
	t.HTTPSteps = rawJSON(p.HTTPSteps, "[]")
	t.UISteps = rawJSON(p.UISteps, "[]")
	t.LoginSteps = rawJSON(p.LoginSteps, "[]")
	t.Capture = rawJSON(p.Capture, `{"mode":"network"}`)
	t.Pagination = rawJSON(p.Pagination, "{}")
	t.Mapping = rawJSON(p.Mapping, "[]")
	t.Sink = rawJSON(p.Sink, `{"type":"table","write_mode":"insert","batch_size":200,"auto_create":true,"export_format":"xlsx"}`)
	t.Dedupe = rawJSON(p.Dedupe, `{"enabled":false,"keys":[]}`)
	return nil
}

func (s *Server) taskView(t *model.Task) map[string]any {
	m := t.ToMap()
	m["next_run"] = s.Sched.NextRun(t.ID)
	m["running"] = s.Engine.IsRunning(t.ID)
	return m
}

func (s *Server) listTasks(c *gin.Context) {
	page, size := pageQuery(c)
	q := s.DB.Model(&model.Task{})
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		q = q.Where("name LIKE ? OR remark LIKE ?", "%"+kw+"%", "%"+kw+"%")
	}
	if mode := c.Query("mode"); mode != "" {
		q = q.Where("mode = ?", mode)
	}
	var total int64
	q.Count(&total)
	var tasks []model.Task
	if err := q.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&tasks).Error; err != nil {
		fail(c, err)
		return
	}
	list := make([]map[string]any, 0, len(tasks))
	for i := range tasks {
		list = append(list, s.taskView(&tasks[i]))
	}
	ok(c, gin.H{"total": total, "page": page, "page_size": size, "list": list})
}

func (s *Server) getTask(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var t model.Task
	if err := s.DB.First(&t, id).Error; err != nil {
		failMsg(c, "任务不存在")
		return
	}
	ok(c, s.taskView(&t))
}

func (s *Server) createTask(c *gin.Context) {
	var p taskPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		failMsg(c, "请求参数错误: "+err.Error())
		return
	}
	t := model.Task{Enabled: true, Notify: true, NotifyOn: "failure", ScheduleType: "manual"}
	if err := p.apply(&t); err != nil {
		fail(c, err)
		return
	}
	if err := s.DB.Create(&t).Error; err != nil {
		fail(c, err)
		return
	}
	s.Sched.Reload(&t)
	s.Log.Info("api", "创建任务「%s」", t.Name)
	ok(c, s.taskView(&t))
}

func (s *Server) updateTask(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var t model.Task
	if err := s.DB.First(&t, id).Error; err != nil {
		failMsg(c, "任务不存在")
		return
	}
	var p taskPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		failMsg(c, "请求参数错误: "+err.Error())
		return
	}
	if err := p.apply(&t); err != nil {
		fail(c, err)
		return
	}
	if err := s.DB.Save(&t).Error; err != nil {
		fail(c, err)
		return
	}
	s.Sched.Reload(&t)
	s.Log.Info("api", "更新任务「%s」", t.Name)
	ok(c, s.taskView(&t))
}

func (s *Server) deleteTask(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	if s.Engine.IsRunning(id) {
		failMsg(c, "任务正在运行中，无法删除")
		return
	}
	s.Sched.Remove(id)
	if err := s.DB.Delete(&model.Task{}, id).Error; err != nil {
		fail(c, err)
		return
	}
	s.DB.Where("task_id = ?", id).Delete(&model.DedupeKey{})
	okMsg(c, "已删除", nil)
}

func (s *Server) runTask(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	run, err := s.Engine.Start(id, "manual")
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, run)
}

func (s *Server) cloneTask(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var t model.Task
	if err := s.DB.First(&t, id).Error; err != nil {
		failMsg(c, "任务不存在")
		return
	}
	cp := t
	cp.ID = 0
	cp.Name = t.Name + " 副本"
	cp.Enabled = false
	cp.ScheduleType = "manual"
	cp.LastRunAt = nil
	cp.LastStatus = ""
	cp.LastMessage = ""
	cp.CreatedAt = time.Time{}
	cp.UpdatedAt = time.Time{}
	if err := s.DB.Create(&cp).Error; err != nil {
		fail(c, err)
		return
	}
	ok(c, s.taskView(&cp))
}

func (s *Server) taskRuns(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	page, size := pageQuery(c)
	var total int64
	q := s.DB.Model(&model.RunLog{}).Where("task_id = ?", id)
	q.Count(&total)
	var list []model.RunLog
	if err := q.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"total": total, "page": page, "page_size": size, "list": list})
}

const exportVersion = 1

func exportEnvelope(tasks []map[string]any) gin.H {
	return gin.H{"version": exportVersion, "exported_at": time.Now().Format(time.RFC3339), "tasks": tasks}
}

func downloadJSON(c *gin.Context, filename string, body any) {
	b, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		"task-export.json", url.PathEscape(filename)))
	c.Data(200, "application/json; charset=utf-8", b)
}

func (s *Server) exportTask(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var t model.Task
	if err := s.DB.First(&t, id).Error; err != nil {
		failMsg(c, "任务不存在")
		return
	}
	downloadJSON(c, t.Name+".json", exportEnvelope([]map[string]any{t.ToMap()}))
}

func (s *Server) exportAllTasks(c *gin.Context) {
	var tasks []model.Task
	if err := s.DB.Order("id asc").Find(&tasks).Error; err != nil {
		fail(c, err)
		return
	}
	list := make([]map[string]any, 0, len(tasks))
	for i := range tasks {
		list = append(list, tasks[i].ToMap())
	}
	downloadJSON(c, fmt.Sprintf("collect-tasks-%s.json", time.Now().Format("20060102-150405")), exportEnvelope(list))
}

// importTasks 支持 JSON 请求体 {"tasks":[...]} 或直接上传导出文件
func (s *Server) importTasks(c *gin.Context) {
	var raw []byte
	if strings.HasPrefix(c.ContentType(), "multipart/form-data") {
		f, _, err := c.Request.FormFile("file")
		if err != nil {
			failMsg(c, "请选择导入文件: "+err.Error())
			return
		}
		defer f.Close()
		raw, err = io.ReadAll(io.LimitReader(f, 32<<20))
		if err != nil {
			fail(c, err)
			return
		}
	} else {
		b, err := io.ReadAll(io.LimitReader(c.Request.Body, 32<<20))
		if err != nil {
			fail(c, err)
			return
		}
		raw = b
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	var req struct {
		Tasks     []taskPayload `json:"tasks"`
		Overwrite bool          `json:"overwrite"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		// 兼容直接上传数组
		var arr []taskPayload
		if err2 := json.Unmarshal(raw, &arr); err2 != nil {
			failMsg(c, "JSON 解析失败: "+err.Error())
			return
		}
		req.Tasks = arr
	}
	if len(req.Tasks) == 0 {
		failMsg(c, "没有可导入的任务")
		return
	}
	created, updated, skipped := 0, 0, 0
	for i := range req.Tasks {
		p := req.Tasks[i]
		t := model.Task{Enabled: true, Notify: true, NotifyOn: "failure", ScheduleType: "manual"}
		if err := p.apply(&t); err != nil {
			skipped++
			continue
		}
		var old model.Task
		err := s.DB.Where("name = ?", t.Name).First(&old).Error
		if err == nil && req.Overwrite {
			t.ID = old.ID
			t.CreatedAt = old.CreatedAt
			if err := s.DB.Save(&t).Error; err != nil {
				skipped++
				continue
			}
			s.Sched.Reload(&t)
			updated++
			continue
		}
		if t.Name != "" && err == nil {
			// 同名任务已存在且未选择覆盖：自动加后缀
			t.Name = fmt.Sprintf("%s (导入 %s)", t.Name, time.Now().Format("0102-1504"))
		}
		if err := s.DB.Create(&t).Error; err != nil {
			skipped++
			continue
		}
		s.Sched.Reload(&t)
		created++
	}
	s.Log.Info("api", "导入任务：新增 %d，覆盖 %d，跳过 %d", created, updated, skipped)
	okMsg(c, fmt.Sprintf("导入完成：新增 %d 个，覆盖 %d 个，跳过 %d 个", created, updated, skipped),
		gin.H{"created": created, "updated": updated, "skipped": skipped})
}

// taskOptions 供下拉框使用
func (s *Server) taskOptions(c *gin.Context) {
	var tasks []model.Task
	if err := s.DB.Select("id", "name", "mode", "enabled").Order("id desc").Find(&tasks).Error; err != nil {
		fail(c, err)
		return
	}
	ok(c, tasks)
}
