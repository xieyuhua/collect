package browser

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 组件库下拉（Element / AntD 风格）：点触发器打开 div 弹层，在 li 上 mousedown 就选中并关闭。
// 真实用户操作后页面里没有 click 动作，录制器必须从 mousedown 推断出选择。
const customDropdownHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>每页条数</title>
<style>
.dd { position: relative; display: inline-block; }
.dd .list { display: none; position: absolute; background: #fff; border: 1px solid #ccc; }
.dd.open .list { display: block; }
.dd li { padding: 4px 12px; cursor: pointer; }
</style></head>
<body>
<div id="sizeDD" class="dd">
  <button id="sizeTrigger" type="button">20 条/页</button>
  <ul class="list">
    <li data-v="10">10 条/页</li>
    <li data-v="20">20 条/页</li>
    <li data-v="50">50 条/页</li>
  </ul>
</div>
<div id="out">20</div>
<script>
var dd = document.getElementById('sizeDD');
var trigger = document.getElementById('sizeTrigger');
var out = document.getElementById('out');
trigger.addEventListener('click', function () { dd.classList.toggle('open'); });
Array.prototype.forEach.call(dd.querySelectorAll('li'), function (li) {
  // 很多组件库在 mousedown 时就提交选择并关闭弹层，click 事件随即失效
  li.addEventListener('mousedown', function (e) {
    e.preventDefault();
    dd.classList.remove('open');
    trigger.textContent = li.textContent;
    out.textContent = li.getAttribute('data-v');
  });
});
// 点空白关闭
document.addEventListener('mousedown', function (e) {
  if (!dd.contains(e.target)) dd.classList.remove('open');
});
</script>
</body></html>`

// 原生 select 选完立即整页跳转：动作必须在跳转前落到持久存储，否则新文档里 window.__rec 就没了
const selectNavHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>跳转</title></head>
<body>
<select id="size">
  <option value="10">10</option>
  <option value="20">20</option>
  <option value="50">50</option>
</select>
<script>
document.getElementById('size').addEventListener('change', function () {
  location.href = '/next.html?size=' + this.value;
});
</script>
</body></html>`

func serveHTML(t *testing.T, pages map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func findAction(rec *Recorder, typ string) *Action {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i := range rec.actions {
		if rec.actions[i].Type == typ {
			a := rec.actions[i]
			return &a
		}
	}
	return nil
}

// 组件库下拉：只在 li 上 mousedown，没有 click，录制器也要产出 select 动作
func TestRecorderCapturesCustomDropdownMousedown(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": customDropdownHTML})

	rec := NewRecorder(page, 256)
	if err := rec.Start(); err != nil {
		t.Fatalf("启动录制失败: %v", err)
	}
	defer rec.Stop()
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()
	time.Sleep(500 * time.Millisecond)

	// 用户：点触发器展开 -> 在 li 上按下鼠标（mousedown 即选中）
	if _, err := page.Eval(`() => {
		document.getElementById('sizeTrigger').click();
		const li = document.querySelectorAll('#sizeDD li')[2];
		li.dispatchEvent(new MouseEvent('mousedown', {bubbles: true, cancelable: true, clientX: 10, clientY: 10}));
		return true;
	}`); err != nil {
		t.Fatalf("模拟操作失败: %v", err)
	}
	time.Sleep(1400 * time.Millisecond)

	sel := findAction(rec, "select")
	if sel == nil {
		t.Fatalf("组件库下拉未录到 select 动作，实际动作: %+v", rec.Actions())
	}
	if sel.Value != "50" || sel.Text != "50 条/页" {
		t.Fatalf("下拉选项识别错误: value=%q text=%q", sel.Value, sel.Text)
	}
	if sel.Selector == "" {
		t.Fatalf("下拉动作缺少选择器")
	}
	// trigger_text 必须与 Go 端 json tag 对齐（snake_case），
	// 之前写成驼峰 triggerText 会被反序列化静默丢弃
	if sel.TriggerText != "20 条/页" {
		t.Fatalf("触发器文字丢失（json 字段名应写 trigger_text）: %+v", sel)
	}
	if sel.Trigger != "#sizeTrigger" {
		t.Fatalf("触发器选择器丢失: %+v", sel)
	}
}

// 原生 select 选完立即跳转：完成后动作仍然要在
func TestRecorderKeepsSelectAcrossNavigation(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{
		"/":          selectNavHTML,
		"/next.html": `<!doctype html><html><body><div id="done">ok</div></body></html>`,
	})

	rec := NewRecorder(page, 256)
	if err := rec.Start(); err != nil {
		t.Fatalf("启动录制失败: %v", err)
	}
	defer rec.Stop()
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()
	time.Sleep(500 * time.Millisecond)

	if _, err := page.Eval(`() => {
		const el = document.getElementById('size');
		el.value = '50';
		el.dispatchEvent(new Event('change', {bubbles: true}));
		return true;
	}`); err != nil {
		t.Fatalf("模拟选择失败: %v", err)
	}
	// 等跳转完成 + 至少一轮轮询
	time.Sleep(2000 * time.Millisecond)

	sel := findAction(rec, "select")
	if sel == nil {
		t.Fatalf("跳转后 select 动作丢失，实际动作: %+v", rec.Actions())
	}
	if sel.Value != "50" {
		t.Fatalf("select 值错误: %q", sel.Value)
	}
}
