package browser

import (
	"testing"
	"time"
)

// 高度还原 Element UI / AntD 的真实行为：
//   - 触发器点击后，弹层通过 portal 挂到 body，position:absolute
//   - 选项 li 只监听 mousedown（click 被 preventDefault / 失焦吞掉）
//   - mousedown 时提交选择并立即隐藏弹层
//
// 这是"录制时点 li 没录到任何东西"的最可能真实场景。
const elementUiLikeHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>Element 风格</title>
<style>
.pg-trigger { display:inline-block; padding:4px 12px; border:1px solid #ccc; cursor:pointer; }
.pg-popper { position:absolute; z-index:9999; background:#fff; border:1px solid #dcdfe6; box-shadow:0 2px 12px rgba(0,0,0,.1); display:none; padding:4px 0; }
.pg-popper.show { display:block; }
.pg-popper .pg-item { padding:6px 20px; cursor:pointer; font-size:14px; }
.pg-popper .pg-item:hover { background:#f5f7fa; }
</style></head>
<body>
<div class="pg-trigger" id="pgTrigger">20 条/页</div>
<table><tbody><tr><td>rowA</td></tr></tbody></table>
<div id="state">20</div>
<script>
var trig = document.getElementById('pgTrigger');
var popper = document.createElement('div');
popper.className = 'pg-popper';
['10','20','50'].forEach(function (v) {
  var item = document.createElement('div');
  item.className = 'pg-item';
  item.textContent = v + ' 条/页';
  // 关键：Element 用 mousedown 提交选择（click 在 blur 时被吞）
  item.addEventListener('mousedown', function (e) {
    e.preventDefault();
    e.stopPropagation();
    document.getElementById('state').textContent = v;
    trig.textContent = v + ' 条/页';
    popper.classList.remove('show');
  });
  popper.appendChild(item);
});
document.body.appendChild(popper);
trig.addEventListener('click', function (e) {
  e.stopPropagation();
  var r = trig.getBoundingClientRect();
  popper.style.left = r.left + 'px';
  popper.style.top = (r.bottom + window.scrollY) + 'px';
  popper.classList.toggle('show');
});
document.addEventListener('mousedown', function (e) {
  if (!popper.contains(e.target) && e.target !== trig) popper.classList.remove('show');
});
</script>
</body></html>`

func TestRecorderCapturesElementUiLikeMousedownOption(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": elementUiLikeHTML})

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

	// 用户：点触发器打开 → 在「50 条/页」上按鼠标（mousedown 即提交）
	if _, err := page.Eval(`() => {
		document.getElementById('pgTrigger').click();
		const item = document.querySelectorAll('.pg-popper .pg-item')[2];
		item.dispatchEvent(new MouseEvent('mousedown', {bubbles:true, cancelable:true, clientX:10, clientY:10}));
		return true;
	}`); err != nil {
		t.Fatalf("模拟操作失败: %v", err)
	}
	time.Sleep(1400 * time.Millisecond)

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
		t.Fatalf("Element 风格 mousedown 选项未录到 select/click，动作: %+v", rec.Actions())
	}
	if sel.Text != "50 条/页" {
		t.Fatalf("选项文字错误: %q", sel.Text)
	}
}
