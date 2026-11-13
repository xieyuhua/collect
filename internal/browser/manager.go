package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"collect/internal/applog"
	"collect/internal/config"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
)

type Manager struct {
	cfg *config.Config
	log *applog.Logger

	mu         sync.Mutex
	downloading bool
	lastErr    string
	binPath    string
	source     string
}

// 浏览器会话模式：isolated（默认）每次全新无痕；shared 所有运行共用一个持久化用户目录
const (
	SessionIsolated = "isolated"
	SessionShared   = "shared"
)

// NormalizeSessionMode 统一取值，无法识别时回落到 isolated（安全默认）
func NormalizeSessionMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case SessionShared, "share", "persist", "persistent":
		return SessionShared
	default:
		return SessionIsolated
	}
}

// 同一个用户目录同一时刻只能被一个 Chromium 进程使用，否则第二个实例会拿不到锁直接退出。
// 这里用「按目录排队」的闸门，把并发运行串行化，避免共享会话下互相抢 profile。
type sessionGate struct{ ch chan struct{} }

var sessionGates sync.Map // 目录 -> *sessionGate

func gateFor(dir string) *sessionGate {
	if v, ok := sessionGates.Load(dir); ok {
		return v.(*sessionGate)
	}
	g := &sessionGate{ch: make(chan struct{}, 1)}
	v, _ := sessionGates.LoadOrStore(dir, g)
	return v.(*sessionGate)
}

func acquireSession(dir string, wait time.Duration) (func(), error) {
	g := gateFor(dir)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case g.ch <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-g.ch }) }, nil
	case <-timer.C:
		return nil, fmt.Errorf("共享会话正被另一个任务使用（同一时间只能有一个浏览器实例跑同一个会话目录），请等它结束后再试")
	}
}

func sessionBusy(dir string) bool {
	if v, ok := sessionGates.Load(dir); ok {
		return len(v.(*sessionGate).ch) > 0
	}
	return false
}

func NewManager(cfg *config.Config, log *applog.Logger) *Manager {
	return &Manager{cfg: cfg, log: log}
}

// SessionMode 返回全局会话模式
func (m *Manager) SessionMode() string {
	if m == nil || m.cfg == nil {
		return SessionIsolated
	}
	return NormalizeSessionMode(m.cfg.Browser.SessionMode)
}

// ProfileDir 返回共享会话使用的用户目录（绝对路径）
func (m *Manager) ProfileDir() string {
	dir := ""
	if m != nil && m.cfg != nil {
		dir = strings.TrimSpace(m.cfg.Browser.ProfileDir)
	}
	if dir == "" {
		dir = config.DataPath("browser", "profiles", "default")
	} else if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	return filepath.Clean(dir)
}

// SessionInfo 返回共享会话状态，供设置页展示
func (m *Manager) SessionInfo() map[string]any {
	dir := m.ProfileDir()
	mode := m.SessionMode()
	files, size := dirStats(dir)
	return map[string]any{
		"session_mode":    mode,
		"profile_dir":     dir,
		"session_exists":  files > 0,
		"session_files":   files,
		"session_size_mb": math.Round(float64(size)/1024/1024*10) / 10,
		"session_locked":  sessionBusy(dir),
		"session_persist": mode == SessionShared,
	}
}

