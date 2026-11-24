package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"collect/internal/applog"
	"collect/internal/model"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

// rod 的 Page 是值拷贝：Timeout()/Frame() 得到的新 Page 各自有一把独立的互斥锁，
// 但它们共享内部的 helpers map。多个协程（录制轮询、脚本注入、回放、会话采集）
// 同时对同一个页面求值会写坏这个 map，表现为 panic: assignment to entry in nil map。
// 这里按 CDP Target 建一张全局表，把同一页面的所有求值串行化。
var evalLocks sync.Map // map[proto.TargetTargetID]*sync.Mutex

// EvalLock 返回与这个页面（含其所有 iframe）关联的互斥锁，调用方负责加解锁
func EvalLock(page *rod.Page) *sync.Mutex {
	key := page.TargetID
	if key == "" {
		key = proto.TargetTargetID(fmt.Sprintf("%p", page))
	}
	v, _ := evalLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// tryEvalLock 在 wait 时间内尝试拿到页面求值锁；wait<=0 时只试一次。
// 抢不到返回 nil（调用方跳过本次操作即可，不要阻塞回放线程）。
func tryEvalLock(page *rod.Page, wait time.Duration) *sync.Mutex {
	l := EvalLock(page)
	if l.TryLock() {
		return l
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		if l.TryLock() {
			return l
		}
	}
	return nil
}

// Replayer 回放录制到的页面动作
type Replayer struct {
	Page    *rod.Page
	Rec     *Recorder
	Log     *applog.Logger
	Context context.Context
	Timeout time.Duration
	OnStep  func(i int, s model.UIStep, d time.Duration, err error)
	// OnStepStart 在每一步动作开始执行前回调，用于按真实开始时间分配日志序号
	OnStepStart func(i int, s model.UIStep)
	Headless    bool
	// autoFrames 记录已经提示过的「自动识别 iframe」，避免每一步重复写日志
	autoFrames map[string]bool
	// lastAutoFrame 当前步骤自动识别到的 iframe（用于点击兜底时不误用顶层坐标）
	lastAutoFrame string
}

func NewReplayer(page *rod.Page, maxBodyKB int, log *applog.Logger) *Replayer {
	rec := NewRecorder(page, maxBodyKB)
	// 回放场景只需要采集接口：不再向页面注入动作录制脚本、也不再轮询 DOM，
	// 避免录制线程与回放线程同时操作页面（rod 并发求值会 panic），回放也更快。
	rec.SkipActions = true
	return &Replayer{
		Page:    page,
		Rec:     rec,
		Log:     log,
		Timeout: 25 * time.Second,
	}
}

// WithContext makes all browser operations interruptible when a run is stopped.
func (r *Replayer) WithContext(ctx context.Context) *Replayer {
	r.Context = ctx
	if ctx != nil {
		r.Page = r.Page.Context(ctx)
	}
	return r
}

func (r *Replayer) Start() error { return r.Rec.Start() }
func (r *Replayer) Stop()        { r.Rec.Stop() }

func (r *Replayer) Run(steps []model.UIStep) error {
	var lastErr error
	for i, s := range steps {
		if r.Context != nil && r.Context.Err() != nil {
			return r.Context.Err()
		}
		start := time.Now()
		if r.OnStepStart != nil {
			r.OnStepStart(i+1, s)
		}
		err := r.Do(s)
		if r.OnStep != nil {
			r.OnStep(i+1, s, time.Since(start), err)
		}
		if err != nil {
			lastErr = fmt.Errorf("第 %d 步(%s)失败: %w", i+1, stepDesc(s), err)
			if r.Log != nil {
				r.Log.Warn("ui", "%v", lastErr)
			}
			// 打开/等待类失败允许继续，其余失败中止
			if s.Action == "click" || s.Action == "input" || s.Action == "select" || s.Action == "open" {
				return lastErr
			}
			if r.Context != nil && r.Context.Err() != nil {
				return r.Context.Err()
			}
		}
	}
	return lastErr
}

func stepDesc(s model.UIStep) string {
	switch s.Action {
	case "open":
		return "打开页面 " + s.Value
	case "click":
		return "点击 " + s.Selector
	case "input":
		return "输入 " + s.Selector
	case "select":
		return "选择 " + s.Selector
	case "press":
		return "按键 " + s.Key
	case "wait":
		return fmt.Sprintf("等待 %dms", s.Ms)
	case "wait_for":
		return "等待元素 " + s.Selector
	case "scroll":
		return "滚动"
	case "eval":
		return "执行脚本"
	}
	return s.Action
}

func (r *Replayer) Do(s model.UIStep) error {
	r.lastAutoFrame = ""
	// 与录制器的轮询/注入协程共用一把页面求值锁，避免并发求值相互踩坏状态
	lock := EvalLock(r.Page)
	lock.Lock()
	defer lock.Unlock()
	p := r.Page.Timeout(r.Timeout)
	// 除「打开页面」外，其余动作都可以指定所在 iframe；
	// 指定后就到 iframe 内部去查找元素 / 执行脚本，而不是主页面。
	if strings.TrimSpace(s.Frame) != "" && !strings.EqualFold(strings.TrimSpace(s.Action), "open") {
		fp, err := r.framePage(s.Frame)
		if err != nil {
			return err
		}
		p = fp.Timeout(r.Timeout)
	}
	switch s.Action {
	case "open":
		target := strings.TrimSpace(s.Value)
		if target == "" {
			target = strings.TrimSpace(s.Selector)
		}
		if target == "" {
			return errors.New("缺少打开地址")
		}
		if !strings.HasPrefix(target, "http") {
			target = "https://" + target
		}
		// 打开前先记一份会话快照：Cookie 数 / 本地存储项数。
		// 打开后如果变成登录页或者存储被清空，说明这一步把登录态弄丢了，日志里直接点出来。
		before := r.sessionSnap(p)
		if strings.EqualFold(strings.TrimSpace(s.OpenMode), "spa") {
			ok := r.spaOpen(p, target)
			if ok {
				after := r.sessionSnap(p)
				if r.Log != nil {
					r.Log.Info("ui", "站内跳转 %s（未刷新页面，登录态保留：Cookie %d / localStorage %d / sessionStorage %d）",
						shortURL(target, 90), after.Cookies, after.LS, after.SS)
				}
				return nil
			}
			if r.Log != nil {
				r.Log.Warn("ui", "「站内跳转」不适用（跨域或页面路由没响应 %s），已改为整页打开", shortURL(target, 90))
			}
		}
		if r.Log != nil && looksSPARoute(target) && sameOrigin(before.URL, target) {
			r.Log.Info("ui", "提示：%s 看起来是站内 SPA 路由，整页打开会重新加载页面并可能丢登录态；"+
				"若之后被踢回登录页，把这一步的「打开方式」改成「站内跳转（不刷新）」", shortURL(target, 90))
		}
		if err := p.Navigate(target); err != nil {
			return err
		}
		_ = p.WaitLoad()
		r.diagnoseOpen(p, target, before)
		return nil
	case "click":
		el, err := r.element(p, s.Selector, s)
		if err == nil {
			_ = el.ScrollIntoView()
			if err = el.Click(proto.InputMouseButtonLeft, 1); err == nil {
				time.Sleep(120 * time.Millisecond)
				return nil
			}
		}
		// 坐标兜底只在主页面有效：iframe 内记录的坐标是相对 iframe 的，
		// 换算到顶层页面会点偏，所以 iframe 内改用脚本点击兜底。
		if r.lastAutoFrame == "" && (s.X > 0 || s.Y > 0) {
			if err2 := r.Page.Mouse.MoveTo(proto.NewPoint(s.X, s.Y)); err2 == nil {
				if err3 := r.Page.Mouse.Click(proto.InputMouseButtonLeft, 1); err3 == nil {
					return nil
				}
			}
		}
		if s.Text != "" {
			ok, _ := r.clickByText(p, s.Text)
			if ok {
				return nil
			}
		}
		// iframe 内再兜底：用脚本直接触发点击（真实鼠标点击优先级更高，这里只是保命）
		if (strings.TrimSpace(s.Frame) != "" || r.lastAutoFrame != "") && strings.TrimSpace(s.Selector) != "" {
			fp := p
			if el != nil && el.Page() != nil {
				fp = el.Page()
			}
			if res, e := fp.Eval(`(sel) => { const el = document.querySelector(sel); if (!el) return false; el.click(); return true; }`, s.Selector); e == nil && res != nil && res.Value.Bool() {
				time.Sleep(120 * time.Millisecond)
				return nil
			}
		}
		return fmt.Errorf("未找到可点击元素: %s", s.Selector)
	case "input":
		el, err := r.element(p, s.Selector, s)
		if err != nil {
			return err
		}
		if err := el.ScrollIntoView(); err != nil {
			return err
		}
		if err := el.SelectAllText(); err != nil {
			// 部分输入框不支持选择，忽略
			_ = err
		}
		if err := el.Input(s.Value); err != nil {
			// 兜底：JS 赋值并触发 input/change 事件
			_, err2 := el.Eval(`(v) => {
				const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value');
				if (setter && setter.set) setter.set.call(this, v); else this.value = v;
				this.dispatchEvent(new Event('input', {bubbles: true}));
				this.dispatchEvent(new Event('change', {bubbles: true}));
				return true;
			}`, s.Value)
			if err2 != nil {
				return err
			}
		}
		return nil
	case "select":
		return r.doSelect(p, s)
	case "press":
		key := mapKey(s.Key)
		if s.Selector != "" {
			if el, err := r.element(p, s.Selector, s); err == nil {
				_ = el.Focus()
				if kp := el.Page(); kp != nil {
					return kp.Keyboard.Press(key)
				}
			}
		}
		return p.Keyboard.Press(key)
	case "wait":
		ms := s.Ms
		if ms <= 0 {
			ms = 1000
		}
		return sleepContext(p.GetContext(), time.Duration(ms)*time.Millisecond)
	case "wait_for":
		_, err := r.element(p, s.Selector, s)
		return err
	case "scroll":
		ms := s.Ms
		if ms <= 0 {
			ms = 600
		}
		_, err := p.Eval(fmt.Sprintf(`() => { window.scrollBy(0, %d); return true; }`, ms))
		return err
	case "eval":
		_, err := p.Eval(s.Value)
		return err
	default:
		return fmt.Errorf("未知动作: %s", s.Action)
	}
}

// doSelect 下拉选择：
//   - 原生 <select>：按 value / 文字选中 option（rod.Select 会触发 input + change）
//   - 自定义下拉（组件库）：先点开触发器，再在弹层里按 value 属性或文字点选项
func (r *Replayer) doSelect(p *rod.Page, s model.UIStep) error {
	trigger := strings.TrimSpace(s.Trigger)
	if trigger == "" {
		el, err := r.element(p, s.Selector, s)
		if err != nil {
			// 选择器可能本身就是自定义下拉的容器（老任务只录到容器）
			if ok, perr := r.pickCustomOption(p, s); ok {
				return nil
			} else if perr != nil {
				return perr
			}
			return err
		}
		// 必须看判断结果，而不是只看求值是否报错：
		// 选择器指向 div 容器时绝不能给它赋 value 假装成功
		isSelect := false
		if v, e := el.Eval(`() => (this.tagName || '').toLowerCase() === 'select'`); e == nil {
			isSelect = v.Value.Bool()
		}
		if isSelect {
			if err := el.Select([]string{s.Value}, true, rod.SelectorTypeText); err == nil {
				return nil
			}
			if _, err2 := el.Eval(`(v) => {
				this.value = v;
				this.dispatchEvent(new Event('input', {bubbles: true}));
				this.dispatchEvent(new Event('change', {bubbles: true}));
				return true;
			}`, s.Value); err2 == nil {
				return nil
			}
		}
		if ok, err := r.pickCustomOption(p, s); ok {
			return nil
		} else if err != nil {
			return err
		}
		return fmt.Errorf("未找到下拉选项: %s = %s", s.Selector, s.Value)
	}

	// 自定义下拉：先点开触发器（选择器优先，其次按文字）
	opened := false
	if el, err := r.element(p, trigger, model.UIStep{Selector: trigger, Frame: s.Frame, Text: s.TriggerText}); err == nil {
		_ = el.ScrollIntoView()
		if err := el.Click(proto.InputMouseButtonLeft, 1); err == nil {
			opened = true
		}
	}
	if !opened && strings.TrimSpace(s.TriggerText) != "" {
		if ok, _ := r.clickByText(p, s.TriggerText); ok {
			opened = true
		}
	}
	if !opened {
		return fmt.Errorf("未找到下拉触发器: %s", trigger)
	}
	time.Sleep(150 * time.Millisecond)
	if ok, err := r.pickCustomOption(p, s); ok {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("下拉已展开，但没有找到匹配 %q 的选项", s.Value)
}

// pickCustomOption 在已展开的下拉弹层里按 value 属性 / 文字点选项。
// 返回 ok=true 表示已点中；err 仅在出现硬错误（元素操作失败）时返回。
func (r *Replayer) pickCustomOption(p *rod.Page, s model.UIStep) (bool, error) {
	val := strings.TrimSpace(s.Value)
	txt := strings.TrimSpace(s.Text)
	if txt == "" {
		txt = val
	}
	optSel := strings.TrimSpace(s.Selector)
	// 选择器可能指向 option 本身（原生 select 的 option 录成了 selector）
	res, err := p.Timeout(4*time.Second).Eval(`(arg) => {
		const norm = (v) => (v == null ? '' : String(v)).replace(/\s+/g, ' ').trim();
		const val = norm(arg.val), txt = norm(arg.txt);
		const attrs = ['data-value', 'data-val', 'data-v', 'data-key', 'data-id', 'value'];
		const valOf = (el) => {
			for (const a of attrs) {
				const v = el.getAttribute(a);
				if (v !== null && v !== '') return v;
			}
			return norm(el.innerText);
		};
		const visible = (el) => {
			const r = el.getBoundingClientRect();
			const cs = getComputedStyle(el);
			return r.width > 0 && r.height > 0 && cs.display !== 'none' && cs.visibility !== 'hidden' && cs.opacity !== '0';
		};
		const cands = Array.from(document.querySelectorAll(
			'[role=option],[role=menuitem],[role=treeitem],li,dd,.el-select-dropdown__item,.ant-select-item-option,option'
		));
		let hit = null;
		if (arg.sel) {
			try {
				const direct = document.querySelector(arg.sel);
				// 直接命中的元素必须自己带选项语义，避免把「包含全部选项文字的容器」当成选项
				const optionish = direct && ['li','dd','option'].includes((direct.tagName||'').toLowerCase())
					|| direct && ['option','menuitem','treeitem'].includes((direct.getAttribute('role')||'').toLowerCase())
					|| direct && !!(direct.getAttribute('data-value') || direct.getAttribute('data-v') || direct.getAttribute('data-val'));
				if (direct && optionish && visible(direct) && (val === '' || valOf(direct) === val || norm(direct.innerText) === txt || norm(direct.innerText).indexOf(txt) >= 0)) {
					hit = direct;
				}
			} catch (e) {}
		}
		if (!hit) {
			const find = (pred) => cands.find((el) => visible(el) && pred(el));
			hit = find((el) => val !== '' && valOf(el) === val) ||
			      find((el) => txt !== '' && norm(el.innerText) === txt) ||
			      find((el) => txt !== '' && norm(el.innerText).indexOf(txt) >= 0);
		}
		if (!hit) return '';
		// 用真实鼠标事件序列，覆盖只在 mousedown 提交的组件库
		const rect = hit.getBoundingClientRect();
		const cx = rect.left + rect.width / 2, cy = rect.top + rect.height / 2;
		const mk = (type, Ctor, extra) => new Ctor(type, Object.assign({bubbles: true, cancelable: true, view: window, clientX: cx, clientY: cy}, extra || {}));
		hit.dispatchEvent(mk('pointerdown', PointerEvent));
		hit.dispatchEvent(mk('mousedown', MouseEvent));
		hit.dispatchEvent(mk('pointerup', PointerEvent));
		hit.dispatchEvent(mk('mouseup', MouseEvent));
		hit.dispatchEvent(mk('click', MouseEvent));
		return norm(hit.innerText) || valOf(hit);
	}`, map[string]any{"val": s.Value, "txt": txt, "sel": optSel})
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(res.Value.Str()) == "" {
		return false, nil
	}
	time.Sleep(120 * time.Millisecond)
	return true, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// framePage 按 iframe 的 CSS 选择器逐层进入，返回 iframe 内部的 Page。
// 支持 ">>" 分隔的嵌套 iframe，例如 "iframe#main >> iframe.inner"。
// iframe 选择器为空时返回主页面（保持旧任务行为不变）。
func (r *Replayer) framePage(sel string) (*rod.Page, error) {
	return PageForFrame(r.Page, sel, r.Timeout)
}

// PageForFrame 按 iframe 的 CSS 选择器逐层进入，返回 iframe 内部的 Page。
// 支持 ">>" 分隔的嵌套 iframe，例如 "iframe#main >> iframe.inner"。
// 选择器为空时返回主页面（保持旧任务行为不变）。
func PageForFrame(page *rod.Page, sel string, timeout time.Duration) (*rod.Page, error) {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return page, nil
	}
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	p := page
	parts := strings.Split(sel, ">>")
	depth := 0
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		depth++
		el, err := p.Timeout(timeout).Element(part)
		if err != nil {
			if depth == 1 {
				return nil, fmt.Errorf("未找到 iframe（%s）：页面里没有匹配的 <iframe>/<frame>，请检查 iframe 选择器", part)
			}
			return nil, fmt.Errorf("未找到第 %d 层 iframe（%s）", depth, part)
		}
		fp, err := el.Frame()
		if err != nil {
			return nil, fmt.Errorf("进入 iframe（%s）失败：%v", part, err)
		}
		p = fp
	}
	return p, nil
}

func (r *Replayer) element(p *rod.Page, sel string, s model.UIStep) (*rod.Element, error) {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return nil, fmt.Errorf("元素选择器为空")
	}
	// 选择器没有显式填「所在 iframe」时，先给主页面一个较短的等待；
	// 找不到就自动到所有 iframe 里找。这样老任务（录制时没带 frame）
	// 也能直接回放，用户不用先去查 iframe 地址 / 选择器。
	mainWait := r.Timeout
	deadline := time.Now().Add(r.Timeout)
	if strings.TrimSpace(s.Frame) == "" {
		mainWait = 6 * time.Second
		if r.Timeout > 0 && mainWait > r.Timeout {
			mainWait = r.Timeout
		}
	}
	el, err := p.Timeout(mainWait).Element(sel)
	if err == nil {
		return el, nil
	}
	// 兼容 XPath
	if strings.HasPrefix(sel, "/") {
		if el2, err2 := p.Timeout(5 * time.Second).ElementX(sel); err2 == nil {
			return el2, nil
		}
	}
	// 主页面没有：自动在 iframe 里找同一个选择器
	if strings.TrimSpace(s.Frame) == "" {
		if el2, fsel, err2 := findFrameElement(r.Page, sel, 1200*time.Millisecond); err2 == nil {
			r.logAutoFrame(sel, fsel)
			_ = el2.ScrollIntoView()
			return el2, nil
		}
	}
	if s.Text != "" {
		if el3, err3 := p.Timeout(5*time.Second).ElementR("button|a|span|div|li|label", regexQuote(s.Text)); err3 == nil {
			return el3, nil
		}
		if strings.TrimSpace(s.Frame) == "" {
			if el3, fsel, err3 := findFrameText(r.Page, s.Text, 1200*time.Millisecond); err3 == nil {
				r.logAutoFrame("文字:"+s.Text, fsel)
				return el3, nil
			}
		}
	}
	// 页面本身加载慢：iframe 里也没有时，用剩余时间再等一次主页面。
	if strings.TrimSpace(s.Frame) == "" {
		if remain := time.Until(deadline); remain > time.Second {
			if el4, err4 := p.Timeout(remain).Element(sel); err4 == nil {
				return el4, nil
			}
		}
		// iframe 可能是在主页面等待期间才挂载出来的，最后再扫一次。
		if el5, fsel, err5 := findFrameElement(r.Page, sel, 1200*time.Millisecond); err5 == nil {
			r.logAutoFrame(sel, fsel)
			_ = el5.ScrollIntoView()
			return el5, nil
		}
	}
	return nil, err
}

