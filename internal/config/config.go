package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Browser 浏览器内核相关配置
type Browser struct {
	BinPath      string `json:"bin_path"`      // 自定义 Chrome/Chromium 可执行文件路径
	Headless     bool   `json:"headless"`      // 采集回放默认无头，录制永远有头
	Proxy        string `json:"proxy"`         // 浏览器代理，如 http://127.0.0.1:7890
	DownloadHost string `json:"download_host"` // google | npm | custom
	CustomHost   string `json:"custom_host"`   // 自定义下载前缀(不含平台/版本/文件名)
	Revision     int    `json:"revision"`      // Chromium 版本号，0 表示使用 rod 默认
	NoSandbox    bool   `json:"no_sandbox"`    // Linux/容器环境建议开启

	// 会话模式：
	//   isolated（默认）= 每次运行都用全新的临时用户目录 + 无痕，跑完即删，互不影响
	//   shared          = 所有运行共用一个持久化用户目录，Cookie / localStorage / IndexedDB
	//                     会保留下来，适合「登录一次，后续任务复用」或需要过滑块/验证码的站点
	SessionMode string `json:"session_mode"`
	// ProfileDir 共享会话使用的用户目录，留空则用 <data>/browser/profiles/default
	ProfileDir string `json:"profile_dir"`

	// 以下为浏览器指纹配置，全部留空表示使用浏览器内核默认值。
	// 纯 UI / 混合模式的回放、录制以及接口采集的默认请求头都会沿用这里的设置，
	// 保证「浏览器里看到的样子」和「接口发出的样子」一致，降低被风控识别的概率。
	UserAgent      string   `json:"user_agent"`      // 自定义 User-Agent
	AcceptLanguage string   `json:"accept_language"` // Accept-Language 头 / navigator.language
	Platform       string   `json:"platform"`        // navigator.platform，如 Win32 / MacIntel
	Timezone       string   `json:"timezone"`        // 时区，如 Asia/Shanghai
	Width          int      `json:"width"`           // 视口宽度，0 表示默认 1440
	Height         int      `json:"height"`          // 视口高度，0 表示默认 960
	ExtraArgs      []string `json:"extra_args"`      // 额外启动参数，如 --disable-gpu
}

type Config struct {
	Addr       string  `json:"addr"`
	DataDir    string  `json:"data_dir"`
	Browser    Browser `json:"browser"`
	MaxBodyKB  int     `json:"max_body_kb"`  // 日志中单个请求/响应体最大保存大小
	Proxy      string  `json:"proxy"`        // 接口采集默认代理
	TimeoutSec int     `json:"timeout_sec"`  // 单请求超时
	RunWorkers int     `json:"run_workers"`  // 预留
}

var (
	mu   sync.RWMutex
	cur  Config
	path string
)

func defaults() Config {
	return Config{
		Addr: "127.0.0.1:8686",
		DataDir: "./data",
		Browser: Browser{
			Headless:     true,
			DownloadHost: "npm",
			NoSandbox:    runtime.GOOS == "linux",
			SessionMode:  "isolated",
		},
		MaxBodyKB:  256,
		TimeoutSec: 60,
		RunWorkers: 2,
	}
}

// Load 读取 config.json，不存在则写入默认配置
func Load() *Config {
	mu.Lock()
	defer mu.Unlock()
	cur = defaults()
	exe, _ := os.Executable()
	base := filepath.Dir(exe)
	if wd, err := os.Getwd(); err == nil && (filepath.Base(wd) != "bin") {
		base = wd
	}
	path = filepath.Join(base, "config.json")
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &cur)
	}
	if cur.Addr == "" {
		cur.Addr = defaults().Addr
	}
	if cur.DataDir == "" {
		cur.DataDir = defaults().DataDir
	}
	if !filepath.IsAbs(cur.DataDir) {
		cur.DataDir = filepath.Join(base, cur.DataDir)
	}
	for _, d := range []string{cur.DataDir, filepath.Join(cur.DataDir, "exports"), filepath.Join(cur.DataDir, "logs"), filepath.Join(cur.DataDir, "browser")} {
		_ = os.MkdirAll(d, 0o755)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = saveLocked(cur)
	}
	return &cur
}

func Get() Config {
	mu.RLock()
	defer mu.RUnlock()
	return cur
}

func Save(c Config) error {
	mu.Lock()
	defer mu.Unlock()
	cur = c
	return saveLocked(c)
}

func saveLocked(c Config) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(path, b, 0o644)
}

// DataPath 返回数据目录下的路径
func DataPath(elem ...string) string {
	c := Get()
	parts := append([]string{c.DataDir}, elem...)
	return filepath.Join(parts...)
}

func FilePath() string {
	mu.RLock()
	defer mu.RUnlock()
	return path
}
