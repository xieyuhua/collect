package browser

import (
	"testing"
	"time"
)

// 复现：页面里有 position:fixed 的导航容器，里面的 li 是普通菜单项。
// 点击菜单 li 必须录成 click，不能被「选项 click 跳过」逻辑误吞。
const fixedMenuHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>fixed 菜单</title>
<style>
body { margin: 0; }
.sidenav { position: fixed; top: 0; left: 0; width: 200px; height: 100%; background: #eee; }
.sidenav li { padding: 10px 16px; cursor: pointer; }
.content { margin-left: 210px; padding: 20px; }
</style></head>
<body>
<ul class="sidenav">
  <li id="menu-drug">药品查询</li>
  <li id="menu-store">门店查询</li>
</ul>
<div class="content" id="out">none</div>
<script>
document.getElementById('menu-drug').addEventListener('click', function () {
  document.getElementById('out').textContent = 'drug';
});
document.getElementById('menu-store').addEventListener('click', function () {
  document.getElementById('out').textContent = 'store';
});
</script>
</body></html>`

func TestRecorderKeepsClickOnFixedLayerMenuItem(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": fixedMenuHTML})

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
		document.getElementById('menu-drug').click();
		return true;
	}`); err != nil {
		t.Fatalf("模拟点击失败: %v", err)
	}
	time.Sleep(1400 * time.Millisecond)

	click := findAction(rec, "click")
	if click == nil {
		t.Fatalf("fixed 菜单里的 li 点击被吞了，动作: %+v", rec.Actions())
	}
	if click.Selector != "#menu-drug" {
		t.Fatalf("选择器错误: %q", click.Selector)
	}
}
