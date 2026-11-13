package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"collect/internal/applog"
	"collect/internal/model"
	"collect/internal/store"

	"gorm.io/gorm"
)

type Notifier struct {
	DB     *gorm.DB
	Log    *applog.Logger
	Client *http.Client
}

func New(db *gorm.DB, log *applog.Logger) *Notifier {
	return &Notifier{DB: db, Log: log, Client: &http.Client{Timeout: 15 * time.Second}}
}

func (n *Notifier) Enabled() bool {
	if store.GetSetting("wecom_enabled") != "true" {
		return false
	}
	return strings.TrimSpace(store.GetSetting("wecom_webhook")) != ""
}

// SendWecom 发送企业微信机器人消息（markdown）
func (n *Notifier) SendWecom(content string) error {
	hook := strings.TrimSpace(store.GetSetting("wecom_webhook"))
	if hook == "" {
		return errors.New("未配置企业微信机器人 Webhook")
	}
	body, _ := json.Marshal(map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": content},
	})
	resp, err := n.Client.Post(hook, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	_ = json.Unmarshal(raw, &r)
	if r.ErrCode != 0 {
		return fmt.Errorf("企业微信返回错误(%d): %s", r.ErrCode, r.ErrMsg)
	}
	return nil
}

func (n *Notifier) Test() error {
	if !n.Enabled() {
		return errors.New("企业微信通知未启用或未配置 Webhook")
	}
	return n.SendWecom(fmt.Sprintf("**采集平台测试消息**\n> 时间: %s\n> 如果你看到这条消息，说明机器人配置成功。", time.Now().Format("2006-01-02 15:04:05")))
}

func (n *Notifier) Record(nt *model.Notification) {
	if nt.CreatedAt.IsZero() {
		nt.CreatedAt = time.Now()
	}
	if err := n.DB.Create(nt).Error; err != nil && n.Log != nil {
		n.Log.Error("notify", "保存消息失败: %v", err)
	}
}

// NotifyRun 运行结束后写入平台消息，并按任务设置推送企业微信
func (n *Notifier) NotifyRun(task *model.Task, run *model.RunLog) {
	level := "success"
	if run.Status == "failed" {
		level = "error"
	} else if run.Status == "partial" {
		level = "error"
	}
	icon := "✅"
	if level == "error" {
		icon = "❌"
	}
	title := fmt.Sprintf("%s 采集%s", task.Name, map[string]string{"success": "成功", "failed": "失败", "partial": "部分成功"}[run.Status])
	content := fmt.Sprintf("%s **%s**\n> 模式: %s\n> 触发: %s\n> 耗时: %.1fs\n> 页数: %d / 抓取: %d / 新增: %d / 重复: %d",
		icon, title, modeName(task.Mode), triggerName(run.Trigger), float64(run.DurationMs)/1000.0,
		run.Pages, run.Fetched, run.Inserted, run.Duplicated)
	if run.Error != "" {
		content += "\n> 错误: " + run.Error
	}
	if run.Message != "" {
		content += "\n> 说明: " + run.Message
	}
	n.Record(&model.Notification{
		TaskID: task.ID, RunID: run.ID, Level: level, Title: title, Content: content,
		Channel: "platform", Status: "sent",
	})
	if !task.Notify {
		return
	}
	on := task.NotifyOn
	if on == "" {
		on = defaultNotifyOn()
	}
	should := false
	switch on {
	case "always":
		should = true
	case "success":
		should = run.Status == "success"
	default:
		should = run.Status != "success"
	}
	if !should || !n.Enabled() {
		if should && n.Log != nil {
			n.Log.Warn("notify", "任务 %s 需要通知，但企业微信未启用", task.Name)
		}
		return
	}
	nt := &model.Notification{
		TaskID: task.ID, RunID: run.ID, Level: level, Title: title, Content: content,
		Channel: "wecom", Status: "sent",
	}
	if err := n.SendWecom(content); err != nil {
		nt.Status = "failed"
		nt.Error = err.Error()
		if n.Log != nil {
			n.Log.Error("notify", "企业微信通知失败: %v", err)
		}
	} else {
		run.Notified = true
	}
	n.Record(nt)
}

// defaultNotifyOn 任务未单独设置时，回落到平台通知策略
func defaultNotifyOn() string {
	success := store.GetSetting("notify_on_success") == "true"
	failure := store.GetSetting("notify_on_failure") == "true"
	switch {
	case success && failure:
		return "always"
	case success:
		return "success"
	case failure:
		return "failure"
	}
	return "failure"
}

// NotifyJob 定时作业执行结束后写入平台消息，并按作业设置推送企业微信
func (n *Notifier) NotifyJob(job *model.Job, run *model.JobRun) {
	if job == nil || run == nil {
		return
	}
	level := "success"
	icon := "✅"
	if run.Status != "success" {
		level = "error"
		icon = "❌"
	}
	title := fmt.Sprintf("作业「%s」执行%s", job.Name,
		map[string]string{"success": "成功", "failed": "失败", "partial": "部分成功"}[run.Status])
	finished := time.Now()
	if run.FinishedAt != nil {
		finished = *run.FinishedAt
	}
	content := fmt.Sprintf("%s **%s**\n> 触发: %s\n> 耗时: %.1fs\n> 步骤: %d / 成功 %d / 失败 %d\n> 时间: %s",
		icon, title, triggerName(run.Trigger), float64(run.DurationMs)/1000.0,
		run.StepsTotal, run.StepsOK, run.StepsFail, finished.Format("2006-01-02 15:04:05"))
	if run.Error != "" {
		content += "\n> 错误: " + run.Error
	}
	n.Record(&model.Notification{
		Level: level, Title: title, Content: content, Channel: "platform", Status: "sent",
	})
	if !job.Notify {
		return
	}
	on := job.NotifyOn
	if on == "" {
		on = defaultNotifyOn()
	}
	should := false
	switch on {
	case "always":
		should = true
	case "success":
		should = run.Status == "success"
	default:
		should = run.Status != "success"
	}
	if !should || !n.Enabled() {
		if should && n.Log != nil {
			n.Log.Warn("notify", "作业 %s 需要通知，但企业微信未启用", job.Name)
		}
		return
	}
	nt := &model.Notification{Level: level, Title: title, Content: content, Channel: "wecom", Status: "sent"}
	if err := n.SendWecom(content); err != nil {
		nt.Status = "failed"
		nt.Error = err.Error()
		if n.Log != nil {
			n.Log.Error("notify", "企业微信通知失败: %v", err)
		}
	} else {
		run.Notified = true
	}
	n.Record(nt)
}

func modeName(m string) string {
	switch m {
	case "api":
		return "纯接口"
	case "ui":
		return "纯UI"
	case "hybrid":
		return "UI+接口"
	}
	return m
}

func triggerName(t string) string {
	if t == "cron" {
		return "定时调度"
	}
	return "手动执行"
}