// logAutoFrame 在日志里告诉用户自动进入了哪个 iframe，方便之后把值固化到「所在 iframe」
func (r *Replayer) logAutoFrame(probe, frameSel string) {
	if r.Log == nil || strings.TrimSpace(frameSel) == "" {
		return
	}
	if r.autoFrames == nil {
		r.autoFrames = map[string]bool{}
	}
	if r.autoFrames[frameSel] {
		r.lastAutoFrame = frameSel
		return
	}
	r.autoFrames[frameSel] = true
	r.lastAutoFrame = frameSel
	r.Log.Info("ui", "「%s」不在主页面，已自动进入 iframe %s（可把该值填到「所在 iframe」）", probe, frameSel)
}

// findFrameElement 在所有 iframe（含嵌套）里查找选择器，返回第一个可见元素与 iframe 选择器。
func findFrameElement(page *rod.Page, sel string, timeout time.Duration) (*rod.Element, string, error) {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return nil, "", errors.New("选择器为空")
	}
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}
	for i, f := range walkFrames(page, 1200*time.Millisecond, 5) {
		if i >= 16 {
			break
		}
		el, err := f.Page.Timeout(timeout).Element(sel)
		if err != nil || el == nil {
			continue
		}
		if vis, e := el.Visible(); e == nil && !vis {
			continue
		}
		return el, f.Sel, nil
	}
	return nil, "", fmt.Errorf("所有 iframe 里都没找到：%s", sel)
}

