package studio

import (
	"testing"
	"time"

	"collect/internal/browser"
	"collect/internal/model"
)

// 构造一次录制会话：登录接口 + 两个数据列表接口
func demoRequests() []browser.CapRequest {
	base := time.Now().Add(-time.Minute)
	return []browser.CapRequest{
		{
			ID: "login", Seq: 1, Method: "POST", URL: "https://demo.test/api/login",
			ReqHeaders: map[string]string{"Content-Type": "application/json"},
			ReqBody:    `{"user":"demo","pass":"123456"}`,
			Status:     200, MimeType: "application/json", IsJSON: true,
			RespBody:  `{"code":0,"data":{"token":"tk_demo_123456","userId":7}}`,
			StartedAt: base,
		},
		{
			ID: "stores", Seq: 2, Method: "GET", URL: "https://demo.test/api/stores?page=1&pageSize=2",
			ReqHeaders: map[string]string{"Authorization": "Bearer tk_demo_123456"},
			Status:     200, MimeType: "application/json", IsJSON: true,
			RespBody:  `{"code":0,"data":{"list":[{"id":1,"name":"A","address":"X"},{"id":2,"name":"B","address":"Y"}],"total":2}}`,
			StartedAt: base.Add(time.Second),
		},
		{
			ID: "orders", Seq: 3, Method: "GET", URL: "https://demo.test/api/orders?size=3",
			ReqHeaders: map[string]string{"Authorization": "Bearer tk_demo_123456"},
			Status:     200, MimeType: "application/json", IsJSON: true,
			RespBody:  `{"result":{"records":[{"orderId":"o1","amount":10.5},{"orderId":"o2","amount":20}]}}`,
			StartedAt: base.Add(2 * time.Second),
		},
	}
}

func demoSession() *Session {
	return &Session{ID: "s1", Mode: "api", StartURL: "https://demo.test/login"}
}

// 纯接口：多接口各自生成独立步骤，分页/映射互不影响
func TestBuildDraftMultiAPI(t *testing.T) {
	task, err := buildDraftFrom(demoSession(), DraftRequest{
		Mode: "api", LoginReqID: "login",
		ListReqIDs: []string{"stores", "orders"},
	}, demoRequests(), nil)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	steps := task["http_steps"].([]model.HTTPStep)
	if len(steps) != 3 {
		t.Fatalf("期望 3 个接口步骤（登录 + 2 个列表），实际 %d", len(steps))
	}
	if steps[0].Name != "登录" || len(steps[0].Extract) == 0 {
		t.Errorf("登录步骤缺少 token 提取: %+v", steps[0])
	}
	s1, s2 := steps[1], steps[2]
	if !s1.Capture || s1.RecordsPath != "data.list" {
		t.Errorf("接口1 记录路径错误: %+v", s1)
	}
	if s1.Pagination == nil || !s1.Pagination.Enabled || !s1.Paginate {
		t.Errorf("接口1 应启用独立分页: %+v", s1.Pagination)
	}
	if len(s1.Mapping) == 0 {
		t.Errorf("接口1 应带独立字段映射")
	}
	if !s2.Capture || s2.RecordsPath != "result.records" {
		t.Errorf("接口2 记录路径错误: %+v", s2)
	}
	// 接口2 没有 page 参数，必须显式关闭分页，避免误用接口1的分页配置
	if s2.Pagination == nil || s2.Pagination.Enabled {
		t.Errorf("接口2 应显式关闭分页: %+v", s2.Pagination)
	}
	if len(s2.Mapping) != 2 {
		t.Fatalf("接口2 应带 2 个独立字段映射: %+v", s2.Mapping)
	}
	got := map[string]bool{}
	for _, m := range s2.Mapping {
		got[m.Source] = true
	}
	// 不能串到接口1 的 id/name/address，必须是接口2 自己的字段
	if !got["orderId"] || !got["amount"] || got["id"] || got["name"] {
		t.Errorf("接口2 映射应来自自己的数据: %+v", s2.Mapping)
	}
	if s1.Name == s2.Name {
		t.Errorf("多接口步骤名称应可区分: %q / %q", s1.Name, s2.Name)
	}
	if _, ok := task["dedupe"].(model.DedupeConfig); !ok {
		t.Errorf("应生成主键去重配置")
	}
}

