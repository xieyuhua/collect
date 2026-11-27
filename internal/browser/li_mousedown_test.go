package browser

import (
	"testing"
	"time"

	"collect/internal/model"
)

// 很多组件库选项监听的是 mousedown（不是 click），因为 click 会在
// 失焦时被吞。回放 visit 必须派发完整鼠标序列，不能只 el.Click。
const liMousedownHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>li mousedown</title>
<style>
.list { position: absolute; background:#fff; border:1px solid #ccc; display:none; list-style:none; margin:0; padding:0; }
.list.show { display:block; }
.list li { padding:4px 16px; cursor:pointer; }
</style></head>
<body>
<button id="trig">每页 20 条</button>
<ul class="list" id="lst">
  <li>每页 10 条</li><li>每页 20 条</li><li>每页 50 条</li>
</ul>
<div id="state">20</div>
<script>
var lst = document.getElementById('lst');
document.getElementById('trig').addEventListener('click', function(){ lst.classList.toggle('show'); });
Array.prototype.forEach.call(lst.querySelectorAll('li'), function(li){
  li.addEventListener('mousedown', function(e){   // 只监听 mousedown
    e.preventDefault();
    document.getElementById('state').textContent = (li.textContent.match(/\d+/)||['20'])[0];
    lst.classList.remove('show');
  });
});
</script>
</body></html>`

// 回放的 click 步骤（选择器命中 li）必须能触发只监听 mousedown 的选项。
func TestReplayClickHitsLiWithOnlyMousedownHandler(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": liMousedownHTML})
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	// 先展开
	if err := rep.Do(model.UIStep{Action: "click", Selector: "#trig"}); err != nil {
		t.Fatalf("展开失败: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	// 再点 li（按文字 / 选择器）
	if err := rep.Do(model.UIStep{Action: "click", Selector: "#lst li:nth-of-type(3)", Text: "每页 50 条"}); err != nil {
		t.Fatalf("点 li 失败: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	got, _ := page.Eval(`() => document.getElementById('state').textContent`)
	if got.Value.Str() != "50" {
		t.Fatalf("只监听 mousedown 的 li 未被点中，state=%s", got.Value.Str())
	}
}
