package api

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"collect/internal/config"
	"collect/internal/model"
	"collect/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/go-rod/rod/lib/proto"
)

var settingKeys = []string{
	"wecom_enabled", "wecom_webhook", "notify_on_failure", "notify_on_success", "max_body_kb",
	"record_run_request", "record_run_response", "record_log_request", "record_log_response",
}

func (s *Server) getSettings(c *gin.Context) {
	out := map[string]string{}
	for _, k := range settingKeys {
		out[k] = store.GetSetting(k)
	}
	cfg := config.Get()
	browserStatus := s.BM.Status()
	ok(c, gin.H{
		"settings":       out,
		"config":         cfg,
		"config_file":    config.FilePath(),
		"browser_status": browserStatus,
		"platform":       browserStatus["platform"],
	})
}

func (s *Server) updateSettings(c *gin.Context) {
	var body map[string]string
	if err := c.ShouldBindJSON(&body); err != nil {
		failMsg(c, "参数错误: "+err.Error())
		return
	}
	allowed := map[string]bool{}
	for _, k := range settingKeys {
		allowed[k] = true
	}
	for k, v := range body {
		if !allowed[k] {
			continue
		}
		if err := store.SetSetting(k, strings.TrimSpace(v)); err != nil {
			fail(c, err)
			return
		}
		if k == "max_body_kb" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				cfg := config.Get()
				cfg.MaxBodyKB = n
				_ = config.Save(cfg)
			}
		}
	}
	s.Log.Info("api", "更新平台设置")
	okMsg(c, "已保存", nil)
}

func (s *Server) testWecom(c *gin.Context) {
	if err := s.NT.Test(); err != nil {
		failMsg(c, "发送失败: "+err.Error())
		return
	}
	okMsg(c, "测试消息已发送，请查看企业微信群", nil)
}

type generalCfgReq struct {
	Addr       *string `json:"addr"`
	DataDir    *string `json:"data_dir"`
	TimeoutSec *int    `json:"timeout_sec"`
	Proxy      *string `json:"proxy"`
	MaxBodyKB  *int    `json:"max_body_kb"`
}

// generalConfig 允许在「系统设置」里自定义运行参数（监听地址、数据目录、超时、代理、包体上限），
// 不需要手改 config.json。listen / data_dir 属于启动期参数，保存后需要重启进程才会生效。
func (s *Server) generalConfig(c *gin.Context) {
	var req generalCfgReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数错误: "+err.Error())
		return
	}
	cfg := config.Get()
	restart := false

	if req.Addr != nil {
		addr := strings.TrimSpace(*req.Addr)
		if addr == "" {
			failMsg(c, "监听地址不能为空，例如 127.0.0.1:8686 或 0.0.0.0:8686")
			return
		}
		if !strings.Contains(addr, ":") {
			failMsg(c, "监听地址需要带端口，例如 127.0.0.1:8686")
			return
		}
		if addr != cfg.Addr {
			cfg.Addr = addr
			restart = true
		}
	}
	if req.DataDir != nil {
		dir := strings.TrimSpace(*req.DataDir)
		if dir == "" {
			failMsg(c, "数据目录不能为空")
			return
		}
		if !filepath.IsAbs(dir) {
			if abs, err := filepath.Abs(dir); err == nil {
				dir = abs
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			failMsg(c, "数据目录不可用: "+err.Error())
			return
		}
		if dir != cfg.DataDir {
			cfg.DataDir = dir
			restart = true
		}
	}
	if req.TimeoutSec != nil {
		n := *req.TimeoutSec
		if n < 1 || n > 1800 {
			failMsg(c, "单请求超时需要在 1 - 1800 秒之间")
			return
		}
		cfg.TimeoutSec = n
	}
	if req.Proxy != nil {
		p := strings.TrimSpace(*req.Proxy)
		if p != "" && !strings.HasPrefix(p, "http://") && !strings.HasPrefix(p, "https://") && !strings.HasPrefix(p, "socks5://") {
			failMsg(c, "代理地址需要以 http:// / https:// / socks5:// 开头")
			return
		}
		cfg.Proxy = p
	}
	if req.MaxBodyKB != nil {
		n := *req.MaxBodyKB
		if n < 8 || n > 8192 {
			failMsg(c, "日志包体上限需要在 8 - 8192 KB 之间")
			return
		}
		cfg.MaxBodyKB = n
		_ = store.SetSetting("max_body_kb", strconv.Itoa(n))
	}
	if err := config.Save(cfg); err != nil {
		fail(c, err)
		return
	}
	*s.Cfg = cfg
	s.Log.Info("api", "更新运行参数（重启生效: %v）", restart)
	ok(c, gin.H{"config": cfg, "restart_required": restart})
}

