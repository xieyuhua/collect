package main

// 录制工作台集成自测：启动录制会话 -> 模拟浏览器操作 -> 校验捕获的请求/动作 -> 生成任务草稿。
// 用法：先启动 tools/mocksite.py，然后 go run ./tools/rectest

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"collect/internal/applog"
	"collect/internal/browser"
	"collect/internal/config"
	"collect/internal/studio"
)

func main() {
	cfg := config.Load()
	log := applog.New(config.DataPath("logs"))
	bm := browser.NewManager(cfg, log)
	mgr := studio.NewManager(cfg, log, bm)
	defer mgr.CloseAll()

	s, err := mgr.Start("http://127.0.0.1:18099/login.html", "api")
	if err != nil {
		fmt.Println("启动会话失败:", err)
		os.Exit(1)
	}
	defer mgr.Stop(s.ID)

	page := s.Page
	if err := page.Timeout(25*time.Second).WaitLoad(); err != nil {
		fmt.Println("等待登录页失败:", err)
	}
	time.Sleep(500 * time.Millisecond)
	page.Timeout(10*time.Second).MustElement("#user").MustInput("demo")
	page.Timeout(10*time.Second).MustElement("#pass").MustInput("123456")
	page.Timeout(10*time.Second).MustElement("#submit").MustClick()
	time.Sleep(3 * time.Second)
	page.Timeout(15*time.Second).MustElement("#kw").MustInput("北京")
	page.Timeout(10*time.Second).MustElement("#search").MustClick()
	time.Sleep(2 * time.Second)

	cap, err := mgr.Capture(s.ID)
	if err != nil {
		fmt.Println("采集失败:", err)
		os.Exit(1)
	}
	reqs := cap["requests"].([]map[string]any)
	acts := cap["actions"].([]browser.Action)
	jsonReqs, withBody := 0, 0
	for _, r := range reqs {
		if r["is_json"] == true {
			jsonReqs++
		}
	}
	fmt.Printf("捕获请求 %d 个（JSON %d 个），页面动作 %d 个\n", len(reqs), jsonReqs, len(acts))
	for _, r := range reqs {
		if r["is_json"] == true {
			fmt.Printf("  - %s %s\n", r["method"], r["url"])
		}
	}
	for _, a := range acts {
		fmt.Printf("  动作: %s %s %s\n", a.Type, a.Selector, a.Value)
	}
	_ = withBody

	draft, err := mgr.BuildDraft(s.ID, studio.DraftRequest{Mode: "api"})
	if err != nil {
		fmt.Println("生成草稿失败:", err)
		os.Exit(1)
	}
	steps := draft["http_steps"]
	b, _ := json.MarshalIndent(map[string]any{
		"name": draft["name"], "mode": draft["mode"], "http_steps": steps,
		"mapping": draft["mapping"], "pagination": draft["pagination"], "dedupe": draft["dedupe"],
	}, "", "  ")
	fmt.Println("草稿:", string(b)[:min(len(b), 6000)])

	raw, _ := json.Marshal(steps)
	var stepList []map[string]any
	_ = json.Unmarshal(raw, &stepList)
	mapsRaw, _ := json.Marshal(draft["mapping"])
	var mapList []map[string]any
	_ = json.Unmarshal(mapsRaw, &mapList)
	fmt.Printf("草稿接口步骤 %d 个，字段映射 %d 个\n", len(stepList), len(mapList))
	ok := len(reqs) >= 3 && jsonReqs >= 2 && len(acts) >= 3 && len(stepList) >= 2 && len(mapList) >= 3
	if !ok {
		fmt.Println("结果: 失败")
		os.Exit(1)
	}
	fmt.Println("结果: 通过")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
