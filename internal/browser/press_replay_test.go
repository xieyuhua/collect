package browser

import (
	"testing"
	"time"

	"collect/internal/model"
)

// 场景 1：只有 keydown 监听（无表单）——真实按键应触发。
const pressKeydownHTML = `<!doctype html><html><body>
<ul><li><input id="kw"></li></ul>
<div id="out">none</div>
<script>
document.getElementById('kw').addEventListener('keydown', function (e) {
  if (e.key === 'Enter') document.getElementById('out').textContent = 'keydown-enter';
});
</script>
</body></html>`

// 场景 2：回车靠表单提交触发（很多查询系统）。
const pressFormHTML = `<!doctype html><html><body>
<form id="f" onsubmit="document.getElementById('out').textContent='submitted';return false;">
  <ul><li><input id="kw" name="kw"></li></ul>
</form>
<div id="out">none</div>
</body></html>`

// 场景 3：既无 keydown 也无表单，回车靠相邻「查询」按钮（回放必须兜底点击）。
const pressAdjacentBtnHTML = `<!doctype html><html><body>
<div><ul><li><input id="kw"></li></ul><button id="q">查询</button></div>
<div id="out">none</div>
<script>
document.getElementById('q').addEventListener('click', function () {
  document.getElementById('out').textContent = 'clicked-query';
});
</script>
</body></html>`

func pressOn(t *testing.T, html, selector string) string {
	t.Helper()
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": html})
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()
	rep := NewReplayer(page, 256, nil)
	if err := rep.Do(model.UIStep{Action: "press", Selector: selector, Key: "Enter"}); err != nil {
		t.Fatalf("press 失败: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	got, _ := page.Eval(`() => document.getElementById('out').textContent`)
	return got.Value.Str()
}

func TestPressTriggersKeydownHandler(t *testing.T) {
	if got := pressOn(t, pressKeydownHTML, "ul > li > input"); got != "keydown-enter" {
		t.Fatalf("keydown 未触发，out=%s", got)
	}
}

func TestPressSubmitsForm(t *testing.T) {
	if got := pressOn(t, pressFormHTML, "form > ul > li > input"); got != "submitted" {
		t.Fatalf("表单未提交，out=%s", got)
	}
}

func TestPressFallsBackToAdjacentButton(t *testing.T) {
	if got := pressOn(t, pressAdjacentBtnHTML, "div > ul > li > input"); got != "clicked-query" {
		t.Fatalf("相邻查询按钮未点击，out=%s", got)
	}
}
