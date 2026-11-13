package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// CapRequest 录制/回放期间捕获的一个网络请求
type CapRequest struct {
	ID            string            `json:"id"`
	Seq           int               `json:"seq"`
	Method        string            `json:"method"`
	URL           string            `json:"url"`
	ResourceType  string            `json:"resource_type"`
	ReqHeaders    map[string]string `json:"req_headers"`
	ReqBody       string            `json:"req_body"`
	Status        int               `json:"status"`
	RespHeaders   map[string]string `json:"resp_headers"`
	RespBody      string            `json:"resp_body"`
	MimeType      string            `json:"mime_type"`
	StartedAt     time.Time         `json:"started_at"`
	DurationMs    int64             `json:"duration_ms"`
	Failed        bool              `json:"failed"`
	Error         string            `json:"error"`
	BodyTruncated bool              `json:"body_truncated"`
	IsJSON        bool              `json:"is_json"`
	Size          int               `json:"size"`
}

// Action 录制到的页面动作
type Action struct {
	Type     string  `json:"type"`
	Selector string  `json:"selector"`
	Text     string  `json:"text"`
	Value    string  `json:"value"`
	Key      string  `json:"key"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	URL      string  `json:"url"`
	At       int64   `json:"at"`
	// Frame 该动作发生在哪个 iframe（CSS 选择器，可能是 "a >> b" 嵌套）；空 = 主页面
	Frame string `json:"frame,omitempty"`
}

// debugDrain 打开后会把 iframe 动作抓取的诊断信息打到 stderr（COLLECT_DEBUG_DRAIN=1）
var debugDrain = os.Getenv("COLLECT_DEBUG_DRAIN") != ""

type Recorder struct {
	page    *rod.Page
	maxBody int

	// OnRequest 在捕获到新的网络请求（RequestWillBeSent）时回调，
	// 用于让调用方按真实请求发起时间分配步骤序号
	OnRequest func(CapRequest)

	mu       sync.Mutex
	reqs     []*CapRequest
	index    map[string]*CapRequest
	seq      int
	actions  []Action
	navURL   string
	stopOnce sync.Once
	stopCh   chan struct{}
	stopFn   func()
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	started  bool

	// SkipActions 为 true 时只采集网络请求，不录页面动作
	// （回放采集场景用：动作是脚本给定的，没必要再往页面注入录制脚本，
	//  也避免录制轮询与回放抢同一页面求值锁拖慢步骤）
	SkipActions bool
}

func NewRecorder(page *rod.Page, maxBodyKB int) *Recorder {
	if maxBodyKB <= 0 {
		maxBodyKB = 256
	}
	return &Recorder{page: page, maxBody: maxBodyKB * 1024, index: map[string]*CapRequest{}, stopCh: make(chan struct{})}
}

func headerMap(h proto.NetworkHeaders) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		s := strings.Trim(v.String(), `"`)
		if s == "" {
			s = fmt.Sprint(v)
		}
		out[k] = s
	}
	return out
}

