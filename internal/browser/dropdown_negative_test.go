package browser

import (
	"testing"

	"collect/internal/model"
)

// 回放「select 指向 div 容器」绝不能假成功：之前给容器赋 value 并派发
// change 后直接返回 nil，页面毫无变化但任务显示成功。
func TestReplaySelectOnNonSelectElementFails(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": customDropdownHTML})

	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	// 选择器指向弹层 li，但弹层未展开：不该谎报成功
	err := rep.Do(model.UIStep{Action: "select", Selector: "#sizeDD li:nth-of-type(2)", Value: "20"})
	if err == nil {
		var out string
		if v, e := page.Eval(`() => document.getElementById('out').textContent`); e == nil {
			out = v.Value.Str()
		}
		t.Fatalf("容器/未展开场景不应成功（页面 out=%s）", out)
	}
}

// 弹层里选中的必须是选项本身：用「包含全部选项文字的容器选择器」
// 不能命中容器，否则点容器等于没选。
func TestReplaySelectDirectSelectorRequiresOptionSemantics(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": customDropdownHTML})

	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	// #sizeDD 是容器，innerText 包含所有选项文字， indexOf 会匹配上——必须拒绝
	err := rep.Do(model.UIStep{Action: "select", Selector: "#sizeDD", Value: "50"})
	if err == nil {
		var out string
		if v, e := page.Eval(`() => document.getElementById('out').textContent`); e == nil {
			out = v.Value.Str()
		}
		if out != "20" {
			t.Fatalf("容器选择器误命中导致页面状态改变: out=%s", out)
		}
	}
}