type browserCfgReq struct {
	BinPath      *string `json:"bin_path"`
	Headless     *bool   `json:"headless"`
	Proxy        *string `json:"proxy"`
	DownloadHost *string `json:"download_host"`
	CustomHost   *string `json:"custom_host"`
	Revision     *int    `json:"revision"`
	NoSandbox    *bool   `json:"no_sandbox"`

	// 会话模式：isolated（默认，每次全新无痕）/ shared（复用同一个用户目录）
	SessionMode *string `json:"session_mode"`
	ProfileDir  *string `json:"profile_dir"`

	// 指纹（留空表示使用内核默认）
	UserAgent      *string   `json:"user_agent"`
	AcceptLanguage *string   `json:"accept_language"`
	Platform       *string   `json:"platform"`
	Timezone       *string   `json:"timezone"`
	Width          *int      `json:"width"`
	Height         *int      `json:"height"`
	ExtraArgs      *[]string `json:"extra_args"`
}

func (s *Server) browserStatus(c *gin.Context) {
	ok(c, s.BM.Status())
}

func (s *Server) browserDownload(c *gin.Context) {
	if err := s.BM.Download(); err != nil {
		fail(c, err)
		return
	}
	okMsg(c, "已开始下载浏览器内核，请稍后刷新查看状态", nil)
}

func (s *Server) browserConfig(c *gin.Context) {
	var req browserCfgReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数错误: "+err.Error())
		return
	}
	cfg := config.Get()
	if req.BinPath != nil {
		cfg.Browser.BinPath = strings.TrimSpace(*req.BinPath)
	}
	if req.Headless != nil {
		cfg.Browser.Headless = *req.Headless
	}
	if req.Proxy != nil {
		cfg.Browser.Proxy = strings.TrimSpace(*req.Proxy)
	}
	if req.DownloadHost != nil {
		cfg.Browser.DownloadHost = strings.TrimSpace(*req.DownloadHost)
	}
	if req.CustomHost != nil {
		cfg.Browser.CustomHost = strings.TrimSpace(*req.CustomHost)
	}
	if req.Revision != nil {
		cfg.Browser.Revision = *req.Revision
	}
	if req.NoSandbox != nil {
		cfg.Browser.NoSandbox = *req.NoSandbox
	}
	if req.SessionMode != nil {
		mode := strings.ToLower(strings.TrimSpace(*req.SessionMode))
		switch mode {
		case "", "isolated", "incognito":
			cfg.Browser.SessionMode = "isolated"
		case "shared", "share":
			cfg.Browser.SessionMode = "shared"
		default:
			failMsg(c, "会话模式只能是 isolated（每次全新）或 shared（共享会话）")
			return
		}
	}
	if req.ProfileDir != nil {
		dir := strings.TrimSpace(*req.ProfileDir)
		if dir != "" && !filepath.IsAbs(dir) {
			if abs, err := filepath.Abs(dir); err == nil {
				dir = abs
			}
		}
		cfg.Browser.ProfileDir = dir
	}
	if req.UserAgent != nil {
		cfg.Browser.UserAgent = strings.TrimSpace(*req.UserAgent)
	}
	if req.AcceptLanguage != nil {
		cfg.Browser.AcceptLanguage = strings.TrimSpace(*req.AcceptLanguage)
	}
	if req.Platform != nil {
		cfg.Browser.Platform = strings.TrimSpace(*req.Platform)
	}
	if req.Timezone != nil {
		cfg.Browser.Timezone = strings.TrimSpace(*req.Timezone)
	}
	if req.Width != nil {
		cfg.Browser.Width = *req.Width
	}
	if req.Height != nil {
		cfg.Browser.Height = *req.Height
	}
	if req.ExtraArgs != nil {
		args := make([]string, 0, len(*req.ExtraArgs))
		for _, a := range *req.ExtraArgs {
			if a = strings.TrimSpace(a); a != "" {
				args = append(args, a)
			}
		}
		cfg.Browser.ExtraArgs = args
	}
	if err := config.Save(cfg); err != nil {
		fail(c, err)
		return
	}
	*s.Cfg = cfg
	s.Log.Info("api", "更新浏览器配置")
	ok(c, s.BM.Status())
}