// ClearSession 清空共享会话目录（Cookie / localStorage / IndexedDB 全部丢弃）
func (m *Manager) ClearSession() error {
	dir := m.ProfileDir()
	if err := safeProfileDir(dir); err != nil {
		return err
	}
	release, err := acquireSession(dir, 3*time.Second)
	if err != nil {
		return errors.New("有任务正在使用共享会话，等运行结束后再清空")
	}
	defer release()
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("清空失败: %w（请先关闭占用该目录的浏览器）", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if m.log != nil {
		m.log.Info("browser", "已清空共享会话目录: %s", dir)
	}
	return nil
}

// safeProfileDir 防止误删磁盘根目录 / 用户主目录 / 数据目录
func safeProfileDir(dir string) error {
	clean := filepath.Clean(dir)
	if clean == "" || clean == "." {
		return errors.New("共享会话目录为空")
	}
	if clean == string(filepath.Separator) ||
		strings.EqualFold(clean, filepath.VolumeName(clean)+string(filepath.Separator)) {
		return errors.New("共享会话目录不能是磁盘根目录")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.EqualFold(clean, filepath.Clean(home)) {
		return errors.New("共享会话目录不能是用户主目录")
	}
	dataDir := filepath.Clean(config.DataPath())
	if strings.EqualFold(clean, dataDir) || isParentDir(clean, dataDir) {
		return errors.New("共享会话目录不能是数据目录本身或其上级目录")
	}
	return nil
}

func isParentDir(parent, child string) bool {
	parent = strings.TrimRight(parent, string(filepath.Separator))
	child = strings.ToLower(child)
	parent = strings.ToLower(parent)
	return child != parent && strings.HasPrefix(child, parent+string(filepath.Separator))
}

func dirStats(dir string) (int, int64) {
	var files int
	var size int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		files++
		if info, e := d.Info(); e == nil {
			size += info.Size()
		}
		return nil
	})
	return files, size
}

// 会话型 Cookie（没有 Expires 的）Chromium 退出时会丢弃，共享会话下需要在关闭前
// 单独快照到用户目录，下次启动再灌回去，否则「登录一次后续复用」对 Session Cookie 无效。
const cookieSnapshotName = "_session_cookies.json"

func cookieSnapshotPath(dir string) string { return filepath.Join(dir, cookieSnapshotName) }

func (m *Manager) saveCookies(b *rod.Browser, dir string) {
	cookies, err := b.Timeout(5 * time.Second).GetCookies()
	if err != nil {
		if m.log != nil {
			m.log.Warn("browser", "保存共享会话 Cookie 失败: %v", err)
		}
		return
	}
	list := make([]*proto.NetworkCookieParam, 0, len(cookies))
	now := time.Now().Unix()
	for _, c := range cookies {
		if c.Expires > 0 && int64(c.Expires) < now {
			continue
		}
		p := &proto.NetworkCookieParam{
			Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
			Secure: c.Secure, HTTPOnly: c.HTTPOnly, SameSite: c.SameSite,
		}
		if c.Expires > 0 {
			p.Expires = proto.TimeSinceEpoch(c.Expires)
		}
		list = append(list, p)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	p := cookieSnapshotPath(dir)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		if m.log != nil {
			m.log.Warn("browser", "写入共享会话 Cookie 失败: %v", err)
		}
		return
	}
	_ = os.Rename(tmp, p)
	if m.log != nil {
		m.log.Info("browser", "已保存共享会话 Cookie %d 个", len(list))
	}
}

func (m *Manager) loadCookies(b *rod.Browser, dir string) {
	data, err := os.ReadFile(cookieSnapshotPath(dir))
	if err != nil || len(data) == 0 {
		return
	}
	var list []*proto.NetworkCookieParam
	if err := json.Unmarshal(data, &list); err != nil {
		if m.log != nil {
			m.log.Warn("browser", "读取共享会话 Cookie 失败: %v", err)
		}
		return
	}
	now := time.Now().Unix()
	keep := make([]*proto.NetworkCookieParam, 0, len(list))
	for _, c := range list {
		if c == nil || c.Name == "" {
			continue
		}
		if c.Expires > 0 && int64(c.Expires) < now {
			continue
		}
		keep = append(keep, c)
	}
	if len(keep) == 0 {
		return
	}
	if err := b.SetCookies(keep); err != nil {
		if m.log != nil {
			m.log.Warn("browser", "恢复共享会话 Cookie 失败: %v", err)
		}
		return
	}
	if m.log != nil {
		m.log.Info("browser", "已恢复共享会话 Cookie %d 个", len(keep))
	}
}

func platformDir() (string, string) {
	switch runtime.GOOS + "_" + runtime.GOARCH {
	case "windows_amd64":
		return "Win_x64", "chrome-win.zip"
	case "windows_386":
		return "Win", "chrome-win.zip"
	case "darwin_amd64":
		return "Mac", "chrome-mac.zip"
	case "darwin_arm64":
		return "Mac_Arm", "chrome-mac.zip"
	case "linux_amd64":
		return "Linux_x64", "chrome-linux.zip"
	case "linux_arm64":
		return "Linux_arm64", "chrome-linux.zip"
	}
	return "Linux_x64", "chrome-linux.zip"
}