// 纯 UI：多接口各自生成一条抓取规则
func TestBuildDraftMultiUI(t *testing.T) {
	task, err := buildDraftFrom(demoSession(), DraftRequest{
		Mode: "ui", ListReqIDs: []string{"stores", "orders"},
	}, demoRequests(), nil)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	cfg := task["capture"].(model.CaptureConfig)
	if len(cfg.Rules) != 2 {
		t.Fatalf("期望 2 条抓取规则，实际 %d", len(cfg.Rules))
	}
	r1, r2 := cfg.Rules[0], cfg.Rules[1]
	if r1.URLContains != "https://demo.test/api/stores" || r1.RecordsPath != "data.list" {
		t.Errorf("规则1 配置错误: %+v", r1)
	}
	if r2.URLContains != "https://demo.test/api/orders" || r2.RecordsPath != "result.records" {
		t.Errorf("规则2 配置错误: %+v", r2)
	}
	if len(r1.Mapping) == 0 || len(r2.Mapping) == 0 {
		t.Errorf("每条规则都应有独立字段映射")
	}
	if r1.Name == "" || r2.Name == "" || r1.Name == r2.Name {
		t.Errorf("规则名称应可区分: %q / %q", r1.Name, r2.Name)
	}
}

// 混合模式：登录走 UI，多接口走接口步骤
func TestBuildDraftMultiHybrid(t *testing.T) {
	task, err := buildDraftFrom(demoSession(), DraftRequest{
		Mode: "hybrid", ListReqIDs: []string{"stores", "orders"},
	}, demoRequests(), nil)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	steps := task["http_steps"].([]model.HTTPStep)
	if len(steps) != 2 {
		t.Fatalf("混合模式登录走 UI，期望 2 个数据步骤，实际 %d", len(steps))
	}
	for i, st := range steps {
		if !st.Capture {
			t.Errorf("步骤%d 应为抓取步骤", i+1)
		}
		if st.Pagination == nil {
			t.Errorf("步骤%d 应有独立分页配置", i+1)
		}
	}
	login := task["login_steps"].([]model.UIStep)
	if len(login) == 0 {
		t.Errorf("混合模式应生成登录 UI 步骤")
	}
}

// 单个接口：保持旧行为（任务级分页/映射，步骤继承）
func TestBuildDraftSingleCompat(t *testing.T) {
	task, err := buildDraftFrom(demoSession(), DraftRequest{
		Mode: "api", LoginReqID: "login", ListReqID: "stores",
	}, demoRequests(), nil)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	steps := task["http_steps"].([]model.HTTPStep)
	if len(steps) != 2 {
		t.Fatalf("期望 2 个步骤，实际 %d", len(steps))
	}
	if steps[1].Pagination != nil {
		t.Errorf("单接口应继承任务级分页，不应写步骤级分页")
	}
	pg := task["pagination"].(model.Pagination)
	if !pg.Enabled {
		t.Errorf("单接口应把分页写到任务级")
	}
	dd := task["dedupe"].(model.DedupeConfig)
	if !dd.Enabled || len(dd.Keys) == 0 || dd.Keys[0] != "id" {
		t.Errorf("单接口应按 id 去重，实际 %+v", dd)
	}
}

// 录制里的「打开 xxx」跳转必须变成 UI 步骤：
// 首页跳转和起始地址相同要跳过，跳到内嵌列表页要保留，否则回放找错页面
func TestBuildDraftKeepsNavigateSteps(t *testing.T) {
	now := time.Now().UnixMilli()
	actions := []browser.Action{
		{Type: "navigate", URL: "https://demo.test/login", At: now},
		{Type: "input", Selector: "#user", Value: "xhoo", At: now + 100},
		{Type: "click", Selector: "#loginBtn", Text: "登录", At: now + 200},
		{Type: "navigate", URL: "https://demo.test/layout.htm?listconfigid=2025081103", At: now + 900},
		{Type: "click", Selector: "#unchecked", Text: "未对码", At: now + 1500},
	}
	task, err := buildDraftFrom(demoSession(), DraftRequest{Mode: "ui", ListReqIDs: []string{"stores"}},
		demoRequests(), actions)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	steps := task["ui_steps"].([]model.UIStep)
	want := []string{"open", "input", "click", "open", "click"}
	if len(steps) != len(want) {
		t.Fatalf("期望 %d 个 UI 步骤，实际 %d: %+v", len(want), len(steps), steps)
	}
	for i, w := range want {
		if steps[i].Action != w {
			t.Errorf("第 %d 步应为 %s，实际 %s（%+v）", i+1, w, steps[i].Action, steps[i])
		}
	}
	if steps[0].Value != "https://demo.test/login" {
		t.Errorf("首页打开地址错误: %+v", steps[0])
	}
	if steps[3].Value != "https://demo.test/layout.htm?listconfigid=2025081103" {
		t.Errorf("跳转步骤地址错误: %+v", steps[3])
	}
}

