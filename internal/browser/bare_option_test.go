package browser

import (
	"testing"
	"time"

	"collect/internal/model"
)

// 真实站点常见形态：弹层选项是「裸 span/li」，没有任何 data-* 属性或 role，
// 且组件库在 mousedown 时就提交选择并立即把选项从 DOM 里移除（click 永不触发）。
// 之前严格判定会把这类下拉完全漏录，用户表现为「分页选 10/20 没有任何动作」。
const bareOptionHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>裸选项下拉</title>
<style>
.combo { position: relative; display: inline-block; }
.combo .panel { display: none; position: absolute; top: 100%; left: 0; background: #fff; border: 1px solid #ccc; }
.combo.open .panel { display: block; }
.combo .panel .item { padding: 5px 16px; cursor: pointer; display: block; }
</style></head>
<body>
<div class="combo" id="pager">
  <button id="pagerTrigger" type="button">每页 20 条</button>
  <div class="panel" id="pagerPanel">
    <span class="item">每页 10 条</span>
    <span class="item">每页 20 条</span>
    <span class="item">每页 50 条</span>
  </div>
</div>
<div id="state">20</div>
<script>
var combo = document.getElementById('pager');
document.getElementById('pagerTrigger').addEventListener('click', function () {
  combo.classList.toggle('open');
});
Array.prototype.forEach.call(document.querySelectorAll('#pagerPanel .item'), function (item) {
  // mousedown 提交选择 + 立即移除该元素（真实组件库的常见做法）
  item.addEventListener('mousedown', function (e) {
    e.preventDefault();
    e.stopPropagation();
    document.getElementById('state').textContent = (item.textContent.match(/\d+/) || ['20'])[0];
    combo.classList.remove('open');
    item.remove(); // 元素消失，随后的 click 不会落到它上面
  });
});
document.addEventListener('mousedown', function (e) {
  if (!combo.contains(e.target)) combo.classList.remove('open');
});
</script>
</body></html>`

// 裸 span 选项（无 data-value / role），mousedown 即选且元素被移除：
// 必须录成 select 动作，并带上触发器与选项文字。
func TestRecorderCapturesBareSpanOptionOnMousedown(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": bareOptionHTML})

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
		document.getElementById('pagerTrigger').click();
		const item = document.querySelectorAll('#pagerPanel .item')[2]; // 每页 50 条
		item.dispatchEvent(new MouseEvent('mousedown', {bubbles: true, cancelable: true, clientX: 10, clientY: 10}));
		return true;
	}`); err != nil {
		t.Fatalf("模拟操作失败: %v", err)
	}
	time.Sleep(1400 * time.Millisecond)

	sel := findAction(rec, "select")
	if sel == nil {
		t.Fatalf("裸 span 选项未录到 select，动作: %+v", rec.Actions())
	}
	if sel.Text != "每页 50 条" {
		t.Fatalf("选项文字错误: %q", sel.Text)
	}
	if sel.Trigger != "#pagerTrigger" {
		t.Fatalf("触发器错误: %q", sel.Trigger)
	}
	// 不能同时多出一条重复的 click（mousedown 已记录选择）
	for _, a := range rec.Actions() {
		if a.Type == "click" && a.Selector == "#pagerTrigger" {
			continue // 触发器点击是正常的
		}
		if a.Type == "click" && a.Text == "每页 50 条" {
			t.Fatalf("选项被重复录成了 click: %+v", a)
		}
	}
}

// 回放这条裸 span 选择：先点触发器，再按文字点选项。
func TestReplayBareSpanOption(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": bareOptionHTML})

	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	step := model.UIStep{
		Action: "select", Trigger: "#pagerTrigger", TriggerText: "每页 20 条",
		Text: "每页 50 条", Value: "每页 50 条",
	}
	if err := rep.Do(step); err != nil {
		t.Fatalf("回放裸 span 选择失败: %v", err)
	}
	got, _ := page.Eval(`() => document.getElementById('state').textContent`)
	if got.Value.Str() != "50" {
		t.Fatalf("回放后页面状态 = %s，期望 50", got.Value.Str())
	}
}