// browserSessionClear 清空共享会话目录（Cookie / localStorage / IndexedDB）
func (s *Server) browserSessionClear(c *gin.Context) {
	if err := s.BM.ClearSession(); err != nil {
		fail(c, err)
		return
	}
	okMsg(c, "共享会话已清空，下次运行会重新登录", s.BM.Status())
}

// browserProbeUA 启动一次无头浏览器，回报当前内核真实上报的 UA / 平台，
// 方便用户判断「设置里的 UA」是否和内核默认值不一致（例如被系统镜像改过）。
func (s *Server) browserProbeUA(c *gin.Context) {
	b, cleanup, err := s.BM.LaunchEphemeral(true)
	if err != nil {
		fail(c, err)
		return
	}
	defer cleanup()
	page, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		fail(c, err)
		return
	}
	s.BM.ApplyFingerprint(page)
	ua := ""
	platform := ""
	if v, err := page.Eval("() => navigator.userAgent"); err == nil {
		ua = v.Value.Str()
	}
	if v, err := page.Eval("() => navigator.platform"); err == nil {
		platform = v.Value.Str()
	}
	tz := ""
	if v, err := page.Eval("() => Intl.DateTimeFormat().resolvedOptions().timeZone"); err == nil {
		tz = v.Value.Str()
	}
	lang := ""
	if v, err := page.Eval("() => navigator.language || ''"); err == nil {
		lang = v.Value.Str()
	}
	cfg := config.Get()
	ok(c, gin.H{
		"user_agent": ua,
		"platform":   platform,
		"timezone":   tz,
		"language":   lang,
		"configured": gin.H{
			"user_agent":      cfg.Browser.UserAgent,
			"accept_language": cfg.Browser.AcceptLanguage,
			"platform":        cfg.Browser.Platform,
			"timezone":        cfg.Browser.Timezone,
		},
	})
}

// dashboard 仪表盘统计
func (s *Server) dashboard(c *gin.Context) {
	var taskCount, taskEnabled, runToday int64
	s.DB.Model(&model.Task{}).Count(&taskCount)
	s.DB.Model(&model.Task{}).Where("enabled = ?", true).Count(&taskEnabled)
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	s.DB.Model(&model.RunLog{}).Where("started_at >= ?", startOfDay).Count(&runToday)

	var totalRuns, successRuns int64
	s.DB.Model(&model.RunLog{}).Where("status NOT IN ?", []string{"running", "stopped"}).Count(&totalRuns)
	s.DB.Model(&model.RunLog{}).Where("status = ?", "success").Count(&successRuns)
	rate := 0.0
	if totalRuns > 0 {
		rate = float64(successRuns) * 100 / float64(totalRuns)
	}
	var unread int64
	s.DB.Model(&model.Notification{}).Where("read = ?", false).Count(&unread)
	var running int64
	s.DB.Model(&model.RunLog{}).Where("status = ?", "running").Count(&running)
	var recent []model.RunLog
	s.DB.Order("id desc").Limit(10).Find(&recent)
	ok(c, gin.H{
		"tasks": taskCount, "tasks_enabled": taskEnabled, "runs_today": runToday,
		"runs_total": totalRuns, "success_rate": rate, "running": running,
		"unread": unread, "recent": recent,
	})
}
