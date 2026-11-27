package browser

import (
	"testing"
	"time"

	"collect/internal/model"

	"github.com/go-rod/rod/lib/proto"
)

// 三种真实网站常见的「每页条数」下拉形态，验证录制 → 回放全链路：
//  1. 原生 <select>
//  2. div 弹层（选项是 div/span，AntD 风格）
//  3. ul/li 弹层（选项是 li）
const selectFormHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>原生 select</title></head>
<body>
<select id="sizeSel">
  <option value="10">10 条/页</option>
  <option value="20" selected>20 条/页</option>
  <option value="50">50 条/页</option>
</select>
<div id="state">20</div>
<script>
document.getElementById('sizeSel').addEventListener('change', function () {
  document.getElementById('state').textContent = this.value;
});
</script>
</body></html>`

const divFormHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>div 弹层</title>
<style>
.sel { position: relative; display: inline-block; }
.sel .dropdown { display: none; position: absolute; top: 100%; left: 0; background: #fff; border: 1px solid #ddd; z-index: 10; }
.sel.open .dropdown { display: block; }
.sel .dropdown .opt { padding: 5px 16px; cursor: pointer; font-size: 14px; }
.sel .opt:hover, .sel .opt.active { background: #f5f5f5; }
</style></head>
<body>
<div class="sel" id="sizeSel2">
  <div class="trigger" id="trigger2">20 条/页</div>
  <div class="dropdown" id="dd2">
    <div class="opt" data-value="10">10 条/页</div>
    <div class="opt active" data-value="20">20 条/页</div>
    <div class="opt" data-value="50">50 条/页</div>
  </div>
</div>
<div id="state2">20</div>
<script>
var sel = document.getElementById('sizeSel2');
var dd = document.getElementById('dd2');
document.getElementById('trigger2').addEventListener('click', function () { sel.classList.toggle('open'); });
Array.prototype.forEach.call(dd.querySelectorAll('.opt'), function (opt) {
  opt.addEventListener('mousedown', function (e) {
    e.preventDefault();
    sel.classList.remove('open');
    document.getElementById('trigger2').textContent = opt.textContent;
    document.getElementById('state2').textContent = opt.getAttribute('data-value');
  });
});
document.addEventListener('mousedown', function (e) {
  if (!sel.contains(e.target)) sel.classList.remove('open');
});
</script>
</body></html>`

const ulFormHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>ul li 弹层</title>
<style>
.wrap { position: relative; display: inline-block; }
.wrap ul { display: none; position: absolute; top: 100%; left: 0; margin: 0; padding: 0; list-style: none;
  background: #fff; border: 1px solid #ddd; z-index: 10; }
.wrap.open ul { display: block; }
.wrap ul li { padding: 5px 16px; cursor: pointer; }
</style></head>
<body>
<div class="wrap" id="sizeSel3">
  <button id="trigger3">20 条/页</button>
  <ul id="dd3">
    <li data-v="10">10 条/页</li>
    <li data-v="20">20 条/页</li>
    <li data-v="50">50 条/页</li>
  </ul>
</div>
<div id="state3">20</div>
<script>
var wrap = document.getElementById('sizeSel3');
document.getElementById('trigger3').addEventListener('click', function () { wrap.classList.toggle('open'); });
Array.prototype.forEach.call(document.querySelectorAll('#dd3 li'), function (li) {
  li.addEventListener('mousedown', function (e) {
    e.preventDefault();
    wrap.classList.remove('open');
    document.getElementById('trigger3').textContent = li.textContent;
    document.getElementById('state3').textContent = li.getAttribute('data-v');
  });
});
document.addEventListener('mousedown', function (e) {
  if (!wrap.contains(e.target)) wrap.classList.remove('open');
});
</script>
</body></html>`

// 全链路：录制（点开触发器 + 选选项）→ 检查动作 → 回放
// native=true 表示原生 <select>（真实鼠标点开 + change 派发）；
// false 表示自定义弹层（点触发器 + mousedown 选项）。
func testFormEndToEnd(t *testing.T, pages map[string]string, native bool, triggerSel, optionSel, wantVal, stateSel string) {
	t.Helper()
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, pages)

	// ---- 录制 ----
	rec := NewRecorder(page, 256)
	if err := rec.Start(); err != nil {
		t.Fatalf("启动录制失败: %v", err)
	}
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()
	time.Sleep(500 * time.Millisecond)

	if native {
		// 原生 select：真实鼠标点开，再 JS 改值 + change（两步，中间给 popup 时间）
		loc, err := page.Eval(`(sel) => {
			const r = document.querySelector(sel).getBoundingClientRect();
			return {x: r.left + r.width / 2, y: r.top + r.height / 2};
		}`, triggerSel)
		if err != nil {
			t.Fatalf("定位 select 失败: %v", err)
		}
		m := loc.Value.Map()
		if err := page.Mouse.MoveTo(proto.NewPoint(float64(m["x"].Int()), float64(m["y"].Int()))); err != nil {
			t.Fatalf("鼠标移动失败: %v", err)
		}
		if err := page.Mouse.Click(proto.InputMouseButtonLeft, 1); err != nil {
			t.Fatalf("鼠标点击失败: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		if _, err := page.Eval(`() => {
			const el = document.querySelector('select');
			el.value = '50';
			el.dispatchEvent(new Event('change', {bubbles: true}));
			return true;
		}`); err != nil {
			t.Fatalf("派发 change 失败: %v", err)
		}
	} else {
		// 自定义弹层：点触发器 + mousedown 选项，同一 eval 即可
		if _, err := page.Eval(`(arg) => {
			document.querySelector(arg.trigger).click();
			const opt = document.querySelector(arg.opt);
			opt.dispatchEvent(new MouseEvent('mousedown', {bubbles: true, cancelable: true, clientX: 10, clientY: 10}));
			return true;
		}`, map[string]any{"trigger": triggerSel, "opt": optionSel}); err != nil {
			t.Fatalf("模拟用户操作失败: %v", err)
		}
	}
	time.Sleep(1400 * time.Millisecond)
	rec.Stop()

	sel := findAction(rec, "select")
	if sel == nil {
		t.Fatalf("未录到 select 动作，实际动作: %+v", rec.Actions())
	}
	if sel.Value != wantVal {
		t.Fatalf("选中值错误: got %q want %q", sel.Value, wantVal)
	}

	// ---- 重置页面后回放 ----
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("重新打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	step := model.UIStep{
		Action:  "select",
		Trigger: sel.Trigger, TriggerText: sel.TriggerText,
		Selector: sel.Selector, Value: sel.Value, Text: sel.Text,
	}
	if err := rep.Do(step); err != nil {
		t.Fatalf("回放失败: %v", err)
	}
	got, err := page.Eval(`(id) => document.getElementById(id).textContent`, stateSel)
	if err != nil {
		t.Fatalf("读取页面状态失败: %v", err)
	}
	if got.Value.Str() != wantVal {
		t.Fatalf("回放后页面状态 = %s，期望 %s", got.Value.Str(), wantVal)
	}
}

func TestEndToEndSelectNative(t *testing.T) {
	testFormEndToEnd(t,
		map[string]string{"/": selectFormHTML},
		true, "#sizeSel", "", "50", "state")
}

func TestEndToEndSelectDivPopup(t *testing.T) {
	testFormEndToEnd(t,
		map[string]string{"/": divFormHTML},
		false, "#trigger2", "#dd2 .opt[data-value='50']", "50", "state2")
}

func TestEndToEndSelectUlLiPopup(t *testing.T) {
	testFormEndToEnd(t,
		map[string]string{"/": ulFormHTML},
		false, "#trigger3", "#dd3 li[data-v='50']", "50", "state3")
}
