/* ============================================================
   任务编辑器（Vue）
   ============================================================ */
(function () {
  'use strict';
  const C = Collect;

  let seq = 1;
  function nextKey() { return 'k' + (++seq); }

  function blankTask() {
    return {
      id: 0, name: '', mode: 'api', remark: '', enabled: true, notify: true,
      notify_on: 'failure', schedule_type: 'manual', cron: '', interval_sec: 0, timeout_sec: 60,
      session_mode: '',
      vars: [], http_steps: [], ui_steps: [], login_steps: [], mapping: [],
      capture: { mode: 'network', url_contains: '', method: 'GET', records_path: '', wait_ms: 3000, row_selector: '', rules: [] },
      pagination: {
        enabled: false, type: 'page', page_param: 'page', size_param: 'pageSize', page_size: 20,
        start_page: 1, max_pages: 100, offset_param: 'offset', cursor_param: 'cursor', cursor_path: '',
        records_path: '', total_path: '', stop_when_empty: true, delay_ms: 0, page_in_body: false,
      },
      sink: {
        type: 'table', data_source_id: 0, table: '', write_mode: 'insert', key_columns: [],
        batch_size: 200, auto_create: true, export_format: 'xlsx', sql_text: '', _keysText: '',
      },
      dedupe: { enabled: false, keys: [], _text: '' },
    };
  }

  function headersText(arr) {
    return (arr || []).filter(function (h) { return h.key; })
      .map(function (h) { return h.key + ': ' + (h.value || ''); }).join('\n');
  }
  function parseHeaders(text) {
    return String(text || '').split('\n').map(function (l) { return l.trim(); }).filter(Boolean).map(function (line) {
      const i = line.indexOf(':');
      if (i < 0) return { key: line, value: '' };
      return { key: line.slice(0, i).trim(), value: line.slice(i + 1).trim() };
    });
  }
  function extractText(arr) {
    return (arr || []).filter(function (r) { return r.var; }).map(function (r) {
      let right = r.header ? 'header:' + r.header : (r.path || '');
      if (r.regex) right = (right || '') + ' | ' + r.regex;
      return (r.var + ' = ' + right).trim();
    }).join('\n');
  }
  function parseExtract(text) {
    return String(text || '').split('\n').map(function (l) { return l.trim(); }).filter(Boolean).map(function (line) {
      const i = line.indexOf('=');
      const rule = { var: (i < 0 ? line : line.slice(0, i)).trim(), path: '', prefix: '', header: '', regex: '' };
      if (i < 0) return rule;
      let right = line.slice(i + 1).trim();
      const bar = right.indexOf('|');
      if (bar >= 0) { rule.regex = right.slice(bar + 1).trim(); right = right.slice(0, bar).trim(); }
      if (/^header:/i.test(right)) rule.header = right.slice(7).trim();
      else rule.path = right;
      return rule;
    });
  }

  function blankHttpStep() {
    return {
      name: '', method: 'GET', url: '', headers: [], body: '', content_type: 'application/json',
      extract: [], capture: false, paginate: false, records_path: '', total_path: '', delay_ms: 0,
      _collapsed: true, _sink: blankSink('inherit'), _pageMode: 'inherit', _page: blankPage(), _mapText: '',
    };
  }
  function blankUIStep() {
    return {
      action: 'click', selector: '', value: '', key: '', ms: 0, text: '', desc: '', frame: '',
      x: 0, y: 0, open_mode: '', trigger: '',
    };
  }

  // UI 步骤 → 提交给后端的结构。
  // 注意：x / y 是录制时记下的点击坐标，回放找不到选择器时会用它们兜底点击；
  // 漏掉它们会让「导入能跑、编辑保存后点不动」——所以这里必须原样带上。
  function uiStepPayload(s) {
    return {
      action: s.action, selector: s.selector || '', value: s.value || '', key: s.key || '',
      ms: Number(s.ms || 0), text: s.text || '', desc: s.desc || '', frame: s.frame || '',
      x: Number(s.x || 0), y: Number(s.y || 0),
      open_mode: s.action === 'open' ? (s.open_mode || '') : '',
      trigger: s.action === 'select' ? (s.trigger || '') : '',
      trigger_text: s.action === 'select' ? (s.trigger_text || '') : '',
    };
  }

  function blankSink(mode) {
    return {
      mode: mode || 'inherit', type: 'db', data_source_id: 0, table: '', write_mode: 'insert',
      _keysText: '', batch_size: 200, auto_create: true, sql_text: '', export_format: 'xlsx',
    };
  }
  function blankPage() {
    return {
      enabled: true, mode: 'http', type: 'page', page_param: 'page', size_param: 'pageSize', page_size: 20,
      start_page: 1, max_pages: 100, offset_param: 'offset', cursor_param: 'cursor', cursor_path: '',
      records_path: '', total_path: '', stop_when_empty: true, delay_ms: 0, page_in_body: false,
      next_selector: '', next_text: '下一页', wait_ms: 3000, frame: '',
    };
  }
  const MAP_TYPES = ['string', 'int', 'float', 'bool', 'datetime', 'json'];
  function mappingText(arr) {
    return (arr || []).filter(function (m) { return m && (m.source || m.target); }).map(function (m) {
      let line = (m.source || '') + '=' + (m.target || '');
      if (m.type && m.type !== 'string') line += ':' + m.type;
      if (m.default) line += '|' + m.default;
      return line;
    }).join('\n');
  }
  function parseMappingText(text) {
    return String(text || '').split('\n').map(function (l) { return l.trim(); }).filter(Boolean).map(function (line) {
      let body = line, def = '';
      const bar = body.indexOf('|');
      if (bar >= 0) { def = body.slice(bar + 1).trim(); body = body.slice(0, bar).trim(); }
      const eq = body.indexOf('=');
      const source = (eq < 0 ? '' : body.slice(0, eq)).trim();
      let target = (eq < 0 ? body : body.slice(eq + 1)).trim();
      let type = 'string';
      const colon = target.lastIndexOf(':');
      if (colon > 0 && MAP_TYPES.indexOf(target.slice(colon + 1).trim()) >= 0) {
        type = target.slice(colon + 1).trim();
        target = target.slice(0, colon).trim();
      }
      return { source: source, target: target, type: type, default: def };
    }).filter(function (m) { return m.source || m.target; });
  }
  function sinkPayload(s) {
    return {
      type: s.type || 'db', data_source_id: Number(s.data_source_id || 0), table: String(s.table || '').trim(),
      write_mode: s.write_mode || 'insert',
      key_columns: String(s._keysText || '').split(',').map(function (x) { return x.trim(); }).filter(Boolean),
      batch_size: Number(s.batch_size || 200), auto_create: !!s.auto_create,
      export_format: s.export_format || 'xlsx', sql_text: s.sql_text || '',
    };
  }

  function normalizeTask(t) {
    const b = blankTask();
    const out = Object.assign(b, t || {});
    out.vars = (t && t.vars) || [];
    out.http_steps = (t && t.http_steps) || [];
    out.ui_steps = (t && t.ui_steps) || [];
    out.login_steps = (t && t.login_steps) || [];
    out.mapping = (t && t.mapping) || [];
    out.capture = Object.assign(b.capture, (t && t.capture) || {});
    out.pagination = Object.assign(b.pagination, (t && t.pagination) || {});
    out.sink = Object.assign(b.sink, (t && t.sink) || {});
    out.dedupe = Object.assign(b.dedupe, (t && t.dedupe) || {});

    out.vars.forEach(function (v) { v._k = nextKey(); });
    out.http_steps.forEach(function (s) {
      s._k = nextKey();
      s._headers = headersText(s.headers);
      s._extract = extractText(s.extract);
      s._collapsed = true;
      s._sink = Object.assign(blankSink('inherit'), s.sink || {});
      s._sink.mode = s.sink ? (String(s.sink.type || '') === 'none' ? 'none' : 'custom') : 'inherit';
      s._sink._keysText = (s._sink.key_columns || []).join(',');
      s._pageMode = s.pagination ? 'custom' : 'inherit';
      s._page = Object.assign(blankPage(), s.pagination || {});
      s._mapText = mappingText(s.mapping);
    });
    out.ui_steps.forEach(function (s) {
      s._k = nextKey(); s._collapsed = true; s.frame = s.frame || ''; s.open_mode = s.open_mode || '';
      s.x = Number(s.x || 0); s.y = Number(s.y || 0);
    });
    out.login_steps.forEach(function (s) {
      s._k = nextKey(); s._collapsed = true; s.frame = s.frame || ''; s.open_mode = s.open_mode || '';
      s.x = Number(s.x || 0); s.y = Number(s.y || 0);
    });
    out.mapping.forEach(function (m) { m._k = nextKey(); });
    out.sink._keysText = (out.sink.key_columns || []).join(',');
    out.dedupe._text = (out.dedupe.keys || []).join(',');

    // 抓取规则：旧任务只有单条配置，这里迁移成一条规则便于继续扩展
    const cap = out.capture;
    if (!Array.isArray(cap.rules)) cap.rules = [];
    if (!cap.rules.length) {
      const hasLegacy = cap.url_contains || cap.records_path || cap.row_selector || cap.mode === 'dom';
      cap.rules = hasLegacy ? [{
        name: '默认规则', mode: cap.mode || 'network', url_contains: cap.url_contains || '',
        method: cap.method || '', records_path: cap.records_path || '', row_selector: cap.row_selector || '',
        frame: cap.frame || '',
      }] : [];
    }
    cap.rules.forEach(function (r, i) {
      r._k = nextKey();
      r._collapsed = true;
      if (!r.name) r.name = '规则' + (i + 1);
      r._mapText = mappingText(r.mapping);
      r._frame = r.frame || '';
      r._sink = Object.assign(blankSink('inherit'), r.sink || {});
      r._sink.mode = r.sink ? (String(r.sink.type || '') === 'none' ? 'none' : 'custom') : 'inherit';
      r._sink._keysText = (r._sink.key_columns || []).join(',');
      r._pgOn = !!(r.pagination && r.pagination.enabled);
      r.Pagination = Object.assign(blankPage(), r.pagination || {});
    });
    return out;
  }

  const TABS = [
    { key: 'basic', label: '基本设置' },
    { key: 'vars', label: '变量' },
    { key: 'steps', label: '接口步骤' },
    { key: 'ui', label: 'UI 步骤' },
    { key: 'login', label: '登录步骤' },
    { key: 'capture', label: '抓取配置' },
    { key: 'pagination', label: '分页配置' },
    { key: 'mapping', label: '字段映射' },
    { key: 'sink', label: '落库配置' },
    { key: 'dedupe', label: '去重配置' },
  ];

  /**
   * 不同采集模式只开放与之匹配的配置入口，避免误配：
   *  - api    ：接口步骤 + 任务级分页（登录接口提取 token 的方式）
   *  - hybrid ：登录步骤 + 接口步骤 + 任务级分页
   *  - ui     ：UI 步骤 + 抓取规则（规则内独立分页）
   */
  const MODE_TABS = {
    api: ['basic', 'vars', 'steps', 'pagination', 'mapping', 'sink', 'dedupe'],
    hybrid: ['basic', 'vars', 'login', 'steps', 'pagination', 'mapping', 'sink', 'dedupe'],
    ui: ['basic', 'vars', 'ui', 'capture', 'mapping', 'sink', 'dedupe'],
  };

  function tabEnabled(mode, key) {
    if (key === 'basic') return true;
    const allow = MODE_TABS[mode] || MODE_TABS.api;
    return allow.indexOf(key) >= 0;
  }

  function tabTip(mode, key) {
    if (key === 'ui') {
      if (mode === 'api') return '纯接口模式不启动浏览器，不执行 UI 步骤；需要页面操作请把采集模式改成「混合」或「纯 UI」。';
      return '当前「UI 登录 + 接口」模式只执行登录步骤，不执行 UI 步骤；需要页面动作请加到「登录步骤」，或把采集模式改成「纯 UI」。';
    }
    if (key === 'login') return '仅「UI 登录 + 接口」模式可用：该模式用 UI 回放登录并复用会话';
    if (key === 'capture') return '仅「纯 UI」模式可用：抓取规则决定 UI 回放后采集哪些接口或 DOM';
    if (key === 'steps') return '纯 UI 模式不执行接口步骤，请在「抓取配置」里设置接口分页';
    if (key === 'pagination') return '纯 UI 模式请在「抓取配置 → 启用翻页抓取」里配置（支持页面点击翻页 / 接口翻页）';
    return '当前采集模式不可用';
  }

  const UI_ACTIONS = [
    ['open', '打开页面'], ['click', '点击'], ['input', '输入'], ['select', '下拉选择'],
    ['press', '按键'], ['wait', '等待'], ['wait_for', '等待元素'], ['scroll', '滚动'], ['eval', '执行 JS'],
  ];

  function actionLabel(action) {
    for (let i = 0; i < UI_ACTIONS.length; i++) {
      if (UI_ACTIONS[i][0] === action) return UI_ACTIONS[i][1];
    }
    return '动作';
  }

  /* 含 {{变量}} 的占位文案放在 JS 里，避免被 Vue 模板解析 */
  const PH = {
    url: 'https://api.example.com/store/list?page={{page}}&size={{pageSize}}',
    body: '{"page":{{page}},"size":{{pageSize}}}',
    header: 'Authorization: Bearer {{token}}',
    extract: 'token = data.access_token\ncsrf = header:x-csrf-token',
    sql: "INSERT INTO store (id, name, city, sales, updated_at) VALUES ({{id}}, {{name}}, {{city}}, {{sales}}, '{{now}}')",
    cron: '0 0 8 * * * 或 3600',
  };

  function modeHintText(mode) {
    if (mode === 'hybrid') {
      return '方案三：在「登录步骤」里只回放登录页面动作，采集 Cookie / localStorage / sessionStorage，后续「接口步骤」复用该会话抓数据。适合动态签名参数多的站点。注意：该模式不执行「UI 步骤」，需要登录后的页面动作请一并放进「登录步骤」，或改用纯 UI 模式。';
    }
    if (mode === 'ui') {
      return '方案二：在「UI 步骤」里回放真实页面操作，并在「抓取配置」选择网络请求或 DOM 表格提取数据。适合风控严格、每一步都校验的站点。';
    }
    return '方案一：在「接口步骤」录制登录接口提取 token，再配置列表接口分页抓取，后续请求头用 {{token}} 自动替换。适合接口稳定、验证参数少的站点。注意：纯接口模式不启动浏览器，不执行 UI / 登录步骤。';
  }

  /* 可复用的落库配置块（接口步骤 / 抓取规则共用） */
  C.component('sink-box', {
    props: {
      modelValue: { type: Object, required: true },
      dsList: { type: Array, default: function () { return []; } },
    },
    template: `
      <div class="sink-box" data-sink-box>
        <div class="form-grid">
          <div><label class="field">落库方式</label>
            <select v-model="modelValue.mode" data-act="sink-mode">
              <option value="inherit">继承任务落库设置</option>
              <option value="none">不落库（仅保留预览数据）</option>
              <option value="custom">单独指定数据库与表</option>
            </select>
          </div>
          <div v-if="modelValue.mode === 'custom'"><label class="field">写入方式</label>
            <select v-model="modelValue.type">
              <option value="db">仅写入数据库</option>
              <option value="both">表格 + 数据库</option>
              <option value="table">仅生成表格</option>
            </select>
          </div>
          <div v-if="modelValue.mode === 'custom' && modelValue.type !== 'table'">
            <label class="field">数据源</label>
            <select v-model.number="modelValue.data_source_id">
              <option :value="0">请选择数据源</option>
              <option v-for="ds in dsList" :key="ds.id" :value="ds.id">{{ ds.name }}（{{ ds.type }}）</option>
            </select>
          </div>
          <div v-if="modelValue.mode === 'custom' && modelValue.type !== 'table'">
            <label class="field">目标表名</label><input v-model="modelValue.table" placeholder="collect_store" />
          </div>
          <div v-if="modelValue.mode === 'custom' && modelValue.type !== 'table'">
            <label class="field">写入模式</label>
            <select v-model="modelValue.write_mode">
              <option value="insert">INSERT 追加</option>
              <option value="upsert">UPSERT 按主键更新</option>
              <option value="sql">自定义 SQL 模板</option>
            </select>
          </div>
          <div v-if="modelValue.mode === 'custom' && modelValue.type !== 'table'">
            <label class="field">冲突主键列（逗号分隔）</label><input v-model="modelValue._keysText" placeholder="id" />
          </div>
          <div v-if="modelValue.mode === 'custom' && modelValue.type !== 'table'">
            <label class="field">批量条数</label><input type="number" v-model.number="modelValue.batch_size" />
          </div>
          <div v-if="modelValue.mode === 'custom' && modelValue.type !== 'table'" class="full">
            <label class="check"><input type="checkbox" v-model="modelValue.auto_create" /> 表不存在时自动建表（insert / upsert 生效）</label>
          </div>
          <div v-if="modelValue.mode === 'custom' && modelValue.write_mode === 'sql'" class="full">
            <label class="field">SQL 模板（花括号字段名绑定映射字段，花括号变量名取运行变量）</label>
            <textarea v-model="modelValue.sql_text" rows="6" spellcheck="false"
              placeholder="INSERT INTO store (id, name) VALUES (字段占位符, 字段占位符)"></textarea>
          </div>
          <div v-if="modelValue.mode === 'none'" class="full">
            <p class="hint">该接口的数据不会写入数据库、也不会生成表格，但仍可在运行详情的「数据集预览」中按接口查看。</p>
          </div>
        </div>
      </div>`,
  });

  C.component('page-editor', {
    setup() {
      const route = C.route;
      const task = C.reactive(blankTask());
      const tab = C.ref('basic');
      const dirty = C.ref(false);
      const saving = C.ref(false);
      const loading = C.ref(true);
      const stepKw = C.ref('');
      const pendingNotes = C.reactive([]);
      const uiKw = C.ref('');
      const loginKw = C.ref('');
      const ruleKw = C.ref('');
      const dsList = C.reactive([]);
      const cronPreset = C.ref('');
      let ready = false;

      function syncCronPreset() {
        const expr = String(task.cron || '').trim();
        const hit = (C.CRON_PRESETS || []).filter(function (p) { return p.expr === expr; })[0];
        cronPreset.value = hit ? hit.expr : (expr ? 'custom' : '');
      }
      function applyCronPreset(e) {
        const v = e && e.target ? e.target.value : '';
        cronPreset.value = v;
        if (v && v !== 'custom') task.cron = v;
      }
      const cronPresetLabel = C.computed(function () {
        const expr = String(task.cron || '').trim();
        const hit = (C.CRON_PRESETS || []).filter(function (p) { return p.expr === expr; })[0];
        return hit ? hit.label : '';
      });

      function hydrate(raw) {
        const t = normalizeTask(raw);
        Object.keys(task).forEach(function (k) { delete task[k]; });
        Object.assign(task, t);
        syncCronPreset();
        ready = false;
        Vue.nextTick(function () { ready = true; dirty.value = false; });
      }

      async function load() {
        loading.value = true;
        try {
          await C.ensureDatasources();
          dsList.splice(0, dsList.length, ...(C.sys.datasources || []));
          if (route.id === 'new') {
            const pending = C.pendingTask;
            C.pendingTask = null;
            pendingNotes.splice(0, pendingNotes.length, ...((pending && pending._notes) || []));
            hydrate(pending || blankTask());
          } else {
            hydrate(await C.api('/tasks/' + route.id));
          }
          tab.value = 'basic';
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);

      Vue.watch(task, function () { if (ready) dirty.value = true; }, { deep: true });

      /* ---------- 序列化 ---------- */
      function serialize() {
        return {
          name: String(task.name || '').trim(),
          mode: task.mode,
          remark: task.remark,
          enabled: !!task.enabled,
          notify: !!task.notify,
          notify_on: task.notify_on,
          schedule_type: task.schedule_type,
          cron: task.cron,
          interval_sec: Number(task.interval_sec || 0),
          timeout_sec: Number(task.timeout_sec || 60),
          session_mode: task.session_mode || '',
          vars: (task.vars || []).filter(function (v) { return v.key; }).map(function (v) {
            return { key: v.key, value: v.value || '' };
          }),
          http_steps: (task.http_steps || []).map(function (s) {
            const o = {
              name: s.name || '', method: s.method || 'GET', url: s.url || '',
              headers: parseHeaders(s._headers), body: s.body || '', content_type: s.content_type || '',
              extract: parseExtract(s._extract), capture: !!s.capture, paginate: !!s.paginate,
              records_path: s.records_path || '', total_path: s.total_path || '',
              delay_ms: Number(s.delay_ms || 0),
            };
            if (s._sink && s._sink.mode === 'none') o.sink = { type: 'none' };
            else if (s._sink && s._sink.mode === 'custom') o.sink = sinkPayload(s._sink);
            const mp = parseMappingText(s._mapText);
            if (mp.length) o.mapping = mp;
            if (s._pageMode === 'custom') o.pagination = Object.assign({}, s._page, { enabled: !!s._page.enabled });
            return o;
          }),
          ui_steps: (task.ui_steps || []).map(uiStepPayload),
          login_steps: (task.login_steps || []).map(uiStepPayload),
          capture: (function () {
            const rules = (task.capture.rules || []).map(function (r, i) {
              const o = {
                name: r.name || ('规则' + (i + 1)), mode: r.mode || 'network',
                url_contains: r.url_contains || '', method: r.method || '',
                records_path: r.records_path || '', row_selector: r.row_selector || '',
                wait_ms: Number(task.capture.wait_ms || 0),
              };
              const mp = parseMappingText(r._mapText);
              if (mp.length) o.mapping = mp;
              if (r.mode === 'dom' && String(r._frame || '').trim()) o.frame = String(r._frame).trim();
              if (r._sink && r._sink.mode === 'none') o.sink = { type: 'none' };
              else if (r._sink && r._sink.mode === 'custom') o.sink = sinkPayload(r._sink);
              if (r._pgOn) {
                // DOM 规则只能「点击翻页」（页面里真实点下一页），网络规则可二选一
                const pgMode = r.mode === 'dom' ? 'ui' : (r.Pagination.mode || 'http');
                o.pagination = Object.assign({}, r.Pagination, { enabled: true, mode: pgMode });
                if (r.mode === 'dom' && !String(o.pagination.frame || '').trim() && String(r._frame || '').trim()) {
                  o.pagination.frame = String(r._frame).trim();
                }
              }
              return o;
            });
            const first = rules[0] || {};
            return {
              mode: first.mode || 'network', url_contains: first.url_contains || '',
              method: first.method || '', records_path: first.records_path || '',
              wait_ms: Number(task.capture.wait_ms || 0), row_selector: first.row_selector || '',
              rules: rules,
            };
          })(),
          pagination: {
            enabled: !!task.pagination.enabled, type: task.pagination.type,
            page_param: task.pagination.page_param || '', size_param: task.pagination.size_param || '',
            page_size: Number(task.pagination.page_size || 0), start_page: Number(task.pagination.start_page || 1),
            max_pages: Number(task.pagination.max_pages || 100),
            offset_param: task.pagination.offset_param || '', cursor_param: task.pagination.cursor_param || '',
            cursor_path: task.pagination.cursor_path || '', records_path: task.pagination.records_path || '',
            total_path: task.pagination.total_path || '', stop_when_empty: !!task.pagination.stop_when_empty,
            delay_ms: Number(task.pagination.delay_ms || 0), page_in_body: !!task.pagination.page_in_body,
          },
          mapping: (task.mapping || []).filter(function (m) { return m.source || m.target; }).map(function (m) {
            return { source: m.source || '', target: m.target || '', type: m.type || 'string', default: m.default || '' };
          }),
          sink: {
            type: task.sink.type, data_source_id: Number(task.sink.data_source_id || 0),
            table: task.sink.table || '', write_mode: task.sink.write_mode,
            key_columns: String(task.sink._keysText || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean),
            batch_size: Number(task.sink.batch_size || 200), auto_create: !!task.sink.auto_create,
            export_format: task.sink.export_format || 'xlsx', sql_text: task.sink.sql_text || '',
          },
          dedupe: {
            enabled: !!task.dedupe.enabled,
            keys: String(task.dedupe._text || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean),
          },
        };
      }

      /* ---------- 保存 ---------- */
      async function save() {
        if (!String(task.name || '').trim()) {
          C.toast('请填写任务名称', 'err');
          tab.value = 'basic';
          return null;
        }
        const dbSink = task.sink.type === 'db' || task.sink.type === 'both';
        if (task.sink.write_mode === 'sql') {
          if (!dbSink) {
            C.toast('写入方式选了「自定义 SQL」，请把落库方式改为「仅写入数据库」或「表格 + 数据库」', 'err');
            tab.value = 'sink';
            return null;
          }
          if (!String(task.sink.sql_text || '').trim()) {
            C.toast('请填写 SQL 模板', 'err');
            tab.value = 'sink';
            return null;
          }
        }
        // 接口级落库配置校验
        const badSink = (task.http_steps || []).concat(task.capture.rules || []).find(function (s) {
          const k = s._sink;
          if (!k || k.mode !== 'custom') return false;
          const needDB = k.type === 'db' || k.type === 'both';
          if (needDB && !Number(k.data_source_id || 0)) return true;
          if (k.write_mode === 'sql') return !String(k.sql_text || '').trim();
          return needDB && !String(k.table || '').trim();
        });
        if (badSink) {
          C.toast('有接口/规则选择了「单独落库」，请补全数据源与表名（或 SQL）', 'err');
          tab.value = 'steps';
          return null;
        }
        // UI / 登录 步骤必填项校验：填错或漏填时直接定位到那一步，
        // 避免「加了步骤但跑起来像没执行」。
        if (task.mode === 'ui') {
          const badUI = firstBadStep(task.ui_steps);
          if (badUI) {
            badUI.step._bad = '第 ' + (badUI.i + 1) + ' 步：' + badUI.issue;
            badUI.step._collapsed = false;
            tab.value = 'ui';
            focusNew('#uiBox', badUI.i);
            C.toast('UI 步骤第 ' + (badUI.i + 1) + ' 步：' + badUI.issue, 'err');
            return null;
          }
        }
        if (task.mode === 'hybrid') {
          const badLogin = firstBadStep(task.login_steps);
          if (badLogin) {
            badLogin.step._bad = '第 ' + (badLogin.i + 1) + ' 步：' + badLogin.issue;
            badLogin.step._collapsed = false;
            tab.value = 'login';
            focusNew('#loginBox', badLogin.i);
            C.toast('登录步骤第 ' + (badLogin.i + 1) + ' 步：' + badLogin.issue, 'err');
            return null;
          }
        }
        saving.value = true;
        try {
          const body = serialize();
          const saved = task.id
            ? await C.api('/tasks/' + task.id, { method: 'PUT', body: body })
            : await C.api('/tasks', { method: 'POST', body: body });
          dirty.value = false;
          C.toast('保存成功', 'ok');
          if (!task.id) {
            C.go('/task/' + saved.id);
          } else {
            hydrate(saved);
          }
          return saved;
        } catch (e) {
          C.toast(e.message || String(e), 'err');
          return null;
        } finally {
          saving.value = false;
        }
      }

      async function saveAndRun() {
        const saved = await save();
        if (!saved) return;
        try {
          const run = await C.api('/tasks/' + saved.id + '/run', { method: 'POST', body: {} });
          C.toast('已启动运行 #' + run.id, 'ok');
          C.go('/run/' + run.id);
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        }
      }

      function back() { history.back(); }

      /* ---------- 列表操作 ---------- */
      function focusNew(sel, idx) {
        Vue.nextTick(function () {
          const box = document.querySelector(sel);
          if (!box) return;
          const cards = box.querySelectorAll('[data-step],[data-ui],[data-map],[data-kv]');
          const last = (idx === undefined || idx === null || idx < 0) ? cards[cards.length - 1] : cards[idx];
          if (!last) return;
          last.scrollIntoView({ block: 'center' });
          const input = last.querySelector('input,select,textarea');
          if (input) { input.focus(); if (input.select) input.select(); }
        });
      }

      function addVar() {
        task.vars.push({ key: '', value: '', _k: nextKey() });
        tab.value = 'vars';
        focusNew('#varsBox');
      }
      function delVar(i) { task.vars.splice(i, 1); }
      function addStep() {
        const s = blankHttpStep();
        s._k = nextKey(); s._collapsed = false; s._headers = ''; s._extract = '';
        task.http_steps.push(s);
        tab.value = 'steps';
        focusNew('#stepsBox');
      }
      function delStep(i) { task.http_steps.splice(i, 1); }

      function addRule() {
        if (!Array.isArray(task.capture.rules)) task.capture.rules = [];
        task.capture.rules.push({
          name: '规则' + (task.capture.rules.length + 1), mode: 'network', url_contains: '',
          method: '', records_path: '', row_selector: '', frame: '', _frame: '', _k: nextKey(), _collapsed: false,
          _mapText: '', _sink: blankSink('inherit'), _pgOn: false,
          Pagination: Object.assign(blankPage(), { mode: 'ui' }),
        });
        tab.value = 'capture';
      }
      function delRule(i) { task.capture.rules.splice(i, 1); }
      // DOM 规则只能页面点击翻页；切到 DOM 时自动把翻页方式改成 ui
      function onRuleMode(r) {
        if (r.mode === 'dom') r.Pagination.mode = 'ui';
      }

      // ---- 点击翻页辅助：常见分页组件选择器 + 从已录制的点击动作里猜「下一页」 ----
      const NEXT_SELECTORS = ['.ant-pagination-next', '.el-pagination .btn-next', '.layui-laypage-next', 'a[rel=next]', 'li.next a'];
      const NEXT_KEYWORDS = ['下一页', '下页', 'next', '加载更多', '查看更多', '更多'];
      function guessNextStep() {
        const list = (task.ui_steps || []).concat(task.login_steps || []);
        for (let i = list.length - 1; i >= 0; i--) {
          const s = list[i];
          if (String(s.action || '').toLowerCase() !== 'click') continue;
          const hay = [s.text, s.desc, s.selector, s.value].join(' ').toLowerCase();
          if (!hay) continue;
          if (NEXT_KEYWORDS.some(function (k) { return hay.indexOf(k) >= 0; })) return s;
        }
        return null;
      }
      // 从录制的 UI 动作里识别「下一页 / 加载更多」按钮，自动填选择器与按钮文字
      function detectNext(r, silent) {
        const s = guessNextStep();
        r.Pagination.mode = 'ui';
        if (!s) {
          if (!silent) C.toast('录制动作里没找到「下一页 / 加载更多」这类点击，请手动填选择器，或直接用按钮文字匹配', 'err');
          return false;
        }
        if (s.selector) r.Pagination.next_selector = s.selector;
        if (s.text) r.Pagination.next_text = s.text;
        else if (!r.Pagination.next_text) r.Pagination.next_text = '下一页';
        if (!silent) C.toast('已识别：' + (r.Pagination.next_selector || r.Pagination.next_text), 'ok');
        return true;
      }
      function useNextSelector(r, sel) {
        r.Pagination.mode = 'ui';
        r.Pagination.next_selector = sel;
        C.toast('已填入选择器：' + sel, 'ok');
      }
      // 纯 UI 下从「接口翻页」一键改成「页面点击翻页」
      function switchToClickPaging(r) {
        r.Pagination.mode = 'ui';
        if (!r.Pagination.next_selector && !r.Pagination.next_text) r.Pagination.next_text = '下一页';
        detectNext(r, true);
        C.toast('已改为页面点击翻页，请确认「下一页按钮选择器 / 按钮文字」', 'ok');
      }

      function stepVisible(s) {
        const kw = String(stepKw.value || '').trim().toLowerCase();
        if (!kw) return true;
        return [s.name, s.method, s.url, s.records_path].join(' ').toLowerCase().indexOf(kw) >= 0;
      }
      function uiVisible(kind, s) {
        const box = kind === 'login' ? loginKw : uiKw;
        const kw = String(box.value || '').trim().toLowerCase();
        if (!kw) return true;
        return [s.action, s.selector, s.value, s.text, s.desc, s.frame].join(' ').toLowerCase().indexOf(kw) >= 0;
      }
      function ruleVisible(r) {
        const kw = String(ruleKw.value || '').trim().toLowerCase();
        if (!kw) return true;
        return [r.name, r.mode, r.url_contains, r.method, r.records_path, r.row_selector, r._frame]
          .join(' ').toLowerCase().indexOf(kw) >= 0;
      }
      function toggleAll(kind) {
        let list = task.http_steps;
        if (kind === 'ui') list = task.ui_steps;
        if (kind === 'login') list = task.login_steps;
        if (kind === 'capture') list = task.capture.rules || [];
        const anyOpen = (list || []).some(function (s) { return !s._collapsed; });
        (list || []).forEach(function (s) { s._collapsed = anyOpen; });
      }
      function addUI(kind) {
        const list = kind === 'login' ? task.login_steps : task.ui_steps;
        const s = blankUIStep();
        s._k = nextKey(); s._collapsed = false;
        list.push(s);
        tab.value = kind === 'login' ? 'login' : 'ui';
        focusNew(kind === 'login' ? '#loginBox' : '#uiBox');
        C.toast(kind === 'login' ? '已添加登录动作，选择动作类型并填写选择器' : '已添加 UI 动作，选择动作类型并填写选择器', 'ok');
      }
      // 在当前步骤后面插入一步（顺序很重要：比如要在「登录」之后再「打开某页」）
      function insertUI(kind, i) {
        const list = kind === 'login' ? task.login_steps : task.ui_steps;
        const s = blankUIStep();
        s._k = nextKey(); s._collapsed = false;
        list.splice(i + 1, 0, s);
        tab.value = kind === 'login' ? 'login' : 'ui';
        focusNew(kind === 'login' ? '#loginBox' : '#uiBox', i + 1);
      }

      // 哪些动作需要填 CSS 选择器
      const NEED_SELECTOR = ['click', 'input', 'select', 'press', 'wait_for'];
      function stepNeedsSelector(action) { return NEED_SELECTOR.indexOf(String(action || '')) >= 0; }

      // 单步参数自检：返回缺什么（空字符串表示没问题），与后端 validateUISteps 保持一致
      function stepIssue(st) {
        const a = String((st && st.action) || '').trim();
        const sel = String((st && st.selector) || '').trim();
        const val = String((st && st.value) || '').trim();
        const txt = String((st && st.text) || '').trim();
        if (!a) return '还没有选择动作类型';
        if (a === 'open' && !val && !sel) return '「打开页面」必须填网址（值 / 网址 那一栏）';
        if (a === 'click' && !sel && !txt && !(Number(st.x || 0) > 0)) return '「点击」需要填选择器，或填按钮文本兜底';
        if ((a === 'input' || a === 'select') && !sel) return (a === 'input' ? '「输入」' : '「下拉选择」') + '需要填选择器';
        if (a === 'wait_for' && !sel) return '「等待元素」需要填选择器';
        if (a === 'press' && !String((st && st.key) || '').trim()) return '「按键」需要填按键名（如 Enter）';
        if (a === 'eval' && !val) return '「执行 JS」需要填代码';
        return '';
      }
      // 找出第一处填错的步骤，保存前用它定位
      function firstBadStep(list) {
        const arr = list || [];
        for (let i = 0; i < arr.length; i++) {
          const issue = stepIssue(arr[i]);
          if (issue) return { i: i, step: arr[i], issue: issue };
        }
        return null;
      }
      function delUI(kind, i) { (kind === 'login' ? task.login_steps : task.ui_steps).splice(i, 1); }
      function addMap() {
        task.mapping.push({ source: '', target: '', type: 'string', default: '', _k: nextKey() });
        tab.value = 'mapping';
        focusNew('#mapBox');
      }
      function delMap(i) { task.mapping.splice(i, 1); }

      function move(arr, i, delta) {
        const j = i + delta;
        if (j < 0 || j >= arr.length) return;
        const tmp = arr[i];
        arr.splice(i, 1);
        arr.splice(j, 0, tmp);
      }

      function genSql() {
        const cols = (task.mapping || []).map(function (m) { return String(m.target || '').trim(); }).filter(Boolean);
        if (!cols.length) { C.toast('请先在「字段映射」里配置目标字段', 'err'); return; }
        const table = String(task.sink.table || '').trim() || 'your_table';
        const vals = cols.map(function (c) { return '{{' + c + '}}'; });
        task.sink.sql_text = 'INSERT INTO ' + table + ' (' + cols.join(', ') + ') VALUES (' + vals.join(', ') + ')';
        task.sink.write_mode = 'sql';
        C.toast('已生成 SQL 模板，可再手工调整', 'ok');
      }

      const counts = C.computed(function () {
        return {
          vars: task.vars.length,
          steps: task.http_steps.length,
          capture: (task.capture.rules || []).length,
          ui: task.ui_steps.length,
          login: task.login_steps.length,
          mapping: task.mapping.length,
        };
      });
      const modeHint = C.computed(function () { return modeHintText(task.mode); });

      function isTabEnabled(key) { return tabEnabled(task.mode, key); }
      Vue.watch(function () { return task.cron; }, function () { syncCronPreset(); });
      function tabDisabledTip(key) { return tabTip(task.mode, key); }
      function switchTab(key) {
        if (!isTabEnabled(key)) {
          C.toast(tabDisabledTip(key), 'err');
          return;
        }
        tab.value = key;
      }
      Vue.watch(function () { return task.mode; }, function () {
        if (!isTabEnabled(tab.value)) tab.value = 'basic';
      });

      function onKeydown(e) {
        if ((e.ctrlKey || e.metaKey) && String(e.key).toLowerCase() === 's') {
          e.preventDefault();
          save();
        }
      }
      Vue.onMounted(function () { document.addEventListener('keydown', onKeydown); });
      Vue.onUnmounted(function () { document.removeEventListener('keydown', onKeydown); });

      return {
        task: task, tab: tab, dirty: dirty, saving: saving, loading: loading, dsList: dsList,
        pendingNotes: pendingNotes,
        TABS: TABS, UI_ACTIONS: UI_ACTIONS, PH: PH,
        stepKw: stepKw, uiKw: uiKw, loginKw: loginKw, ruleKw: ruleKw,
        stepVisible: stepVisible, uiVisible: uiVisible, ruleVisible: ruleVisible, toggleAll: toggleAll,
        addRule: addRule, delRule: delRule, onRuleMode: onRuleMode, blankSink: blankSink,
        detectNext: detectNext, useNextSelector: useNextSelector, switchToClickPaging: switchToClickPaging,
        NEXT_SELECTORS: NEXT_SELECTORS,
        actionLabel: actionLabel,
        counts: counts, modeHint: modeHint,
        tabEnabled: isTabEnabled, tabTip: tabDisabledTip, switchTab: switchTab,
        cronPreset: cronPreset, applyCronPreset: applyCronPreset, cronPresetLabel: cronPresetLabel,
        CRON_PRESETS: C.CRON_PRESETS,
        save: save, saveAndRun: saveAndRun, back: back,
        addVar: addVar, delVar: delVar, addStep: addStep, delStep: delStep,
        addUI: addUI, insertUI: insertUI, delUI: delUI, addMap: addMap, delMap: delMap, move: move, genSql: genSql,
        stepNeedsSelector: stepNeedsSelector,
        modeTag: C.modeTag,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div>
          <h2>
            {{ task.id ? (task.name || '任务编辑') : '新建任务' }}
            <span v-if="task.id" class="hint">#{{ task.id }}</span>
            <span v-else class="tag amber">未保存</span>
            <span class="tag amber" id="dirtyTag" v-show="dirty">● 有未保存修改</span>
          </h2>
          <p>
            <span class="tag" :class="modeTag(task.mode).cls">{{ modeTag(task.mode).text }}</span>
            配置采集步骤、字段映射与落库规则
          </p>
        </div>
        <div class="row">
          <button class="btn" data-act="task-back" @click="back">返回</button>
          <button v-if="task.id" class="btn" data-act="task-run" @click="saveAndRun">保存并运行</button>
          <button class="btn primary" data-act="save-task" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存任务' }}</button>
        </div>
      </div>

      <div v-if="pendingNotes.length" style="margin:0 0 14px;padding:10px 14px;border:1px solid #e8c487;background:#fff8e8;border-radius:8px;color:#8a5a00;font-size:12.5px;line-height:1.85">
        <b>录制生成提示：</b>
        <div v-for="(n, i) in pendingNotes" :key="i">· {{ n }}</div>
      </div>

      <div class="card">
        <div class="tabs">
          <button v-for="t in TABS" :key="t.key" data-act="task-tab" :data-tab="t.key"
                  :class="{ active: tab === t.key, disabled: !tabEnabled(t.key) }"
                  :disabled="!tabEnabled(t.key)" :title="tabEnabled(t.key) ? '' : tabTip(t.key)"
                  @click="switchTab(t.key)">
            {{ t.label }}<span v-if="counts[t.key]" class="tab-badge">{{ counts[t.key] }}</span>
          </button>
        </div>

        <div class="card-body">
          <!-- 基本设置 -->
          <div class="pane" :class="{ active: tab === 'basic' }">
            <div class="form-grid">
              <div><label class="field">任务名称 *</label><input id="tName" v-model="task.name" placeholder="如：门店列表采集" /></div>
              <div><label class="field">采集模式 *</label>
                <select id="tMode" v-model="task.mode">
                  <option value="api">方案一 · 纯接口（Token 替换）</option>
                  <option value="hybrid">方案三 · UI 登录 + 接口抓取</option>
                  <option value="ui">方案二 · 纯 UI 回放</option>
                </select>
              </div>
              <div class="full"><p class="hint" id="modeHint">{{ modeHint }}</p></div>
              <div class="full"><label class="field">备注</label><input id="tRemark" v-model="task.remark" /></div>
              <div class="full row">
                <label class="check"><input type="checkbox" id="tEnabled" v-model="task.enabled" /> 启用任务</label>
                <label class="check"><input type="checkbox" id="tNotify" v-model="task.notify" /> 执行后通知</label>
                <select id="tNotifyOn" v-model="task.notify_on" style="max-width:170px">
                  <option value="failure">仅失败时通知</option>
                  <option value="success">仅成功时通知</option>
                  <option value="always">每次都通知</option>
                </select>
              </div>
              <div><label class="field">调度方式</label>
                <select id="tSchedule" v-model="task.schedule_type">
                  <option value="manual">手动触发</option>
                  <option value="cron">Cron 表达式</option>
                  <option value="interval">固定间隔</option>
                </select>
              </div>
              <div class="full">
                <label class="field">Cron 表达式（选常用计划或自定义输入）</label>
                <div class="row" style="gap:8px;flex-wrap:wrap">
                  <select :value="cronPreset" @change="applyCronPreset" style="max-width:250px" data-act="cron-preset">
                    <option value="">选择常用计划…</option>
                    <option v-for="p in CRON_PRESETS" :key="p.expr" :value="p.expr">{{ p.label }}</option>
                    <option value="custom">自定义</option>
                  </select>
                  <input id="tCron" v-model="task.cron" :placeholder="PH.cron" style="flex:1;min-width:220px" data-act="cron-expr" />
                </div>
                <div class="hint" style="margin-top:4px">
                  <template v-if="cronPresetLabel">当前计划：{{ cronPresetLabel }} → <span class="code">{{ task.cron }}</span></template>
                  <template v-else-if="task.cron">自定义表达式：<span class="code">{{ task.cron }}</span></template>
                </div>
              </div>
              <div><label class="field">间隔秒数（间隔模式）</label><input id="tInterval" type="number" v-model.number="task.interval_sec" /></div>
              <div><label class="field">单次超时（秒）</label><input id="tTimeout" type="number" v-model.number="task.timeout_sec" /></div>
              <div><label class="field">浏览器会话</label>
                <select id="tSession" v-model="task.session_mode" data-act="task-session-mode">
                  <option value="">跟随系统设置</option>
                  <option value="isolated">每次全新会话（无痕）</option>
                  <option value="shared">共享会话（复用登录态）</option>
                </select>
              </div>
              <div class="full"><p class="hint" style="margin:0">
                共享会话会把 Cookie / localStorage 保存到「系统设置 → 浏览器内核 → 共享会话目录」，下次运行直接复用，适合要过验证码或频繁登录的站点；
                同一时间只允许一个任务使用该会话目录。纯接口模式不启动浏览器，此项不生效。
              </p></div>
            </div>
            <p class="hint" style="margin-top:14px">
              Cron 支持 5 段或 6 段（秒 分 时 日 月 周）。示例：<span class="code" v-pre>0 0 8 * * *</span> 每天 8 点；
              <span class="code" v-pre>0 */30 * * * *</span> 每 30 分钟。
            </p>
          </div>

          <!-- 变量 -->
          <div class="pane" :class="{ active: tab === 'vars' }">
            <p class="hint">
              任务级变量，可在 URL / Header / Body 中以 <span class="code" v-pre>{{变量名}}</span> 引用。
              内置变量：now、today、timestamp、ts、uuid、random、page、pageSize、offset、cursor。
            </p>
            <div id="varsBox" style="margin-top:12px">
              <div class="kv-row" data-kv v-for="(v, i) in task.vars" :key="v._k">
                <input v-model="v.key" placeholder="变量名" />
                <input v-model="v.value" placeholder="值" />
                <button class="btn sm danger" data-act="del-var" @click="delVar(i)">删除</button>
              </div>
            </div>
            <div v-if="!task.vars.length" class="pane-empty">
              还没有任务变量。常用示例：<span class="code" v-pre>user = demo</span>、<span class="code" v-pre>pass = 123456</span>，
              在请求体里用 <span class="code" v-pre>{{user}}</span> 引用。
            </div>
            <button class="btn sm" data-act="add-var" @click="addVar">+ 添加变量</button>
          </div>

          <!-- 接口步骤 -->
          <div class="pane" :class="{ active: tab === 'steps' }">
            <p class="hint">
              按顺序执行。典型配置：① 登录接口（提取 token）② 列表接口（勾选数据抓取 + 分页）。
              Token 会自动替换为 <span class="code" v-pre>{{token}}</span>。
              每个接口可以单独设置分页、字段映射和落库目标，互不影响。
            </p>
            <div class="row" style="margin-top:10px">
              <input v-model="stepKw" placeholder="搜索接口：名称 / 方法 / URL / 记录路径" style="max-width:340px" />
              <button class="btn sm ghost" @click="toggleAll('steps')">展开 / 收起全部</button>
            </div>
            <div id="stepsBox" style="margin-top:12px">
              <div class="sub-card" data-step v-for="(st, i) in task.http_steps" :key="st._k" v-show="stepVisible(st)" :class="{ collapsed: st._collapsed }">
                <div class="sub-head">
                  <span class="sub-title">
                    <span class="step-no">{{ i + 1 }}</span>{{ st.name || '接口请求' }}
                    <span v-if="st.method" class="tag gray">{{ st.method }}</span>
                    <span v-if="st.capture" class="tag green">抓取</span>
                    <span v-if="st.paginate" class="tag blue">分页</span>
                    <span v-if="st._sink && st._sink.mode === 'none'" class="tag amber">不落库</span>
                    <span v-else-if="st._sink && st._sink.mode === 'custom'" class="tag blue">独立落库</span>
                    <span v-if="st._pageMode === 'custom'" class="tag blue">独立分页</span>
                    <span v-if="st._mapText" class="tag gray">独立映射</span>
                  </span>
                  <div class="actions">
                    <button class="btn sm ghost" data-act="card-toggle" title="折叠/展开" @click="st._collapsed = !st._collapsed">⌃</button>
                    <button class="btn sm" data-act="step-up" :disabled="i === 0" @click="move(task.http_steps, i, -1)">↑</button>
                    <button class="btn sm" data-act="step-down" :disabled="i === task.http_steps.length - 1" @click="move(task.http_steps, i, 1)">↓</button>
                    <button class="btn sm danger" data-act="del-step" @click="delStep(i)">删除</button>
                  </div>
                </div>
                <div class="form-grid">
                  <div><label class="field">名称</label><input v-model="st.name" placeholder="如：登录 / 门店列表" /></div>
                  <div><label class="field">请求方法</label>
                    <select v-model="st.method">
                      <option v-for="m in ['GET','POST','PUT','PATCH','DELETE']" :key="m">{{ m }}</option>
                    </select>
                  </div>
                  <div class="full"><label class="field">URL（支持变量）</label><input v-model="st.url" :placeholder="PH.url" /></div>
                  <div class="full"><label class="field">请求头（每行 Key: Value，如 Authorization: Bearer Token）</label>
                    <textarea v-model="st._headers" rows="3" :placeholder="PH.header"></textarea>
                  </div>
                  <div class="full"><label class="field">请求体（JSON，占位符不加引号）</label>
                    <textarea v-model="st.body" rows="4" :placeholder="PH.body"></textarea>
                  </div>
                  <div><label class="field">Content-Type</label><input v-model="st.content_type" placeholder="application/json / application/x-www-form-urlencoded" /></div>
                  <div><label class="field">请求前延迟 (ms)</label><input type="number" v-model.number="st.delay_ms" /></div>
                  <div class="full"><label class="field">提取变量（每行：变量 = JSON路径，或 变量 = header:响应头名，可加 | 正则）</label>
                    <textarea v-model="st._extract" rows="3" :placeholder="PH.extract"></textarea>
                  </div>
                  <div class="full row">
                    <label class="check"><input type="checkbox" v-model="st.capture" /> 该步骤为数据抓取步骤</label>
                    <label class="check"><input type="checkbox" v-model="st.paginate" /> 该步骤按分页配置循环</label>
                  </div>
                  <div><label class="field">记录列表路径</label><input v-model="st.records_path" placeholder="data.list / data.records / result" /></div>
                  <div><label class="field">总数字段路径</label><input v-model="st.total_path" placeholder="data.total" /></div>
                </div>
                <div class="form-grid" style="margin-top:14px">
                  <div class="full">
                    <div class="section-title">单独字段映射（留空 = 继承任务「字段映射」）</div>
                    <textarea v-model="st._mapText" rows="3" spellcheck="false"
                      placeholder="源字段=目标字段[:类型][|默认值]，每行一条，例如 name=store_name"></textarea>
                  </div>
                  <div>
                    <label class="field">分页设置</label>
                    <select v-model="st._pageMode" data-act="step-page-mode">
                      <option value="inherit">继承任务「分页配置」</option>
                      <option value="custom">该接口单独分页</option>
                    </select>
                  </div>
                  <div v-if="st._pageMode === 'custom'">
                    <label class="field">分页类型</label>
                    <select v-model="st._page.type">
                      <option value="page">页码 page</option>
                      <option value="offset">偏移量 offset</option>
                      <option value="cursor">游标 cursor</option>
                    </select>
                  </div>
                  <template v-if="st._pageMode === 'custom'">
                    <div><label class="field">页码参数名</label><input v-model="st._page.page_param" /></div>
                    <div><label class="field">每页条数参数名</label><input v-model="st._page.size_param" /></div>
                    <div><label class="field">每页条数</label><input type="number" v-model.number="st._page.page_size" /></div>
                    <div><label class="field">起始页</label><input type="number" v-model.number="st._page.start_page" /></div>
                    <div><label class="field">最大页数</label><input type="number" v-model.number="st._page.max_pages" /></div>
                    <div><label class="field">记录列表路径</label><input v-model="st._page.records_path" placeholder="留空沿用步骤的记录路径" /></div>
                    <div><label class="field">总数字段路径</label><input v-model="st._page.total_path" placeholder="data.total" /></div>
                    <div><label class="field">每页间隔 (ms)</label><input type="number" v-model.number="st._page.delay_ms" /></div>
                    <div class="row"><label class="check"><input type="checkbox" v-model="st._page.page_in_body" /> 分页参数放 Body</label></div>
                    <div class="row"><label class="check"><input type="checkbox" v-model="st._page.stop_when_empty" /> 空页自动停止</label></div>
                  </template>
                  <div class="full">
                    <div class="section-title">该接口落库方式</div>
                    <sink-box :model-value="st._sink" :ds-list="dsList" />
                  </div>
                </div>
              </div>
            </div>
            <div v-if="!task.http_steps.length" class="pane-empty">
              还没有接口步骤。方案一：加「登录接口」提取 token，再加「列表接口」勾选分页抓取；也可以到录制工作台录制后自动生成。
            </div>
            <button class="btn sm" data-act="add-step" @click="addStep">+ 添加接口步骤</button>
          </div>

          <!-- UI 步骤 -->
          <div class="pane" :class="{ active: tab === 'ui' }">
            <p class="hint">
              从上到下依次回放（第 1 步 → 最后一步），全部是真实浏览器动作。
              新增的步骤默认排在最后，也可以点卡片右上角的「＋」把它插到某一步之后；卡片上的 ↑ ↓ 调整顺序，顺序就是执行顺序。
              只有「纯 UI」模式会执行这里的步骤（「UI 登录 + 接口」模式只执行「登录步骤」，纯接口模式不执行任何页面动作）。
              页面元素在 iframe 里时，在「所在 iframe」填 iframe 的 CSS 选择器（如 <span class="code">#mainFrame</span>），支持 <span class="code" v-pre>iframe#a &gt;&gt; iframe.inner</span> 逐层进入。
            </p>
            <div class="row" style="margin-top:10px">
              <input v-model="uiKw" placeholder="搜索 UI 步骤：动作 / 选择器 / 内容" style="max-width:340px" />
              <button class="btn sm ghost" @click="toggleAll('ui')">展开 / 收起全部</button>
            </div>
            <div id="uiBox" style="margin-top:12px">
              <div class="sub-card" data-ui="ui" v-for="(st, i) in task.ui_steps" :key="st._k"
                   v-show="uiVisible('ui', st)" :class="{ collapsed: st._collapsed }">
                <div class="sub-head">
                  <span class="sub-title">
                    <span class="step-no">{{ i + 1 }}</span>{{ actionLabel(st.action) }}
                    <span v-if="st.selector" class="hint">{{ st.selector }}</span>
                  </span>
                  <div class="actions">
                    <button class="btn sm ghost" data-act="card-toggle" title="折叠/展开" @click="st._collapsed = !st._collapsed">⌃</button>
                    <button class="btn sm" data-act="ui-up" :disabled="i === 0" @click="move(task.ui_steps, i, -1)">↑</button>
                    <button class="btn sm" data-act="ui-down" :disabled="i === task.ui_steps.length - 1" @click="move(task.ui_steps, i, 1)">↓</button>
                    <button class="btn sm ghost" data-act="ui-insert" title="在此步骤之后插入一步" @click="insertUI('ui', i)">＋</button>
                    <button class="btn sm danger" data-act="del-ui" @click="delUI('ui', i)">删除</button>
                  </div>
                </div>
                <div class="form-grid" @input="st._bad = ''" @change="st._bad = ''">
                  <div><label class="field">动作</label>
                    <select v-model="st.action" data-act="ui-action"><option v-for="a in UI_ACTIONS" :key="a[0]" :value="a[0]">{{ a[1] }}</option></select>
                  </div>
                  <div class="full" v-if="st.action === 'open'"><label class="field">网址（必填）</label>
                    <input v-model="st.value" data-act="ui-value" placeholder="https://example.com/store/list" />
                    <p class="hint">iframe 内嵌页面请填<b>外层页面地址</b>，不要填 iframe 内部的 URL（会丢父页面会话）；后续动作的「所在 iframe」可留空自动识别。</p>
                  </div>
                  <div class="full" v-if="st.action === 'open'"><label class="field">打开方式</label>
                    <select v-model="st.open_mode" data-act="ui-open-mode">
                      <option value="">整页打开（默认，重新加载页面）</option>
                      <option value="spa">站内跳转（不刷新页面，保留登录态）</option>
                    </select>
                    <p class="hint">整页打开会重新加载文档：登录态若只存在 JS 内存里（Vuex / 全局变量）、或打开的是另一个域名 / 端口，就会「掉登录」。<b>同源 SPA</b>（hash 路由、路由守卫校验内存 token）请选「站内跳转」。</p>
                  </div>
                  <div :class="{ full: st.action === 'wait_for' }" v-if="stepNeedsSelector(st.action)">
                    <label class="field">{{ st.action === 'press' ? '选择器（可空，先聚焦该元素）' : '选择器(CSS)' }}</label>
                    <input v-model="st.selector" data-act="ui-selector" placeholder="#username / button.login / a[rel=next]" />
                  </div>
                  <div class="full" v-if="st.action !== 'open'"><label class="field">所在 iframe（页面内嵌 iframe 时必填，留空 = 主页面）</label>
                    <input v-model="st.frame" data-act="ui-frame" placeholder="#mainFrame / iframe[name=content] / iframe#a &gt;&gt; iframe.inner" />
                  </div>
                  <div v-if="st.action === 'input'"><label class="field">输入内容</label>
                    <input v-model="st.value" data-act="ui-value" placeholder="要填入的内容，可用 {{变量}}" />
                  </div>
                  <div v-if="st.action === 'select'"><label class="field">下拉触发器（自定义下拉必填）</label>
                    <input v-model="st.trigger" data-act="ui-trigger" placeholder="#sizeTrigger / .el-select（留空 = 原生 select）" />
                  </div>
                  <div v-if="st.action === 'select'"><label class="field">选项值 / 文本</label>
                    <input v-model="st.value" data-act="ui-value" placeholder="要选中的 option 值或文字" />
                  </div>
                  <div v-if="st.action === 'click'"><label class="field">按钮文本（无选择器时按文字点）</label>
                    <input v-model="st.text" data-act="ui-text" placeholder="下一页 / 登录 / 查询" />
                  </div>
                  <div v-if="st.action === 'press'"><label class="field">按键</label>
                    <input v-model="st.key" data-act="ui-key" placeholder="Enter / Tab / Escape" />
                  </div>
                  <div v-if="st.action === 'wait'"><label class="field">等待毫秒</label>
                    <input type="number" v-model.number="st.ms" placeholder="1000" />
                  </div>
                  <div v-if="st.action === 'scroll'"><label class="field">滚动像素</label>
                    <input type="number" v-model.number="st.ms" placeholder="600" />
                  </div>
                  <div class="full" v-if="st.action === 'eval'"><label class="field">JS 代码（必填）</label>
                    <textarea v-model="st.value" rows="2" placeholder="() => { window.scrollTo(0, document.body.scrollHeight); return true }"></textarea>
                  </div>
                  <div class="full"><label class="field">说明</label><input v-model="st.desc" /></div>
                </div>
                <p class="hint" v-if="st._bad" style="color:#b91c1c">{{ st._bad }}</p>
                <p class="hint" v-if="st.action === 'wait_for'">按选择器等元素出现，最长等 25 秒（可在设置里改超时）；元素出现后继续下一步。</p>
              </div>
            </div>
            <div v-if="!task.ui_steps.length" class="pane-empty">
              还没有 UI 动作。点击下方「+ 添加 UI 动作」手工添加，或在「录制工作台」用方案二录制页面操作后自动生成。
            </div>
            <button class="btn sm" data-act="add-ui" data-kind="ui" @click="addUI('ui')">+ 添加 UI 动作</button>
          </div>

          <!-- 登录步骤 -->
          <div class="pane" :class="{ active: tab === 'login' }">
            <p class="hint">方案三专用：只回放登录相关的 UI 动作，采集 Cookie / localStorage / sessionStorage 后交给接口步骤。若使用方案一可留空。</p>
            <div class="row" style="margin-top:10px">
              <input v-model="loginKw" placeholder="搜索登录步骤：动作 / 选择器 / 内容" style="max-width:340px" />
              <button class="btn sm ghost" @click="toggleAll('login')">展开 / 收起全部</button>
            </div>
            <div id="loginBox" style="margin-top:12px">
              <div class="sub-card" data-ui="login" v-for="(st, i) in task.login_steps" :key="st._k"
                   v-show="uiVisible('login', st)" :class="{ collapsed: st._collapsed }">
                <div class="sub-head">
                  <span class="sub-title">
                    <span class="step-no">{{ i + 1 }}</span>{{ actionLabel(st.action) }}
                    <span v-if="st.selector" class="hint">{{ st.selector }}</span>
                  </span>
                  <div class="actions">
                    <button class="btn sm ghost" data-act="card-toggle" title="折叠/展开" @click="st._collapsed = !st._collapsed">⌃</button>
                    <button class="btn sm" data-act="ui-up" :disabled="i === 0" @click="move(task.login_steps, i, -1)">↑</button>
                    <button class="btn sm" data-act="ui-down" :disabled="i === task.login_steps.length - 1" @click="move(task.login_steps, i, 1)">↓</button>
                    <button class="btn sm ghost" data-act="ui-insert" title="在此步骤之后插入一步" @click="insertUI('login', i)">＋</button>
                    <button class="btn sm danger" data-act="del-ui" @click="delUI('login', i)">删除</button>
                  </div>
                </div>
                <div class="form-grid" @input="st._bad = ''" @change="st._bad = ''">
                  <div><label class="field">动作</label>
                    <select v-model="st.action" data-act="ui-action"><option v-for="a in UI_ACTIONS" :key="a[0]" :value="a[0]">{{ a[1] }}</option></select>
                  </div>
                  <div class="full" v-if="st.action === 'open'"><label class="field">网址（必填）</label>
                    <input v-model="st.value" data-act="ui-value" placeholder="https://example.com/login" />
                    <p class="hint">iframe 内嵌页面请填<b>外层页面地址</b>，不要填 iframe 内部的 URL；登录表单在 iframe 里时，后面的输入 / 点击步骤填「所在 iframe」。</p>
                  </div>
                  <div class="full" v-if="st.action === 'open'"><label class="field">打开方式</label>
                    <select v-model="st.open_mode" data-act="ui-open-mode">
                      <option value="">整页打开（默认，重新加载页面）</option>
                      <option value="spa">站内跳转（不刷新页面，保留登录态）</option>
                    </select>
                    <p class="hint">登录步骤里的「打开」一般用整页打开；只有「登录之后还要在同源 SPA 内换页面」时，才用站内跳转，否则会丢内存里的登录态。</p>
                  </div>
                  <div :class="{ full: st.action === 'wait_for' }" v-if="stepNeedsSelector(st.action)">
                    <label class="field">{{ st.action === 'press' ? '选择器（可空，先聚焦该元素）' : '选择器(CSS)' }}</label>
                    <input v-model="st.selector" data-act="ui-selector" placeholder="#username / button.login" />
                  </div>
                  <div class="full" v-if="st.action !== 'open'"><label class="field">所在 iframe（页面内嵌 iframe 时必填，留空 = 主页面）</label>
                    <input v-model="st.frame" data-act="ui-frame" placeholder="#loginFrame / iframe[name=content] / iframe#a &gt;&gt; iframe.inner" />
                  </div>
                  <div v-if="st.action === 'input'"><label class="field">输入内容</label>
                    <input v-model="st.value" data-act="ui-value" placeholder="要填入的内容，可用 {{变量}}" />
                  </div>
                  <div v-if="st.action === 'select'"><label class="field">下拉触发器（自定义下拉必填）</label>
                    <input v-model="st.trigger" data-act="ui-trigger" placeholder="#sizeTrigger / .el-select（留空 = 原生 select）" />
                  </div>
                  <div v-if="st.action === 'select'"><label class="field">选项值 / 文本</label>
                    <input v-model="st.value" data-act="ui-value" placeholder="要选中的 option 值或文字" />
                  </div>
                  <div v-if="st.action === 'click'"><label class="field">按钮文本（无选择器时按文字点）</label>
                    <input v-model="st.text" data-act="ui-text" placeholder="登录 / 提交" />
                  </div>
                  <div v-if="st.action === 'press'"><label class="field">按键</label>
                    <input v-model="st.key" data-act="ui-key" placeholder="Enter / Tab / Escape" />
                  </div>
                  <div v-if="st.action === 'wait'"><label class="field">等待毫秒</label>
                    <input type="number" v-model.number="st.ms" placeholder="1000" />
                  </div>
                  <div v-if="st.action === 'scroll'"><label class="field">滚动像素</label>
                    <input type="number" v-model.number="st.ms" placeholder="600" />
                  </div>
                  <div class="full" v-if="st.action === 'eval'"><label class="field">JS 代码（必填）</label>
                    <textarea v-model="st.value" rows="2" placeholder="() => { return true }"></textarea>
                  </div>
                  <div class="full"><label class="field">说明</label><input v-model="st.desc" /></div>
                </div>
                <p class="hint" v-if="st._bad" style="color:#b91c1c">{{ st._bad }}</p>
                <p class="hint" v-if="st.action === 'wait_for'">按选择器等元素出现，最长等 25 秒；元素出现后继续下一步。</p>
              </div>
            </div>
            <div v-if="!task.login_steps.length" class="pane-empty">
              还没有登录动作。典型顺序：打开登录页 → 输入用户名 → 输入密码 → 点击登录 → 等待列表元素出现。
            </div>
            <button class="btn sm" data-act="add-ui" data-kind="login" @click="addUI('login')">+ 添加登录动作</button>
          </div>

          <!-- 抓取配置 -->
          <div class="pane" :class="{ active: tab === 'capture' }">
            <p class="hint">
              <b>一条规则 = 一条数据来源</b>：要么是某个<b>接口</b>（按「URL 包含」匹配这次回放里发出的响应），
              要么是页面上的<b>表格</b>（DOM 行选择器）。UI 步骤只负责把页面操作出来，规则负责「从这次回放里取哪份数据」。
              一次 UI 回放可以配多条规则：不同接口、不同字段映射、不同落库目标（也可以「不落库」，只做数据预览）。
              翻页只在规则内生效：勾选「启用翻页抓取」后选「页面点击翻页」（真实点下一页，风控最友好）或「接口翻页」（复用登录会话逐页请求），翻出来的每一页都按这条规则的映射与落库配置入库。
            </p>
            <div class="form-grid" style="margin-top:12px">
              <div><label class="field">回放后等待数据加载（毫秒，所有规则共用）</label>
                <input id="capWait" type="number" v-model.number="task.capture.wait_ms" />
              </div>
            </div>
            <div class="row" style="margin-top:12px">
              <input id="ruleKw" v-model="ruleKw" placeholder="搜索规则：名称 / URL / 记录路径 / 选择器" style="max-width:340px" />
              <button class="btn sm ghost" @click="toggleAll('capture')">展开 / 收起全部</button>
            </div>
            <div id="capBox" style="margin-top:12px">
              <div class="sub-card" data-cap v-for="(r, i) in task.capture.rules" :key="r._k"
                   v-show="ruleVisible(r)" :class="{ collapsed: r._collapsed }">
                <div class="sub-head">
                  <span class="sub-title">
                    <span class="step-no">{{ i + 1 }}</span>{{ r.name || '抓取规则' }}
                    <span class="tag gray">{{ r.mode === 'dom' ? 'DOM' : '接口' }}</span>
                    <span v-if="r.mode !== 'dom' && r.url_contains" class="hint">{{ r.url_contains }}</span>
                    <span v-else-if="r.mode === 'dom' && r.row_selector" class="hint">{{ r.row_selector }}</span>
                    <span v-if="r._sink && r._sink.mode === 'none'" class="tag amber">不落库</span>
                    <span v-else-if="r._sink && r._sink.mode === 'custom'" class="tag blue">独立落库</span>
                    <span v-if="r._mapText" class="tag gray">独立映射</span>
                    <span v-if="r._pgOn" class="tag purple">{{ (r.mode === 'dom' || r.Pagination.mode === 'ui') ? '点击翻页' : '接口翻页' }}</span>
                  </span>
                  <div class="actions">
                    <button class="btn sm ghost" data-act="card-toggle" title="折叠/展开" @click="r._collapsed = !r._collapsed">⌃</button>
                    <button class="btn sm" :disabled="i === 0" @click="move(task.capture.rules, i, -1)">↑</button>
                    <button class="btn sm" :disabled="i === task.capture.rules.length - 1" @click="move(task.capture.rules, i, 1)">↓</button>
                    <button class="btn sm danger" data-act="del-rule" @click="delRule(i)">删除</button>
                  </div>
                </div>
                <div class="form-grid">
                  <div><label class="field">规则名称</label><input v-model="r.name" placeholder="如：门店列表 / 订单列表" /></div>
                  <div><label class="field">抓取来源</label>
                    <select v-model="r.mode" @change="onRuleMode(r)">
                      <option value="network">网络请求（JSON）</option>
                      <option value="dom">DOM 表格</option>
                    </select>
                  </div>
                  <template v-if="r.mode !== 'dom'">
                    <div><label class="field">目标接口 URL 包含</label><input v-model="r.url_contains" placeholder="/api/store/list" /></div>
                    <div><label class="field">请求方法（留空不限）</label><input v-model="r.method" placeholder="GET / POST" /></div>
                    <div class="full"><label class="field">记录列表路径（留空自动识别数组）</label><input v-model="r.records_path" placeholder="data.list" /></div>
                  </template>
                  <template v-else>
                    <div class="full"><label class="field">DOM 行选择器</label><input v-model="r.row_selector" placeholder="table tbody tr" /></div>
                    <div class="full"><label class="field">表格所在 iframe（页面内嵌 iframe 时必填，留空 = 主页面）</label>
                      <input v-model="r._frame" data-act="rule-frame" placeholder="#mainFrame / iframe[name=content] / iframe#a &gt;&gt; iframe.inner" />
                    </div>
                  </template>
                </div>

                <!-- 纯 UI 模式：翻页抓取（页面点击翻页 / 接口翻页） -->
                <div class="form-grid" style="margin-top:14px"><div class="full">
                  <label class="check">
                    <input type="checkbox" v-model="r._pgOn" data-act="rule-pagination" />
                    启用翻页抓取（翻页过程中每页的数据都会按本规则的映射与落库配置入库）
                  </label>
                  <template v-if="r._pgOn">
                    <p class="hint" style="margin:8px 0 0">
                      翻页只负责「怎么翻到下一页」，本规则的数据来源仍然是：
                      <b>{{ r.mode === 'dom' ? ('页面表格 ' + (r.row_selector || '(请在下面填 DOM 行选择器)')) : ('接口 ' + (r.url_contains || '(请在下面填 URL 包含)')) }}</b>
                    </p>
                    <div class="form-grid" style="margin-top:10px">
                      <div><label class="field">翻页方式</label>
                        <select v-model="r.Pagination.mode" data-act="rule-pg-mode" :disabled="r.mode === 'dom'">
                          <option value="ui">页面点击翻页（真实点「下一页」，推荐）</option>
                          <option value="http">接口翻页（复用登录会话逐页请求）</option>
                        </select>
                      </div>
                      <div><label class="field">最大页数</label><input type="number" v-model.number="r.Pagination.max_pages" /></div>
                      <div><label class="field">每页间隔 (ms)</label><input type="number" v-model.number="r.Pagination.delay_ms" /></div>
                      <div class="row"><label class="check"><input type="checkbox" v-model="r.Pagination.stop_when_empty" /> 空页自动停止</label></div>
                    </div>
                    <div v-if="r.mode === 'dom' || r.Pagination.mode !== 'http'" class="form-grid" style="margin-top:10px">
                      <div class="full"><label class="field">下一页 / 加载更多按钮选择器（可留空，只按按钮文字找）</label>
                        <input v-model="r.Pagination.next_selector" data-act="rule-pg-next" placeholder=".pagination .next / a[rel=next] / #nextPage（留空则按文字匹配）" />
                      </div>
                      <div class="full">
                        <div class="row" style="gap:6px;flex-wrap:wrap">
                          <button class="btn sm" data-act="rule-pg-detect" @click="detectNext(r)">从录制动作识别</button>
                          <button v-for="sel in NEXT_SELECTORS" :key="sel" class="btn sm ghost" data-act="rule-pg-common" @click="useNextSelector(r, sel)">{{ sel }}</button>
                        </div>
                      </div>
                      <div><label class="field">按钮文字（选择器兜底，支持包含匹配）</label><input v-model="r.Pagination.next_text" data-act="rule-pg-text" placeholder="下一页 / 加载更多" /></div>
                      <div><label class="field">点击后等待加载 (ms)</label><input type="number" v-model.number="r.Pagination.wait_ms" /></div>
                      <div class="full"><label class="field">「下一页」按钮所在 iframe（内嵌 iframe 时必填，留空 = 主页面；DOM 规则默认跟随表格所在 iframe）</label>
                        <input v-model="r.Pagination.frame" data-act="rule-pg-frame" placeholder="#mainFrame / iframe[name=content]" />
                      </div>
                      <div class="full">
                        <p class="hint">
                          运行时会在浏览器里真实点击这个元素（鼠标点击，不是 JS 直接触发），等页面自己把新数据加载出来，再把这一页新出现的接口响应（或 DOM 表格）解析入库。
                          按钮文字按「包含」匹配：页面显示「下一页 ›」「Next &gt;」时填「下一页」「Next」即可；只有图标没有文字时请填选择器。
                          按钮消失 / 置灰（disabled、aria-disabled、class 含 disabled）即视为最后一页。
                          「加载更多」这类按钮也能用：每点一次新加载的数据会按主键去重后追加入库。这种方式不构造额外请求，最接近真人操作，适合风控严格的站点。
                        </p>
                      </div>
                    </div>
                    <div v-if="r.mode !== 'dom' && r.Pagination.mode === 'http'" class="form-grid" style="margin-top:10px">
                      <div><label class="field">分页类型</label>
                        <select v-model="r.Pagination.type">
                          <option value="page">页码 page</option>
                          <option value="offset">偏移量 offset</option>
                          <option value="cursor">游标 cursor</option>
                        </select>
                      </div>
                      <div><label class="field">页码参数名</label><input v-model="r.Pagination.page_param" placeholder="page" /></div>
                      <div><label class="field">每页条数参数名</label><input v-model="r.Pagination.size_param" placeholder="pageSize" /></div>
                      <div><label class="field">每页条数</label><input type="number" v-model.number="r.Pagination.page_size" /></div>
                      <div><label class="field">起始页</label><input type="number" v-model.number="r.Pagination.start_page" /></div>
                      <div><label class="field">offset 参数名</label><input v-model="r.Pagination.offset_param" placeholder="offset" /></div>
                      <div><label class="field">cursor 参数名</label><input v-model="r.Pagination.cursor_param" placeholder="cursor" /></div>
                      <div><label class="field">cursor 取值路径</label><input v-model="r.Pagination.cursor_path" placeholder="data.next_cursor" /></div>
                      <div><label class="field">记录列表路径（可覆盖规则）</label><input v-model="r.Pagination.records_path" placeholder="data.list" /></div>
                      <div><label class="field">总数字段路径</label><input v-model="r.Pagination.total_path" placeholder="data.total" /></div>
                      <div class="row"><label class="check"><input type="checkbox" v-model="r.Pagination.page_in_body" /> 分页参数放在 Body(JSON)</label></div>
                      <div class="full">
                        <p class="hint">
                          回放时会先执行 UI 步骤并捕获该接口的真实请求，再用它的 URL / 方法 / 请求头作为模板逐页请求并入库；
                          分页参数（page / size / offset / cursor）会在模板 URL 或 Body 里自动替换。
                        </p>
                      </div>
                    </div>
                    <div v-if="task.mode === 'ui' && r.mode !== 'dom' && r.Pagination.mode === 'http'" class="full" style="margin-top:10px">
                      <p class="hint" style="color:#b45309">当前是「接口翻页」：会额外构造请求，风控严格的站点建议改成「页面点击翻页」。</p>
                      <button class="btn sm" data-act="rule-pg-to-ui" @click="switchToClickPaging(r)">一键改成点击翻页</button>
                    </div>
                  </template>
                </div></div>
                <div class="form-grid" style="margin-top:14px">
                  <div class="full">
                    <div class="section-title">该规则字段映射（留空 = 继承任务「字段映射」）</div>
                    <textarea v-model="r._mapText" rows="3" spellcheck="false"
                      placeholder="源字段=目标字段[:类型][|默认值]，每行一条"></textarea>
                  </div>
                  <div class="full">
                    <div class="section-title">该规则落库方式</div>
                    <sink-box :model-value="r._sink" :ds-list="dsList" />
                  </div>
                </div>
              </div>
            </div>
            <div v-if="!task.capture.rules.length" class="pane-empty">
              还没有抓取规则。纯 UI 模式下至少添加一条：选择要采集的接口（URL 包含）或 DOM 表格（行选择器）。
            </div>
            <button class="btn sm" data-act="add-rule" @click="addRule">+ 添加抓取规则</button>
          </div>

          <!-- 分页配置 -->
          <div class="pane" :class="{ active: tab === 'pagination' }">
            <label class="check" style="margin-bottom:12px"><input type="checkbox" id="pgEnabled" v-model="task.pagination.enabled" /> 启用分页抓取</label>
            <div class="form-grid">
              <div><label class="field">分页类型</label>
                <select id="pgType" v-model="task.pagination.type">
                  <option value="page">页码 page</option>
                  <option value="offset">偏移量 offset</option>
                  <option value="cursor">游标 cursor</option>
                </select>
              </div>
              <div><label class="field">页码参数名</label><input id="pgPageParam" v-model="task.pagination.page_param" /></div>
              <div><label class="field">每页条数参数名</label><input id="pgSizeParam" v-model="task.pagination.size_param" /></div>
              <div><label class="field">每页条数</label><input id="pgPageSize" type="number" v-model.number="task.pagination.page_size" /></div>
              <div><label class="field">起始页</label><input id="pgStart" type="number" v-model.number="task.pagination.start_page" /></div>
              <div><label class="field">最大页数</label><input id="pgMax" type="number" v-model.number="task.pagination.max_pages" /></div>
              <div><label class="field">offset 参数名</label><input id="pgOffsetParam" v-model="task.pagination.offset_param" /></div>
              <div><label class="field">cursor 参数名</label><input id="pgCursorParam" v-model="task.pagination.cursor_param" /></div>
              <div><label class="field">cursor 取值路径</label><input id="pgCursorPath" v-model="task.pagination.cursor_path" placeholder="data.next_cursor" /></div>
              <div><label class="field">记录列表路径（可覆盖步骤）</label><input id="pgRecords" v-model="task.pagination.records_path" /></div>
              <div><label class="field">总数字段路径</label><input id="pgTotal" v-model="task.pagination.total_path" placeholder="data.total" /></div>
              <div><label class="field">每页间隔 (ms)</label><input id="pgDelay" type="number" v-model.number="task.pagination.delay_ms" /></div>
              <div class="row"><label class="check"><input type="checkbox" id="pgInBody" v-model="task.pagination.page_in_body" /> 分页参数放在 Body(JSON)</label></div>
              <div class="row"><label class="check"><input type="checkbox" id="pgStopEmpty" v-model="task.pagination.stop_when_empty" /> 空页自动停止</label></div>
            </div>
          </div>

          <!-- 字段映射 -->
          <div class="pane" :class="{ active: tab === 'mapping' }">
            <p class="hint">
              来源支持 JSON 路径（如 <span class="code" v-pre>store.name</span>、<span class="code" v-pre>list[0].id</span>），
              DOM 模式下填单元格 CSS 选择器（<span class="code" v-pre>td:nth-child(2)</span>、<span class="code" v-pre>a@href</span> 取属性）。
              目标字段即表格列名 / 数据库列名。
            </p>
            <div id="mapBox" style="margin-top:12px">
              <div class="step-row" data-map v-for="(m, i) in task.mapping" :key="m._k">
                <input v-model="m.source" placeholder="来源字段" />
                <input v-model="m.target" placeholder="目标字段" />
                <select v-model="m.type">
                  <option v-for="x in ['string','int','float','bool','datetime','json']" :key="x">{{ x }}</option>
                </select>
                <input v-model="m.default" placeholder="默认值" />
                <button class="btn sm danger" data-act="del-map" @click="delMap(i)">删</button>
              </div>
            </div>
            <div v-if="!task.mapping.length" class="pane-empty">还没有字段映射。至少配置一列目标字段，才能导出表格或写入数据库。</div>
            <button class="btn sm" data-act="add-map" @click="addMap">+ 添加字段映射</button>
          </div>

          <!-- 落库配置 -->
          <div class="pane" :class="{ active: tab === 'sink' }">
            <div class="form-grid">
              <div><label class="field">落库方式</label>
                <select id="sinkType" v-model="task.sink.type">
                  <option value="table">仅生成表格（Excel/CSV）</option>
                  <option value="db">仅写入数据库</option>
                  <option value="both">表格 + 数据库</option>
                  <option value="none">不保留（仅测试）</option>
                </select>
              </div>
              <div><label class="field">目标数据源</label>
                <select id="sinkDs" v-model.number="task.sink.data_source_id">
                  <option :value="0">请选择数据源</option>
                  <option v-for="ds in dsList" :key="ds.id" :value="ds.id">{{ ds.name }} ({{ ds.type }})</option>
                </select>
              </div>
              <div><label class="field">目标表名</label><input id="sinkTable" v-model="task.sink.table" placeholder="collect_store" /></div>
              <div><label class="field">写入方式</label>
                <select id="sinkMode" v-model="task.sink.write_mode">
                  <option value="insert">插入 insert（按表批量）</option>
                  <option value="upsert">存在则更新 upsert</option>
                  <option value="sql">自定义 SQL 语句（占位符）</option>
                </select>
              </div>
              <div><label class="field">冲突主键列（逗号分隔，upsert 用）</label><input id="sinkKeys" v-model="task.sink._keysText" placeholder="id" /></div>
              <div><label class="field">批量条数</label><input id="sinkBatch" type="number" v-model.number="task.sink.batch_size" /></div>
              <div><label class="field">表格导出格式</label>
                <select id="sinkFormat" v-model="task.sink.export_format">
                  <option value="xlsx">Excel (xlsx)</option>
                  <option value="csv">CSV</option>
                </select>
              </div>
              <div class="row"><label class="check"><input type="checkbox" id="sinkAuto" v-model="task.sink.auto_create" /> 表不存在时自动建表（仅 insert/upsert 生效）</label></div>
            </div>

            <div id="sinkSqlWrap" :hidden="task.sink.write_mode !== 'sql'" style="margin-top:14px">
              <div class="row" style="justify-content:space-between;align-items:center">
                <label class="field" style="margin:0">SQL 模板（选择「自定义 SQL 语句」时生效，每行数据执行一次）</label>
                <button class="btn sm" data-act="sink-sql-gen" @click="genSql">按字段映射生成模板</button>
              </div>
              <textarea id="sinkSql" v-model="task.sink.sql_text" rows="8" spellcheck="false" :placeholder="PH.sql"></textarea>
              <p class="hint" style="margin-top:8px">
                取值方式：<br />
                · <span class="code" v-pre>{{字段}}</span> 取字段映射的「目标字段」，生成绑定参数（安全，推荐）<br />
                · <span class="code" v-pre>{{变量}}</span> 取运行期变量（<span class="code" v-pre>token</span>
                <span class="code" v-pre>page</span> <span class="code" v-pre>now</span> <span class="code" v-pre>uuid</span> 等），
                支持 <span class="code" v-pre>|url</span> <span class="code" v-pre>|b64</span> <span class="code" v-pre>|upper</span>，会自动按类型加引号转义<br />
                · 原生占位符 <span class="code" v-pre>?</span> / <span class="code" v-pre>:1</span> / <span class="code" v-pre>$1</span>：
                模板里不写 <span class="code" v-pre>{{字段}}</span> 时，按字段映射顺序逐列绑定<br />
                · 每条 SQL 在事务内批量执行，失败整批回滚；可用数据源页「执行 SQL」按钮先验证语句
              </p>
            </div>
            <p class="hint" style="margin-top:12px">
              写入数据库时，字段映射的「目标字段」就是数据库列名。首次可勾选自动建表，后续会按主键 upsert 更新；
              需要写入多表 / 复杂语句时改用「自定义 SQL 语句」。
            </p>
          </div>

          <!-- 去重配置 -->
          <div class="pane" :class="{ active: tab === 'dedupe' }">
            <label class="check" style="margin-bottom:12px"><input type="checkbox" id="ddEnabled" v-model="task.dedupe.enabled" /> 启用主键去重（跨运行永久生效）</label>
            <div class="form-grid">
              <div class="full"><label class="field">去重字段（逗号分隔，映射后的目标字段名）</label>
                <input id="ddKeys" v-model="task.dedupe._text" placeholder="store_id 或 store_id,date" />
              </div>
            </div>
            <p class="hint" style="margin-top:12px">去重基于字段值生成 SHA1 指纹存储，重复数据不会写入数据集与目标表，并在运行记录里统计「重复」数量。</p>
          </div>
        </div>
      </div>
    </div>`,
  });
})();
