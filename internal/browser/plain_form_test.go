package browser

import (
	"testing"
	"time"
)

// 用户实际遇到的 bug：普通表单区域点击（section>form 里的输入框/按钮）
// 被误判成「下拉选择」。这里还原一个带定位容器的表单页，
// 验证点击都录成 click，不产生任何 select 动作。
const plainFormHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>表单页</title>
<style>
.page { position: relative; padding: 20px; }
section { position: relative; }
.field { margin: 8px 0; }
.field input { padding: 4px 8px; }
.btn { padding: 4px 12px; }
</style></head>
<body>
<div class="page">
  <section>
    <form id="f">
      <div class="field"><input id="uname" placeholder="账号"></div>
      <div class="field"><input id="pwd" type="password" placeholder="密码"></div>
      <div class="field"><button id="submit" class="btn" type="button">查询</button></div>
    </form>
  </section>
</div>
<div id="out">none</div>
<script>
document.getElementById('submit').addEventListener('click', function () {
  document.getElementById('out').textContent = 'submitted';
});
</script>
</body></html>`

func TestPlainFormClicksAreNotRecordedAsSelect(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": plainFormHTML})

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

	// 依次在输入框、密码框、按钮上派发完整鼠标事件序列（贴近真实点击）
	if _, err := page.Eval(`() => {
		const fire = (el) => {
			const r = el.getBoundingClientRect();
			const opt = {bubbles:true, cancelable:true, clientX:r.left+r.width/2, clientY:r.top+r.height/2};
			el.dispatchEvent(new MouseEvent('mousedown', opt));
			el.dispatchEvent(new MouseEvent('mouseup', opt));
			el.dispatchEvent(new MouseEvent('click', opt));
		};
		fire(document.getElementById('uname'));
		fire(document.getElementById('pwd'));
		fire(document.getElementById('submit'));
		return true;
	}`); err != nil {
		t.Fatalf("模拟操作失败: %v", err)
	}
	time.Sleep(1400 * time.Millisecond)

	acts := rec.Actions()
	for _, a := range acts {
		if a.Type == "select" {
			t.Fatalf("普通表单点击被误记为下拉选择: %+v", a)
		}
	}
	found := false
	for _, a := range acts {
		if a.Type == "click" && a.Text == "查询" {
			found = true
		}
	}
	if !found {
		t.Fatalf("按钮点击未录到，动作: %+v", acts)
	}
}
