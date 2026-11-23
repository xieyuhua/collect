package browser

import (
	"testing"

	"collect/internal/model"
)

// 自定义下拉（Element 风格）：选项在 mousedown 时才提交。
// 回放时必须先点开触发器，再在弹层里选中目标选项。
func TestReplayCustomDropdownSelect(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": customDropdownHTML})

	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	step := model.UIStep{
		Action:      "select",
		Trigger:     "#sizeTrigger",
		TriggerText: "20 条/页",
		Selector:    "#sizeDD li:nth-of-type(3)",
		Value:       "50",
		Text:        "50 条/页",
	}
	if err := rep.Do(step); err != nil {
		t.Fatalf("回放自定义下拉失败: %v", err)
	}
	got, err := page.Eval(`() => ({
		out: document.getElementById('out').textContent,
		label: document.getElementById('sizeTrigger').textContent.trim(),
		open: document.getElementById('sizeDD').classList.contains('open'),
	})`)
	if err != nil {
		t.Fatalf("读取结果失败: %v", err)
	}
	m := got.Value.Map()
	if m["out"].Str() != "50" {
		t.Fatalf("下拉未生效: %+v", m)
	}
	if m["label"].Str() != "50 条/页" {
		t.Fatalf("触发器文字未更新: %+v", m)
	}
	if m["open"].Bool() {
		t.Fatalf("选中后弹层应关闭: %+v", m)
	}
}

// 只带触发器、没有选项选择器时，按 value 在弹层里找选项
func TestReplayCustomDropdownByValueOnly(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": customDropdownHTML})

	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	if err := rep.Do(model.UIStep{Action: "select", Trigger: "#sizeTrigger", Value: "10"}); err != nil {
		t.Fatalf("回放失败: %v", err)
	}
	got, _ := page.Eval(`() => document.getElementById('out').textContent`)
	if got.Value.Str() != "10" {
		t.Fatalf("按 value 选择未生效: %s", got.Value.Str())
	}
}
