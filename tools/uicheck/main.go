package main

// 前端冒烟测试：用无头浏览器打开平台页面，逐个路由检查渲染与 JS 异常。
// 用法：先启动 collect 服务，然后 go run ./tools/uicheck

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"collect/internal/applog"
	"collect/internal/browser"
	"collect/internal/config"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

var fails []string

// evalInt 在页面里执行 JS 并取回整数值，出错返回 -1
func evalInt(page *rod.Page, js string) int {
	res, err := page.Timeout(5 * time.Second).Eval(js)
	if err != nil || res == nil {
		return -1
	}
	return int(res.Value.Int())
}

// evalBool 在页面里执行 JS 并取回布尔值
func evalBool(page *rod.Page, js string) bool {
	res, err := page.Timeout(5 * time.Second).Eval(js)
	if err != nil || res == nil {
		return false
	}
	return res.Value.Bool()
}

func check(name string, cond bool, detail string) {
	if cond {
		fmt.Println("  [OK]   " + name)
	} else {
		fmt.Println("  [FAIL] " + name + " -> " + detail)
		fails = append(fails, name)
	}
}

func main() {
	base := "http://127.0.0.1:18080"
	if len(os.Args) > 1 {
		base = os.Args[1]
	}
	cfg := config.Load()
	log := applog.New(config.DataPath("logs"))
	bm := browser.NewManager(cfg, log)
	b, cleanup, err := bm.Launch(true, true)
	if err != nil {
		fmt.Println("启动浏览器失败:", err)
		os.Exit(1)
	}
	defer cleanup()
	page, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		fmt.Println("创建页面失败:", err)
		os.Exit(1)
	}
	_ = proto.RuntimeEnable{}.Call(page)
	_ = proto.LogEnable{}.Call(page)
	var errs []string
	var mu sync.Mutex
	addErr := func(s string) {
		mu.Lock()
		errs = append(errs, s)
		mu.Unlock()
	}
	go page.EachEvent(
		func(e *proto.RuntimeExceptionThrown) {
			if e.ExceptionDetails != nil {
				addErr("exception: " + e.ExceptionDetails.Text)
			}
		},
		func(e *proto.RuntimeConsoleAPICalled) {
			if e.Type == proto.RuntimeConsoleAPICalledTypeError {
				var parts []string
				for _, a := range e.Args {
					if a.Description != "" {
						parts = append(parts, a.Description)
					}
				}
				addErr("console.error: " + strings.Join(parts, " "))
			}
		},
		func(e *proto.LogEntryAdded) {
			if e.Entry != nil && e.Entry.Level == proto.LogLogEntryLevelError {
				addErr("log: " + e.Entry.Text)
			}
		},
	)()

	if err := page.Timeout(20 * time.Second).Navigate(base + "/"); err != nil {
		fmt.Println("打开首页失败:", err)
		os.Exit(1)
	}
	_ = page.Timeout(20 * time.Second).WaitLoad()
	time.Sleep(1500 * time.Millisecond)

	// 弹窗初始状态 / 关闭按钮回归
	modalVisible := func() bool {
		res, err := page.Timeout(5*time.Second).Eval(`() => {
			const m = document.getElementById('modalMask');
			if (!m) return false;
			const cs = getComputedStyle(m);
			return cs.display !== 'none' && cs.visibility !== 'hidden' && m.getBoundingClientRect().height > 0;
		}`)
		if err != nil || res == nil {
			return false
		}
		return res.Value.Bool()
	}
	check("初始不弹窗", !modalVisible(), "首屏 modal 遮罩可见（hidden 失效）")
	_, _ = page.Timeout(5*time.Second).Eval(`() => { openModal('测试', '<div>x</div>'); return true }`)
	time.Sleep(200 * time.Millisecond)
	check("openModal 可弹出", modalVisible(), "openModal 后遮罩不可见")
	_, _ = page.Timeout(5*time.Second).Eval(`() => { document.querySelector('[data-act="modal-close"]').click(); return true }`)
	time.Sleep(200 * time.Millisecond)
	check("点 X 可关闭弹窗", !modalVisible(), "点击关闭按钮后遮罩仍可见")
	_, _ = page.Timeout(5*time.Second).Eval(`() => { openModal('测试', '<div>x</div>'); return true }`)
	time.Sleep(200 * time.Millisecond)
	_, _ = page.Timeout(5*time.Second).Eval(`() => { document.getElementById('modalMask').click(); return true }`)
	time.Sleep(200 * time.Millisecond)
	check("点遮罩可关闭弹窗", !modalVisible(), "点击遮罩后未关闭")
	_, _ = page.Timeout(5*time.Second).Eval(`() => { openModal('测试', '<div>x</div>'); return true }`)
	time.Sleep(200 * time.Millisecond)
	_, _ = page.Timeout(5*time.Second).Eval(`() => { document.querySelector('[data-act="modal-close"]').click(); return true }`)
	time.Sleep(200 * time.Millisecond)

	routes := []string{"dashboard", "tasks", "studio", "jobs", "jobruns", "runs", "datasources", "logs", "notifications", "settings"}
	for _, rt := range routes {
		_, err := page.Timeout(10*time.Second).Eval(`(h) => { location.hash = h; return true }`, "#/"+rt)
		if err != nil {
			check("路由 "+rt, false, err.Error())
			continue
		}
		time.Sleep(1200 * time.Millisecond)
		text, err := page.Timeout(10*time.Second).Eval(`() => {
			const v = document.getElementById('view');
			if (!v) return 'NO_VIEW';
			return v.innerText.trim().slice(0, 120);
		}`)
		if err != nil {
			check("路由 "+rt, false, err.Error())
			continue
		}
		txt := strings.TrimSpace(text.Value.Str())
		check("路由 "+rt+" 渲染", len(txt) > 10 && !strings.Contains(txt, "加载中"), txt)
	}

	// 设置页：浏览器指纹（UA / 语言 / 平台）自定义
	_, _ = page.Timeout(10*time.Second).Eval(`() => { location.hash = '#/settings'; return true }`)
	time.Sleep(1200 * time.Millisecond)
	check("设置页含 UA 自定义输入框", evalBool(page, `() => !!document.getElementById('brUA')`), "缺少 User-Agent 输入框")
	check("设置页含 UA 预设下拉", evalBool(page, `() => !!document.querySelector('[data-act="ua-preset"]')`), "缺少 UA 预设下拉")
	_, _ = page.Timeout(5*time.Second).Eval(`() => {
		const s = document.querySelector('[data-act="ua-preset"]');
		if (!s) return false;
		s.value = 'win';
		s.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	}`)
	time.Sleep(300 * time.Millisecond)
	check("UA 预设可一键填入", evalBool(page, `() => {
		const ua = document.getElementById('brUA');
		const pf = document.getElementById('brPlatform');
		return !!ua && ua.value.indexOf('Windows NT') > 0 && !!pf && pf.value === 'Win32';
	}`), "预设未填入 UA / 平台")
	check("设置页含指纹保存按钮", evalBool(page, `() => !!document.querySelector('[data-act="fingerprint-save"]')`), "缺少保存指纹配置按钮")

	// 设置页：浏览器会话模式（隔离 / 共享）
	check("设置页含会话模式下拉", evalBool(page, `() => !!document.getElementById('brSession')`), "缺少会话模式下拉")
	check("设置页含共享会话目录", evalBool(page, `() => !!document.getElementById('brProfile')`), "缺少共享会话目录输入")
	check("设置页含清空会话按钮", evalBool(page, `() => !!document.querySelector('[data-act="browser-session-clear"]')`), "缺少清空共享会话按钮")
	check("设置页显示会话状态", evalBool(page, `() => !!document.querySelector('[data-act="browser-session-info"]')`), "缺少会话状态说明")
	_, _ = page.Timeout(5*time.Second).Eval(`() => {
		const s = document.getElementById('brSession');
		if (!s) return false;
		s.value = 'shared';
		s.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	}`)
	time.Sleep(300 * time.Millisecond)
	check("共享会话目录随模式启用", evalBool(page, `() => { const p = document.getElementById('brProfile'); return !!p && !p.disabled; }`), "切换共享模式后目录输入仍禁用")

	// 设置页：运行参数可自定义
	check("设置页含监听地址输入", evalBool(page, `() => !!document.getElementById('cfAddr')`), "缺少监听地址输入框")
	check("设置页含数据目录输入", evalBool(page, `() => !!document.getElementById('cfDataDir')`), "缺少数据目录输入框")
	check("设置页含超时输入", evalBool(page, `() => Number((document.getElementById('cfTimeout')||{}).value) > 0`), "缺少单请求超时输入框")
	check("设置页含代理输入", evalBool(page, `() => !!document.getElementById('cfProxy')`), "缺少接口代理输入框")
	check("设置页含包体上限输入", evalBool(page, `() => Number((document.getElementById('cfMaxBody')||{}).value) > 0`), "缺少包体上限输入框")
	check("设置页含运行参数保存按钮", evalBool(page, `() => !!document.querySelector('[data-act="general-save"]')`), "缺少运行参数保存按钮")
	_, _ = page.Timeout(5*time.Second).Eval(`() => {
		const b = document.querySelector('[data-act="general-reset"]');
		if (!b) return false;
		b.click();
		return true;
	}`)
	time.Sleep(300 * time.Millisecond)
	check("恢复默认值按钮生效", evalBool(page, `() => Number((document.getElementById('cfTimeout')||{}).value) === 60`), "点击恢复默认后超时不是 60")

	// 任务编辑器：新建 -> 保存
	_, _ = page.Timeout(10*time.Second).Eval(`() => { location.hash = '#/task/new'; return true }`)
	time.Sleep(1500 * time.Millisecond)
	hasEditor, _ := page.Timeout(5*time.Second).Eval(`() => !!document.querySelector('[data-act="save-task"]')`)
	editorOK := hasEditor != nil && hasEditor.Value.Bool()
	check("任务编辑器渲染", editorOK, "")
	if editorOK {
		// 采集模式与可编辑页签联动
		setMode := func(mode string) {
			_, _ = page.Timeout(5*time.Second).Eval(`(m) => {
				const s = document.getElementById('tMode');
				if (!s) return false;
				s.value = m;
				s.dispatchEvent(new Event('change', { bubbles: true }));
				return true;
			}`, mode)
			time.Sleep(350 * time.Millisecond)
		}
		tabDisabled := func(key string) bool {
			return evalBool(page, `() => { const b = document.querySelector('[data-act="task-tab"][data-tab="`+key+`"]'); return !b || b.disabled; }`)
		}
		tabExists := func(key string) bool {
			return evalBool(page, `() => !!document.querySelector('[data-act="task-tab"][data-tab="`+key+`"]')`)
		}
		origMode, _ := page.Timeout(5*time.Second).Eval(`() => { const s = document.getElementById('tMode'); return s ? s.value : 'api'; }`)
		orig := origMode.Value.Str()

		setMode("api")
		check("纯接口模式：UI 页签置灰", tabExists("ui") && tabDisabled("ui"), "UI 页签仍可编辑")
		check("纯接口模式：登录页签置灰", tabDisabled("login"), "登录页签仍可编辑")
		check("纯接口模式：抓取配置置灰", tabDisabled("capture"), "抓取配置仍可编辑")
		check("纯接口模式：接口步骤可编辑", !tabDisabled("steps"), "接口步骤被禁用")

		setMode("hybrid")
		check("混合模式：登录页签可用", !tabDisabled("login"), "登录页签被禁用")
		check("混合模式：UI 步骤置灰", tabDisabled("ui"), "UI 页签仍可编辑")

		setMode("ui")
		check("纯 UI 模式：UI 步骤可用", !tabDisabled("ui"), "UI 页签被禁用")
		check("纯 UI 模式：抓取配置可用", !tabDisabled("capture"), "抓取配置被禁用")
		check("纯 UI 模式：接口步骤置灰", tabDisabled("steps"), "接口步骤仍可编辑")
		check("纯 UI 模式：分页页签置灰", tabDisabled("pagination"), "分页页签仍可编辑")

		setMode(orig)

		// 任务编辑器：Cron 常用计划
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const s = document.querySelector('[data-act="cron-preset"]');
			if (!s) return false;
			s.value = '0 0 9 * * *';
			s.dispatchEvent(new Event('change', { bubbles: true }));
			return true;
		}`)
		time.Sleep(300 * time.Millisecond)
		check("任务编辑器 Cron 常用计划填入", evalBool(page, `() => { const i = document.getElementById('tCron'); return !!i && i.value === '0 0 9 * * *'; }`), "未填入表达式")
		_, _ = page.Timeout(5*time.Second).Eval(`() => { const i = document.getElementById('tCron'); if (i) { i.value = ''; i.dispatchEvent(new Event('input', { bubbles: true })); } return true }`)
		time.Sleep(200 * time.Millisecond)

		// 落库配置：自定义 SQL 面板
		_, _ = page.Timeout(5*time.Second).Eval(`() => { document.querySelector('[data-act="task-tab"][data-tab="sink"]').click(); return true }`)
		time.Sleep(300 * time.Millisecond)
		hiddenDefault, _ := page.Timeout(5*time.Second).Eval(`() => {
			const w = document.getElementById('sinkSqlWrap');
			const m = document.getElementById('sinkMode');
			const opt = m ? !!m.querySelector('option[value="sql"]') : false;
			return (w ? w.hidden : false) && opt;
		}`)
		check("落库 SQL 面板默认隐藏且有 sql 选项", hiddenDefault != nil && hiddenDefault.Value.Bool(), "面板未隐藏或缺少 sql 选项")
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const m = document.getElementById('sinkMode');
			m.value = 'sql';
			m.dispatchEvent(new Event('change', { bubbles: true }));
			return true;
		}`)
		time.Sleep(300 * time.Millisecond)
		shown, _ := page.Timeout(5*time.Second).Eval(`() => {
			const w = document.getElementById('sinkSqlWrap');
			const t = document.getElementById('sinkSql');
			if (!w || !t) return false;
			const cs = getComputedStyle(w);
			return !w.hidden && cs.display !== 'none' && !!document.querySelector('[data-act="sink-sql-gen"]');
		}`)
		check("选择自定义 SQL 后显示模板输入框", shown != nil && shown.Value.Bool(), "SQL 模板框未显示")
		// 还原为 insert，避免影响后续保存
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const m = document.getElementById('sinkMode');
			m.value = 'insert';
			m.dispatchEvent(new Event('change', { bubbles: true }));
			return true;
		}`)

		// 回归：新增条目按钮（历史问题：rerenderEditor 里重复 syncEditor 会覆盖刚 push 的条目，点击无反应）
		addCase := func(tab, box, label, actSel string) {
			_, _ = page.Timeout(5*time.Second).Eval(`(t) => {
				const b = document.querySelector('[data-act="task-tab"][data-tab="' + t + '"]');
				if (b) b.click();
				return true;
			}`, tab)
			time.Sleep(200 * time.Millisecond)
			before := evalInt(page, `() => document.querySelectorAll('`+box+`').length`)
			_, _ = page.Timeout(5*time.Second).Eval(`(s) => {
				const b = document.querySelector(s);
				if (b) b.click();
				return true;
			}`, actSel)
			time.Sleep(250 * time.Millisecond)
			after := evalInt(page, `() => document.querySelectorAll('`+box+`').length`)
			check("新增条目可用："+label, before >= 0 && after == before+1, fmt.Sprintf("添加前 %d 条，添加后 %d 条", before, after))
		}
		addCase("ui", `[data-ui="ui"]`, "UI 动作", `[data-act="add-ui"][data-kind="ui"]`)
		addCase("login", `[data-ui="login"]`, "登录动作", `[data-act="add-ui"][data-kind="login"]`)
		addCase("steps", "#stepsBox [data-step]", "接口步骤", `[data-act="add-step"]`)
		addCase("capture", "#capBox [data-cap]", "抓取规则", `[data-act="add-rule"]`)
		addCase("vars", "#varsBox [data-kv]", "任务变量", `[data-act="add-var"]`)
		addCase("mapping", "#mapBox [data-map]", "字段映射", `[data-act="add-map"]`)

		// UI 步骤卡片：非「打开页面」动作应显示「所在 iframe」输入框（iframe 内嵌页面回放用）
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const b = document.querySelector('[data-act="task-tab"][data-tab="ui"]');
			if (b) b.click();
			return true;
		}`)
		time.Sleep(200 * time.Millisecond)
		check("UI 步骤含所在 iframe 输入", evalBool(page, `() => {
			const cards = document.querySelectorAll('[data-ui="ui"]');
			const last = cards[cards.length - 1];
			return !!last && !!last.querySelector('[data-act="ui-frame"]');
		}`), "UI 步骤缺少「所在 iframe」输入框")
		check("UI 步骤打开页面时不显示 iframe 输入", evalBool(page, `() => {
			const cards = document.querySelectorAll('[data-ui="ui"]');
			const last = cards[cards.length - 1];
			const sel = last && last.querySelector('[data-act="ui-action"]');
			if (!sel) return false;
			sel.value = 'open';
			sel.dispatchEvent(new Event('change', { bubbles: true }));
			return true;
		}`), "UI 步骤缺少动作下拉")
		time.Sleep(250 * time.Millisecond)
		check("UI 步骤 open 动作隐藏 iframe 输入", evalBool(page, `() => {
			const cards = document.querySelectorAll('[data-ui="ui"]');
			const last = cards[cards.length - 1];
			return !!last && !last.querySelector('[data-act="ui-frame"]');
		}`), "open 动作下仍显示 iframe 输入")
		check("UI 步骤 open 动作显示打开方式下拉", evalBool(page, `() => {
			const cards = document.querySelectorAll('[data-ui="ui"]');
			const last = cards[cards.length - 1];
			const sel = last && last.querySelector('[data-act="ui-open-mode"]');
			if (!sel) return false;
			const vals = Array.prototype.map.call(sel.options, o => o.value);
			return vals.indexOf('') >= 0 && vals.indexOf('spa') >= 0;
		}`), "open 动作缺少「打开方式」下拉（整页打开 / 站内跳转）")
		// 还原成 click，避免影响后面保存
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const cards = document.querySelectorAll('[data-ui="ui"]');
			const last = cards[cards.length - 1];
			const sel = last && last.querySelector('[data-act="ui-action"]');
			if (sel) { sel.value = 'click'; sel.dispatchEvent(new Event('change', { bubbles: true })); }
			return true;
		}`)
		time.Sleep(200 * time.Millisecond)

		// 接口步骤支持独立落库配置
		check("接口步骤含独立落库配置", evalBool(page, `() => {
			const steps = document.querySelectorAll('#stepsBox [data-step]');
			const last = steps[steps.length - 1];
			return !!last && !!last.querySelector('.sink-box');
		}`), "步骤卡片缺少 sink-box")

		// 步骤卡片支持折叠（点击一次应切换状态）
		beforeCollapsed := evalBool(page, `() => { const c = document.querySelector('[data-ui="ui"]'); return !!c && c.classList.contains('collapsed'); }`)
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const b = document.querySelector('[data-ui="ui"] [data-act="card-toggle"]');
			if (b) b.click();
			return true;
		}`)
		time.Sleep(200 * time.Millisecond)
		afterCollapsed := evalBool(page, `() => { const c = document.querySelector('[data-ui="ui"]'); return !!c && c.classList.contains('collapsed'); }`)
		check("步骤卡片可折叠", beforeCollapsed != afterCollapsed, "折叠状态点击后未切换")

		// 接口步骤：独立分页 = 自定义时应展开分页字段
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const b = document.querySelector('[data-act="task-tab"][data-tab="steps"]');
			if (b) b.click();
			return true;
		}`)
		time.Sleep(200 * time.Millisecond)
		beforePag := evalInt(page, `() => {
			const steps = document.querySelectorAll('#stepsBox [data-step]');
			const last = steps[steps.length - 1];
			return last ? last.querySelectorAll('input,select,textarea').length : -1;
		}`)
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const steps = document.querySelectorAll('#stepsBox [data-step]');
			const last = steps[steps.length - 1];
			const sel = last && last.querySelector('[data-act="step-page-mode"]');
			if (sel) { sel.value = 'custom'; sel.dispatchEvent(new Event('change', { bubbles: true })); }
			return true;
		}`)
		time.Sleep(300 * time.Millisecond)
		afterPag := evalInt(page, `() => {
			const steps = document.querySelectorAll('#stepsBox [data-step]');
			const last = steps[steps.length - 1];
			return last ? last.querySelectorAll('input,select,textarea').length : -1;
		}`)
		check("接口步骤独立分页展开字段", beforePag > 0 && afterPag > beforePag, fmt.Sprintf("切换前 %d 个字段，切换后 %d 个", beforePag, afterPag))

		// 接口步骤：独立落库 = 单独指定数据库与表
		beforeSink := evalInt(page, `() => {
			const steps = document.querySelectorAll('#stepsBox [data-step]');
			const last = steps[steps.length - 1];
			const box = last && last.querySelector('.sink-box');
			return box ? box.querySelectorAll('input,select,textarea').length : -1;
		}`)
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const steps = document.querySelectorAll('#stepsBox [data-step]');
			const last = steps[steps.length - 1];
			const sel = last && last.querySelector('.sink-box [data-act="sink-mode"]');
			if (sel) { sel.value = 'custom'; sel.dispatchEvent(new Event('change', { bubbles: true })); }
			return true;
		}`)
		time.Sleep(300 * time.Millisecond)
		afterSink := evalInt(page, `() => {
			const steps = document.querySelectorAll('#stepsBox [data-step]');
			const last = steps[steps.length - 1];
			const box = last && last.querySelector('.sink-box');
			return box ? box.querySelectorAll('input,select,textarea').length : -1;
		}`)
		check("接口步骤独立落库展开数据库字段", beforeSink > 0 && afterSink > beforeSink, fmt.Sprintf("切换前 %d 个字段，切换后 %d 个", beforeSink, afterSink))
		// 还原为继承，避免影响保存
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const steps = document.querySelectorAll('#stepsBox [data-step]');
			const last = steps[steps.length - 1];
			const sel = last && last.querySelector('.sink-box [data-act="sink-mode"]');
			if (sel) { sel.value = 'inherit'; sel.dispatchEvent(new Event('change', { bubbles: true })); }
			const pm = last && last.querySelector('[data-act="step-page-mode"]');
			if (pm) { pm.value = 'inherit'; pm.dispatchEvent(new Event('change', { bubbles: true })); }
			return true;
		}`)

		// 抓取规则：翻页方式（页面点击翻页 / 接口翻页）
		setMode("ui")
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const b = document.querySelector('[data-act="task-tab"][data-tab="capture"]');
			if (b) b.click();
			return true;
		}`)
		time.Sleep(250 * time.Millisecond)
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			const cb = last && last.querySelector('[data-act="rule-pagination"]');
			if (cb && !cb.checked) cb.click();
			return true;
		}`)
		time.Sleep(300 * time.Millisecond)
		check("抓取规则含翻页方式下拉", evalBool(page, `() => !!document.querySelector('#capBox [data-cap] [data-act="rule-pg-mode"]')`), "缺少翻页方式下拉")
		check("点击翻页显示下一页选择器", evalBool(page, `() => !!document.querySelector('#capBox [data-cap] [data-act="rule-pg-next"]')`), "缺少下一页选择器输入框")
		check("点击翻页显示按钮文字与等待时间", evalBool(page, `() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			const t = last ? (last.innerText || '') : '';
			return t.indexOf('按钮文字') >= 0 && t.indexOf('点击后等待加载') >= 0;
		}`), "缺少点击翻页参数")
		check("点击翻页含按钮文字输入与识别按钮", evalBool(page, `() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			return !!last && !!last.querySelector('[data-act="rule-pg-text"]') && !!last.querySelector('[data-act="rule-pg-detect"]');
		}`), "缺少按钮文字输入框或「从录制动作识别」按钮")
		// 切到「接口翻页」：应出现「一键改成点击翻页」，且点击翻页字段隐藏
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			const m = last && last.querySelector('[data-act="rule-pg-mode"]');
			if (m) { m.value = 'http'; m.dispatchEvent(new Event('change', { bubbles: true })); }
			return true;
		}`)
		time.Sleep(350 * time.Millisecond)
		check("接口翻页显示一键切换按钮", evalBool(page, `() => !!document.querySelector('#capBox [data-cap] [data-act="rule-pg-to-ui"]')`), "接口翻页未提示可一键改成点击翻页")
		check("接口翻页不再显示点击翻页字段", evalBool(page, `() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			const t = last ? (last.innerText || '') : '';
			return t.indexOf('分页类型') >= 0 && t.indexOf('加载更多按钮选择器') < 0;
		}`), "接口翻页与点击翻页字段同时出现")
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const b = document.querySelector('#capBox [data-cap] [data-act="rule-pg-to-ui"]');
			if (b) b.click();
			return true;
		}`)
		time.Sleep(350 * time.Millisecond)
		check("一键改成点击翻页生效", evalBool(page, `() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			const m = last && last.querySelector('[data-act="rule-pg-mode"]');
			const txt = last && last.querySelector('[data-act="rule-pg-text"]');
			return !!m && m.value === 'ui' && !!txt && ((txt.value || '').length > 0);
		}`), "切换后未回到页面点击翻页或未填按钮文字")
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			const sel = last && last.querySelector('select');
			if (sel) { sel.value = 'dom'; sel.dispatchEvent(new Event('change', { bubbles: true })); }
			return true;
		}`)
		time.Sleep(400 * time.Millisecond)
		check("DOM 规则翻页方式锁定为点击翻页", evalBool(page, `() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			const m = last && last.querySelector('[data-act="rule-pg-mode"]');
			return !!m && m.disabled && m.value === 'ui' && !!last.querySelector('[data-act="rule-pg-next"]');
		}`), "DOM 规则的翻页方式未锁定为点击翻页")
		check("DOM 规则显示表格 iframe 输入", evalBool(page, `() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			return !!last && !!last.querySelector('[data-act="rule-frame"]');
		}`), "DOM 规则缺少「表格所在 iframe」输入框")
		check("DOM 规则翻页显示按钮 iframe 输入", evalBool(page, `() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			return !!last && !!last.querySelector('[data-act="rule-pg-frame"]');
		}`), "翻页配置缺少「下一页按钮所在 iframe」输入框")
		// 还原：切回网络规则并关闭翻页，避免影响后续保存
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			const sel = last && last.querySelector('select');
			if (sel) { sel.value = 'network'; sel.dispatchEvent(new Event('change', { bubbles: true })); }
			return true;
		}`)
		time.Sleep(300 * time.Millisecond)
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const last = cards[cards.length - 1];
			const cb = last && last.querySelector('[data-act="rule-pagination"]');
			if (cb && cb.checked) cb.click();
			return true;
		}`)
		time.Sleep(250 * time.Millisecond)
		setMode(orig)

		// 多抓取规则：命名后可搜索过滤
		addCase("capture", `#capBox [data-cap]`, "抓取规则（第二条）", `[data-act="add-rule"]`)
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const cards = document.querySelectorAll('#capBox [data-cap]');
			const names = ['门店列表规则', '订单列表规则'];
			cards.forEach((c, i) => {
				const inp = c.querySelector('.form-grid input');
				if (inp && names[i]) { inp.value = names[i]; inp.dispatchEvent(new Event('input', { bubbles: true })); }
			});
			return true;
		}`)
		time.Sleep(300 * time.Millisecond)
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const k = document.getElementById('ruleKw');
			if (k) { k.value = '门店'; k.dispatchEvent(new Event('input', { bubbles: true })); }
			return true;
		}`)
		time.Sleep(300 * time.Millisecond)
		visibleRules := evalInt(page, `() => Array.prototype.filter.call(document.querySelectorAll('#capBox [data-cap]'), c => c.offsetParent !== null).length`)
		totalRules := evalInt(page, `() => document.querySelectorAll('#capBox [data-cap]').length`)
		check("抓取规则搜索过滤", totalRules >= 2 && visibleRules == 1, fmt.Sprintf("共 %d 条，命中 %d 条", totalRules, visibleRules))
		_, _ = page.Timeout(5*time.Second).Eval(`() => {
			const k = document.getElementById('ruleKw');
			if (k) { k.value = ''; k.dispatchEvent(new Event('input', { bubbles: true })); }
			return true;
		}`)
		time.Sleep(200 * time.Millisecond)

		_, _ = page.Timeout(5*time.Second).Eval(`() => { const i = document.querySelector('#tName'); if (i) { i.value = 'UI冒烟测试任务'; i.dispatchEvent(new Event('input', {bubbles:true})); } return true }`)
		time.Sleep(300 * time.Millisecond)
		_, _ = page.Timeout(5*time.Second).Eval(`() => { document.querySelector('[data-act="save-task"]').click(); return true }`)
		time.Sleep(2500 * time.Millisecond)
		hash, _ := page.Timeout(5*time.Second).Eval(`() => location.hash`)
		h := hash.Value.Str()
		check("保存任务跳转详情", strings.HasPrefix(h, "#/task/"), h)

		// 已保存任务重新打开：抓取规则默认收起
		if strings.HasPrefix(h, "#/task/") {
			taskID := strings.TrimPrefix(h, "#/task/")
			_, _ = page.Timeout(10*time.Second).Eval(`(id) => { location.hash = '#/task/' + id; return true }`, taskID)
			time.Sleep(1800 * time.Millisecond)
			_, _ = page.Timeout(5*time.Second).Eval(`() => {
				const b = document.querySelector('[data-act="task-tab"][data-tab="capture"]');
				if (b) b.click();
				return true;
			}`)
			time.Sleep(400 * time.Millisecond)
			savedRules := evalInt(page, `() => document.querySelectorAll('#capBox [data-cap]').length`)
			collapsedRules := evalInt(page, `() => document.querySelectorAll('#capBox [data-cap].collapsed').length`)
			check("已保存任务抓取规则默认收起", savedRules >= 1 && collapsedRules == savedRules, fmt.Sprintf("共 %d 条，收起 %d 条", savedRules, collapsedRules))
		}
	}

	// 运行详情：按数据来源分组预览（有多个来源的运行记录才检查）
	runID := ""
	if res, err := page.Timeout(10*time.Second).Eval(`async () => {
		const r = await fetch('/api/runs?page_size=10');
		const j = await r.json();
		for (const run of ((j.data && j.data.list) || [])) {
			const s = await (await fetch('/api/runs/' + run.id + '/sources')).json();
			if (s.data && s.data.list && s.data.list.length > 1) return String(run.id);
		}
		return '';
	}`); err == nil && res != nil {
		runID = res.Value.Str()
	}
	if runID != "" {
		_, _ = page.Timeout(10*time.Second).Eval(`(id) => { location.hash = '#/run/' + id; return true }`, runID)
		time.Sleep(1500 * time.Millisecond)
		check("运行详情两个大页签", evalBool(page, `() => !!document.querySelector('[data-act="detail-tab-steps"]') && !!document.querySelector('[data-act="detail-tab-data"]')`), "缺少执行步骤/数据集预览页签")
		_, _ = page.Timeout(5*time.Second).Eval(`() => { const b = document.querySelector('[data-act="detail-tab-data"]'); if (b) b.click(); return true }`)
		time.Sleep(400 * time.Millisecond)
		check("运行详情可切到数据集预览", evalBool(page, `() => { const p = document.querySelector('[data-act="detail-tab-data"]'); return !!p && p.classList.contains('active'); }`), "页签未激活")
		_, _ = page.Timeout(5*time.Second).Eval(`() => { const b = document.querySelector('[data-act="detail-tab-steps"]'); if (b) b.click(); return true }`)
		time.Sleep(400 * time.Millisecond)
		check("执行步骤默认最近在前", evalBool(page, `() => { const i = Array.prototype.find.call(document.querySelectorAll('input[type=checkbox]'), x => (x.parentElement.innerText || '').indexOf('最近的在前') >= 0); return !!i && i.checked; }`), "默认未勾选最近的在前")
		check("运行详情按来源分组预览", evalBool(page, `() => document.querySelectorAll('[data-source]').length >= 2`), "未渲染数据来源按钮")
		check("默认选中第一个来源", evalBool(page, `() => {
			const b = document.querySelector('[data-source]');
			return !!b && b.classList.contains('primary');
		}`), "首个来源未高亮")
	} else {
		fmt.Println("  [skip] 运行详情来源分组（没有多来源的运行记录）")
	}

	// 定时作业编辑器
	_, _ = page.Timeout(10*time.Second).Eval(`() => { location.hash = '#/job/new'; return true }`)
	time.Sleep(1500 * time.Millisecond)
	addBtns := evalInt(page, `() => document.querySelectorAll('[data-act="add-jobstep"]').length`)
	check("定时作业编辑器渲染", addBtns == 3, fmt.Sprintf("添加按钮 %d 个（应为 3）", addBtns))
	_, _ = page.Timeout(5*time.Second).Eval(`() => { const b = document.querySelector('[data-act="add-jobstep"][data-kind="http"]'); if (b) b.click(); return true }`)
	time.Sleep(400 * time.Millisecond)
	stepsAfter := evalInt(page, `() => document.querySelectorAll('[data-jobstep]').length`)
	check("定时作业可添加 HTTP 步骤", stepsAfter == 1, fmt.Sprintf("步骤数 %d", stepsAfter))
	check("HTTP 步骤含提取变量配置", evalBool(page, `() => { const c = document.querySelector('[data-jobstep]'); return !!c && (c.innerText || '').indexOf('提取变量') >= 0; }`), "缺少提取变量输入框")
	_, _ = page.Timeout(5*time.Second).Eval(`() => { const b = document.querySelector('[data-act="add-jobstep"][data-kind="sql"]'); if (b) b.click(); return true }`)
	time.Sleep(400 * time.Millisecond)
	check("定时作业可添加 SQL 步骤", evalInt(page, `() => document.querySelectorAll('[data-jobstep]').length`) == 2, "SQL 步骤未添加")
	// Cron 常用计划
	presetCount := evalInt(page, `() => { const s = document.querySelector('[data-act="cron-preset"]'); return s ? s.options.length : -1; }`)
	check("Cron 提供常用计划", presetCount >= 10, fmt.Sprintf("下拉项 %d 个", presetCount))
	_, _ = page.Timeout(5*time.Second).Eval(`() => {
		const s = document.querySelector('[data-act="cron-preset"]');
		if (!s) return false;
		s.value = '0 0 8 * * *';
		s.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	}`)
	time.Sleep(300 * time.Millisecond)
	check("选择常用计划自动填入表达式", evalBool(page, `() => { const i = document.querySelector('[data-act="cron-expr"]'); return !!i && i.value === '0 0 8 * * *'; }`), "表达式未填入")
	_, _ = page.Timeout(5*time.Second).Eval(`() => {
		const s = document.querySelector('[data-act="cron-preset"]');
		if (!s) return false;
		s.value = 'custom';
		s.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	}`)
	time.Sleep(300 * time.Millisecond)
	check("切到自定义保留表达式", evalBool(page, `() => { const i = document.querySelector('[data-act="cron-expr"]'); return !!i && i.value === '0 0 8 * * *'; }`), "自定义切换后表达式被清空")

	if len(errs) > 0 {
		fmt.Println("JS 异常:")
		for _, e := range errs {
			fmt.Println("   ", e)
		}
	}
	check("无 JS 异常", len(errs) == 0, fmt.Sprintf("%d 条", len(errs)))

	if len(fails) > 0 {
		fmt.Println("失败项:", strings.Join(fails, ", "))
		os.Exit(1)
	}
	fmt.Println("全部通过")
}
