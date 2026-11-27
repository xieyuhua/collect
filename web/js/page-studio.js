/* ============================================================
   录制工作台
   ============================================================ */
(function () {
  'use strict';
  const C = Collect;

  /* ---------------- 请求详情弹窗 ---------------- */
  C.component('req-detail-modal', {
    props: { req: { type: Object, required: true } },
    setup(props) {
      function hdrs(o) {
        const list = Object.entries(o || {}).map(function (kv) { return kv[0] + ': ' + kv[1]; });
        return list.length ? list.join('\n') : '-';
      }
      return { hdrs: hdrs, fmt: C.fmt, fmtDur: C.fmtDur, bytes: C.bytes, jsonPretty: C.jsonPretty };
    },
    template: `
    <div>
      <div class="kv-list">
        <span class="k">地址</span><span class="code">{{ req.method }} {{ req.url }}</span>
        <span class="k">状态码</span>
        <span>{{ req.status }} <span v-if="req.failed" class="tag red">失败</span></span>
        <span class="k">资源类型</span><span>{{ req.resource_type || '-' }}</span>
        <span class="k">耗时</span><span>{{ fmtDur(req.duration_ms) }}</span>
        <span class="k">大小</span><span>{{ bytes(req.size) }} <span v-if="req.body_truncated">（已截断）</span></span>
        <span class="k">请求时间</span><span>{{ fmt(req.started_at) }}</span>
      </div>
      <h4>请求头</h4><div class="pre">{{ hdrs(req.req_headers) }}</div>
      <h4>请求体</h4><div class="pre">{{ jsonPretty(req.req_body) || '-' }}</div>
      <h4>响应头</h4><div class="pre">{{ hdrs(req.resp_headers) }}</div>
      <h4>响应体</h4><div class="pre">{{ jsonPretty(req.resp_body) || '-' }}</div>
    </div>`,
  });

  /* ---------------- 动作复现 JS 弹窗 ---------------- */
  C.component('action-js-modal', {
    props: { js: { type: String, required: true }, action: { type: Object, default: () => ({}) } },
    setup(props) {
      const copied = C.ref(false);
      // 复制：navigator.clipboard 只在 HTTPS/localhost 可用；
      // 通过 http://IP:8686 访问时必须用 execCommand 兜底。
      function fallbackCopy(text) {
        try {
          const ta = document.createElement('textarea');
          ta.value = text;
          ta.setAttribute('readonly', '');
          ta.style.position = 'fixed';
          ta.style.top = '-9999px';
          document.body.appendChild(ta);
          ta.select();
          ta.setSelectionRange(0, ta.value.length);
          const ok = document.execCommand('copy');
          document.body.removeChild(ta);
          return ok;
        } catch (e) { return false; }
      }
      async function copy() {
        let ok = false;
        try {
          if (navigator.clipboard && window.isSecureContext) {
            await navigator.clipboard.writeText(props.js);
            ok = true;
          }
        } catch (e) { ok = false; }
        if (!ok) ok = fallbackCopy(props.js);
        if (ok) {
          copied.value = true;
          setTimeout(() => { copied.value = false; }, 1500);
        } else {
          C.toast('复制失败：请手动选中上面的代码复制', 'warn');
        }
      }
      return { copied, copy };
    },
    template: `
    <div>
      <div class="hint" style="margin-bottom:8px">
        把下面的代码粘贴到<b>目标页面</b>的浏览器控制台（F12）执行，验证这一步能否选到元素并触发页面反应。
        执行后 console 会打印命中的元素；没命中说明回放也会在这里失败。
      </div>
      <div class="pre" style="max-height:400px;overflow:auto" @click="$event.target.tagName === 'PRE' && (function(n){ const r=document.createRange(); r.selectNodeContents(n); const s=window.getSelection(); s.removeAllRanges(); s.addRange(r); })($event.currentTarget)">{{ js }}</div>
      <div class="row" style="margin-top:10px;justify-content:flex-end">
        <button class="btn sm" data-act="action-js-copy" @click="copy">{{ copied ? '已复制 ✓' : '复制代码' }}</button>
      </div>
    </div>`,
  });

  C.component('page-studio', {
    setup() {
      const sessions = C.reactive([]);
      const studioId = C.ref('');
      const session = C.reactive({});
      const requests = C.reactive([]);
      const actions = C.reactive([]);
      const diagnostics = C.reactive([]);
      const showDiag = C.ref(false);
      const jsonOnly = C.ref(true);
      const keyword = C.ref('');
      const selReq = C.ref('');
      const starting = C.ref(false);
      const candidates = C.reactive([]);
      const draftKw = C.ref('');
      const draft = C.reactive({
        name: '', mode: 'api', login_req_id: '', list_req_id: '', list_req_ids: [], start_url: '',
      });
      const draftNotes = C.reactive([]);
      const form = C.reactive({ url: '', mode: 'api' });
      let timer = 0;
      let sessionTimer = 0;

      async function loadSessions() {
        try {
          const list = await C.api('/studio/sessions');
          sessions.splice(0, sessions.length, ...(list || []));
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      async function refreshCapture() {
        if (!studioId.value) return;
        try {
          const d = await C.api('/studio/sessions/' + studioId.value + '/capture');
          Object.assign(session, d.session || {});
          requests.splice(0, requests.length, ...(d.requests || []));
          actions.splice(0, actions.length, ...(d.actions || []));
          diagnostics.splice(0, diagnostics.length, ...(d.diagnostics || []));
          candidates.splice(0, candidates.length, ...(d.candidates || []));
          // 首次加载时默认勾选最像数据列表的那个接口
          if (!draft.list_req_ids.length && candidates.length) {
            draft.list_req_ids.push(candidates[0].id);
            draft.list_req_id = candidates[0].id;
          }
        } catch (e) { /* 轮询错误忽略 */ }
      }

      function startPolling() {
        clearInterval(timer);
        timer = setInterval(refreshCapture, 1600);
      }

      Vue.onMounted(async function () {
        await loadSessions();
        // 录制中会话的时长需要实时变化，列表定时刷新
        sessionTimer = setInterval(loadSessions, 5000);
        if (studioId.value) { await refreshCapture(); startPolling(); }
      });
      Vue.onUnmounted(function () { clearInterval(timer); clearInterval(sessionTimer); });

      async function start() {
        if (!String(form.url || '').trim()) { C.toast('请填写网址', 'err'); return; }
        starting.value = true;
        try {
          const s = await C.api('/studio/sessions', { method: 'POST', body: { url: form.url, mode: form.mode } });
          studioId.value = s.id;
          selReq.value = '';
          // 关键：草稿模式必须跟随本次录制模式。之前只有 openSession 会同步，
          // 直接「启动录制 → 生成草稿」时 draft.mode 还是默认的 api，
          // 导致纯 UI 录到的动作按「纯接口」生成，UI 步骤全部丢失。
          if (s.mode === 'api' || s.mode === 'ui' || s.mode === 'hybrid') draft.mode = s.mode;
          C.toast('录制已开始，请在弹出的浏览器窗口操作', 'ok');
          await loadSessions();
          await refreshCapture();
          startPolling();
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          starting.value = false;
        }
      }

      async function openSession(s) {
        studioId.value = s.id;
        selReq.value = '';
        // 草稿默认跟随录制会话的模式，避免在「纯 UI」录完却按「纯接口」生成，
        // 结果 UI 步骤一个都不出现
        if (s.mode === 'api' || s.mode === 'ui' || s.mode === 'hybrid') draft.mode = s.mode;
        await loadSessions();
        await refreshCapture();
        startPolling();
      }

      async function stopSession(s) {
        try {
          await C.api('/studio/sessions/' + s.id + '/stop', { method: 'POST', body: {} });
          C.toast('已停止录制', 'ok');
          await loadSessions();
          await refreshCapture();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      async function removeSession(s) {
        if (!confirm('确定删除该会话吗？')) return;
        try {
          await C.api('/studio/sessions/' + s.id, { method: 'DELETE' });
          if (studioId.value === s.id) { studioId.value = ''; clearInterval(timer); }
          await loadSessions();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      const filtered = C.computed(function () {
        const kw = String(keyword.value || '').toLowerCase();
        return requests.filter(function (r) {
          if (jsonOnly.value && !r.is_json) return false;
          if (kw && String(r.url || '').toLowerCase().indexOf(kw) < 0) return false;
          return true;
        });
      });

      const jsonReqs = C.computed(function () {
        return requests.filter(function (r) { return r.is_json; });
      });

      const filteredCands = C.computed(function () {
        const kw = String(draftKw.value || '').trim().toLowerCase();
        if (!kw) return candidates;
        return candidates.filter(function (c) {
          return String(c.url || '').toLowerCase().indexOf(kw) >= 0 ||
            String(c.name || '').toLowerCase().indexOf(kw) >= 0;
        });
      });

      const selectedCount = C.computed(function () {
        return (draft.list_req_ids || []).length;
      });

      function toggleCand(c) {
        const ids = draft.list_req_ids;
        const i = ids.indexOf(c.id);
        if (i < 0) ids.push(c.id); else ids.splice(i, 1);
        draft.list_req_id = ids[0] || '';
      }

      function candChecked(c) {
        return (draft.list_req_ids || []).indexOf(c.id) >= 0;
      }

      function selectAllCands() {
        const ids = filteredCands.value.map(function (c) { return c.id; });
        ids.forEach(function (id) { if (draft.list_req_ids.indexOf(id) < 0) draft.list_req_ids.push(id); });
        draft.list_req_id = draft.list_req_ids[0] || '';
      }

      function clearCands() {
        draft.list_req_ids.splice(0, draft.list_req_ids.length);
        draft.list_req_id = '';
      }

      async function showReq(r) {
        selReq.value = r.id;
        try {
          const detail = await C.api('/studio/sessions/' + studioId.value + '/requests/' + r.id);
          C.openModal(detail.method + ' ' + C.shortUrl(detail.url, 70), 'req-detail-modal', { req: detail });
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      function actionLabel(a) {
        const f = a.frame ? ('［iframe ' + a.frame + '］') : '';
        if (a.type === 'click') return f + '点击 ' + (a.selector || a.text || '');
        if (a.type === 'input') return f + '输入 ' + (a.selector || '') + ' = ' + (a.value || a.text || '');
        if (a.type === 'navigate') return f + '打开 ' + (a.url || '');
        if (a.type === 'select') return f + '选择 ' + (a.selector || '') + ' = ' + (a.value || a.text || '');
        return f + a.type + ' ' + (a.selector || '');
      }

      // 生成动作的等价复现 JS：粘贴到目标页面控制台执行，
      // 验证「这一步到底能不能选到元素、选到后有没有反应」。
      // 逻辑与回放引擎（replay.go）一一对应，方便排错时对照。
      // 生成动作的等价复现 JS：粘贴到目标页面控制台执行，
      // 验证「这一步到底能不能选到元素、选到后有没有反应」。
      // 与回放引擎（replay.go）对应：先尝试主文档，找不到就进入 iframe 并用
      // 「去掉 iframe 前缀后的选择器」查找（iframe 元素本身不在其内部文档里）。
      function actionJS(a) {
        const sel = a.selector || '';
        const frame = a.frame || '';
        const L = [];
        // ---- 统一元素定位辅助：返回 { el, doc, where } 或 null ----
        L.push(`function __locate(sel, frame) {`);
        L.push(`  const strip = (s) => {`);
        L.push(`    if (frame && s.startsWith(frame + ' > ')) return s.slice(frame.length + 3);`);
        L.push(`    return s.replace(/^(#[\\w-]+|iframe[^>]*) > /, '');`);
        L.push(`  };`);
        L.push(`  const main = document.querySelector(sel);`);
        L.push(`  if (main) return { el: main, doc: document, where: '主文档' };`);
        L.push(`  const frames = frame ? Array.from(document.querySelectorAll(frame)) : Array.from(document.querySelectorAll('iframe,frame'));`);
        L.push(`  const inner = strip(sel);`);
        L.push(`  for (const f of frames) {`);
        L.push(`    let d; try { d = f.contentDocument; } catch (e) { continue; }`);
        L.push(`    if (!d) continue;`);
        L.push(`    let hit = null; try { hit = d.querySelector(sel) || d.querySelector(inner); } catch (e) {}`);
        L.push(`    if (hit) {`);
        L.push(`      const label = f.id ? '#' + f.id : (f.name || f.src || 'iframe');`);
        L.push(`      return { el: hit, doc: d, where: 'iframe ' + label + (inner !== sel ? '（去前缀: ' + inner + '）' : '') };`);
        L.push(`    }`);
        L.push(`  }`);
        L.push(`  return null;`);
        L.push(`}`);
        L.push(``);

        if (a.type === 'click') {
          if (sel) {
            L.push(`const got = __locate(${JSON.stringify(sel)}, ${JSON.stringify(frame)});`);
            L.push(`if (!got) { console.warn('❌ 未找到元素。请确认前面的操作已完成，或元素是否在跨域 iframe 内。'); }`);
            L.push(`else { console.log('✅ 命中 @ ' + got.where, got.el); got.el.scrollIntoView && got.el.scrollIntoView({block:'center'}); got.el.click(); console.log('已点击'); }`);
          } else if (a.text) {
            L.push(`// 无选择器：按文字找（与回放兜底一致）`);
            L.push(`const els = [...document.querySelectorAll('a,button,[role=button],li,span,div,label')].filter(e => (e.innerText||'').trim() === ${JSON.stringify(a.text)});`);
            L.push(`console.log('命中', els.length, '个'); els[0] && els[0].click();`);
          } else {
            L.push(`const el = document.elementFromPoint(${a.x}, ${a.y}); console.log('该坐标下的元素:', el);`);
            L.push(`el && el.click();`);
          }
        } else if (a.type === 'press') {
          const key = a.key || 'Enter';
          L.push(`const got = __locate(${JSON.stringify(sel)}, ${JSON.stringify(frame)});`);
          L.push(`if (!got) { console.warn('❌ 未找到元素，无法聚焦按键'); } else {`);
          L.push(`  const el = got.el, doc = got.doc;`);
          L.push(`  console.log('✅ 命中 @ ' + got.where, el);`);
          L.push(`  el.scrollIntoView && el.scrollIntoView({block:'center'});`);
          L.push(`  el.focus && el.focus();`);
          L.push(`  const key = ${JSON.stringify(key)};`);
          L.push(`  const codeMap = { Enter: 13, Tab: 9, Escape: 27, ' ': 32, ArrowUp: 38, ArrowDown: 40, ArrowLeft: 37, ArrowRight: 39 };`);
          L.push(`  const keyCode = codeMap[key] || (key.length === 1 ? key.toUpperCase().charCodeAt(0) : 0);`);
          L.push(`  const win = doc.defaultView || window;`);
          L.push(`  const opt = { key: key, code: key === ' ' ? 'Space' : key, keyCode: keyCode, which: keyCode, bubbles: true, cancelable: true };`);
          L.push(`  el.dispatchEvent(new win.KeyboardEvent('keydown', opt));`);
          L.push(`  el.dispatchEvent(new win.KeyboardEvent('keypress', opt));`);
          L.push(`  el.dispatchEvent(new win.KeyboardEvent('keyup', opt));`);
          L.push(`  console.log('已派发按键 ' + key);`);
          if (key == 'Enter') {
            L.push(`  // Enter 常见副作用：表单提交 / 相邻查询按钮`);
            L.push(`  const form = el.form || el.closest('form');`);
            L.push(`  if (form) {`);
            L.push(`    if (form.requestSubmit) { try { form.requestSubmit(); } catch (e) {} }`);
            L.push(`    else { try { form.submit(); } catch (e) {} }`);
            L.push(`  } else {`);
            L.push(`    const box = el.closest('li,div');`);
            L.push(`    const btn = box && box.querySelector('button,input[type=button],a');`);
            L.push(`    if (btn) { btn.click(); console.log('已点击相邻按钮'); }`);
            L.push(`  }`);
          }
          L.push(`}`);
        } else if (a.type === 'select') {
          if (a.trigger) {
            L.push(`const gotT = __locate(${JSON.stringify(a.trigger)}, ${JSON.stringify(frame)});`);
            L.push(`if (!gotT) { console.warn('❌ 未找到下拉触发器'); } else {`);
            L.push(`  gotT.el.click();`);
            L.push(`  console.log('已点开下拉，500ms 后选择选项…');`);
            L.push(`  setTimeout(() => {`);
            L.push(`    const doc = gotT.doc, win = doc.defaultView || window;`);
            L.push(`    const norm = s => (s||'').replace(/\\s+/g,' ').trim();`);
            L.push(`    const val = ${JSON.stringify(a.value || '')}, txt = norm(${JSON.stringify(a.text || a.value || '')});`);
            L.push(`    const valOf = el => ['data-value','data-val','data-v','value'].map(x=>el.getAttribute(x)).find(v=>v) || norm(el.innerText);`);
            L.push(`    const cands = [...doc.querySelectorAll('[role=option],li,dd,span,div,.el-select-dropdown__item,.ant-select-item-option')]`);
            L.push(`      .filter(e => { const r=e.getBoundingClientRect(); const cs=getComputedStyle(e); return r.width>0&&r.height>0&&cs.display!=='none'&&cs.visibility!=='hidden'; });`);
            L.push(`    const hit = cands.find(e => txt && norm(e.innerText) === txt) || cands.find(e => val && valOf(e) === val) || cands.find(e => txt && norm(e.innerText).includes(txt));`);
            L.push(`    console.log('选项命中:', hit);`);
            L.push(`    if (hit) {`);
            L.push(`      const r = hit.getBoundingClientRect(), cx = r.left + r.width/2, cy = r.top + r.height/2;`);
            L.push(`      const mk = (t, C) => new C(t, {bubbles:true, cancelable:true, clientX:cx, clientY:cy});`);
            L.push(`      hit.dispatchEvent(mk('pointerdown', win.PointerEvent)); hit.dispatchEvent(mk('mousedown', win.MouseEvent));`);
            L.push(`      hit.dispatchEvent(mk('pointerup', win.PointerEvent)); hit.dispatchEvent(mk('mouseup', win.MouseEvent));`);
            L.push(`      hit.dispatchEvent(mk('click', win.MouseEvent));`);
            L.push(`      console.log('已选择');`);
            L.push(`    }`);
            L.push(`  }, 500);`);
            L.push(`}`);
          } else {
            L.push(`const got = __locate(${JSON.stringify(sel)}, ${JSON.stringify(frame)});`);
            L.push(`if (!got) { console.warn('❌ 未找到 select 元素'); } else {`);
            L.push(`  const el = got.el, doc = got.doc, win = doc.defaultView || window;`);
            L.push(`  el.value = ${JSON.stringify(a.value || '')};`);
            L.push(`  el.dispatchEvent(new win.Event('input', {bubbles:true}));`);
            L.push(`  el.dispatchEvent(new win.Event('change', {bubbles:true}));`);
            L.push(`  console.log('已设置并派发 change，当前值:', el.value);`);
            L.push(`}`);
          }
        } else if (a.type === 'input') {
          L.push(`const got = __locate(${JSON.stringify(sel)}, ${JSON.stringify(frame)});`);
          L.push(`if (!got) { console.warn('❌ 未找到输入框'); } else {`);
          L.push(`  const el = got.el, doc = got.doc, win = doc.defaultView || window;`);
          L.push(`  el.focus(); el.select && el.select();`);
          L.push(`  const proto = el.tagName === 'TEXTAREA' ? win.HTMLTextAreaElement.prototype : win.HTMLInputElement.prototype;`);
          L.push(`  const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;`);
          L.push(`  setter.call(el, ${JSON.stringify(a.value || '')});`);
          L.push(`  el.dispatchEvent(new win.Event('input', {bubbles:true}));`);
          L.push(`  el.dispatchEvent(new win.Event('change', {bubbles:true}));`);
          L.push(`  console.log('已输入，当前值:', el.value);`);
          L.push(`}`);
        } else if (a.type === 'wait_for') {
          L.push(`// 等待元素出现（最多 25 秒），与回放 wait_for 一致`);
          L.push(`(function waitFor(){`);
          L.push(`  let n = 0;`);
          L.push(`  const t = setInterval(() => {`);
          L.push(`    const got = __locate(${JSON.stringify(sel)}, ${JSON.stringify(frame)});`);
          L.push(`    if (got || ++n > 50) { clearInterval(t); console.log(got ? '✅ 元素已出现 @ ' + got.where : '❌ 25 秒内未出现'); }`);
          L.push(`  }, 500);`);
          L.push(`})();`);
        } else if (a.type === 'wait') {
          L.push(`console.log('该步骤是等待 ${a.ms || 1000}ms，无需操作');`);
        } else if (a.type === 'scroll') {
          L.push(`window.scrollBy(0, ${a.ms || 600}); console.log('已滚动 ${a.ms || 600}px');`);
        } else if (a.type === 'eval') {
          L.push(`// 执行录制的 JS`);
          L.push(a.value || '// (空)');
        } else if (a.type === 'open') {
          L.push(`console.log('该步骤是打开页面：${a.value || ''}');`);
        } else {
          L.push(`const got = __locate(${JSON.stringify(sel)}, ${JSON.stringify(frame)});`);
          L.push(`console.log(got ? ('✅ 命中 @ ' + got.where) : '❌ 未找到', got && got.el);`);
        }
        if (frame) {
          L.unshift(`// 该动作录制于 iframe ${frame} 内：下面的脚本会自动进入该 iframe 查找，无需手动切换控制台上下文`);
        }
        return L.join('\n');
      }

      function showActionJS(a) {
        C.openModal('动作复现 JS · ' + (a.type) + ' ' + (a.selector || a.text || '').slice(0, 30),
          'action-js-modal', { js: actionJS(a), action: a });
      }

      async function buildDraft(save) {
        if (!studioId.value) { C.toast('请先查看一个录制会话', 'err'); return; }
        try {
          const body = {
            name: draft.name || '', mode: draft.mode,
            login_req_id: draft.login_req_id || '', list_req_id: draft.list_req_id || '',
            list_req_ids: (draft.list_req_ids || []).slice(),
            start_url: draft.start_url || '',
          };
          const t = await C.api('/studio/sessions/' + studioId.value + '/draft', { method: 'POST', body: body });
          draftNotes.splice(0, draftNotes.length, ...((t && t._notes) || []));
          if (save) {
            const saved = await C.api('/tasks', { method: 'POST', body: t });
            C.toast('任务已保存：' + saved.name, 'ok');
            C.go('/task/' + saved.id);
            return;
          }
          C.pendingTask = t;
          if (draftNotes.length) C.toast(draftNotes[0], 'warn');
          else C.toast('草稿已生成，请检查后保存', 'ok');
          C.go('/task/new');
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        }
      }

      return {
        sessions: sessions, studioId: studioId, session: session, requests: requests, actions: actions,
        diagnostics: diagnostics, showDiag: showDiag,
        draftNotes: draftNotes,
        jsonOnly: jsonOnly, keyword: keyword, selReq: selReq, starting: starting, draft: draft, form: form,
        filtered: filtered, jsonReqs: jsonReqs, candidates: candidates, draftKw: draftKw,
        filteredCands: filteredCands, selectedCount: selectedCount,
        toggleCand: toggleCand, candChecked: candChecked, selectAllCands: selectAllCands, clearCands: clearCands,
        loadSessions: loadSessions, refreshCapture: refreshCapture, start: start,
        openSession: openSession, stopSession: stopSession, removeSession: removeSession,
        showReq: showReq, actionLabel: actionLabel, buildDraft: buildDraft,
        showActionJS: showActionJS,
        modeTag: C.modeTag, shortUrl: C.shortUrl, bytes: C.bytes, fmtDur: C.fmtDur, timeHMS: C.timeHMS, fmt: C.fmt,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div><h2>录制工作台</h2><p>打开真实浏览器，录制登录与列表操作，自动生成采集任务</p></div>
      </div>

      <div class="grid grid-2">
        <div class="card">
          <div class="card-head"><h3>开始录制</h3></div>
          <div class="card-body">
            <div class="form-grid">
              <div class="full"><label class="field">网址</label><input id="studioUrl" v-model="form.url" placeholder="https://example.com/login" /></div>
              <div class="full"><label class="field">录制模式</label>
                <select id="studioMode" v-model="form.mode">
                  <option value="api">方案一 · 纯接口（录制登录 + 列表接口）</option>
                  <option value="hybrid">方案三 · UI 登录 + 接口抓取</option>
                  <option value="ui">方案二 · 纯 UI（录制页面操作）</option>
                </select>
              </div>
            </div>
            <div class="row" style="margin-top:16px">
              <button class="btn primary" data-act="studio-start" :disabled="starting" @click="start">{{ starting ? '正在启动浏览器…' : '启动浏览器并开始录制' }}</button>
            </div>
            <p class="hint" style="margin-top:12px">
              会打开一个独立 Chrome 窗口（有头模式）。请在窗口里完成登录、翻页等操作，本页面会实时显示捕获的请求与页面动作。
              完成后点击「生成任务草稿」。
            </p>
          </div>
        </div>

        <div class="card">
          <div class="card-head"><h3>录制会话</h3><button class="btn sm" data-act="studio-refresh-sessions" @click="loadSessions">刷新</button></div>
          <c-empty v-if="!sessions.length" text="还没有录制会话，先在上方启动一个" icon="🎬" />
          <div v-else class="tbl-wrap">
            <table class="tbl">
              <thead><tr><th>会话</th><th>模式</th><th>状态</th><th>开始时间</th><th>录制时长</th><th>请求</th><th>动作</th><th>操作</th></tr></thead>
              <tbody>
                <tr v-for="s in sessions" :key="s.id">
                  <td class="code">{{ String(s.id).slice(0, 8) }}</td>
                  <td><span class="tag" :class="modeTag(s.mode).cls">{{ modeTag(s.mode).text }}</span></td>
                  <td><span class="tag" :class="s.status === 'recording' ? 'green' : 'gray'">{{ s.status === 'recording' ? '录制中' : s.status }}</span></td>
                  <td class="nowrap">{{ fmt(s.started_at || s.created_at) }}</td>
                  <td class="nowrap">{{ fmtDur(s.duration_ms) }}<span v-if="s.status === 'recording'" class="hint"> · 计时中</span></td>
                  <td>{{ s.requests || 0 }} <span class="hint">JSON {{ s.json_requests || 0 }}</span></td>
                  <td>{{ s.actions || 0 }}</td>
                  <td>
                    <div class="actions">
                      <button class="btn sm primary" data-act="studio-open" @click="openSession(s)">查看</button>
                      <button v-if="s.status === 'recording'" class="btn sm" data-act="studio-stop" @click="stopSession(s)">停止</button>
                      <button class="btn sm danger" data-act="studio-remove" @click="removeSession(s)">删除</button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <div class="card" style="margin-top:16px" v-show="studioId">
        <div class="card-head">
          <h3>抓包与动作
            <span class="hint">{{ String(session.id || '').slice(0, 8) }} · {{ session.status }} · 开始 {{ fmt(session.started_at || session.created_at) }} · 已录制 {{ fmtDur(session.duration_ms) }} · 请求 {{ session.requests || 0 }} / JSON {{ session.json_requests || 0 }} · 动作 {{ session.actions || 0 }}</span>
          </h3>
          <div class="row">
            <label class="check"><input type="checkbox" id="reqJsonOnly" v-model="jsonOnly" /> 只看 JSON</label>
            <input id="reqKw" v-model="keyword" placeholder="过滤 URL" style="max-width:180px" />
            <button class="btn sm" data-act="studio-refresh" @click="refreshCapture">刷新</button>
          </div>
        </div>
        <div class="card-body">
          <div class="grid grid-2">
            <div>
              <h4 style="margin:0 0 8px">网络请求 <span class="hint">({{ filtered.length }}/{{ requests.length }})</span></h4>
              <c-empty v-if="!filtered.length" text="暂无匹配的请求，请在浏览器里操作页面" icon="🕸️" />
              <div v-else style="max-height:460px;overflow:auto">
                <table class="tbl">
                  <thead><tr><th>#</th><th>方法</th><th>URL</th><th>状态</th><th>大小</th><th>耗时</th></tr></thead>
                  <tbody>
                    <tr v-for="r in filtered" :key="r.id" class="req-row" :class="{ selected: selReq === r.id }" data-act="studio-req" @click="showReq(r)">
                      <td>{{ r.seq }}</td>
                      <td><span class="tag" :class="r.method === 'GET' ? 'green' : 'blue'">{{ r.method }}</span></td>
                      <td>
                        <div class="req-url" :title="r.url">{{ shortUrl(r.url, 70) }}</div>
                        <span v-if="r.is_json" class="tag blue">JSON</span>
                        <span v-if="r.failed" class="tag red">失败</span>
                      </td>
                      <td>{{ r.status || '-' }}</td>
                      <td>{{ bytes(r.size) }}</td>
                      <td>{{ fmtDur(r.duration_ms) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <p class="hint">点击请求可查看完整请求头、请求体、响应头与响应体。</p>
            </div>
            <div>
              <h4 style="margin:0 0 8px">页面动作
                <span class="hint">已录 {{ actions.length }} 条 · 诊断事件 {{ diagnostics.length }} 条</span>
                <button v-if="diagnostics.length" class="btn sm ghost" style="margin-left:8px;padding:0 6px"
                        data-act="studio-diag" @click="showDiag = !showDiag">{{ showDiag ? '收起诊断' : '录制诊断' }}</button>
              </h4>
              <div v-if="showDiag" class="pre" style="max-height:200px;overflow:auto;margin-bottom:8px">
                <div v-for="(d, i) in diagnostics.slice(-80)" :key="i">
                  {{ timeHMS(d.at) }} [{{ d.ev }}] &lt;{{ d.tag }}&gt; {{ d.text }}
                  <span v-if="d.rec" style="color:#2f7d32">→ {{ d.rec }}</span>
                  <span v-else style="color:#c43239">→ 跳过:{{ d.skip }}</span>
                  <span v-if="d.frame" style="color:#888"> (frame {{ d.frame }})</span>
                </div>
              </div>
              <div class="timeline" style="max-height:460px;overflow:auto">
                <div v-if="!actions.length" class="hint">暂无页面动作</div>
                <div v-for="(a, i) in actions" :key="i" class="tl-item" :class="a.type">
                  <span class="tl-time">{{ timeHMS(a.at) }}</span>{{ actionLabel(a) }}
                  <button class="btn sm ghost" style="margin-left:6px;padding:0 6px" data-act="action-js"
                          title="生成等价 JS，粘贴到页面控制台复现这一步"
                          @click="showActionJS(a)">JS</button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div class="card" style="margin-top:16px" v-show="studioId">
        <div class="card-head"><h3>生成任务草稿</h3><span class="hint">选择登录接口和列表接口，自动识别 Token、分页与字段映射</span></div>
        <div class="card-body">
          <div class="pane-empty" style="text-align:left;margin:0 0 14px">
            <b>「生成任务草稿」做什么：</b>把本次录制的请求 / 页面动作自动整理成一个采集任务配置，包括：
            登录接口 or 登录动作、Token / Cookie 引用、列表接口、分页参数、记录路径、字段映射、主键去重。
            <b>它不会直接入库或保存</b>，「生成并打开编辑器」会跳到任务编辑器让你检查后保存，「直接保存任务」才会立即创建任务。
            <br />
            <b>纯接口：</b>登录接口生成接口步骤并提取 token，勾选的每个列表接口生成一个独立接口步骤（各自分页 / 映射 / 落库）。
            <b>UI + 接口：</b>登录页面动作生成「登录步骤」，后续只走接口抓取，适合签名 / 验证参数多的站点。
            <b>纯 UI：</b>页面动作生成「UI 步骤」，勾选的接口生成多条抓取规则，一次回放同时采集；需要翻页时在规则里勾选「按该接口分页抓取」。
          </div>
          <div class="form-grid">
            <div><label class="field">任务名称</label><input id="draftName" v-model="draft.name" placeholder="留空自动生成" /></div>
            <div><label class="field">模式</label>
              <select id="draftMode" v-model="draft.mode">
                <option value="api">纯接口</option>
                <option value="hybrid">UI + 接口</option>
                <option value="ui">纯 UI</option>
              </select>
              <p class="hint" v-if="draft.mode === 'api'">纯接口模式只用录制到的请求：<b>不会</b>生成 UI 步骤（页面动作全部丢弃）。</p>
              <p class="hint" v-else-if="draft.mode === 'hybrid'">混合模式：页面动作 → 「登录步骤」，后续只跑接口，不执行 UI 步骤。</p>
              <p class="hint" v-else>纯 UI 模式：页面动作（含录制中的「打开 xxx」跳转）→ 「UI 步骤」，一次回放抓取全部选中接口。</p>
            </div>
            <div class="full"><label class="field">登录请求（可选，方案一用于提取 Token）</label>
              <select id="draftLogin" v-model="draft.login_req_id">
                <option value="">自动识别</option>
                <option v-for="r in jsonReqs" :key="r.id" :value="r.id">{{ r.method }} {{ shortUrl(r.url, 60) }}</option>
              </select>
            </div>
            <div class="full">
              <label class="field">数据接口（可多选，每个接口分别生成独立步骤 / 抓取规则）</label>
              <div class="row" style="margin:6px 0">
                <input id="draftListKw" v-model="draftKw" placeholder="搜索接口 URL / 名称" style="max-width:240px" />
                <button class="btn sm ghost" data-act="draft-list-all" @click="selectAllCands">全选</button>
                <button class="btn sm ghost" data-act="draft-list-clear" @click="clearCands">清空</button>
                <span class="hint">已选 {{ selectedCount }} 个</span>
              </div>
              <c-empty v-if="!filteredCands.length" text="还没有识别到包含数据列表的接口，请先在浏览器里打开列表页" icon="📄" />
              <div v-else id="draftList" class="cand-list">
                <label v-for="c in filteredCands" :key="c.id" class="cand-item" :class="{ on: candChecked(c) }" :data-cand="c.id">
                  <input type="checkbox" :checked="candChecked(c)" @change="toggleCand(c)" />
                  <span class="tag" :class="c.method === 'GET' ? 'green' : 'blue'">{{ c.method }}</span>
                  <span class="cand-url" :title="c.url">{{ shortUrl(c.url, 70) }}</span>
                  <span class="tag gray">{{ c.rows }} 行</span>
                  <span class="hint">{{ c.records_path }}</span>
                </label>
              </div>
              <p class="hint">纯接口 / 混合模式：每个勾选的接口生成一个独立接口步骤；纯 UI 模式：生成多条抓取规则，一次回放同时采集。生成后可在任务编辑器里分别设置字段映射、分页与落库目标。</p>
            </div>
            <div class="full"><label class="field">起始网址（UI 回放用）</label><input id="draftStart" v-model="draft.start_url" placeholder="默认使用会话网址" /></div>
          </div>
          <div class="row" style="margin-top:16px;justify-content:flex-end">
            <button class="btn" data-act="studio-draft-save" @click="buildDraft(true)">直接保存任务</button>
            <button class="btn primary" data-act="studio-draft" @click="buildDraft(false)">生成并打开编辑器</button>
          </div>
          <div v-if="draftNotes.length" style="margin-top:12px;padding:10px 14px;border:1px solid #e8c487;background:#fff8e8;border-radius:8px;color:#8a5a00;font-size:12.5px;line-height:1.85">
            <div v-for="(n, i) in draftNotes" :key="i">· {{ n }}</div>
          </div>
        </div>
      </div>
    </div>`,
  });
})();