func (m *Manager) launcherBrowser() *launcher.Browser {
	b := launcher.NewBrowser()
	if m.cfg.Browser.Revision > 0 {
		b.Revision = m.cfg.Browser.Revision
	}
	b.RootDir = config.DataPath("browser")
	switch strings.ToLower(m.cfg.Browser.DownloadHost) {
	case "google":
		b.Hosts = []launcher.Host{launcher.HostGoogle}
	case "npm":
		b.Hosts = []launcher.Host{launcher.HostNPM, launcher.HostGoogle}
	case "custom":
		if m.cfg.Browser.CustomHost != "" {
			prefix := strings.TrimRight(m.cfg.Browser.CustomHost, "/")
			dir, zip := platformDir()
			b.Hosts = []launcher.Host{func(rev int) string {
				return fmt.Sprintf("%s/%s/%d/%s", prefix, dir, rev, zip)
			}}
		}
	}
	return b
}

// SystemCandidates 返回系统中已安装的浏览器路径
func SystemCandidates() []string {
	var list []string
	if runtime.GOOS == "windows" {
		envs := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")}
		rels := []string{
			`Google\Chrome\Application\chrome.exe`,
			`Microsoft\Edge\Application\msedge.exe`,
			`Chromium\Application\chrome.exe`,
			`BraveSoftware\Brave-Browser\Application\brave.exe`,
		}
		for _, e := range envs {
			if e == "" {
				continue
			}
			for _, r := range rels {
				list = append(list, filepath.Join(e, r))
			}
		}
	} else if runtime.GOOS == "darwin" {
		list = append(list,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		)
	} else {
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
			if p, err := exec.LookPath(name); err == nil {
				list = append(list, p)
			}
		}
	}
	return list
}

// Resolve 返回可用的浏览器可执行文件
func (m *Manager) Resolve() (string, string, error) {
	if p := m.cfg.Browser.BinPath; p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, "custom", nil
		}
	}
	if p := m.launcherBrowser().BinPath(); fileExists(p) {
		return p, "downloaded", nil
	}
	for _, p := range SystemCandidates() {
		if fileExists(p) {
			return p, "system", nil
		}
	}
	return "", "", errors.New("未找到可用浏览器内核，请在【设置-浏览器内核】中下载 Chromium 或指定可执行文件路径")
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func (m *Manager) Status() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	path, source, err := m.Resolve()
	status := map[string]any{
		"downloading":  m.downloading,
		"last_error":   m.lastErr,
		"platform":     runtime.GOOS + "/" + runtime.GOARCH,
		"download_dir": config.DataPath("browser"),
		"configured":   m.cfg.Browser.BinPath,
		"revision":     m.launcherBrowser().Revision,
	}
	for k, v := range m.SessionInfo() {
		status[k] = v
	}
	if err != nil {
		status["available"] = false
		status["message"] = err.Error()
	} else {
		status["available"] = true
		status["path"] = path
		status["source"] = source
	}
	return status
}

// Download 异步下载 Chromium 内核
func (m *Manager) Download() error {
	m.mu.Lock()
	if m.downloading {
		m.mu.Unlock()
		return errors.New("正在下载中，请稍候")
	}
	m.downloading = true
	m.lastErr = ""
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			m.downloading = false
			m.mu.Unlock()
		}()
		b := m.launcherBrowser()
		m.log.Info("browser", "开始下载 Chromium r%d（镜像: %s）", b.Revision, m.cfg.Browser.DownloadHost)
		start := time.Now()
		p, err := b.Get()
		if err != nil {
			m.mu.Lock()
			m.lastErr = err.Error()
			m.mu.Unlock()
			m.log.Error("browser", "浏览器内核下载失败: %v", err)
			return
		}
		m.log.Info("browser", "浏览器内核就绪: %s（耗时 %s）", p, time.Since(start).Round(time.Second))
	}()
	return nil
}

