package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"collect/internal/applog"
	"collect/internal/collector"
	"collect/internal/jobs"
	"collect/internal/model"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// Scheduler 基于 cron 的任务调度器
type Scheduler struct {
	db     *gorm.DB
	engine *collector.Engine
	jobs   *jobs.Runner
	log    *applog.Logger

	c      *cron.Cron
	mu     sync.Mutex
	entry  map[uint]cron.EntryID
	jentry map[uint]cron.EntryID
}

func New(db *gorm.DB, engine *collector.Engine, log *applog.Logger) *Scheduler {
	parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	return &Scheduler{
		db: db, engine: engine, log: log,
		c:      cron.New(cron.WithParser(parser), cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger))),
		entry:  map[uint]cron.EntryID{},
		jentry: map[uint]cron.EntryID{},
	}
}

// SetJobRunner 注入定时作业执行器（作业可独立于采集任务调度）
func (s *Scheduler) SetJobRunner(r *jobs.Runner) { s.jobs = r }

func (s *Scheduler) Start() {
	s.c.Start()
	var tasks []model.Task
	s.db.Where("enabled = ?", true).Find(&tasks)
	for i := range tasks {
		s.Reload(&tasks[i])
	}
	var jobList []model.Job
	s.db.Where("enabled = ?", true).Find(&jobList)
	for i := range jobList {
		s.ReloadJob(&jobList[i])
	}
	s.log.Info("scheduler", "调度器已启动，加载采集任务 %d 个、定时作业 %d 个", len(tasks), len(jobList))
}

func (s *Scheduler) Stop() {
	if s.c != nil {
		s.c.Stop()
	}
}

// spec 生成 cron 表达式，返回空表示无需调度
func spec(t *model.Task) string {
	if t == nil || !t.Enabled || t.ScheduleType == "" || t.ScheduleType == "manual" {
		return ""
	}
	switch t.ScheduleType {
	case "cron":
		return strings.TrimSpace(t.CronExpr)
	case "interval":
		if t.IntervalSec < 5 {
			return ""
		}
		return fmt.Sprintf("@every %ds", t.IntervalSec)
	}
	return ""
}

// Reload 重新加载单个任务的调度
func (s *Scheduler) Reload(t *model.Task) {
	if t == nil {
		return
	}
	s.Remove(t.ID)
	expr := spec(t)
	if expr == "" {
		return
	}
	taskID := t.ID
	entryID, err := s.c.AddFunc(expr, func() {
		if _, err := s.engine.Start(taskID, "cron"); err != nil {
			s.log.Warn("scheduler", "定时任务 %d 触发失败: %v", taskID, err)
		}
	})
	if err != nil {
		s.log.Error("scheduler", "任务「%s」调度表达式错误(%s): %v", t.Name, expr, err)
		return
	}
	s.mu.Lock()
	s.entry[t.ID] = entryID
	s.mu.Unlock()
	s.log.Info("scheduler", "任务「%s」已调度: %s", t.Name, expr)
}

func (s *Scheduler) Remove(taskID uint) {
	s.mu.Lock()
	id, ok := s.entry[taskID]
	if ok {
		delete(s.entry, taskID)
	}
	s.mu.Unlock()
	if ok {
		s.c.Remove(id)
	}
}

// NextRuns 返回各任务下次触发时间，用于前端展示
func (s *Scheduler) NextRuns() map[uint]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[uint]string{}
	now := time.Now()
	for id, eid := range s.entry {
		e := s.c.Entry(eid)
		if !e.Next.IsZero() && e.Next.After(now) {
			out[id] = e.Next.Format("2006-01-02 15:04:05")
		}
	}
	return out
}

// NextRun 返回单个任务的下次触发时间
func (s *Scheduler) NextRun(taskID uint) string {
	s.mu.Lock()
	id, ok := s.entry[taskID]
	s.mu.Unlock()
	if !ok {
		return ""
	}
	e := s.c.Entry(id)
	if e.Next.IsZero() {
		return ""
	}
	return e.Next.Format("2006-01-02 15:04:05")
}

// Describe 生成调度描述
func Describe(t *model.Task) string {
	if t == nil || !t.Enabled {
		return "已停用"
	}
	switch t.ScheduleType {
	case "cron":
		if strings.TrimSpace(t.CronExpr) == "" {
			return "未设置"
		}
		return t.CronExpr
	case "interval":
		if t.IntervalSec < 5 {
			return "未设置"
		}
		return "每 " + strconv.Itoa(t.IntervalSec) + " 秒"
	}
	return "手动"
}

// ---------- 定时作业 ----------

func jobSpec(j *model.Job) string {
	if j == nil || !j.Enabled || j.ScheduleType == "" || j.ScheduleType == "manual" {
		return ""
	}
	switch j.ScheduleType {
	case "cron":
		return strings.TrimSpace(j.CronExpr)
	case "interval":
		if j.IntervalSec < 5 {
			return ""
		}
		return fmt.Sprintf("@every %ds", j.IntervalSec)
	}
	return ""
}

// ValidateCron 校验 Cron 表达式（与调度器使用同一解析器）
func ValidateCron(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return fmt.Errorf("请填写 Cron 表达式")
	}
	parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if _, err := parser.Parse(expr); err != nil {
		return fmt.Errorf("Cron 表达式不正确: %v", err)
	}
	return nil
}

// ReloadJob 重新加载单个定时作业
func (s *Scheduler) ReloadJob(j *model.Job) {
	if j == nil || s.jobs == nil {
		return
	}
	s.RemoveJob(j.ID)
	expr := jobSpec(j)
	if expr == "" {
		return
	}
	jobID := j.ID
	entryID, err := s.c.AddFunc(expr, func() {
		if _, err := s.jobs.Start(jobID, "cron"); err != nil {
			s.log.Warn("scheduler", "定时作业 %d 触发失败: %v", jobID, err)
		}
	})
	if err != nil {
		s.log.Error("scheduler", "定时作业「%s」调度表达式错误(%s): %v", j.Name, expr, err)
		return
	}
	s.mu.Lock()
	s.jentry[j.ID] = entryID
	s.mu.Unlock()
	s.log.Info("scheduler", "定时作业「%s」已调度: %s", j.Name, expr)
}

func (s *Scheduler) RemoveJob(jobID uint) {
	s.mu.Lock()
	id, ok := s.jentry[jobID]
	if ok {
		delete(s.jentry, jobID)
	}
	s.mu.Unlock()
	if ok {
		s.c.Remove(id)
	}
}

func (s *Scheduler) NextRunJob(jobID uint) string {
	s.mu.Lock()
	id, ok := s.jentry[jobID]
	s.mu.Unlock()
	if !ok {
		return ""
	}
	e := s.c.Entry(id)
	if e.Next.IsZero() {
		return ""
	}
	return e.Next.Format("2006-01-02 15:04:05")
}

// DescribeJob 生成作业调度描述
func DescribeJob(j *model.Job) string {
	if j == nil || !j.Enabled {
		return "已停用"
	}
	switch j.ScheduleType {
	case "cron":
		if strings.TrimSpace(j.CronExpr) == "" {
			return "未设置"
		}
		return j.CronExpr
	case "interval":
		if j.IntervalSec < 5 {
			return "未设置"
		}
		return "每 " + strconv.Itoa(j.IntervalSec) + " 秒"
	}
	return "手动"
}
