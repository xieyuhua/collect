package api

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"collect/internal/model"
	"collect/internal/scheduler"

	"github.com/gin-gonic/gin"
)

type jobPayload struct {
	Name         string          `json:"name"`
	Remark       string          `json:"remark"`
	Enabled      *bool           `json:"enabled"`
	Notify       *bool           `json:"notify"`
	NotifyOn     string          `json:"notify_on"`
	ScheduleType string          `json:"schedule_type"`
	Cron         string          `json:"cron"`
	IntervalSec  int             `json:"interval_sec"`
	TimeoutSec   int             `json:"timeout_sec"`
	Steps        json.RawMessage `json:"steps"`
}

func (p *jobPayload) apply(j *model.Job) error {
	j.Name = strings.TrimSpace(p.Name)
	if j.Name == "" {
		return errors.New("请填写作业名称")
	}
	j.Remark = p.Remark
	if p.Enabled != nil {
		j.Enabled = *p.Enabled
	}
	if p.Notify != nil {
		j.Notify = *p.Notify
	}
	if p.NotifyOn == "" {
		p.NotifyOn = "failure"
	}
	j.NotifyOn = p.NotifyOn
	if p.ScheduleType == "" {
		p.ScheduleType = "manual"
	}
	switch p.ScheduleType {
	case "manual", "cron", "interval":
	default:
		return errors.New("调度方式必须是 manual / cron / interval")
	}
	j.ScheduleType = p.ScheduleType
	j.CronExpr = strings.TrimSpace(p.Cron)
	j.IntervalSec = p.IntervalSec
	j.TimeoutSec = p.TimeoutSec
	if j.TimeoutSec <= 0 {
		j.TimeoutSec = 60
	}
	switch j.ScheduleType {
	case "cron":
		// 无论是否启用都校验，避免保存后到调度时才报错
		if j.CronExpr != "" || j.Enabled {
			if err := scheduler.ValidateCron(j.CronExpr); err != nil {
				return err
			}
		}
	case "interval":
		if j.IntervalSec > 0 && j.IntervalSec < 5 {
			return errors.New("固定间隔至少 5 秒")
		}
		if j.Enabled && j.IntervalSec <= 0 {
			return errors.New("请填写固定间隔秒数（至少 5 秒）")
		}
	}
	raw := strings.TrimSpace(string(p.Steps))
	if raw == "" || raw == "null" {
		raw = "[]"
	}
	var steps []model.JobStep
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		return errors.New("步骤配置格式错误: " + err.Error())
	}
	if len(steps) == 0 {
		return errors.New("请至少添加一个步骤")
	}
	for i := range steps {
		steps[i].Type = strings.ToLower(strings.TrimSpace(steps[i].Type))
		switch steps[i].Type {
		case "http":
			if strings.TrimSpace(steps[i].URL) == "" {
				return errors.New("HTTP 步骤缺少 URL")
			}
			for _, kv := range steps[i].Extract {
				name := strings.TrimSpace(kv.Key)
				if name == "" && strings.TrimSpace(kv.Value) == "" {
					continue
				}
				if !varNameRe.MatchString(name) {
					return errors.New("提取变量名只能使用字母、数字、下划线，且不能以数字开头: " + name)
				}
				if reservedVars[name] {
					return errors.New("提取变量名与内置变量冲突: " + name)
				}
				if strings.TrimSpace(kv.Value) == "" {
					return errors.New("提取变量 " + name + " 缺少取值表达式")
				}
			}
		case "shell":
			if strings.TrimSpace(steps[i].Command) == "" {
				return errors.New("Shell 步骤缺少命令")
			}
		case "sql":
			if strings.TrimSpace(steps[i].SQL) == "" {
				return errors.New("SQL 步骤缺少 SQL 语句")
			}
			if steps[i].DataSourceID == 0 {
				return errors.New("SQL 步骤需要选择数据库")
			}
		default:
			return errors.New("步骤类型必须是 http / shell / sql")
		}
	}
	out, _ := json.Marshal(steps)
	j.Steps = string(out)
	return nil
}

var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var reservedVars = map[string]bool{
	"now": true, "datetime": true, "today": true, "date": true,
	"timestamp": true, "ts": true, "uuid": true,
}

func (s *Server) jobView(j *model.Job) map[string]any {
	m := j.ToMap()
	m["schedule_text"] = scheduler.DescribeJob(j)
	m["next_run"] = s.Sched.NextRunJob(j.ID)
	m["running"] = s.Jobs != nil && s.Jobs.IsRunning(j.ID)
	return m
}

