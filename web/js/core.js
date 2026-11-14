/* ============================================================
   采集平台 · 前端核心（Vue 3）
   - API 封装
   - 格式化工具 / 标签映射
   - Toast / Modal / 路由 / 全局状态
   - 组件注册表与页面映射
   ============================================================ */
window.Collect = (function () {
  'use strict';

  const reactive = Vue.reactive;
  const computed = Vue.computed;
  const ref = Vue.ref;

  /* ---------------- API ---------------- */
  async function api(path, opt) {
    opt = opt || {};
    const init = { method: opt.method || 'GET', headers: {} };
    if (opt.body !== undefined) {
      init.headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(opt.body);
    }
    const res = await fetch('/api' + path, init);
    const text = await res.text();
    let json;
    try { json = JSON.parse(text); } catch (e) { throw new Error('返回数据异常 (HTTP ' + res.status + ')'); }
    if (!res.ok) throw new Error(json.msg || ('HTTP ' + res.status));
    if (json.code !== 0) throw new Error(json.msg || '请求失败');
    return json.data;
  }

  async function upload(path, file) {
    const form = new FormData();
    form.append('file', file);
    const res = await fetch('/api' + path, { method: 'POST', body: form });
    const json = await res.json();
    if (json.code !== 0) throw new Error(json.msg || '上传失败');
    return json;
  }

  /* ---------------- 格式化 ---------------- */
  function pad(n) { return String(n).padStart(2, '0'); }

  function fmt(ts) {
    if (!ts) return '-';
    const d = new Date(ts);
    if (isNaN(d.getTime())) return String(ts);
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + ' ' +
      pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
  }

  function fmtDur(ms) {
    if (ms === null || ms === undefined || ms === '') return '-';
    const n = Number(ms);
    if (isNaN(n)) return '-';
    if (n < 1000) return n + 'ms';
    if (n < 60000) return (n / 1000).toFixed(1) + 's';
    return Math.floor(n / 60000) + 'm' + Math.round((n % 60000) / 1000) + 's';
  }

  function bytes(n) {
    const v = Number(n || 0);
    if (!v) return '-';
    if (v < 1024) return v + 'B';
    if (v < 1024 * 1024) return (v / 1024).toFixed(1) + 'KB';
    return (v / 1024 / 1024).toFixed(1) + 'MB';
  }

  function jsonPretty(txt) {
    if (!txt) return '';
    try { return JSON.stringify(JSON.parse(txt), null, 2); } catch (e) { return txt; }
  }

  function shortUrl(u, n) {
    u = String(u || '');
    const max = n || 90;
    return u.length > max ? u.slice(0, max) + '…' : u;
  }

  function timeHMS(ts) {
    if (!ts) return '';
    const d = new Date(ts);
    if (isNaN(d.getTime())) return '';
    return pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
  }

  /* ---------------- Cron 常用计划（6 段：秒 分 时 日 月 周） ---------------- */
  const CRON_PRESETS = [
    { label: '每 5 分钟', expr: '0 */5 * * * *' },
    { label: '每 10 分钟', expr: '0 */10 * * * *' },
    { label: '每 30 分钟', expr: '0 */30 * * * *' },
    { label: '每小时整点', expr: '0 0 * * * *' },
    { label: '每 2 小时', expr: '0 0 */2 * * *' },
    { label: '每天 00:00', expr: '0 0 0 * * *' },
    { label: '每天 08:00', expr: '0 0 8 * * *' },
    { label: '每天 09:00', expr: '0 0 9 * * *' },
    { label: '每天 12:00', expr: '0 0 12 * * *' },
    { label: '每天 18:00', expr: '0 0 18 * * *' },
    { label: '工作日 09:00（周一至周五）', expr: '0 0 9 * * 1-5' },
    { label: '每周一 09:00', expr: '0 0 9 * * 1' },
    { label: '每月 1 号 00:00', expr: '0 0 0 1 * *' },
    { label: '每年 1 月 1 日 00:00', expr: '0 0 0 1 1 *' },
  ];

  /* ---------------- 标签映射 ---------------- */
  const STATUS = {
    running: ['blue', '运行中'],
    success: ['green', '成功'],
    failed: ['red', '失败'],
    partial: ['amber', '部分成功'],
    stopped: ['gray', '已停止'],
  };
  const MODE = {
    api: ['blue', '纯接口'],
    hybrid: ['amber', 'UI + 接口'],
    ui: ['purple', '纯 UI'],
  };
  const DS_TYPE = {
    mysql: ['blue', 'MySQL'],
    oracle: ['red', 'Oracle'],
    postgres: ['green', 'PostgreSQL'],
    sqlite: ['amber', 'SQLite'],
  };
  const STEP_KIND = {
    http: ['blue', '接口'],
    ui: ['purple', 'UI'],
    sink: ['green', '落库'],
    login: ['amber', '登录'],
    capture: ['gray', '抓取'],
  };

  function pick(map, key, fallback) {
    const hit = map[key];
    if (hit) return { cls: hit[0], text: hit[1] };
    return { cls: 'gray', text: key || fallback || '-' };
  }

  function statusTag(s) { return pick(STATUS, s, '未知'); }
  function modeTag(m) { return pick(MODE, m); }
  function dsTypeTag(t) { return pick(DS_TYPE, t); }
  function stepKindTag(k) { return pick(STEP_KIND, k); }

  /* ---------------- Toast ---------------- */
  const toasts = reactive([]);
  let toastSeq = 0;
  function toast(msg, type) {
    const id = ++toastSeq;
    toasts.push({ id: id, msg: String(msg), type: type || '' });
    setTimeout(function () {
      const i = toasts.findIndex(function (t) { return t.id === id; });
      if (i >= 0) toasts.splice(i, 1);
    }, 3200);
  }

  /* ---------------- Modal ---------------- */
  const modal = reactive({ visible: false, title: '详情', html: '', comp: null, props: {}, wide: false });

  const components = {};
  function component(name, def) {
    if (def) { components[name] = def; return def; }
    return components[name];
  }
  function hasComponent(name) {
    return !!(name && components[name]);
  }

  /**
   * 打开弹窗。
   * openModal(title, '组件名', props)  → 渲染 Vue 组件
   * openModal(title, '<div>html</div>') → 渲染原始 HTML（兼容旧用法）
   */
  function openModal(title, body, props) {
    modal.title = title || '详情';
    if (typeof body === 'string' && hasComponent(body)) {
      modal.comp = body;
      modal.props = props || {};
      modal.html = '';
    } else {
      modal.comp = null;
      modal.props = {};
      modal.html = typeof body === 'string' ? body : '';
    }
    modal.width = (props && props.width) || '';
    modal.visible = true;
  }

  function closeModal() {
    modal.visible = false;
    modal.comp = null;
    modal.html = '';
    modal.props = {};
  }

  /* ---------------- 路由 ---------------- */
  const navMap = {
    dashboard: '仪表盘', tasks: '任务管理', task: '任务编辑', studio: '录制工作台',
    runs: '运行记录', run: '运行详情', datasources: '数据源', logs: '运行日志',
    jobs: '定时作业', job: '作业编辑', jobruns: '作业执行记录', jobrun: '作业执行详情',
    notifications: '消息中心', settings: '系统设置',
  };
  const pageMap = {
    dashboard: 'page-dashboard', tasks: 'page-tasks', task: 'page-editor',
    studio: 'page-studio', runs: 'page-runs', run: 'page-run-detail',
    jobs: 'page-jobs', job: 'page-job', jobruns: 'page-jobruns', jobrun: 'page-jobrun',
    datasources: 'page-datasources', logs: 'page-logs',
    notifications: 'page-notifications', settings: 'page-settings',
  };

  const route = reactive({ name: 'dashboard', id: '', extra: '', key: 0 });

  function parseHash(hash) {
    const raw = String(hash === undefined ? location.hash : hash).replace(/^#\/?/, '');
    const parts = raw.split('/');
    const name = navMap[parts[0]] ? parts[0] : 'dashboard';
    return { name: name, id: parts[1] || '', extra: parts[2] || '' };
  }

  function syncRoute() {
    const r = parseHash();
    route.name = r.name;
    route.id = r.id;
    route.extra = r.extra;
    route.key++;
    closeModal();
  }

  function pageOf(name) { return pageMap[name] || pageMap.dashboard; }

  function crumbOf(name, id) {
    const label = navMap[name] || '仪表盘';
    return id && id !== 'new' ? label + ' #' + id : label;
  }

  function go(hash) {
    const h = '#' + (String(hash).startsWith('/') ? hash : '/' + hash);
    if (location.hash === h) { syncRoute(); return; }
    location.hash = h;
  }

  function reload() {
    route.key++;
    toast('已刷新', 'ok');
  }

  /* ---------------- 全局状态 ---------------- */
  const sys = reactive({ version: '1.0.0', uptime: '', ok: false, msg: '连接中…', unread: 0, datasources: [] });

  /* 录制工作台生成的草稿：跨路由传递到任务编辑器（new 页） */
  let pendingTask = null;

  async function refreshHealth() {
    try {
      const d = await api('/health');
      sys.ok = true;
      sys.version = d.version || '1.0.0';
      sys.uptime = d.uptime || '';
      sys.msg = '服务正常 · ' + sys.version + (sys.uptime ? ' · 运行 ' + sys.uptime : '');
    } catch (e) {
      sys.ok = false;
      sys.msg = '服务连接失败';
    }
  }

  async function refreshBadge() {
    try {
      const d = await api('/notifications?read=false&page_size=1');
      sys.unread = d.unread || 0;
    } catch (e) { /* ignore */ }
  }

  async function ensureDatasources(force) {
    if (!force && sys.datasources.length) return sys.datasources;
    try { sys.datasources = await api('/datasources'); } catch (e) { sys.datasources = []; }
    return sys.datasources;
  }

  return {
    // api
    api: api, upload: upload,
    // 格式化
    fmt: fmt, fmtDur: fmtDur, bytes: bytes, jsonPretty: jsonPretty, shortUrl: shortUrl, timeHMS: timeHMS,
    CRON_PRESETS: CRON_PRESETS,
    // 标签
    statusTag: statusTag, modeTag: modeTag, dsTypeTag: dsTypeTag, stepKindTag: stepKindTag,
    // toast / modal
    toasts: toasts, toast: toast,
    modal: modal, openModal: openModal, closeModal: closeModal,
    // 路由
    route: route, navMap: navMap, pageOf: pageOf, crumbOf: crumbOf, go: go, reload: reload, syncRoute: syncRoute,
    // 组件
    component: component, components: components, hasComponent: hasComponent,
    // 全局状态
    sys: sys, refreshHealth: refreshHealth, refreshBadge: refreshBadge, ensureDatasources: ensureDatasources,
    // 录制草稿
    get pendingTask() { return pendingTask; },
    set pendingTask(v) { pendingTask = v; },
    // Vue 快捷方法
    reactive: reactive, computed: computed, ref: ref,
  };
})();
