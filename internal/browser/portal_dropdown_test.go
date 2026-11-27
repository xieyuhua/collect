package browser

import (
	"testing"
	"time"

	"collect/internal/model"
)

// 真实组件库（AntD / Element）最常见形态：下拉弹层通过 portal 挂到 <body> 末尾，
// 不在触发器的组件根内。选项是 li，点击 li 提交选择。
const portalDropdownHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>portal 下拉</title>
<style>
.trigger { display: inline-block; padding: 4px 12px; border: 1px solid #ccc; cursor: pointer; }
.portal-list { position: absolute; z-index: 9999; background: #fff; border: 1px solid #ccc; display: none; list-style: none; margin: 0; padding: 0; }
.portal-list.show { display: block; }
.portal-list li { padding: 4px 16px; cursor: pointer; }
</style></head>
<body>
<button id="pgTrigger" class="trigger">每页 20 条</button>
<table><tbody><tr><td>row A</td></tr><tr><td>row B</td></tr></tbody></table>
<div id="state">20</div>
<script>
var trig = document.getElementById('pgTrigger');
var list = document.createElement('ul');
list.className = 'portal-list';
['10','20','50'].forEach(function (v) {
  var li = document.createElement('li');
  li.textContent = '每页 ' + v + ' 条';
  li.setAttribute('data-page', v);
  li.addEventListener('click', function () {
    document.getElementById('state').textContent = v;
    trig.textContent = '每页 ' + v + ' 条';
    list.classList.remove('show');
  });
  list.appendChild(li);
});
// portal：直接挂到 body，绝对定位到触发器下方
document.body.appendChild(list);
trig.addEventListener('click', function (e) {
  e.stopPropagation();
  var r = trig.getBoundingClientRect();
  list.style.left = r.left + 'px';
  list.style.top = (r.bottom + window.scrollY) + 'px';
  list.classList.toggle('show');
});
document.addEventListener('click', function () { list.classList.remove('show'); });
</script>
</body></html>`

// portal 到 body 的下拉：录制 + 回放都必须选中 li。
// portal 到 body 且选项是裸 span（无 data/role）：回放也必须能选中，
// 不能因为组件根里找不到选项就放弃。
const portalBareSpanHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>portal 裸 span</title>
<style>
.pg { display:inline-block; padding:4px 12px; border:1px solid #ccc; cursor:pointer; }
.pg-panel { position:absolute; z-index:9999; background:#fff; border:1px solid #ccc; display:none; }
.pg-panel.on { display:block; }
.pg-panel .cell { display:block; padding:5px 18px; cursor:pointer; }
</style></head>
<body>
<button id="pgBtn" class="pg">每页 20 条</button>
<div id="state">20</div>
<script>
var panel = document.createElement('div');
panel.className = 'pg-panel';
['10','20','50'].forEach(function (v) {
  var cell = document.createElement('span');   // 裸 span，无任何属性
  cell.className = 'cell';
  cell.textContent = v + ' 条/页';
  cell.addEventListener('click', function () {
    document.getElementById('state').textContent = v;
    panel.classList.remove('on');
  });
  panel.appendChild(cell);
});
document.body.appendChild(panel);   // portal 到 body
document.getElementById('pgBtn').addEventListener('click', function (e) {
  e.stopPropagation();
  var r = this.getBoundingClientRect();
  panel.style.left = r.left + 'px';
  panel.style.top = (r.bottom + window.scrollY) + 'px';
  panel.classList.toggle('on');
});
</script>
</body></html>`

func TestReplayPortalBareSpanOption(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": portalBareSpanHTML})
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	step := model.UIStep{
		Action: "select", Trigger: "#pgBtn", TriggerText: "每页 20 条",
		Text: "50 条/页", Value: "50 条/页",
	}
	if err := rep.Do(step); err != nil {
		t.Fatalf("回放 portal 裸 span 失败: %v", err)
	}
	got, _ := page.Eval(`() => document.getElementById('state').textContent`)
	if got.Value.Str() != "50" {
		t.Fatalf("回放后状态 = %s，期望 50", got.Value.Str())
	}
}

func TestPortalDropdownRecordAndReplay(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": portalDropdownHTML})

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

	// 用户：点触发器 → 点 li（click）
	if _, err := page.Eval(`() => {
		document.getElementById('pgTrigger').click();
		const li = document.querySelectorAll('.portal-list li')[2]; // 每页 50 条
		li.click();
		return true;
	}`); err != nil {
		t.Fatalf("模拟操作失败: %v", err)
	}
	time.Sleep(1400 * time.Millisecond)
	rec.Stop()

	acts := rec.Actions()
	t.Logf("录到的动作: %+v", acts)
	// 诊断应包含 li 的点击记录
	diags := rec.Diagnostics()
	t.Logf("诊断: %+v", diags)
	found := false
	for _, d := range diags {
		if d.Tag == "li" && d.Rec == "click" {
			found = true
		}
	}
	if !found {
		t.Errorf("诊断里没有 li 的 click 记录: %+v", diags)
	}

	// ---- 回放 ----
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	// 回放序列：先点触发器，再点 li（模拟录制得到的点击步骤）
	steps := []model.UIStep{
		{Action: "click", Selector: "#pgTrigger"},
		{Action: "click", Text: "每页 50 条"},
	}
	for i, s := range steps {
		if err := rep.Do(s); err != nil {
			t.Fatalf("回放第 %d 步失败: %v", i+1, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	got, _ := page.Eval(`() => document.getElementById('state').textContent`)
	if got.Value.Str() != "50" {
		t.Fatalf("回放后页面状态 = %s，期望 50", got.Value.Str())
	}
}
