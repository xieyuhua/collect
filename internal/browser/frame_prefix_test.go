package browser

import (
	"time"

	"testing"

	"collect/internal/model"
)

func TestStripFramePrefix(t *testing.T) {
	cases := []struct{ sel, frame, want string }{
		// 录制器把 iframe 自身的选择器拼进了内部路径
		{"#listfrm > div > ul > li > input", "#listfrm", "div > ul > li > input"},
		{"iframe#main > div > p", "iframe#main", "div > p"},
		// 嵌套 iframe：只用最后一段匹配
		{"#inner > span", "iframe#main >> #inner", "span"},
		// 没有前缀时保持原样
		{"div > ul > li", "#listfrm", "div > ul > li"},
		// 空 frame 不处理
		{"#listfrm > div", "", "#listfrm > div"},
	}
	for _, c := range cases {
		if got := stripFramePrefix(c.sel, c.frame); got != c.want {
			t.Errorf("stripFramePrefix(%q, %q) = %q, want %q", c.sel, c.frame, got, c.want)
		}
	}
}

// 页面结构：外层页面 + iframe#listfrm，iframe 内是表格 + 输入框。
// 录制器给出的选择器带 "#listfrm > " 前缀，回放必须剥掉才能命中。
const framePrefixOuter = `<!doctype html><html><body>
<iframe id="listfrm" src="/inner-frame.html" style="width:100%;height:300px;border:0"></iframe>
</body></html>`

const framePrefixInner = `<!doctype html><html><body>
<div>
  <ul><li><input id="kw" placeholder="关键词"></li></ul>
</div>
<div id="out">none</div>
<script>
document.getElementById('kw').addEventListener('keydown', function (e) {
  if (e.key === 'Enter') document.getElementById('out').textContent = 'searched';
});
</script>
</body></html>`

func TestReplayPressInIframeWithPrefixedSelector(t *testing.T) {
	page, cleanup := testBrowser(t)
	defer cleanup()
	srv := serveHTML(t, map[string]string{
		"/":                 framePrefixOuter,
		"/inner-frame.html": framePrefixInner,
	})
	if err := page.Navigate(srv.URL); err != nil {
		t.Fatalf("打开页面失败: %v", err)
	}
	_ = page.WaitLoad()

	rep := NewReplayer(page, 256, nil)
	// 录制器给出的选择器带 iframe 前缀
	step := model.UIStep{
		Action:   "press",
		Selector: "#listfrm > div > ul > li > input",
		Key:      "Enter",
		Frame:    "#listfrm",
	}
	if err := rep.Do(step); err != nil {
		t.Fatalf("回放 press 失败: %v", err)
	}

	// 在 iframe 内部断言状态
	fp, err := PageForFrame(page, "#listfrm", 5*time.Second)
	if err != nil {
		t.Fatalf("进入 iframe 失败: %v", err)
	}
	got, _ := fp.Eval(`() => document.getElementById('out').textContent`)
	if got.Value.Str() != "searched" {
		t.Fatalf("iframe 内按键未生效，out=%s", got.Value.Str())
	}
}