// findFrameText 在所有 iframe 里按文字找可点击元素。
func findFrameText(page *rod.Page, text string, timeout time.Duration) (*rod.Element, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, "", errors.New("文字为空")
	}
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}
	for i, f := range walkFrames(page, 1200*time.Millisecond, 5) {
		if i >= 16 {
			break
		}
		el, err := f.Page.Timeout(timeout).ElementR("button|a|span|div|li|label", regexQuote(text))
		if err != nil || el == nil {
			continue
		}
		if vis, e := el.Visible(); e == nil && !vis {
			continue
		}
		return el, f.Sel, nil
	}
	return nil, "", fmt.Errorf("所有 iframe 里都没找到文字：%s", text)
}

// DetectFrameForSelector 在页面所有 iframe 中找选择器，返回可直接填到「所在 iframe」的值。
// 找不到返回空字符串与错误。
func DetectFrameForSelector(page *rod.Page, selector string, timeout time.Duration) (string, error) {
	_, fsel, err := findFrameElement(page, selector, timeout)
	return fsel, err
}

// PageHasSelector 快速判断当前页面（主文档）里有没有这个元素。
func PageHasSelector(page *rod.Page, selector string, timeout time.Duration) bool {
	if strings.TrimSpace(selector) == "" {
		return false
	}
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}
	lock := EvalLock(page)
	lock.Lock()
	defer lock.Unlock()
	_, err := page.Timeout(timeout).Element(selector)
	return err == nil
}

func regexQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `.`, `\.`, `+`, `\+`, `*`, `\*`, `?`, `\?`, `(`, `\(`, `)`, `\)`, `[`, `\[`, `]`, `\]`, `{`, `\{`, `}`, `\}`, `^`, `\^`, `$`, `\$`, `|`, `\|`)
	return r.Replace(s)
}

func (r *Replayer) clickByText(p *rod.Page, text string) (bool, error) {
	res, err := p.Timeout(6*time.Second).Eval(`(t) => {
		const all = Array.from(document.querySelectorAll('a,button,[role=button],li,span,div,label'));
		const el = all.find(e => ((e.innerText || '').trim() === t) && e.offsetParent !== null);
		if (!el) return false;
		el.click();
		return true;
	}`, strings.TrimSpace(text))
	if err != nil {
		return false, err
	}
	return res.Value.Bool(), nil
}

// clickableJS 判断一个元素是否「可见且可点击」：
// 禁用、aria-disabled、class 含 disabled、隐藏、pointer-events:none 都视为不可点击。
// 用 getBoundingClientRect + getComputedStyle，兼容 position:fixed（offsetParent 为 null）的元素。
const clickableJS = `(e) => {
	if (!e) return false;
	if (e.disabled) return false;
	if ((e.getAttribute('aria-disabled') || '').toLowerCase() === 'true') return false;
	if ((e.getAttribute('aria-hidden') || '').toLowerCase() === 'true') return false;
	const cls = (e.className || '').toString().toLowerCase();
	if (cls.includes('disabled') || cls.includes('is-disabled')) return false;
	const st = window.getComputedStyle(e);
	if (!st || st.display === 'none' || st.visibility === 'hidden' || st.pointerEvents === 'none') return false;
	if (Number(st.opacity) === 0) return false;
	const rc = e.getBoundingClientRect();
	return rc.width > 0 && rc.height > 0;
}`