// 组件库下拉：录到的 select 动作必须把触发器和选项文字带进 UI 步骤，
// 否则回放时选项元素还不存在，找不到下拉
func TestBuildDraftSelectKeepsTrigger(t *testing.T) {
	now := time.Now().UnixMilli()
	actions := []browser.Action{
		{
			Type: "select", Selector: "#sizeDD li:nth-of-type(3)", Value: "50", Text: "50 条/页",
			Trigger: "#sizeTrigger", TriggerText: "20 条/页", At: now,
		},
	}
	s := &Session{ID: "s1", Mode: "ui", StartURL: "https://demo.test/list"}
	task, err := buildDraftFrom(s, DraftRequest{Mode: "ui"}, demoRequests(), actions)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	steps := task["ui_steps"].([]model.UIStep)
	var sel *model.UIStep
	for i := range steps {
		if steps[i].Action == "select" {
			sel = &steps[i]
		}
	}
	if sel == nil {
		t.Fatalf("草稿里没有 select 步骤: %+v", steps)
	}
	if sel.Trigger != "#sizeTrigger" {
		t.Errorf("触发器丢失: %+v", sel)
	}
	if sel.TriggerText != "20 条/页" {
		t.Errorf("触发器文字丢失: %+v", sel)
	}
	if sel.Value != "50" || sel.Text != "50 条/页" {
		t.Errorf("选项值 / 文字丢失: %+v", sel)
	}
}

// 自定义下拉录制会同时产出「click 触发器」与「select(trigger=触发器)」，
// 生成草稿时相邻的触发器 click 必须去掉：select 回放会自己点开，
// 保留 click 会把弹层 toggle 回去导致选不到选项。
func TestBuildDraftSelectDropsDuplicateTriggerClick(t *testing.T) {
	now := time.Now().UnixMilli()
	actions := []browser.Action{
		{Type: "click", Selector: "#sizeTrigger", Text: "20 条/页", At: now},
		{
			Type: "select", Selector: "#sizeDD li:nth-of-type(3)", Value: "50", Text: "50 条/页",
			Trigger: "#sizeTrigger", TriggerText: "20 条/页", At: now + 100,
		},
	}
	s := &Session{ID: "s1", Mode: "ui", StartURL: "https://demo.test/list"}
	task, err := buildDraftFrom(s, DraftRequest{Mode: "ui"}, demoRequests(), actions)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	steps := task["ui_steps"].([]model.UIStep)
	for i, st := range steps {
		if st.Action == "click" && st.Selector == "#sizeTrigger" {
			t.Errorf("第 %d 步保留了与 select 触发器相同的 click，回放会 toggle 关闭弹层: %+v", i+1, st)
		}
	}
	found := false
	for _, st := range steps {
		if st.Action == "select" && st.Trigger == "#sizeTrigger" {
			found = true
		}
	}
	if !found {
		t.Errorf("select 步骤丢失: %+v", steps)
	}
}