func (r *Recorder) Start() error {
	if r.started {
		return nil
	}
	r.started = true
	// 保留较大的响应缓存，便于取回响应体
	total, res := 200<<20, 50<<20
	_ = proto.NetworkEnable{MaxTotalBufferSize: &total, MaxResourceBufferSize: &res}.Call(r.page)
	_ = proto.PageEnable{}.Call(r.page)
	// 对所有新文档（含后续动态创建的 iframe）自动注入录制脚本，
	// 这样 iframe 内嵌的登录框 / 列表页也能录到动作。
	if !r.SkipActions {
		evalLocked(r.page, 10*time.Second, func(p *rod.Page) {
			_, _ = p.EvalOnNewDocument("(" + recordJS + ")()")
		})
	}
	// rod 的 EachEvent 返回的是阻塞的事件分发循环，必须放到独立协程里运行，
	// 否则录制期间事件不会分发（会一直积压到 Stop）。
	ctx, cancel := context.WithCancel(r.page.GetContext())
	r.cancel = cancel
	r.stopFn = r.page.Context(ctx).EachEvent(
		func(e *proto.NetworkRequestWillBeSent) {
			if e.Request == nil {
				return
			}
			typ := string(e.Type)
			req := &CapRequest{
				ID:           string(e.RequestID),
				Method:       e.Request.Method,
				URL:          e.Request.URL,
				ResourceType: typ,
				ReqHeaders:   headerMap(e.Request.Headers),
				ReqBody:      e.Request.PostData,
				StartedAt:    time.Now(),
			}
			if e.RedirectResponse != nil {
				req.Status = e.RedirectResponse.Status
			}
			r.mu.Lock()
			isNew := false
			if old, ok := r.index[req.ID]; ok {
				old.URL = req.URL
				old.Method = req.Method
				old.ReqHeaders = req.ReqHeaders
				old.ReqBody = req.ReqBody
				old.ResourceType = typ
			} else {
				r.seq++
				req.Seq = r.seq
				r.reqs = append(r.reqs, req)
				r.index[req.ID] = req
				isNew = true
			}
			cb := r.OnRequest
			var snapshot CapRequest
			if isNew {
				snapshot = *req
			}
			r.mu.Unlock()
			if isNew && cb != nil {
				cb(snapshot)
			}
		},
		func(e *proto.NetworkResponseReceived) {
			if e.Response == nil {
				return
			}
			r.mu.Lock()
			req, ok := r.index[string(e.RequestID)]
			r.mu.Unlock()
			if !ok {
				return
			}
			r.mu.Lock()
			req.Status = e.Response.Status
			req.RespHeaders = headerMap(e.Response.Headers)
			req.MimeType = e.Response.MIMEType
			req.ResourceType = string(e.Type)
			if !req.StartedAt.IsZero() {
				req.DurationMs = time.Since(req.StartedAt).Milliseconds()
			}
			isText := strings.Contains(strings.ToLower(req.MimeType), "json") ||
				strings.Contains(strings.ToLower(req.MimeType), "text") ||
				strings.Contains(strings.ToLower(req.MimeType), "javascript") ||
				strings.Contains(strings.ToLower(req.MimeType), "xml") ||
				strings.HasPrefix(strings.TrimSpace(req.RespBody), "{") ||
				strings.HasPrefix(strings.TrimSpace(req.RespBody), "[")
			r.mu.Unlock()
			if isText {
				r.fetchBody(string(e.RequestID))
			}
		},
		func(e *proto.NetworkLoadingFinished) {
			r.mu.Lock()
			req, ok := r.index[string(e.RequestID)]
			var typ string
			if ok {
				typ = req.ResourceType
				if req.DurationMs == 0 && !req.StartedAt.IsZero() {
					req.DurationMs = time.Since(req.StartedAt).Milliseconds()
				}
			}
			r.mu.Unlock()
			if !ok {
				return
			}
			switch strings.ToLower(typ) {
			case "image", "stylesheet", "font", "media", "script", "other":
				if !strings.Contains(strings.ToLower(req.MimeType), "json") {
					return
				}
			}
			r.fetchBody(string(e.RequestID))
		},
		func(e *proto.NetworkLoadingFailed) {
			r.mu.Lock()
			if req, ok := r.index[string(e.RequestID)]; ok {
				req.Failed = true
				req.Error = e.ErrorText
			}
			r.mu.Unlock()
		},
		func(e *proto.PageFrameNavigated) {
			if e.Frame == nil {
				return
			}
			if e.Frame.ParentID != "" {
				// 子 iframe 导航/新建：重新注入录制脚本（轮询每轮都会重新扫描 iframe）
				go r.inject()
				return
			}
			url := e.Frame.URL
			r.mu.Lock()
			if url != "" && url != r.navURL && url != "about:blank" {
				r.navURL = url
				r.actions = append(r.actions, Action{Type: "navigate", URL: url, At: time.Now().UnixMilli()})
			}
			r.mu.Unlock()
			go r.inject()
		},
		func(e *proto.PageLoadEventFired) {
			go r.inject()
		},
	)
	go r.stopFn()
	if !r.SkipActions {
		r.inject()
		go r.poll()
	}
	return nil
}