// Launch 启动一个新的浏览器实例（会话模式取全局配置）
func (m *Manager) Launch(headless bool, incognito bool) (*rod.Browser, func(), error) {
	return m.LaunchTask(headless, incognito, "")
}

// LaunchEphemeral 强制使用临时无痕会话（不受「共享会话」设置影响），用于探测 UA 等一次性操作
func (m *Manager) LaunchEphemeral(headless bool) (*rod.Browser, func(), error) {
	return m.launch(headless, true, SessionIsolated)
}

// LaunchTask 启动浏览器；modeOverride 为空时使用全局会话模式，任务级可单独指定
func (m *Manager) LaunchTask(headless bool, incognito bool, modeOverride string) (*rod.Browser, func(), error) {
	mode := strings.TrimSpace(modeOverride)
	if mode == "" {
		mode = m.SessionMode()
	}
	return m.launch(headless, incognito, NormalizeSessionMode(mode))
}

func (m *Manager) launch(headless bool, incognito bool, mode string) (*rod.Browser, func(), error) {
	bin, _, err := m.Resolve()
	if err != nil {
		return nil, nil, err
	}
	shared := mode == SessionShared
	var releaseSession func()
	br := m.cfg.Browser
	width, height := br.Width, br.Height
	if width <= 0 {
		width = 1440
	}
	if height <= 0 {
		height = 960
	}
	l := launcher.New().
		Bin(bin).
		Headless(headless).
		Leakless(false).
		Set("disable-blink-features", "AutomationControlled").
		Set("window-size", fmt.Sprintf("%d,%d", width, height))
	// 指纹：UA / 语言在启动参数层面设置，保证新开的标签页、弹窗也一致
	if ua := strings.TrimSpace(br.UserAgent); ua != "" {
		l = l.Set("user-agent", ua)
	}
	// Accept-Language 只能用启动参数设置：页面级 Network.setUserAgentOverride
	// 会与内核已有的语言列表拼接，出现 zh-CN,zh;q=0.9;q=0.9 这种重复 q 值。
	// 这里只同步 --lang（navigator.language），Accept-Language 请求头在
	// ApplyFingerprint 里用 setExtraHTTPHeaders 精确覆盖。
	if lang := strings.TrimSpace(br.AcceptLanguage); lang != "" {
		l = l.Set("lang", primaryLang(lang))
	} else {
		l = l.Set("lang", "zh-CN")
	}
	for _, raw := range br.ExtraArgs {
		a := strings.TrimSpace(raw)
		if a == "" || !strings.HasPrefix(a, "-") {
			continue
		}
		a = strings.TrimLeft(a, "-")
		if k, v, found := strings.Cut(a, "="); found {
			l = l.Set(flags.Flag(strings.TrimSpace(k)), strings.TrimSpace(v))
		} else {
			l = l.Set(flags.Flag(strings.TrimSpace(a)))
		}
	}
	if m.cfg.Browser.NoSandbox {
		l = l.NoSandbox(true).Set("disable-dev-shm-usage")
	}
	if m.cfg.Browser.Proxy != "" {
		l = l.Proxy(m.cfg.Browser.Proxy)
	}
	if incognito && !shared {
		l = l.Set("incognito")
	}
	if shared {
		dir := m.ProfileDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("创建共享会话目录失败: %w", err)
		}
		release, err := acquireSession(dir, 120*time.Second)
		if err != nil {
			return nil, nil, err
		}
		releaseSession = release
		// 固定用户目录 + 关闭首次运行向导，Cookie / localStorage / IndexedDB 会保留到下次运行
		l = l.UserDataDir(dir).Set("no-first-run").Set("no-default-browser-check")
		if m.log != nil {
			m.log.Info("browser", "共享会话模式：复用用户目录 %s（登录态会保留）", dir)
		}
	}
	l = l.Set("ignore-certificate-errors")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	u, err := l.Context(ctx).Launch()
	if err != nil {
		if releaseSession != nil {
			releaseSession()
		}
		return nil, nil, fmt.Errorf("启动浏览器失败: %w", err)
	}
	// 注意：这里不能用 b.Timeout(60s) 作为父上下文。rod 的事件订阅（goob）会继承该上下文，
	// 一旦连接超时上下文被取消（例如 CancelTimeout），所有 CDP 事件（Network/Page）都会静默丢失，
	// 导致录制拿不到任何请求。因此连接超时用独立 goroutine 守护，浏览器上下文保持 Background。
	b := rod.New().ControlURL(u)
	errCh := make(chan error, 1)
	go func() { errCh <- b.Connect() }()
	select {
	case err = <-errCh:
	case <-time.After(45 * time.Second):
		l.Kill()
		if releaseSession != nil {
			releaseSession()
		}
		return nil, nil, errors.New("连接浏览器超时（45s）")
	}
	if err != nil {
		l.Kill()
		if releaseSession != nil {
			releaseSession()
		}
		return nil, nil, fmt.Errorf("连接浏览器失败: %w", err)
	}
	if shared {
		// 把上次运行快照的 Cookie（含 Session Cookie）灌回浏览器，赶在页面导航之前
		m.loadCookies(b, m.ProfileDir())
	}
	cleanup := func() {
		if shared {
			// 共享模式：先快照 Cookie，再优雅关闭，最后只释放闸门（不删用户目录）
			m.saveCookies(b, m.ProfileDir())
			_ = b.Close()
			l.Kill()
			if releaseSession != nil {
				releaseSession()
			}
			return
		}
		_ = b.Close()
		l.Kill()
		l.Cleanup()
	}
	return b, cleanup, nil
}