func (s *Server) listJobs(c *gin.Context) {
	page, size := pageQuery(c)
	q := s.DB.Model(&model.Job{})
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		q = q.Where("name LIKE ? OR remark LIKE ?", "%"+kw+"%", "%"+kw+"%")
	}
	var total int64
	q.Count(&total)
	var list []model.Job
	if err := q.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		fail(c, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, s.jobView(&list[i]))
	}
	ok(c, gin.H{"total": total, "page": page, "page_size": size, "list": out})
}

func (s *Server) getJob(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var j model.Job
	if err := s.DB.First(&j, id).Error; err != nil {
		failMsg(c, "作业不存在")
		return
	}
	ok(c, s.jobView(&j))
}

func (s *Server) createJob(c *gin.Context) {
	var p jobPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		failMsg(c, "请求参数错误: "+err.Error())
		return
	}
	j := model.Job{Enabled: true, Notify: true, NotifyOn: "failure", ScheduleType: "manual", TimeoutSec: 60}
	if err := p.apply(&j); err != nil {
		fail(c, err)
		return
	}
	if err := s.DB.Create(&j).Error; err != nil {
		fail(c, err)
		return
	}
	s.Sched.ReloadJob(&j)
	s.Log.Info("api", "创建定时作业「%s」", j.Name)
	ok(c, s.jobView(&j))
}

func (s *Server) updateJob(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var j model.Job
	if err := s.DB.First(&j, id).Error; err != nil {
		failMsg(c, "作业不存在")
		return
	}
	var p jobPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		failMsg(c, "请求参数错误: "+err.Error())
		return
	}
	if err := p.apply(&j); err != nil {
		fail(c, err)
		return
	}
	if err := s.DB.Save(&j).Error; err != nil {
		fail(c, err)
		return
	}
	s.Sched.ReloadJob(&j)
	s.Log.Info("api", "更新定时作业「%s」", j.Name)
	ok(c, s.jobView(&j))
}

func (s *Server) deleteJob(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	if s.Jobs != nil && s.Jobs.IsRunning(id) {
		failMsg(c, "作业正在执行中，无法删除")
		return
	}
	s.Sched.RemoveJob(id)
	s.DB.Where("job_run_id IN (?)", s.DB.Model(&model.JobRun{}).Select("id").Where("job_id = ?", id)).
		Delete(&model.JobStepLog{})
	s.DB.Where("job_id = ?", id).Delete(&model.JobRun{})
	s.DB.Delete(&model.Job{}, id)
	okMsg(c, "已删除", nil)
}

func (s *Server) runJob(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	run, err := s.Jobs.Start(id, "manual")
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, run)
}

func (s *Server) cloneJob(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var j model.Job
	if err := s.DB.First(&j, id).Error; err != nil {
		failMsg(c, "作业不存在")
		return
	}
	cp := j
	cp.ID = 0
	cp.Name = j.Name + " 副本"
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
	ok(c, s.jobView(&cp))
}

func (s *Server) jobRuns(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	page, size := pageQuery(c)
	q := s.DB.Model(&model.JobRun{}).Where("job_id = ?", id)
	var total int64
	q.Count(&total)
	var list []model.JobRun
	if err := q.Order("started_at desc, id desc").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"total": total, "page": page, "page_size": size, "list": list})
}

func (s *Server) listJobRuns(c *gin.Context) {
	page, size := pageQuery(c)
	q := s.DB.Model(&model.JobRun{})
	if v := c.Query("job_id"); v != "" {
		q = q.Where("job_id = ?", v)
	}
	if v := c.Query("status"); v != "" {
		q = q.Where("status = ?", v)
	}
	var total int64
	q.Count(&total)
	var list []model.JobRun
	if err := q.Order("started_at desc, id desc").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"total": total, "page": page, "page_size": size, "list": list})
}

func (s *Server) getJobRun(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var run model.JobRun
	if err := s.DB.First(&run, id).Error; err != nil {
		failMsg(c, "执行记录不存在")
		return
	}
	var steps []model.JobStepLog
	if err := s.DB.Where("job_run_id = ?", id).Order("seq asc, id asc").Find(&steps).Error; err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"run": run, "steps": steps})
}

func (s *Server) deleteJobRun(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var run model.JobRun
	if err := s.DB.First(&run, id).Error; err != nil {
		failMsg(c, "执行记录不存在")
		return
	}
	if run.Status == "running" {
		failMsg(c, "执行中的记录不能删除")
		return
	}
	s.DB.Where("job_run_id = ?", id).Delete(&model.JobStepLog{})
	s.DB.Delete(&model.JobRun{}, id)
	okMsg(c, "已删除", nil)
}