func (r *Recorder) fetchBody(id string) {
	r.mu.Lock()
	req, ok := r.index[id]
	if !ok || req.RespBody != "" {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		res, err := proto.NetworkGetResponseBody{RequestID: proto.NetworkRequestID(id)}.
			Call(r.page.Timeout(8 * time.Second))
		if err != nil || res == nil {
			return
		}
		body := res.Body
		if res.Base64Encoded {
			if b, err := base64.StdEncoding.DecodeString(body); err == nil {
				body = string(b)
			}
		}
		truncated := false
		if len(body) > r.maxBody {
			body = body[:r.maxBody]
			truncated = true
		}
		r.mu.Lock()
		req.RespBody = body
		req.BodyTruncated = truncated
		req.Size = len(body)
		trim := strings.TrimSpace(body)
		req.IsJSON = strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[")
		if req.DurationMs == 0 && !req.StartedAt.IsZero() {
			req.DurationMs = time.Since(req.StartedAt).Milliseconds()
		}
		r.mu.Unlock()
	}()
}

func (r *Recorder) poll() {
	t := time.NewTicker(350 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-t.C:
			// 轮询抢不到页面求值锁（回放正在操作页面）就跳过本轮，
			// 动作都缓存在页面里的 window.__rec，下一轮再取不会丢。
			r.drainActions(0)
		}
	}
}

// evalLocked 拿到页面求值锁后执行 fn；wait 内抢不到锁返回 false（调用方跳过本次即可）。
func evalLocked(page *rod.Page, wait time.Duration, fn func(p *rod.Page)) bool {
	l := tryEvalLock(page, wait)
	if l == nil {
		return false
	}
	defer l.Unlock()
	fn(page)
	return true
}

// framePages 返回需要采集动作的页面（主页面 + 各 iframe），带选择器标注。
// 每次都重新扫描：iframe 导航后旧 Page 的 jsCtxID 会失效（读到的是加载前的空窗口），
// 缓存复用会导致 iframe 里的动作一直录不到，所以宁可贵一点也要拿新的。
func (r *Recorder) framePages(wait time.Duration) []frameRef {
	var frames []frameRef
	evalLocked(r.page, wait, func(p *rod.Page) {
		frames = walkFrames(p, 1200*time.Millisecond, 5)
	})
	return frames
}

// drainActions 把主页面与所有 iframe 里累计的录制动作取出来。
// wait>0 时会在该时长内等待页面求值锁（Stop 收尾用），否则抢不到就放弃本轮。
// 注意：每个页面单独抢锁，取完立刻释放，避免长时间阻塞回放。
func (r *Recorder) drainActions(wait time.Duration) {
	pages := append([]frameRef{{Sel: "", Page: r.page}}, r.framePages(wait)...)
	if debugDrain {
		fmt.Fprintf(os.Stderr, "[drain] pages=%d\n", len(pages))
	}
	for _, f := range pages {
		var raw string
		var evalErr error
		got := false
		evalLocked(f.Page, wait, func(p *rod.Page) {
			res, err := p.Timeout(4 * time.Second).Eval(`() => {
				if (!window.__rec) return '[]';
				const a = window.__rec;
				window.__rec = [];
				return JSON.stringify(a);
			}`)
			if err != nil || res == nil {
				evalErr = err
				return
			}
			raw, got = res.Value.Str(), true
		})
		if !got {
			if debugDrain {
				fmt.Fprintf(os.Stderr, "[drain] frame=%q eval 未成功: %v\n", f.Sel, evalErr)
			}
			continue
		}
		if debugDrain && strings.TrimSpace(raw) != "[]" {
			fmt.Fprintf(os.Stderr, "[drain] frame=%q raw=%s\n", f.Sel, raw)
		}
		var list []Action
		if json.Unmarshal([]byte(raw), &list) != nil {
			continue
		}
		if len(list) == 0 {
			continue
		}
		for i := range list {
			if list[i].Frame == "" {
				list[i].Frame = f.Sel
			}
		}
		r.mu.Lock()
		for _, a := range list {
			// 连续输入合并：同一 iframe 内同一元素只保留最后一次输入
			if a.Type == "input" && len(r.actions) > 0 {
				last := r.actions[len(r.actions)-1]
				if last.Type == "input" && last.Selector == a.Selector && last.Frame == a.Frame && a.At-last.At < 3000 {
					r.actions[len(r.actions)-1] = a
					continue
				}
			}
			r.actions = append(r.actions, a)
		}
		r.mu.Unlock()
	}
}

func (r *Recorder) inject() {
	if r.SkipActions {
		return
	}
	// 主页面 + 已存在的 iframe 都注入一遍（页面跳转中可能失败，忽略）。
	// 每次求值单独抢锁，避免占着锁把回放步骤堵住。
	evalLocked(r.page, 2*time.Second, func(p *rod.Page) {
		_, _ = p.Timeout(1500 * time.Millisecond).Eval("(" + recordJS + ")()")
	})
	for _, f := range r.framePages(1500 * time.Millisecond) {
		evalLocked(f.Page, 1200*time.Millisecond, func(p *rod.Page) {
			_, _ = p.Timeout(1500 * time.Millisecond).Eval("(" + recordJS + ")()")
		})
	}
}

