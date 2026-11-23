package browser

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"collect/internal/model"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const selectPageHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>分页条数</title></head>
<body>
<select id="size" name="pageSize">
  <option value="10">每页 10 条</option>
  <option value="20">每页 20 条</option>
  <option value="50">每页 50 条</option>
</select>
<div id="out">10</div>
<div id="changes">0</div>
<script>
var n = 0;
document.getElementById('size').addEventListener('change', function () {
  n++;
  document.getElementById('out').textContent = this.value;
  document.getElementById('changes').textContent = String(n);
});
</script>
</body></html>`

// testBrowser 启动本机浏览器（无头），找不到时跳过。
func testBrowser(t *testing.T) (*rod.Page, func()) {
	t.Helper()
	bin := ""
	for _, p := range SystemCandidates() {
		if fileExists(p) {
			bin = p
			break
		}
	}
	if bin == "" {
		t.Skip("未找到可用浏览器，跳过")
	}
	u, err := launcher.New().Bin(bin).Headless(true).Set("disable-gpu").Set("no-sandbox").Launch()
	if err != nil {
		t.Fatalf("启动浏览器失败: %v", err)
	}
	b := rod.New().ControlURL(u)
	if err := b.Connect(); err != nil {
		_ = u
		t.Fatalf("连接浏览器失败: %v", err)
	}
	page, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	cleanup := func() {
		_ = b.Close()
	}
	return page, cleanup
}

func serveSelectPage(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(selectPageHTML))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// 录制器必须把 <select> 的 change 录成 select 动作，并带上 option 的 value
func TestRecorderCapturesSelectChange(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveSelectPage(t)

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

	// 模拟用户在下拉里选「每页 50 条」
	if _, err := page.Eval(`() => {
		const el = document.getElementById('size');
		el.value = '50';
		el.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	}`); err != nil {
		t.Fatalf("模拟选择失败: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)

	var sel *Action
	rec.mu.Lock()
	for i := range rec.actions {
		if rec.actions[i].Type == "select" {
			a := rec.actions[i]
			sel = &a
		}
	}
	rec.mu.Unlock()
	if sel == nil {
		t.Fatalf("未录到 select 动作，实际动作: %+v", rec.Actions())
	}
	if sel.Value != "50" {
		t.Fatalf("select 动作的值 = %q，期望 50", sel.Value)
	}
	if !strings.Contains(sel.Selector, "size") {
		t.Fatalf("select 选择器异常: %q", sel.Selector)
	}
}

// 回放 select 动作必须真正改变下拉值，并触发一次 change
func TestReplaySelectAppliesValue(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveSelectPage(t)

	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	err := rep.Do(model.UIStep{Action: "select", Selector: "#size", Value: "50"})
	if err != nil {
		t.Fatalf("回放 select 失败: %v", err)
	}
	got, err := page.Eval(`() => ({
		value: document.getElementById('size').value,
		out: document.getElementById('out').textContent,
		changes: document.getElementById('changes').textContent,
	})`)
	if err != nil {
		t.Fatalf("读取结果失败: %v", err)
	}
	m := got.Value.Map()
	if m["value"].Str() != "50" {
		t.Fatalf("下拉值未生效: %v", m)
	}
	if m["out"].Str() != "50" {
		t.Fatalf("change 未生效，页面状态: %v", m)
	}
	if m["changes"].Str() != "1" {
		t.Fatalf("change 触发次数 = %s，期望 1", m["changes"].Str())
	}
}

// option 的 value 与显示文字不同（如 value=g50、文字「50 条/页」）时，
// 录到的是 value，回放也必须能选中。
func TestReplaySelectValueDiffersFromText(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<select id="size"><option value="g10">10 条/页</option><option value="g50">50 条/页</option></select>
<div id="out">g10</div>
<script>document.getElementById('size').addEventListener('change',function(){document.getElementById('out').textContent=this.value;});</script>
</body></html>`))
	}))
	defer srv.Close()
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	if err := rep.Do(model.UIStep{Action: "select", Selector: "#size", Value: "g50"}); err != nil {
		t.Fatalf("回放 select 失败: %v", err)
	}
	got, _ := page.Eval(`() => document.getElementById('out').textContent`)
	if got.Value.Str() != "g50" {
		t.Fatalf("value 与文字不同的下拉未生效: %s", got.Value.Str())
	}
}