// clickStateJS 返回元素状态：0=不可见/不存在，1=可见但不可点（置灰、disabled），2=可点击。
const clickStateJS = `(e) => {
	if (!e) return 0;
	const st = window.getComputedStyle(e);
	if (!st || st.display === 'none' || st.visibility === 'hidden' || Number(st.opacity) === 0) return 0;
	const rc = e.getBoundingClientRect();
	if (!(rc.width > 0 && rc.height > 0)) return 0;
	if (e.disabled) return 1;
	if ((e.getAttribute('aria-disabled') || '').toLowerCase() === 'true') return 1;
	const cls = (e.className || '').toString().toLowerCase();
	if (cls.includes('disabled') || cls.includes('is-disabled')) return 1;
	if (st.pointerEvents === 'none') return 1;
	return 2;
}`

// ClickNext 点击「下一页 / 加载更多」按钮，用于纯 UI 模式的真实点击翻页。
// 返回 clicked=false 表示按钮不存在 / 已禁用（视为已到末页），不是错误。
// 文字支持包含匹配：页面显示「下一页 ›」「Next >」时，填「下一页」「Next」即可。
func (r *Replayer) ClickNext(selector, text, frame string) (bool, error) {
	selector = strings.TrimSpace(selector)
	text = strings.TrimSpace(text)
	if selector == "" && text == "" {
		return false, errors.New("未配置下一页按钮选择器或按钮文字")
	}
	lock := EvalLock(r.Page)
	lock.Lock()
	defer lock.Unlock()
	p := r.Page
	if strings.TrimSpace(frame) != "" {
		fp, err := r.framePage(frame)
		if err != nil {
			return false, err
		}
		p = fp
	}
	if selector != "" {
		clicked, found, err := r.clickNextBySelector(p, selector)
		if err != nil || clicked {
			return clicked, err
		}
		// 选择器命中了「看得见但点不动」的按钮（置灰 / disabled）= 已经到最后一页，
		// 此时不要再按文字兜底，否则可能误点到别的元素（如分页容器）造成空转。
		// 只有选择器完全没命中（站点改版 / 选择器写错）时才用按钮文字兜底。
		if found {
			return false, nil
		}
		// 没填「下一页按钮所在 iframe」时，自动去 iframe 里找按钮。
		if strings.TrimSpace(frame) == "" {
			if el, fsel, e := findFrameElement(r.Page, selector, 900*time.Millisecond); e == nil {
				r.logAutoFrame("下一页 "+selector, fsel)
				if clicked, _, e2 := r.clickNextElement(el); e2 == nil && clicked {
					return true, nil
				}
			}
		}
	}
	if text == "" {
		return false, nil
	}
	clicked, err := r.clickNextByText(p, text)
	if err != nil || clicked {
		return clicked, err
	}
	if strings.TrimSpace(frame) == "" {
		if el, fsel, e := findFrameText(r.Page, text, 900*time.Millisecond); e == nil {
			r.logAutoFrame("下一页 "+text, fsel)
			if c, _, e2 := r.clickNextElement(el); e2 == nil {
				return c, nil
			}
		}
	}
	return false, nil
}