// 组件库弹层选项（span/li 挂在 body 下、无 id）的选择器以 html > body 开头，
// 之前被 actionToStep 一律丢弃，导致「录制时明明点了、生成时步骤不见了」。
// 有按钮文字时必须保留（回放按文字兜底）。
func TestBuildDraftKeepsWeakSelectorClickWithText(t *testing.T) {
	now := time.Now().UnixMilli()
	actions := []browser.Action{
		{
			// cpath 无 id/testid/name 锚点时的典型产物
			Type: "click", Selector: "html > body > div:nth-of-type(2) > ul > li",
			Text: "50 条/页", At: now,
		},
		{
			// 连文字都没有但录到了坐标：保留坐标兜底
			Type: "click", Selector: "html > body > span:nth-of-type(3)",
			X: 412, Y: 806, At: now + 100,
		},
		{
			// 无锚点、无文字、无坐标：真的没用，应丢弃
			Type: "click", Selector: "html > body > div", At: now + 200,
		},
	}
	s := &Session{ID: "s1", Mode: "ui", StartURL: "https://demo.test/list"}
	task, err := buildDraftFrom(s, DraftRequest{Mode: "ui"}, demoRequests(), actions)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	steps := task["ui_steps"].([]model.UIStep)
	clicks := 0
	for _, st := range steps {
		if st.Action != "click" {
			continue
		}
		clicks++
		if st.Selector == "html > body > div:nth-of-type(2) > ul > li" && st.Text == "" {
			t.Errorf("带文字的弱选择器点击被丢弃: %+v", st)
		}
	}
	if clicks < 2 {
		t.Fatalf("期望保留 2 条弱选择器点击，实际 %d: %+v", clicks, steps)
	}
}

// 前端 bug 兜底：录制会话是纯 UI、有页面动作，但请求按「纯接口」提交时，
// 不能静默丢掉 UI 步骤，应自动按会话模式生成并给出提示。
func TestBuildDraftCorrectsApiModeForUISession(t *testing.T) {
	now := time.Now().UnixMilli()
	actions := []browser.Action{
		{Type: "click", Selector: "#pager li:nth-of-type(3)", Text: "50 条/页", At: now},
		{Type: "click", Selector: "#search", Text: "查询", At: now + 100},
	}
	s := &Session{ID: "s1", Mode: "ui", StartURL: "https://demo.test/list"}
	task, err := buildDraftFrom(s, DraftRequest{Mode: "api", ListReqIDs: []string{"stores"}},
		demoRequests(), actions)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	if got := task["mode"]; got != "ui" {
		t.Fatalf("模式未被纠正为 ui，实际 %v", got)
	}
	steps := task["ui_steps"].([]model.UIStep)
	if len(steps) < 2 {
		t.Fatalf("UI 步骤丢失，实际 %d 条: %+v", len(steps), steps)
	}
	notes, _ := task["_notes"].([]string)
	if len(notes) == 0 {
		t.Fatalf("应给出自动纠正模式的提示")
	}
}

// 会话本来就是纯接口时，不做纠正（正常忽略动作）。
func TestBuildDraftApiSessionNoCorrection(t *testing.T) {
	now := time.Now().UnixMilli()
	actions := []browser.Action{{Type: "click", Selector: "#x", Text: "x", At: now}}
	s := &Session{ID: "s1", Mode: "api", StartURL: "https://demo.test/list"}
	task, err := buildDraftFrom(s, DraftRequest{Mode: "api", ListReqIDs: []string{"stores"}},
		demoRequests(), actions)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	if got := task["mode"]; got != "api" {
		t.Fatalf("纯接口会话不应被纠正，实际 %v", got)
	}
	if steps := task["ui_steps"].([]model.UIStep); len(steps) != 0 {
		t.Fatalf("纯接口模式不应生成 UI 步骤: %+v", steps)
	}
}

// 纯接口模式：页面动作会被丢弃，但要给出提示
func TestBuildDraftAPINotesDroppedActions(t *testing.T) {
	now := time.Now().UnixMilli()
	actions := []browser.Action{
		{Type: "click", Selector: "#menu", Text: "门店", At: now},
	}
	task, err := buildDraftFrom(demoSession(), DraftRequest{
		Mode: "api", LoginReqID: "login", ListReqIDs: []string{"stores"},
	}, demoRequests(), actions)
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	if steps := task["ui_steps"].([]model.UIStep); len(steps) != 0 {
		t.Fatalf("纯接口模式不应生成 UI 步骤: %+v", steps)
	}
	notes, _ := task["_notes"].([]string)
	if len(notes) == 0 {
		t.Fatalf("应给出「纯接口不生成 UI 步骤」提示")
	}
}
