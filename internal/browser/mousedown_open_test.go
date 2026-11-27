package browser

import (
	"testing"
	"time"
)

// 有些组件库在「触发器 mousedown」时就展开弹层（比 click 更早）。
// 之前快照只在 click 时拍摄，等 click 到来时弹层已可见并被记入快照，
// 导致随后点选项被误判为「常驻布局」而漏录。
const mousedownOpenHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>mousedown 展开</title>
<style>
.trig { display:inline-block; padding:4px 12px; border:1px solid #ccc; cursor:pointer; }
.panel { position:absolute; z-index:99; background:#fff; border:1px solid #ccc; display:none; padding:4px 0; }
.panel.on { display:block; }
.panel .cell { padding:5px 18px; cursor:pointer; }
</style></head>
<body>
<div class="trig" id="trig">20 条/页</div>
<div id="state">20</div>
<script>
var trig = document.getElementById('trig');
var panel = document.createElement('div');
panel.className = 'panel'; panel.id = 'panel';
['10','20','50'].forEach(function (v) {
  var cell = document.createElement('div');
  cell.className = 'cell';
  cell.textContent = v + ' 条/页';
  cell.addEventListener('mousedown', function (e) {
    e.preventDefault();
    document.getElementById('state').textContent = v;
    panel.classList.remove('on');
  });
  panel.appendChild(cell);
});
document.body.appendChild(panel);
// 关键：在 mousedown 就展开（不是 click）
trig.addEventListener('mousedown', function (e) {
  e.preventDefault();
  var r = trig.getBoundingClientRect();
  panel.style.left = r.left + 'px';
  panel.style.top = (r.bottom + window.scrollY) + 'px';
  panel.classList.add('on');
});
document.addEventListener('mousedown', function (e) {
  if (!panel.contains(e.target) && e.target !== trig) panel.classList.remove('on');
});
</script>
</body></html>`

func TestRecorderCapturesOptionOpenedByMousedown(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": mousedownOpenHTML})

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

	// 完整真实鼠标序列：mousedown 触发器（展开）→ mouseup → click → mousedown 选项
	if _, err := page.Eval(`() => {
		const fire = (el, down) => {
			const r = el.getBoundingClientRect();
			const opt = {bubbles:true, cancelable:true, clientX:r.left+r.width/2, clientY:r.top+r.height/2};
			el.dispatchEvent(new MouseEvent('mousedown', opt));
			el.dispatchEvent(new MouseEvent('mouseup', opt));
			if (!down) el.dispatchEvent(new MouseEvent('click', opt));
		};
		fire(document.getElementById('trig'), false);
		fire(document.querySelectorAll('#panel .cell')[2], true);
		return true;
	}`); err != nil {
		t.Fatalf("模拟操作失败: %v", err)
	}
	time.Sleep(1400 * time.Millisecond)

	sel := findAction(rec, "select")
	if sel == nil {
		t.Fatalf("mousedown 展开的下拉未录到 select，动作: %+v", rec.Actions())
	}
	if sel.Text != "50 条/页" {
		t.Fatalf("选项文字错误: %q", sel.Text)
	}
}