// clickNextElement 对一个已定位的「下一页」元素做可见/置灰判断并真实点击。
func (r *Replayer) clickNextElement(el *rod.Element) (clicked bool, found bool, err error) {
	if el == nil {
		return false, false, nil
	}
	// Element.Eval 把元素作为 this 传入；箭头函数拿不到 this，
	// 必须显式用 this 调 clickStateJS，否则状态永远是 0（元素被判成不可见）。
	state, e := el.Eval(`() => (` + clickStateJS + `)(this)`)
	if e != nil || state == nil {
		return false, false, e
	}
	switch state.Value.Int() {
	case 0:
		return false, false, nil
	case 1:
		return false, true, nil
	}
	if err := el.ScrollIntoView(); err != nil {
		return false, false, err
	}
	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return false, false, err
	}
	time.Sleep(120 * time.Millisecond)
	return true, true, nil
}

// clickNextBySelector 在匹配到的元素里挑第一个「可见且未禁用」的，真实鼠标点击。
// found 表示选择器命中了「可见」的元素（即使它是置灰不可点的）。
func (r *Replayer) clickNextBySelector(p *rod.Page, selector string) (clicked bool, found bool, err error) {
	els, err := p.Timeout(r.Timeout).Elements(selector)
	if err != nil || els == nil {
		return false, false, nil
	}
	for _, el := range els {
		res, e := el.Eval(`() => (` + clickStateJS + `)(this)`)
		if e != nil || res == nil {
			continue
		}
		switch res.Value.Int() {
		case 0: // 不可见：跳过（可能是隐藏的模板元素）
			continue
		case 1: // 可见但置灰：记为「找到了但点不动」
			found = true
			continue
		}
		found = true
		if err := el.ScrollIntoView(); err != nil {
			continue
		}
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			continue
		}
		time.Sleep(120 * time.Millisecond)
		return true, true, nil
	}
	return false, found, nil
}

// clickNextByText 按按钮文字（含 aria-label / title，支持包含匹配）找按钮，
// 命中后打临时标记再用真实鼠标点击，避免误点外层容器。
func (r *Replayer) clickNextByText(p *rod.Page, text string) (bool, error) {
	res, err := p.Timeout(10*time.Second).Eval(`(t) => {
		const target = String(t || '').trim().toLowerCase();
		if (!target) return false;
		const clickable = `+clickableJS+`;
		const label = (e) => (((e.innerText || e.textContent || '') + ' ' +
			(e.getAttribute('aria-label') || '') + ' ' + (e.getAttribute('title') || ''))
			.replace(/\s+/g, ' ').trim().toLowerCase());
		// 只考虑「按钮语义」的元素，不要 div：容器 div 也含文字，点了没反应
		const all = Array.from(document.querySelectorAll('a,button,[role=button],li,span,input[type=button]'));
		const cands = [];
		for (const e of all) {
			if (!clickable(e)) continue;
			const txt = label(e);
			if (!txt || txt.length > 40) continue;
			let score = -1;
			if (txt === target) score = 100;
			else if (txt.startsWith(target)) score = 85;
			else if (txt.indexOf(target) >= 0) score = 70;
			else continue;
			// 文本越短越可能是按钮本身，避免命中包裹容器
			score -= Math.min(25, Math.max(0, txt.length - target.length));
			cands.push({ e, score });
		}
		if (!cands.length) return false;
		// 优先点最内层的元素：去掉「包含其它候选」的外层容器
		const inner = cands.filter((c) => !cands.some((o) => o.e !== c.e && c.e.contains(o.e)));
		const pool = inner.length ? inner : cands;
		let best = null, bestScore = -1;
		for (const c of pool) {
			if (c.score > bestScore) { bestScore = c.score; best = c.e; }
		}
		if (!best) return false;
		document.querySelectorAll('[data-collect-next]').forEach((x) => x.removeAttribute('data-collect-next'));
		best.setAttribute('data-collect-next', '1');
		return true;
	}`, text)
	if err != nil {
		return false, err
	}
	if res == nil || !res.Value.Bool() {
		return false, nil
	}
	el, err := p.Timeout(5 * time.Second).Element(`[data-collect-next="1"]`)
	if err != nil {
		return false, nil
	}
	_, _ = p.Eval(`() => { document.querySelectorAll('[data-collect-next]').forEach((x) => x.removeAttribute('data-collect-next')); return true }`)
	if err := el.ScrollIntoView(); err != nil {
		return false, err
	}
	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return false, err
	}
	time.Sleep(120 * time.Millisecond)
	return true, nil
}

