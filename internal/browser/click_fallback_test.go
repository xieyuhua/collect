package browser

import (
	"testing"
	"time"

	"collect/internal/model"
)

// 分页器：分页控件在数据加载后才渲染出来（回放跑到该步时可能还没出现）。
// 回放必须等待并重试，最终点到。
const latePagerHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>延迟分页器</title></head>
<body>
<div id="list">loading</div>
<div id="pager"></div>
<script>
// 模拟异步：1 秒后才有数据并渲染分页器
setTimeout(function () {
  document.getElementById('list').textContent = 'data';
  var pager = document.getElementById('pager');
  pager.id = 'pagination-page-list';
  ['1','2','3'].forEach(function (n) {
    var a = document.createElement('a');
    var s = document.createElement('span');
    s.textContent = n;
    a.appendChild(s);
    a.addEventListener('click', function () { document.getElementById('list').textContent = 'page ' + n; });
    pager.appendChild(a);
  });
}, 1000);
</script>
</body></html>`

func TestReplayClickWaitsForLateRenderedPager(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": latePagerHTML})
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	// 立即尝试点第 3 页——此时分页器还没渲染出来
	step := model.UIStep{Action: "click", Selector: "#pagination-page-list > a:nth-of-type(3) > span"}
	if err := rep.Do(step); err != nil {
		t.Fatalf("延迟渲染的分页器点击失败: %v", err)
	}
	got, _ := page.Eval(`() => document.getElementById('list').textContent`)
	if got.Value.Str() != "page 3" {
		t.Fatalf("点击未生效，list=%s", got.Value.Str())
	}
}

// 节点尺寸为 0 / 被遮挡：rod 的 Element 定位可能失败，但原生 querySelector 能命中。
const zeroSizeSpanHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>零尺寸 span</title>
<style>
.wrap { display: inline-block; }
/* span 本身不承担布局，尺寸为 0，但父 a 可点 */
.wrap a { display:inline-block; padding:4px 10px; cursor:pointer; }
.wrap a span { display:inline; }
</style></head>
<body>
<div id="pagination-page-list">
  <a href="javascript:void(0)"><span>1</span></a>
  <a href="javascript:void(0)"><span>2</span></a>
</div>
<div id="out">none</div>
<script>
document.querySelectorAll('#pagination-page-list a').forEach(function (a, i) {
  a.addEventListener('click', function () { document.getElementById('out').textContent = 'clicked ' + (i + 1); });
});
</script>
</body></html>`

func TestReplayClickSpanInsideAnchor(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{"/": zeroSizeSpanHTML})
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	step := model.UIStep{Action: "click", Selector: "#pagination-page-list > a:nth-of-type(2) > span"}
	if err := rep.Do(step); err != nil {
		t.Fatalf("点击 a > span 失败: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	got, _ := page.Eval(`() => document.getElementById('out').textContent`)
	if got.Value.Str() != "clicked 2" {
		t.Fatalf("点击未生效，out=%s", got.Value.Str())
	}
}
