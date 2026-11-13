package studio

import (
	"testing"
	"time"

	"collect/internal/browser"
	"collect/internal/model"
)

// iframe 内录到的动作，生成草稿时要把 frame 选择器带到 UI 步骤里，
// 否则回放时会到主页面找元素而失败。
func TestBuildDraftUIKeepsFrame(t *testing.T) {
	base := time.Now().Add(-time.Minute)
	actions := []browser.Action{
		{Type: "input", Selector: "#user", Value: "demo", Frame: "#mainFrame", At: base.UnixMilli()},
		{Type: "input", Selector: "#pass", Value: "123456", Frame: "#mainFrame", At: base.Add(time.Second).UnixMilli()},
		{Type: "click", Selector: "#submit", Text: "登录", Frame: "#mainFrame", At: base.Add(2 * time.Second).UnixMilli()},
	}
	s := &Session{ID: "s1", Mode: "ui", StartURL: "https://demo.test/outer.html"}
	task, err := buildDraftFrom(s, DraftRequest{Mode: "ui"}, demoRequests(), actions)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	steps := task["ui_steps"].([]model.UIStep)
	if len(steps) != 4 { // 打开首页 + 3 个动作
		t.Fatalf("期望 4 个 UI 步骤，实际 %d", len(steps))
	}
	if steps[0].Action != "open" || steps[0].Frame != "" {
		t.Errorf("第 1 步应是主页面的打开页: %+v", steps[0])
	}
	for i, st := range steps[1:] {
		if st.Frame != "#mainFrame" {
			t.Errorf("第 %d 步缺少 iframe 选择器: %+v", i+2, st)
		}
	}
}