func mapKey(k string) input.Key {
	k = strings.TrimSpace(k)
	switch strings.ToLower(k) {
	case "enter":
		return input.Enter
	case "tab":
		return input.Tab
	case "escape", "esc":
		return input.Escape
	case "space":
		return input.Space
	case "backspace":
		return input.Backspace
	case "arrowdown", "down":
		return input.ArrowDown
	case "arrowup", "up":
		return input.ArrowUp
	case "arrowleft", "left":
		return input.ArrowLeft
	case "arrowright", "right":
		return input.ArrowRight
	case "delete", "del":
		return input.Delete
	case "home":
		return input.Home
	case "end":
		return input.End
	case "pageup":
		return input.PageUp
	case "pagedown":
		return input.PageDown
	}
	// 注意：必须在 TrimSpace 之后再判断单字符，否则「 a 」这类带空格
	// 输入会落不进单字符分支，静默退化成 Enter 触发了错误的按键。
	if len([]rune(k)) == 1 {
		return input.Key(k[0])
	}
	return input.Enter
}

// frameRef 一个已定位的 iframe（选择器 + 该 iframe 内的 Page）
type frameRef struct {
	Sel  string
	Page *rod.Page
}

// walkFrames 递归遍历页面里的 iframe，返回每个 iframe 的 CSS 选择器与内部 Page。
// 登录页、数据页放在 iframe 里时，录制动作用它注入脚本，会话采集用它读 storage。
// 选择器用 ">>" 连接嵌套层级，可直接填到「所在 iframe」里复用。
func walkFrames(page *rod.Page, timeout time.Duration, maxDepth int) []frameRef {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	var out []frameRef
	var walk func(p *rod.Page, prefix string, depth int)
	walk = func(p *rod.Page, prefix string, depth int) {
		if depth >= maxDepth {
			return
		}
		els, err := p.Timeout(timeout).Elements("iframe,frame")
		if err != nil || els == nil {
			return
		}
		for i, el := range els {
			sel := fmt.Sprintf("iframe:nth-of-type(%d)", i+1)
			if id, e := el.Attribute("id"); e == nil && id != nil && strings.TrimSpace(*id) != "" {
				sel = "#" + strings.TrimSpace(*id)
			} else if nm, e := el.Attribute("name"); e == nil && nm != nil && strings.TrimSpace(*nm) != "" {
				sel = `iframe[name="` + strings.TrimSpace(*nm) + `"]`
			}
			if prefix != "" {
				sel = prefix + " >> " + sel
			}
			fp, e := el.Frame()
			if e != nil {
				continue
			}
			out = append(out, frameRef{Sel: sel, Page: fp})
			walk(fp, sel, depth+1)
		}
	}
	walk(page, "", 0)
	return out
}

// Harvest 收集浏览器会话（Cookie + localStorage + sessionStorage）
// localStorage / sessionStorage 会一并采集所有 iframe 内的（主页面优先）。
func Harvest(page *rod.Page) (map[string]string, []*http.Cookie, error) {
	lock := EvalLock(page)
	lock.Lock()
	defer lock.Unlock()
	out := map[string]string{}
	cookies, err := page.Timeout(10 * time.Second).Cookies([]string{})
	if err != nil {
		cookies = nil
	}
	hc := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		// Chrome 对会话 Cookie 返回的过期时间为 0 或 -1，
		// 直接转成 time.Time 会变成 1970 年而被 CookieJar 立即丢弃
		var expires time.Time
		if epoch := float64(c.Expires); epoch > 0 {
			expires = c.Expires.Time()
		}
		hc = append(hc, &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			Expires:  expires,
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
		})
		out["cookie."+c.Name] = c.Value
	}
	type storageDump struct {
		Local   map[string]string `json:"local"`
		Session map[string]string `json:"session"`
	}
	const dumpJS = `() => {
		const dump = (st) => { const o = {}; try { for (let i = 0; i < st.length; i++) { const k = st.key(i); o[k] = st.getItem(k); } } catch (e) {} return o; };
		return JSON.stringify({local: dump(localStorage), session: dump(sessionStorage)});
	}`
	pages := []*rod.Page{page}
	for _, f := range walkFrames(page, 3*time.Second, 5) {
		pages = append(pages, f.Page)
	}
	for _, p := range pages {
		res, e := p.Timeout(10 * time.Second).Eval(dumpJS)
		if e != nil || res == nil {
			continue
		}
		var data storageDump
		if json.Unmarshal([]byte(res.Value.Str()), &data) != nil {
			continue
		}
		for k, v := range data.Local {
			if _, exist := out["local."+k]; !exist {
				out["local."+k] = v
			}
		}
		for k, v := range data.Session {
			if _, exist := out["session."+k]; !exist {
				out["session."+k] = v
			}
		}
	}
	cur, err := page.Timeout(5 * time.Second).Info()
	if err == nil && cur != nil {
		out["__url"] = cur.URL
	}
	return out, hc, nil
}

// AddCookiesToJar 把浏览器 Cookie 写入 http.CookieJar
func AddCookiesToJar(jar http.CookieJar, cookies []*http.Cookie, rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	jar.SetCookies(u, cookies)
}

// ---------- 「打开页面」的会话诊断 / 站内跳转 ----------

// sessSnap 打开页面前的会话快照，用来判断这一步有没有把登录态弄丢
type sessSnap struct {
	URL     string
	Cookies int
	LS      int
	SS      int
}