func (r *Recorder) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
	})
	// 取消事件分发上下文即可结束 EachEvent 的循环（不要直接调用 stopFn，它会一直阻塞）
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
	}
	// 最后一次拾取动作
	if !r.SkipActions {
		r.drainActions(3 * time.Second)
	}
}

func (r *Recorder) Requests() []CapRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CapRequest, 0, len(r.reqs))
	for _, q := range r.reqs {
		out = append(out, *q)
	}
	return out
}

func (r *Recorder) Actions() []Action {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Action, len(r.actions))
	copy(out, r.actions)
	return out
}

const recordJS = `() => {
  if (window.__recordInstalled) return true;
  window.__recordInstalled = true;
  window.__rec = [];
  const push = (o) => { try { window.__rec.push(o); } catch (e) {} };
  const cpath = (el) => {
    if (!el || el.nodeType !== 1) return '';
    try {
      if (el.id) return '#' + CSS.escape(el.id);
      const tid = el.getAttribute('data-testid') || el.getAttribute('data-test');
      if (tid) return '[data-testid="' + String(tid).replace(/"/g, '\\"') + '"]';
      const nm = el.getAttribute('name');
      if (nm) return el.tagName.toLowerCase() + '[name="' + String(nm).replace(/"/g, '\\"') + '"]';
      const parts = [];
      let cur = el, depth = 0;
      while (cur && cur.nodeType === 1 && depth < 6) {
        let part = cur.tagName.toLowerCase();
        const parent = cur.parentElement;
        if (parent) {
          const same = Array.prototype.filter.call(parent.children, c => c.tagName === cur.tagName);
          if (same.length > 1) part += ':nth-of-type(' + (same.indexOf(cur) + 1) + ')';
        }
        parts.unshift(part);
        if (cur.id) { parts[0] = '#' + CSS.escape(cur.id); break; }
        cur = parent; depth++;
      }
      return parts.join(' > ');
    } catch (e) { return ''; }
  };
  const info = (el) => ({ selector: cpath(el), text: ((el.innerText || el.value || el.getAttribute('aria-label') || '') + '').trim().slice(0, 80), url: location.href, at: Date.now() });
  document.addEventListener('click', (ev) => {
    let el = ev.target;
    if (!el || el.nodeType !== 1) return;
    const tag = (el.tagName || '').toLowerCase();
    if (tag === 'input' && (el.type === 'text' || el.type === 'password' || el.type === 'number' || el.type === 'search' || el.type === 'email')) return;
    if (tag === 'textarea') return;
    el = el.closest('button,a,[role=button],input,select,textarea,label,li,td,th,img,span,i') || el;
    const o = info(el); o.type = 'click'; o.x = ev.clientX; o.y = ev.clientY;
    push(o);
  }, true);
  const timers = new WeakMap();
  document.addEventListener('input', (ev) => {
    const el = ev.target;
    if (!el || !el.tagName) return;
    const tag = el.tagName.toLowerCase();
    if (tag !== 'input' && tag !== 'textarea') return;
    const ty = (el.type || 'text').toLowerCase();
    if (ty === 'checkbox' || ty === 'radio' || ty === 'file' || ty === 'range') return;
    if (timers.has(el)) clearTimeout(timers.get(el));
    timers.set(el, setTimeout(() => { const o = info(el); o.type = 'input'; o.value = el.value; push(o); }, 350));
  }, true);
  document.addEventListener('change', (ev) => {
    const el = ev.target;
    if (!el || !el.tagName) return;
    const tag = el.tagName.toLowerCase();
    const ty = (el.type || '').toLowerCase();
    const o = info(el);
    if (tag === 'select') { o.type = 'select'; o.value = el.value; push(o); return; }
    if (ty === 'checkbox' || ty === 'radio') { o.type = 'input'; o.value = el.checked ? 'true' : 'false'; push(o); }
  }, true);
  document.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter' || ev.key === 'Tab') {
      const o = info(ev.target);
      o.type = 'press'; o.key = ev.key;
      push(o);
    }
  }, true);
  return true;
}`
