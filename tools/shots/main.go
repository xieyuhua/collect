// 临时工具：对后台各页面截图，用于人工核对 UI
// 用法：go run ./tools/shots http://127.0.0.1:18080 E:\temp\shots
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"collect/internal/applog"
	"collect/internal/browser"
	"collect/internal/config"

	"github.com/go-rod/rod/lib/proto"
)

func main() {
	base := "http://127.0.0.1:18080"
	out := filepath.Join(os.TempDir(), "collect-shots")
	if len(os.Args) > 1 {
		base = os.Args[1]
	}
	if len(os.Args) > 2 {
		out = os.Args[2]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		panic(err)
	}

	cfg := config.Load()
	log := applog.New(config.DataPath("logs"))
	bm := browser.NewManager(cfg, log)
	b, cleanup, err := bm.Launch(true, true)
	if err != nil {
		fmt.Println("启动浏览器失败:", err)
		os.Exit(1)
	}
	defer cleanup()

	page, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		fmt.Println("创建页面失败:", err)
		os.Exit(1)
	}
	page = page.Timeout(60 * time.Second)
	_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{Width: 1560, Height: 1000, DeviceScaleFactor: 1})
	if err := page.Navigate(base + "/#/dashboard"); err != nil {
		fmt.Println("打开首页失败:", err)
		os.Exit(1)
	}
	_ = page.WaitLoad()
	time.Sleep(1500 * time.Millisecond)

	shot := func(name string) {
		p := filepath.Join(out, name+".png")
		if err := page.MustScreenshot(p); err != nil {
			fmt.Println("截图失败", name, err)
			return
		}
		fmt.Println("已保存", p)
	}

	routes := []string{"dashboard", "tasks", "studio", "runs", "datasources", "logs", "notifications", "settings"}
	for _, rt := range routes {
		_, _ = page.Eval(`(h) => { location.hash = h; return true }`, "#/"+rt)
		time.Sleep(1100 * time.Millisecond)
		shot(rt)
	}

	// 任务编辑器各页签
	_, _ = page.Eval(`() => { location.hash = '#/task/new'; return true }`)
	time.Sleep(1500 * time.Millisecond)
	shot("editor-basic")
	for _, tab := range []string{"vars", "steps", "ui", "login", "mapping", "sink", "pagination", "dedupe"} {
		_, _ = page.Eval(`(t) => { const b = document.querySelector('[data-act="task-tab"][data-tab="'+t+'"]'); if (b) b.click(); return true }`, tab)
		time.Sleep(400 * time.Millisecond)
		// 需要先添加一条，避免空态截图看不出卡片
		switch tab {
		case "steps":
			_, _ = page.Eval(`() => { const b = document.querySelector('[data-act="add-step"]'); if (b) b.click(); return true }`)
		case "ui":
			_, _ = page.Eval(`() => { const b = document.querySelector('[data-act="add-ui"][data-kind="ui"]'); if (b) b.click(); return true }`)
		case "login":
			_, _ = page.Eval(`() => { const b = document.querySelector('[data-act="add-ui"][data-kind="login"]'); if (b) b.click(); return true }`)
		case "mapping":
			_, _ = page.Eval(`() => { const b = document.querySelector('[data-act="add-map"]'); if (b) b.click(); return true }`)
		case "vars":
			_, _ = page.Eval(`() => { const b = document.querySelector('[data-act="add-var"]'); if (b) b.click(); return true }`)
		}
		time.Sleep(500 * time.Millisecond)
		shot("editor-" + tab)
	}
}