func (r *Replayer) sessionSnap(p *rod.Page) sessSnap {
	var s sessSnap
	if info, err := p.Timeout(5 * time.Second).Info(); err == nil && info != nil {
		s.URL = info.URL
	}
	if ck, err := (proto.NetworkGetCookies{}).Call(p); err == nil && ck != nil {
		s.Cookies = len(ck.Cookies)
	}
	res, err := p.Timeout(5 * time.Second).Eval(`() => {
		try {
			const l = (window.localStorage && localStorage.length) || 0;
			const s = (window.sessionStorage && sessionStorage.length) || 0;
			return l + '/' + s;
		} catch (e) { return '0/0' }
	}`)
	if err == nil && res != nil {
		fmt.Sscanf(strings.TrimSpace(res.Value.Str()), "%d/%d", &s.LS, &s.SS)
	}
	return s
}

// diagnoseOpen 整页打开之后记录会话状态；如果被踢回登录页就明确告警，
// 避免用户以为是「步骤没执行」而反复排查。
func (r *Replayer) diagnoseOpen(p *rod.Page, target string, before sessSnap) {
	after := r.sessionSnap(p)
	if r.Log != nil {
		r.Log.Info("ui", "打开 %s（Cookie %d 个 / localStorage %d 项 / sessionStorage %d 项）",
			shortURL(target, 90), after.Cookies, after.LS, after.SS)
	}
	if after.URL == "" || isLoginURL(after.URL) == false || isLoginURL(before.URL) {
		return
	}
	if r.Log == nil {
		return
	}
	r.Log.Warn("ui", "打开 %s 后页面跳到了登录页 %s —— 这一步把登录态弄丢了。"+
		"可以：① 该步骤的「打开方式」改成「站内跳转（不刷新）」；② 改成在页面里点菜单 / 链接进入；"+
		"③ 确认这条打开步骤排在登录步骤之后", shortURL(target, 90), shortURL(after.URL, 90))
}

// spaOpen 在同源页面上做站内路由跳转（不重新加载文档），
// 用于 SPA 把登录态放在内存里（刷新就丢）的站点。返回 true 表示跳转已生效。
func (r *Replayer) spaOpen(p *rod.Page, target string) bool {
	cu, err := p.Timeout(5 * time.Second).Info()
	if err != nil || cu == nil || cu.URL == "" {
		return false
	}
	cur, err1 := url.Parse(cu.URL)
	tu, err2 := url.Parse(target)
	if err1 != nil || err2 != nil || cur.Host == "" || tu.Host == "" {
		return false
	}
	if !strings.EqualFold(cur.Scheme, tu.Scheme) || !strings.EqualFold(cur.Host, tu.Host) {
		return false
	}
	quoted, _ := json.Marshal(target)
	js := fmt.Sprintf(`() => {
		try {
			const t = new URL(%s);
			if (t.pathname === location.pathname && t.search === location.search && t.hash) {
				location.hash = t.hash;
				return 'hash';
			}
			history.pushState({}, '', t.pathname + t.search + t.hash);
			window.dispatchEvent(new PopStateEvent('popstate', { state: null }));
			window.dispatchEvent(new HashChangeEvent('hashchange'));
			return 'push';
		} catch (e) { return '' }
	}`, string(quoted))
	res, err := p.Timeout(10 * time.Second).Eval(js)
	if err != nil || res == nil || res.Value.Str() == "" {
		return false
	}
	time.Sleep(350 * time.Millisecond)
	// 校验 URL 真的变了，否则说明页面接管了跳转（例如守卫拦回登录页），退回整页打开逻辑
	now, err := p.Timeout(5 * time.Second).Info()
	if err != nil || now == nil || !samePageURL(now.URL, target) {
		return false
	}
	return true
}

// looksSPARoute 粗判一个地址是不是站内 hash 路由（#/xxx），用于提示改用站内跳转
func looksSPARoute(raw string) bool {
	s := strings.TrimSpace(raw)
	i := strings.Index(s, "#/")
	return i > 0
}

// sameOrigin 判断两个地址是否同源（协议 + 域名 + 端口）
func sameOrigin(a, b string) bool {
	ua, err1 := url.Parse(strings.TrimSpace(a))
	ub, err2 := url.Parse(strings.TrimSpace(b))
	if err1 != nil || err2 != nil || ua.Host == "" || ub.Host == "" {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host)
}

// isLoginURL 粗判一个地址是不是登录页
func isLoginURL(raw string) bool {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return false
	}
	for _, k := range []string{
		"/login", "#/login", "login.html", "login.htm", "/signin", "#/signin",
		"/sso", "passport", "oauth2/authorize", "/auth/login", "/auth/signin",
	} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// samePageURL 比较两个地址是否指向同一页面（忽略末尾斜杠与 host 大小写）
func samePageURL(a, b string) bool {
	ua, err1 := url.Parse(strings.TrimSpace(a))
	ub, err2 := url.Parse(strings.TrimSpace(b))
	if err1 != nil || err2 != nil {
		return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
	}
	if !strings.EqualFold(ua.Host, ub.Host) {
		return false
	}
	if strings.TrimRight(ua.Path, "/") != strings.TrimRight(ub.Path, "/") {
		return false
	}
	return ua.RawQuery == ub.RawQuery && ua.Fragment == ub.Fragment
}

// shortURL 截断长地址，只用于日志
func shortURL(raw string, n int) string {
	r := []rune(strings.TrimSpace(raw))
	if n <= 0 || len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