// primaryLang 取语言列表里的第一项并去掉 q 权重，例如
// "zh-CN,zh;q=0.9,en;q=0.8" -> "zh-CN"，用于同步 --lang（navigator.language）。
func primaryLang(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, ",;"); i > 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// ApplyFingerprint 把「设置 → 浏览器指纹」里的 UA / 语言 / 平台 / 时区 / 视口
// 应用到一个新建页面上。全部留空时不注入任何覆盖，保持浏览器内核默认行为。
// 必须在页面导航之前调用，否则首个文档仍会用旧指纹加载。
func (m *Manager) ApplyFingerprint(page *rod.Page) {
	if m == nil || page == nil {
		return
	}
	br := m.cfg.Browser
	ua := strings.TrimSpace(br.UserAgent)
	platform := strings.TrimSpace(br.Platform)
	// 说明：Accept-Language 在 Launch 时用 --accept-lang 设置（见 primaryLang），
	// 这里不再传 AcceptLanguage，否则请求头会出现重复的 q 值。
	if ua != "" || platform != "" {
		if ua == "" {
			// 只覆盖语言/平台时，先取当前 UA，避免把 UA 置空
			if v, err := page.Eval("() => navigator.userAgent"); err == nil {
				ua = v.Value.Str()
			}
		}
		req := &proto.NetworkSetUserAgentOverride{UserAgent: ua}
		if platform != "" {
			req.Platform = platform
		}
		if err := page.SetUserAgent(req); err != nil && m.log != nil {
			m.log.Warn("browser", "应用 UA 覆盖失败（继续使用内核默认值）: %v", err)
		}
	}
	if tz := strings.TrimSpace(br.Timezone); tz != "" {
		if err := (proto.EmulationSetTimezoneOverride{TimezoneID: tz}).Call(page); err != nil && m.log != nil {
			m.log.Warn("browser", "应用时区覆盖失败: %v", err)
		}
	}
	// Accept-Language 直接覆盖请求头，避免与内核语言列表拼接出重复的 q 值
	if lang := strings.TrimSpace(br.AcceptLanguage); lang != "" {
		if undo, err := page.SetExtraHeaders([]string{"Accept-Language", lang}); err != nil {
			if m.log != nil {
				m.log.Warn("browser", "应用 Accept-Language 覆盖失败: %v", err)
			}
		} else {
			_ = undo
		}
	}
	if br.Width > 0 && br.Height > 0 {
		_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
			Width:             br.Width,
			Height:            br.Height,
			DeviceScaleFactor: 1,
			Mobile:            false,
		})
	}
}
