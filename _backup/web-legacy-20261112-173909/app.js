/* ============ 基础工具 ============ */
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({
  '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
}[c]));

function fmt(ts) {
  if (!ts) return '-';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return String(ts);
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}
function fmtDur(ms) {
  if (!ms && ms !== 0) return '-';
  if (ms < 1000) return ms + 'ms';
  if (ms < 60000) return (ms / 1000).toFixed(1) + 's';
  return Math.floor(ms / 60000) + 'm' + Math.round((ms % 60000) / 1000) + 's';
}
function bytes(n) {
  if (!n) return '-';
  if (n < 1024) return n + 'B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + 'KB';
  return (n / 1024 / 1024).toFixed(1) + 'MB';
}
function statusTag(s) {
  const map = { running: ['blue', '运行中'], success: ['green', '成功'], failed: ['red', '失败'], partial: ['amber', '部分成功'] };
  const [cls, text] = map[s] || ['gray', s || '未知'];
  return `<span class="tag ${cls}">${text}</span>`;
}
function modeTag(m) {
  const map = { api: ['blue', '纯接口'], ui: ['purple', '纯 UI'], hybrid: ['amber', 'UI + 接口'] };
  const [cls, text] = map[m] || ['gray', m];
  return `<span class="tag ${cls}">${text}</span>`;
}
function jsonPretty(txt) {
  if (!txt) return '';
  try { return JSON.stringify(JSON.parse(txt), null, 2); } catch { return txt; }
}
function shortUrl(u, n = 90) {
  u = String(u || '');
  return u.length > n ? u.slice(0, n) + '…' : u;
}

const state = {
  tasks: [],
  taskOptions: [],
  datasources: [],
  timers: [],
  studioId: '',
  studioSession: null,
  pendingTask: null,
  lastLogId: 0,
};

function stopTimers() {
  state.timers.forEach((t) => clearInterval(t));
  state.timers = [];
}
function every(ms, fn) {
  const t = setInterval(fn, ms);
  state.timers.push(t);
  return t;
}

async function api(path, { method = 'GET', body } = {}) {
  const opt = { method, headers: {} };
  if (body !== undefined) {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body);
  }
  const res = await fetch('/api' + path, opt);
  const text = await res.text();
  let json;
  try { json = JSON.parse(text); } catch { throw new Error(`返回数据异常 (HTTP ${res.status})`); }
  if (!res.ok) throw new Error(json.msg || `HTTP ${res.status}`);
  if (json.code !== 0) throw new Error(json.msg || '请求失败');
  return json.data;
}

function toast(msg, type = '') {
  const div = document.createElement('div');
  div.className = 'toast ' + type;
  div.textContent = msg;
  $('#toastWrap').appendChild(div);
  setTimeout(() => { div.style.opacity = '0'; div.style.transition = 'opacity .25s'; }, 3000);
  setTimeout(() => div.remove(), 3300);
}

function openModal(title, html) {
  $('#modalTitle').textContent = title;
  $('#modalBody').innerHTML = html;
  $('#modalMask').hidden = false;
}
function closeModal() {
  const mask = $('#modalMask');
  if (!mask) return;
  mask.hidden = true;
  const body = $('#modalBody');
  if (body) body.innerHTML = '';
}

// 点击遮罩空白处 / 按 ESC 关闭弹窗
document.addEventListener('click', (ev) => {
  if (ev.target && ev.target.id === 'modalMask') closeModal();
});
document.addEventListener('keydown', (ev) => {
  if (ev.key === 'Escape') {
    const mask = $('#modalMask');
    if (mask && !mask.hidden) closeModal();
  }
});

function emptyBox(text, icon = '📭') {
  return `<div class="empty"><div class="big">${icon}</div>${esc(text)}</div>`;
}

function table(headers, rows, emptyText = '暂无数据') {
  if (!rows.length) return emptyBox(emptyText);
  return `<div class="tbl-wrap"><table class="tbl"><thead><tr>${headers.map((h) => `<th>${h}</th>`).join('')}</tr></thead>
  <tbody>${rows.join('')}</tbody></table></div>`;
}

function pager(total, page, size, act) {
  const pages = Math.max(1, Math.ceil(total / size));
  return `<div class="pager">共 ${total} 条 · 第 ${page}/${pages} 页
    <button class="btn sm" data-act="${act}" data-page="${page - 1}" ${page <= 1 ? 'disabled' : ''}>上一页</button>
    <button class="btn sm" data-act="${act}" data-page="${page + 1}" ${page >= pages ? 'disabled' : ''}>下一页</button>
  </div>`;
}

/* ============ 路由 ============ */
const navMap = {
  dashboard: '仪表盘', tasks: '任务管理', task: '任务编辑', studio: '录制工作台',
  runs: '运行记录', run: '运行详情', datasources: '数据源', logs: '运行日志',
  notifications: '消息中心', settings: '系统设置',
};

function currentRoute() {
  const h = location.hash.replace(/^#\/?/, '');
  const [name, id, extra] = h.split('/');
  return { name: name || 'dashboard', id: id || '', extra: extra || '' };
}

async function render() {
  stopTimers();
  const r = currentRoute();
  const name = navMap[r.name] ? r.name : 'dashboard';
  $$('#nav a').forEach((a) => a.classList.toggle('active', a.dataset.nav === name || (name === 'task' && a.dataset.nav === 'tasks') || (name === 'run' && a.dataset.nav === 'runs')));
  $('#crumbs').textContent = navMap[name] + (r.id && r.id !== 'new' ? ` #${r.id}` : '');
  const view = $('#view');
  view.innerHTML = '<div class="loading">加载中…</div>';
  try {
    switch (name) {
      case 'dashboard': await renderDashboard(); break;
      case 'tasks': await renderTasks(); break;
      case 'task': await renderTaskPage(r.id); break;
      case 'studio': await renderStudio(); break;
      case 'runs': if (!r.id) runQuery.task_id = ''; await renderRuns(r.id); break;
      case 'run': await renderRun(r.id); break;
      case 'datasources': await renderDataSources(); break;
      case 'logs': await renderLogs(); break;
      case 'notifications': await renderNotifications(); break;
      case 'settings': await renderSettings(); break;
      default: await renderDashboard();
    }
  } catch (e) {
    view.innerHTML = `<div class="empty"><div class="big">⚠️</div>${esc(e.message)}</div>`;
  }
}

/* ============ 仪表盘 ============ */
async function renderDashboard() {
  const d = await api('/dashboard');
  const rows = (d.recent || []).map((r) => `<tr>
    <td>#${r.id}</td>
    <td><a href="#/run/${r.id}">${esc(r.task_name)}</a></td>
    <td>${modeTag(r.mode)}</td>
    <td>${statusTag(r.status)}</td>
    <td>${fmt(r.started_at)}</td>
    <td>${fmtDur(r.duration_ms)}</td>
    <td>${r.fetched} / ${r.inserted} / ${r.duplicated}</td>
  </tr>`);
  const failed = (d.recent || []).filter((r) => r.status === 'failed' || r.status === 'partial');
  const failTip = failed.length
    ? `<div class="card" style="margin-bottom:16px;border-color:#f6d7d8;background:#fffafa">
        <div class="card-body row" style="gap:10px">
          <span class="tag red">最近异常</span>
          <span class="hint grow">最近 ${failed.length} 次运行未完全成功，可到运行记录查看请求与响应报文。</span>
          <a class="btn sm" href="#/runs">查看运行记录</a>
        </div>
      </div>`
    : '';
  $('#view').innerHTML = `
  <div class="page-head">
    <div><h2>仪表盘</h2><p>三种采集模式：纯接口 · 纯 UI · UI 登录 + 接口抓取</p></div>
    <div class="row">
      <a class="btn" href="#/tasks">任务管理</a>
      <a class="btn primary" href="#/studio">开始录制</a>
    </div>
  </div>
  <div class="grid grid-4">
    <div class="card stat accent"><span class="k">🧩 任务总数</span><span class="v">${d.tasks}</span><span class="s">启用 ${d.tasks_enabled} 个 · <a href="#/tasks">去管理</a></span></div>
    <div class="card stat accent"><span class="k">📄 今日运行</span><span class="v">${d.runs_today}</span><span class="s">累计 ${d.runs_total} 次 · 运行中 ${d.running} 个</span></div>
    <div class="card stat accent"><span class="k">✅ 成功率</span><span class="v">${d.success_rate.toFixed(1)}%</span><span class="s">${d.success_rate >= 95 ? '整体稳定' : d.success_rate >= 80 ? '存在少量失败' : '建议排查失败任务'}</span></div>
    <div class="card stat accent"><span class="k">🔔 未读消息</span><span class="v">${d.unread}</span><span class="s"><a href="#/notifications">查看消息中心</a></span></div>
  </div>
  ${failTip}
  <div class="card" style="margin-top:16px">
    <div class="card-head"><h3>最近运行</h3><a class="btn sm" href="#/runs">全部记录</a></div>
    ${table(['运行', '任务', '模式', '状态', '开始时间', '耗时', '抓取/新增/重复'], rows, '还没有运行记录，去任务管理里执行一个任务吧')}
  </div>
  <div class="card" style="margin-top:16px">
    <div class="card-head"><h3>模式选型建议</h3></div>
    <div class="card-body grid grid-3">
      <div><div class="tag blue">方案一 · 纯接口</div><p class="hint">网站使用明显的 Token（Authorization / access_token），直接录制登录接口，提取 Token 后替换后续请求头，分页拉取列表。<br><b>推荐：</b>接口稳定、验证参数少。</p></div>
      <div><div class="tag amber">方案三 · UI + 接口</div><p class="hint">UI 只回放登录页面，收集 Cookie / localStorage 会话，后续列表接口用 HTTP 直接抓。<br><b>推荐：</b>动态签名参数多、接口本身可复用。</p></div>
      <div><div class="tag purple">方案二 · 纯 UI</div><p class="hint">浏览器真实操作：打开、输入、点击、滚动，再从页面网络或 DOM 表格中抓数据。<br><b>推荐：</b>风控严格、每一步都校验的站点。</p></div>
    </div>
  </div>`;
}

/* ============ 任务列表 ============ */
let taskQuery = { page: 1, keyword: '', mode: '' };

async function renderTasks() {
  const q = new URLSearchParams({ page: taskQuery.page, page_size: 15, keyword: taskQuery.keyword, mode: taskQuery.mode });
  const d = await api('/tasks?' + q.toString());
  const rows = (d.list || []).map((t) => `<tr>
    <td>#${t.id}</td>
    <td><a href="#/task/${t.id}"><b>${esc(t.name)}</b></a><div class="hint">${esc(t.remark || '')}</div></td>
    <td>${modeTag(t.mode)}</td>
    <td>${esc(scheduleText(t))}<div class="hint">${t.next_run ? '下次 ' + fmt(t.next_run) : ''}</div></td>
    <td>${t.running ? '<span class="tag blue">运行中</span>' : statusTag(t.last_status || '')}</td>
    <td>${fmt(t.last_run_at)}<div class="hint">${esc(t.last_message || '')}</div></td>
    <td><div class="actions">
      <button class="btn sm primary" data-act="run-task" data-id="${t.id}">运行</button>
      <a class="btn sm" href="#/task/${t.id}">编辑</a>
      <button class="btn sm" data-act="clone-task" data-id="${t.id}">复制</button>
      <a class="btn sm" href="#/runs/${t.id}">记录</a>
      <button class="btn sm" data-act="export-task" data-id="${t.id}">导出</button>
      <button class="btn sm danger" data-act="del-task" data-id="${t.id}">删除</button>
    </div></td>
  </tr>`);
  $('#view').innerHTML = `
  <div class="page-head">
    <div><h2>任务管理</h2><p>共 ${d.total} 个任务，可导入导出 JSON 在其它设备共享</p></div>
    <div class="row">
      <input type="file" id="importFile" accept=".json,application/json" hidden />
      <button class="btn" data-act="import-task">导入</button>
      <button class="btn" data-act="export-all">导出全部</button>
      <a class="btn primary" href="#/task/new">+ 新建任务</a>
    </div>
  </div>
  <div class="card">
    <div class="card-head">
      <div class="row grow">
        <input type="text" id="taskKw" placeholder="搜索任务名称 / 备注" value="${esc(taskQuery.keyword)}" style="max-width:260px" />
        <select id="taskMode" style="max-width:160px">
          <option value="">全部模式</option>
          <option value="api" ${taskQuery.mode === 'api' ? 'selected' : ''}>纯接口</option>
          <option value="ui" ${taskQuery.mode === 'ui' ? 'selected' : ''}>纯 UI</option>
          <option value="hybrid" ${taskQuery.mode === 'hybrid' ? 'selected' : ''}>UI + 接口</option>
        </select>
        <button class="btn sm" data-act="task-search">搜索</button>
      </div>
      <button class="btn sm ghost" data-act="task-refresh" title="刷新列表">↻ 刷新</button>
    </div>
    ${table(['ID', '任务', '模式', '调度', '状态', '上次运行', '操作'], rows, '还没有任务，点击右上角新建，或到录制工作台录制一个')}
    ${pager(d.total, d.page, d.page_size, 'task-page')}
  </div>`;
  $('#taskKw')?.addEventListener('keydown', (e) => { if (e.key === 'Enter') { taskQuery.keyword = e.target.value; taskQuery.page = 1; renderTasks(); } });
  $('#taskMode')?.addEventListener('change', (e) => { taskQuery.mode = e.target.value; taskQuery.page = 1; renderTasks(); });
}

function scheduleText(t) {
  if (!t.enabled) return '已停用';
  switch (t.schedule_type) {
    case 'cron': return 'Cron ' + (t.cron || '未设置');
    case 'interval': return t.interval_sec ? `每 ${t.interval_sec} 秒` : '未设置';
    default: return '手动';
  }
}

/* ============ 运行记录 ============ */
let runQuery = { page: 1, task_id: '', status: '' };

async function renderRuns(taskId) {
  if (taskId) { runQuery.task_id = taskId; runQuery.page = 1; }
  if (!state.taskOptions.length) {
    try { state.taskOptions = await api('/tasks/options'); } catch { state.taskOptions = []; }
  }
  const q = new URLSearchParams({ page: runQuery.page, page_size: 20 });
  if (runQuery.task_id) q.set('task_id', runQuery.task_id);
  if (runQuery.status) q.set('status', runQuery.status);
  const d = await api('/runs?' + q.toString());
  const rows = (d.list || []).map((r) => `<tr>
    <td>#${r.id}</td>
    <td><a href="#/run/${r.id}">${esc(r.task_name)}</a></td>
    <td>${modeTag(r.mode)}</td>
    <td>${r.trigger === 'cron' ? '<span class="tag amber">定时</span>' : '<span class="tag gray">手动</span>'}</td>
    <td>${statusTag(r.status)}</td>
    <td>${fmt(r.started_at)}</td>
    <td>${fmtDur(r.duration_ms)}</td>
    <td>${r.pages} / ${r.fetched} / ${r.inserted} / ${r.duplicated}</td>
    <td><div class="actions">
      <a class="btn sm" href="#/run/${r.id}">详情</a>
      <button class="btn sm" data-act="run-export" data-id="${r.id}" data-format="xlsx">Excel</button>
      <button class="btn sm danger" data-act="del-run" data-id="${r.id}">删除</button>
    </div></td>
  </tr>`);
  const opts = state.taskOptions.map((t) => `<option value="${t.id}" ${String(runQuery.task_id) === String(t.id) ? 'selected' : ''}>${esc(t.name)}</option>`).join('');
  const statusOpts = [['running', '运行中'], ['success', '成功'], ['failed', '失败'], ['partial', '部分成功']]
    .map(([v, t]) => `<option value="${v}" ${runQuery.status === v ? 'selected' : ''}>${t}</option>`).join('');
  $('#view').innerHTML = `
  <div class="page-head"><div><h2>运行记录</h2><p>查看每次执行的请求、响应、步骤日志与采集数据</p></div></div>
  <div class="card">
    <div class="card-head"><div class="row grow">
      <select id="runTask" style="max-width:240px"><option value="">全部任务</option>${opts}</select>
      <select id="runStatus" style="max-width:150px">
        <option value="">全部状态</option>
        ${statusOpts}
      </select>
      <button class="btn sm" data-act="run-search">筛选</button>
    </div>
    <button class="btn sm ghost" data-act="run-refresh">↻ 刷新</button></div>
    ${table(['运行', '任务', '模式', '触发', '状态', '开始时间', '耗时', '页/抓取/新增/重复', '操作'], rows, '还没有运行记录')}
    ${pager(d.total, d.page, d.page_size, 'run-page')}
  </div>`;
  $('#runStatus')?.addEventListener('change', () => { runQuery.status = $('#runStatus').value; runQuery.page = 1; renderRuns(); });
}

async function renderRun(id) {
  if (!id) { location.hash = '#/runs'; return; }
  const [detail, steps, data] = await Promise.all([
    api('/runs/' + id), api(`/runs/${id}/steps`), api(`/runs/${id}/data?limit=20&offset=0`),
  ]);
  const r = detail.run;
  state.currentRun = { id, headers: data.headers, offset: 0, limit: 20, total: data.total };
  const kindTag = (k) => {
    const map = { http: ['blue', '接口'], ui: ['purple', 'UI'], sink: ['green', '落库'], login: ['amber', '登录'], capture: ['gray', '抓取'] };
    const [cls, text] = map[k] || ['gray', k || '-'];
    return `<span class="tag ${cls}">${text}</span>`;
  };
  const stepRows = (steps || []).map((s, i) => `<tr>
    <td>${i + 1}</td>
    <td>${kindTag(s.kind)} ${esc(s.name)}</td>
    <td>${s.method ? `<span class="tag gray">${esc(s.method)}</span>` : ''} <span class="hint">${esc(shortUrl(s.url, 60))}</span></td>
    <td>${s.resp_status || '-'}</td>
    <td>${fmtDur(s.duration_ms)}</td>
    <td>${s.status === 'failed' ? '<span class="tag red">失败</span>' : s.error ? `<span class="tag red">${esc(s.error).slice(0, 40)}</span>` : '<span class="tag green">OK</span>'}</td>
    <td><button class="btn sm" data-act="step-detail" data-id="${s.id}" ${s.kind === 'http' || s.kind === 'sink' ? '' : 'disabled'}>详情</button></td>
  </tr>`);
  $('#view').innerHTML = `
  <div class="page-head">
    <div><h2>${esc(r.task_name)} <span class="hint">运行 #${r.id}</span></h2>
    <p>${statusTag(r.status)} ${modeTag(r.mode)} · 开始 ${fmt(r.started_at)} · 耗时 ${fmtDur(r.duration_ms)}</p></div>
    <div class="row">
      <button class="btn" onclick="history.back()">返回</button>
      <button class="btn" data-act="run-export" data-id="${r.id}" data-format="csv">导出 CSV</button>
      <button class="btn primary" data-act="run-export" data-id="${r.id}" data-format="xlsx">导出 Excel</button>
    </div>
  </div>
  ${r.error ? `<div class="card"><div class="card-body" style="color:#c43239"><b>错误：</b>${esc(r.error)}</div></div>` : ''}
  <div class="grid grid-4" style="margin-bottom:16px">
    <div class="card stat"><span class="k">页数</span><span class="v">${r.pages}</span></div>
    <div class="card stat"><span class="k">抓取 / 新增</span><span class="v">${r.fetched} / ${r.inserted}</span></div>
    <div class="card stat"><span class="k">重复跳过</span><span class="v">${r.duplicated}</span></div>
    <div class="card stat"><span class="k">失败</span><span class="v">${r.failed}</span></div>
  </div>
  <div class="card">
    <div class="card-head"><h3>执行步骤</h3><span class="hint">点击「请求/响应」查看完整报文</span></div>
    ${table(['#', '步骤', '请求', '状态码', '耗时', '结果', ''], stepRows, '没有步骤日志')}
  </div>
  <div class="card" style="margin-top:16px">
    <div class="card-head"><h3>数据集预览</h3><span class="hint">共 ${data.total} 条</span></div>
    <div id="runDataBox">${renderDataTable(data)}</div>
  </div>`;
}

function renderDataTable(data) {
  const heads = data.headers || [];
  const rows = (data.rows || []).map((row, i) => `<tr><td>${(data.offset || 0) + i + 1}</td>${heads.map((h) => {
    const v = row[h];
    const s = v && typeof v === 'object' ? JSON.stringify(v) : (v == null ? '' : String(v));
    return `<td title="${esc(s)}">${esc(s.length > 60 ? s.slice(0, 60) + '…' : s)}</td>`;
  }).join('')}</tr>`);
  const total = data.total || 0;
  const page = Math.floor((data.offset || 0) / (data.limit || 20)) + 1;
  const pages = Math.max(1, Math.ceil(total / (data.limit || 20)));
  return table(['#', ...heads.map((h) => esc(h))], rows, '暂无采集数据') +
    `<div class="pager">共 ${total} 条 · 第 ${page}/${pages} 页
      <button class="btn sm" data-act="data-page" data-offset="${Math.max(0, (data.offset || 0) - (data.limit || 20))}" ${page <= 1 ? 'disabled' : ''}>上一页</button>
      <button class="btn sm" data-act="data-page" data-offset="${(data.offset || 0) + (data.limit || 20)}" ${page >= pages ? 'disabled' : ''}>下一页</button>
    </div>`;
}

/* ============ 数据源 ============ */
function dsTypeTag(t) {
  const map = { mysql: ['blue', 'MySQL'], oracle: ['red', 'Oracle'], postgres: ['green', 'PostgreSQL'], sqlite: ['amber', 'SQLite'] };
  const [cls, label] = map[t] || ['gray', t];
  return `<span class="tag ${cls}">${esc(label)}</span>`;
}

async function renderDataSources() {
  state.datasources = await api('/datasources');
  const rows = state.datasources.map((d) => `<tr>
    <td>#${d.id}</td>
    <td><b>${esc(d.name)}</b></td>
    <td>${dsTypeTag(d.type)}</td>
    <td><span class="code">${esc(d.dsn ? d.dsn.replace(/:[^:@/]+@/, ':***@') : `${d.host || ''}${d.port ? ':' + d.port : ''}/${d.database || ''}`)}</span></td>
    <td>${esc(d.user || '-')}</td>
    <td><div class="actions">
      <button class="btn sm" data-act="ds-test" data-id="${d.id}">测试连接</button>
      <button class="btn sm" data-act="ds-sql" data-id="${d.id}">执行 SQL</button>
      <button class="btn sm" data-act="ds-edit" data-id="${d.id}">编辑</button>
      <button class="btn sm danger" data-act="ds-del" data-id="${d.id}">删除</button>
    </div></td>
  </tr>`);
  $('#view').innerHTML = `
  <div class="page-head">
    <div><h2>数据源</h2><p>支持 MySQL / Oracle / PostgreSQL / SQLite，任务可按字段映射写入目标表，也可直接执行 SQL 调试</p></div>
    <button class="btn primary" data-act="ds-new">+ 新增数据源</button>
  </div>
  <div class="grid grid-4" style="margin-bottom:16px">
    <div class="card stat"><span class="k">🗄️ 数据源总数</span><span class="v">${state.datasources.length}</span><span class="s">可在任务落库配置中选择</span></div>
    <div class="card stat"><span class="k">🐬 MySQL</span><span class="v">${state.datasources.filter((x) => x.type === 'mysql').length}</span><span class="s">默认端口 3306</span></div>
    <div class="card stat"><span class="k">🐘 PostgreSQL</span><span class="v">${state.datasources.filter((x) => x.type === 'postgres').length}</span><span class="s">默认端口 5432</span></div>
    <div class="card stat"><span class="k">🪶 SQLite / Oracle</span><span class="v">${state.datasources.filter((x) => x.type === 'sqlite' || x.type === 'oracle').length}</span><span class="s">SQLite 填文件路径即可</span></div>
  </div>
  <div class="card">${table(['ID', '名称', '类型', '连接', '用户名', '操作'], rows, '还没有数据源，点击右上角「+ 新增数据源」添加一个数据库连接')}</div>`;
}

function dsForm(d = {}) {
  const types = ['mysql', 'oracle', 'postgres', 'sqlite'];
  return `
  <div class="form-grid">
    <input type="hidden" id="dsId" value="${d.id || ''}" />
    <div><label class="field">名称</label><input id="dsName" value="${esc(d.name || '')}" placeholder="如：业务库 MySQL" /></div>
    <div><label class="field">类型</label><select id="dsType">${types.map((t) => `<option value="${t}" ${d.type === t ? 'selected' : ''}>${t}</option>`).join('')}</select></div>
    <div><label class="field">主机（SQLite 可留空）</label><input id="dsHost" value="${esc(d.host || '')}" placeholder="127.0.0.1" /></div>
    <div><label class="field">端口</label><input id="dsPort" type="number" value="${d.port || ''}" placeholder="3306 / 1521 / 5432" /></div>
    <div><label class="field">用户名</label><input id="dsUser" value="${esc(d.user || '')}" /></div>
    <div><label class="field">密码</label><input id="dsPass" type="password" value="${esc(d.password || '')}" placeholder="${d.id ? '留空表示不修改' : ''}" /></div>
    <div><label class="field">数据库 / SQLite 文件路径</label><input id="dsDb" value="${esc(d.database || '')}" placeholder="mydb 或 ./data/local.sqlite" /></div>
    <div><label class="field">附加参数（可选）</label><input id="dsParams" value="${esc(d.params || '')}" placeholder="charset=utf8mb4&parseTime=True" /></div>
    <div class="full"><label class="field">DSN（可选，填写后优先使用）</label><input id="dsDsn" value="${esc(d.dsn || '')}" placeholder="user:pass@tcp(host:port)/db?charset=utf8mb4" /></div>
  </div>
  <div class="row" style="margin-top:16px;justify-content:flex-end">
    <button class="btn" data-act="ds-form-test">测试连接</button>
    <button class="btn primary" data-act="ds-save">保存</button>
  </div>`;
}

function dsFormValues() {
  return {
    id: Number($('#dsId')?.value || 0),
    name: $('#dsName')?.value || '',
    type: $('#dsType')?.value || 'mysql',
    host: $('#dsHost')?.value || '',
    port: Number($('#dsPort')?.value || 0),
    user: $('#dsUser')?.value || '',
    password: $('#dsPass')?.value || '',
    database: $('#dsDb')?.value || '',
    params: $('#dsParams')?.value || '',
    dsn: $('#dsDsn')?.value || '',
  };
}

/* ============ 运行日志 ============ */
async function renderLogs() {
  state.lastLogId = 0;
  state.logPaused = false;
  $('#view').innerHTML = `
  <div class="page-head"><div><h2>运行日志</h2><p>采集执行、浏览器、调度与接口调用日志，实时刷新</p></div>
    <div class="row">
      <select id="logLevel" style="width:130px">
        <option value="">全部级别</option><option>INFO</option><option>WARN</option><option>ERROR</option><option>DEBUG</option>
      </select>
      <button class="btn" data-act="log-pause">暂停</button>
      <button class="btn" data-act="log-clear">清屏</button>
    </div></div>
  <div class="card" style="margin-bottom:12px"><div class="card-body row" style="padding:12px 16px">
    <label class="check"><input type="checkbox" id="logAutoScroll" checked /> 自动滚动到底部</label>
    <span class="hint grow" id="logStat">正在接收日志…</span>
    <span class="hint">ERROR / WARN 会在平台写入通知（按设置）</span>
  </div></div>
  <div class="log-box" id="logBox"><div class="log-line"><span class="m">加载中…</span></div></div>`;
  await loadLogs(true);
  $('#logLevel')?.addEventListener('change', async () => { state.lastLogId = 0; $('#logBox').innerHTML = ''; await loadLogs(true); });
  every(2500, () => { if (!state.logPaused) loadLogs(false); });
}

async function loadLogs(reset) {
  const level = $('#logLevel')?.value || '';
  const box = $('#logBox');
  if (!box) return;
  try {
    const d = await api(`/logs?after=${state.lastLogId}&limit=500&level=${encodeURIComponent(level)}`);
    const list = d.list || [];
    if (!list.length) { if (reset) box.innerHTML = '<div class="log-line"><span class="m">暂无日志</span></div>'; return; }
    if (reset) box.innerHTML = '';
    const frag = document.createElement('div');
    frag.innerHTML = list.map((e) => `<div class="log-line ${e.level}"><span class="t">${esc(e.time)}</span><span class="s">[${esc(e.scope)}]</span><span class="l">${esc(e.level)}</span><span class="m">${esc(e.msg)}</span></div>`).join('');
    while (frag.firstChild) box.appendChild(frag.firstChild);
    state.lastLogId = list[list.length - 1].id;
    while (box.children.length > 2000) box.removeChild(box.firstChild);
    const auto = $('#logAutoScroll');
    if (!auto || auto.checked) box.scrollTop = box.scrollHeight;
    const stat = $('#logStat');
    if (stat) stat.textContent = `已加载 ${box.children.length} 行${level ? ' · 级别 ' + level : ''}`;
  } catch (e) { /* 忽略轮询错误 */ }
}

/* ============ 消息中心 ============ */
let notifPage = 1;
let notifUnread = false;
async function renderNotifications() {
  const d = await api(`/notifications?page=${notifPage}&page_size=20${notifUnread ? '&read=false' : ''}`);
  const rows = (d.list || []).map((n) => `<tr class="${n.read ? '' : 'unread'}">
    <td>${n.read ? '<span class="tag gray">已读</span>' : '<span class="tag red">未读</span>'}</td>
    <td><span class="tag ${n.level === 'error' ? 'red' : n.level === 'success' ? 'green' : 'blue'}">${n.level}</span></td>
    <td style="${n.read ? '' : 'font-weight:600'}">${esc(n.title)}</td>
    <td class="hint" style="max-width:520px;white-space:pre-wrap">${esc(n.content)}</td>
    <td>${n.channel === 'wecom' ? '企业微信' : '平台'}</td>
    <td>${fmt(n.created_at)}</td>
    <td><div class="actions">
      ${n.read ? '' : `<button class="btn sm" data-act="notif-read" data-id="${n.id}">标记已读</button>`}
      <button class="btn sm danger" data-act="notif-del" data-id="${n.id}">删除</button>
    </div></td>
  </tr>`);
  $('#view').innerHTML = `
  <div class="page-head">
    <div><h2>消息中心</h2><p>任务执行结果统一提醒，未读 ${d.unread} 条</p></div>
    <div class="row">
      <button class="btn" data-act="notif-read-all">全部已读</button>
      <button class="btn danger" data-act="notif-clear">清空消息</button>
    </div>
  </div>
  <div class="card">
    <div class="card-head"><div class="chip-group">
      <button class="chip ${notifUnread ? '' : 'active'}" data-act="notif-filter" data-read="all">全部消息</button>
      <button class="chip ${notifUnread ? 'active' : ''}" data-act="notif-filter" data-read="unread">只看未读</button>
    </div><span class="hint">共 ${d.total} 条</span></div>
    ${table(['状态', '级别', '标题', '内容', '渠道', '时间', '操作'], rows, '暂无消息')}
    ${pager(d.total, d.page, d.page_size, 'notif-page')}
  </div>`;
}

/* ============ 系统设置 ============ */
async function renderSettings() {
  const d = await api('/settings');
  const s = d.settings || {};
  const cfg = d.config || {};
  const b = cfg.browser || {};
  const bs = d.browser_status || {};
  $('#view').innerHTML = `
  <div class="page-head"><div><h2>系统设置</h2><p>配置文件：<span class="code">${esc(d.config_file)}</span>（修改 addr 需重启）</p></div></div>
  <div class="grid grid-2">
    <div class="card">
      <div class="card-head"><h3>通知设置</h3></div>
      <div class="card-body">
        <div class="form-grid">
          <div class="full"><label class="check"><input type="checkbox" id="setWecomEnabled" ${s.wecom_enabled === 'true' ? 'checked' : ''} /> 启用企业微信机器人</label></div>
          <div class="full"><label class="field">企业微信 Webhook 地址</label><input id="setWebhook" value="${esc(s.wecom_webhook || '')}" placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx" /></div>
          <div><label class="field">默认失败通知</label><select id="setNotifyFailure"><option value="true" ${s.notify_on_failure !== 'false' ? 'selected' : ''}>开启</option><option value="false" ${s.notify_on_failure === 'false' ? 'selected' : ''}>关闭</option></select></div>
          <div><label class="field">默认成功通知</label><select id="setNotifySuccess"><option value="true" ${s.notify_on_success === 'true' ? 'selected' : ''}>开启</option><option value="false" ${s.notify_on_success !== 'true' ? 'selected' : ''}>关闭</option></select></div>
          <div><label class="field">单次请求/响应体最大保存（KB）</label><input id="setMaxBody" type="number" value="${esc(s.max_body_kb || '256')}" /></div>
        </div>
        <div class="row" style="margin-top:16px;justify-content:flex-end">
          <button class="btn" data-act="wecom-test">发送测试消息</button>
          <button class="btn primary" data-act="settings-save">保存设置</button>
        </div>
        <p class="hint" style="margin-top:12px">任务级别还可以单独选择「通知 / 不通知」及通知时机；平台消息中心始终记录所有执行结果。</p>
      </div>
    </div>
    <div class="card">
      <div class="card-head"><h3>浏览器内核</h3><span class="tag ${bs.available ? 'green' : 'amber'}">${bs.available ? '可用' : '未就绪'}</span></div>
      <div class="card-body">
        <div class="kv-list">
          <span class="k">运行平台</span><span>${esc(bs.platform || '')}</span>
          <span class="k">内核版本</span><span>Chromium r${esc(String(bs.revision || ''))}</span>
          <span class="k">当前路径</span><span class="code" style="word-break:break-all">${esc(bs.path || '未找到，可点击下方下载或填写系统 Chrome/Edge 路径')}</span>
          <span class="k">来源</span><span>${esc(bs.source || '-')}</span>
          <span class="k">下载目录</span><span class="code">${esc(bs.download_dir || '')}</span>
          ${bs.message ? `<span class="k">说明</span><span class="hint">${esc(bs.message)}</span>` : ''}
        </div>
        <div class="row" style="margin-bottom:14px">
          <button class="btn" data-act="browser-download" ${bs.downloading ? 'disabled' : ''}>${bs.downloading ? '下载中…' : '下载 Chromium 内核'}</button>
          <button class="btn" data-act="settings-refresh">刷新状态</button>
        </div>
        <div class="form-grid">
          <div class="full"><label class="field">自定义浏览器路径（留空自动探测系统 Chrome/Edge）</label><input id="brBin" value="${esc(b.bin_path || '')}" placeholder="C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe" /></div>
          <div><label class="field">下载镜像</label><select id="brHost">
            ${['npm', 'google', 'custom'].map((x) => `<option value="${x}" ${b.download_host === x ? 'selected' : ''}>${x}</option>`).join('')}
          </select></div>
          <div><label class="field">自定义镜像前缀</label><input id="brCustom" value="${esc(b.custom_host || '')}" placeholder="https://npmmirror.com/mirrors/chromium-browser-snapshots" /></div>
          <div><label class="field">Chromium 版本号（0 默认）</label><input id="brRev" type="number" value="${b.revision || 0}" /></div>
          <div><label class="field">浏览器代理</label><input id="brProxy" value="${esc(b.proxy || '')}" placeholder="http://127.0.0.1:7890" /></div>
          <div><label class="check"><input type="checkbox" id="brHeadless" ${b.headless ? 'checked' : ''} /> 采集时无头运行</label></div>
          <div><label class="check"><input type="checkbox" id="brNoSandbox" ${b.no_sandbox ? 'checked' : ''} /> 关闭沙箱（Linux/容器建议）</label></div>
        </div>
        <div class="row" style="margin-top:14px;justify-content:flex-end">
          <button class="btn primary" data-act="browser-save">保存浏览器配置</button>
        </div>
      </div>
    </div>
  </div>
  <div class="card">
    <div class="card-head"><h3>关于平台</h3></div>
    <div class="card-body hint">
      <p>· 方案一「纯接口」：录制登录接口 → 自动识别 Token 并替换为 <span class="code">{{token}}</span> → 列表接口分页抓取，适合验证参数少的站点。</p>
      <p>· 方案三「UI + 接口」：UI 只回放登录，采集 Cookie / localStorage / sessionStorage，再走接口抓取，适合动态签名参数多的站点。</p>
      <p>· 方案二「纯 UI」：回放真实页面点击/输入并捕获网络或 DOM 表格，风控严格的站点还原度最高。</p>
      <p>· 变量占位符：<span class="code">{{token}}</span> <span class="code">{{page}}</span> <span class="code">{{pageSize}}</span> <span class="code">{{now}}</span> <span class="code">{{today}}</span> <span class="code">{{timestamp}}</span> <span class="code">{{uuid}}</span>；支持 <span class="code">{{var|url}}</span>、<span class="code">{{var|b64}}</span> 等修饰符。</p>
    </div>
  </div>`;
}

/* ============ 全局事件 ============ */
document.addEventListener('click', async (ev) => {
  const btn = ev.target.closest('[data-act]');
  if (!btn) return;
  const act = btn.dataset.act;
  const id = btn.dataset.id;
  try {
    switch (act) {
      case 'modal-close': closeModal(); return;
      case 'page-reload': { await render(); toast('已刷新', 'ok'); return; }
      case 'task-search': taskQuery.keyword = $('#taskKw').value; taskQuery.mode = $('#taskMode').value; taskQuery.page = 1; await renderTasks(); return;
      case 'task-page': taskQuery.page = Number(btn.dataset.page); await renderTasks(); return;
      case 'run-search': runQuery.task_id = $('#runTask').value; runQuery.status = $('#runStatus').value; runQuery.page = 1; await renderRuns(); return;
      case 'run-page': runQuery.page = Number(btn.dataset.page); await renderRuns(); return;
      case 'notif-page': notifPage = Number(btn.dataset.page); await renderNotifications(); return;
      case 'notif-filter': notifUnread = btn.dataset.read === 'unread'; notifPage = 1; await renderNotifications(); return;
      case 'run-task': {
        btn.disabled = true; btn.textContent = '启动中…';
        const run = await api(`/tasks/${id}/run`, { method: 'POST', body: {} });
        toast(`任务已启动，运行 #${run.id}`, 'ok');
        setTimeout(async () => { await renderTasks(); }, 800);
        return;
      }
      case 'clone-task': { const t = await api(`/tasks/${id}/clone`, { method: 'POST', body: {} }); toast('已复制为「' + t.name + '」', 'ok'); await renderTasks(); return; }
      case 'del-task': {
        if (!confirm('确定删除该任务吗？运行记录会保留。')) return;
        await api(`/tasks/${id}`, { method: 'DELETE' }); toast('已删除', 'ok'); await renderTasks(); return;
      }
      case 'export-task': window.open(`/api/tasks/${id}/export`, '_blank'); return;
      case 'export-all': window.open('/api/tasks/export', '_blank'); return;
      case 'import-task': $('#importFile').click(); return;
      case 'task-run-refresh': await renderTasks(); return;
      case 'task-refresh': await renderTasks(); return;
      case 'run-refresh': await renderRuns(); return;
      case 'log-pause': {
        state.logPaused = !state.logPaused;
        btn.textContent = state.logPaused ? '继续' : '暂停';
        btn.classList.toggle('primary', state.logPaused);
        return;
      }
      case 'data-page': {
        const data = await api(`/runs/${state.currentRun.id}/data?limit=20&offset=${btn.dataset.offset}`);
        $('#runDataBox').innerHTML = renderDataTable(data);
        return;
      }
      case 'run-export': window.open(`/api/runs/${id}/export?format=${btn.dataset.format || 'xlsx'}`, '_blank'); return;
      case 'del-run': {
        if (!confirm('确定删除该运行记录及其数据吗？')) return;
        await api(`/runs/${id}`, { method: 'DELETE' }); toast('已删除', 'ok'); await renderRuns(); return;
      }
      case 'step-detail': {
        const runId = state.currentRun?.id;
        const steps = await api(`/runs/${runId}/steps`);
        const s = (steps || []).find((x) => String(x.id) === String(id));
        if (!s) return;
        if (s.kind === 'sink') {
          openModal(`落库：${s.name}`, `
            <div class="kv-list">
              <span class="k">状态</span><span>${s.status === 'failed' ? '<span class="tag red">失败</span>' : '<span class="tag green">成功</span>'}</span>
              <span class="k">耗时</span><span>${fmtDur(s.duration_ms)}</span>
              ${s.error ? `<span class="k">错误</span><span style="color:#c43239">${esc(s.error)}</span>` : ''}
            </div>
            <h4>SQL 模板 / 目标表</h4><div class="pre">${esc(s.url || '-')}</div>
            <h4>执行结果</h4><div class="pre">${esc(s.resp_body || '-')}</div>`);
          return;
        }
        openModal(`步骤：${s.name}`, `
          <div class="kv-list">
            <span class="k">请求</span><span>${esc(s.method)} <span class="code">${esc(s.url)}</span></span>
            <span class="k">状态码</span><span>${s.resp_status}</span>
            <span class="k">耗时</span><span>${fmtDur(s.duration_ms)}</span>
            ${s.error ? `<span class="k">错误</span><span style="color:#c43239">${esc(s.error)}</span>` : ''}
          </div>
          <h4>请求头</h4><div class="pre">${esc(s.req_headers || '-')}</div>
          <h4>请求体</h4><div class="pre">${esc(jsonPretty(s.req_body) || '-')}</div>
          <h4>响应头</h4><div class="pre">${esc(s.resp_headers || '-')}</div>
          <h4>响应体</h4><div class="pre">${esc(jsonPretty(s.resp_body) || '-')}</div>`);
        return;
      }
      case 'ds-new': openModal('新增数据源', dsForm()); return;
      case 'ds-edit': {
        const d = state.datasources.find((x) => String(x.id) === String(id));
        openModal('编辑数据源', dsForm(d || {}));
        return;
      }
      case 'ds-save': {
        const body = dsFormValues();
        const saved = await api(body.id ? `/datasources/${body.id}` : '/datasources', { method: body.id ? 'PUT' : 'POST', body });
        closeModal(); toast('已保存', 'ok'); await renderDataSources();
        return;
      }
      case 'ds-test': { const r = await api(`/datasources/${id}/test`, { method: 'POST', body: {} }); toast('连接成功', 'ok'); return; }
      case 'ds-sql': {
        const d = state.datasources.find((x) => String(x.id) === String(id));
        openModal(`执行 SQL · ${d ? d.name : '#' + id}`, dsSQLForm(d || { id, name: '#' + id }));
        return;
      }
      case 'ds-sql-run': {
        const box = $('#dsSqlResult');
        const sql = ($('#dsSqlText') || {}).value || '';
        if (!sql.trim()) { toast('请填写 SQL', 'err'); return; }
        btn.disabled = true;
        if (box) box.innerHTML = '<div class="loading">执行中…</div>';
        try {
          const r = await api(`/datasources/${id}/exec`, { method: 'POST', body: { sql, limit: 200 } });
          if (box) box.innerHTML = renderSQLResult(r);
          toast('执行完成，耗时 ' + r.elapsed_ms + ' ms', 'ok');
        } catch (e) {
          if (box) box.innerHTML = `<div class="empty"><div class="big">⚠️</div>${esc(e.message || String(e))}</div>`;
        } finally {
          btn.disabled = false;
        }
        return;
      }
      case 'ds-form-test': { const body = dsFormValues(); await api('/datasources/test', { method: 'POST', body }); toast('连接成功', 'ok'); return; }
      case 'ds-del': {
        if (!confirm('确定删除该数据源吗？')) return;
        await api(`/datasources/${id}`, { method: 'DELETE' }); toast('已删除', 'ok'); await renderDataSources(); return;
      }
      case 'log-clear': $('#logBox').innerHTML = ''; state.lastLogId = 0; return;
      case 'notif-read': await api(`/notifications/${id}/read`, { method: 'POST', body: {} }); await renderNotifications(); await refreshBadge(); return;
      case 'notif-read-all': await api('/notifications/read-all', { method: 'POST', body: {} }); await renderNotifications(); await refreshBadge(); return;
      case 'notif-del': await api(`/notifications/${id}`, { method: 'DELETE' }); await renderNotifications(); await refreshBadge(); return;
      case 'notif-clear': { if (!confirm('确定清空全部消息吗？')) return; await api('/notifications', { method: 'DELETE' }); await renderNotifications(); await refreshBadge(); return; }
      case 'settings-save': {
        await api('/settings', { method: 'PUT', body: {
          wecom_enabled: $('#setWecomEnabled').checked ? 'true' : 'false',
          wecom_webhook: $('#setWebhook').value,
          notify_on_failure: $('#setNotifyFailure').value,
          notify_on_success: $('#setNotifySuccess').value,
          max_body_kb: $('#setMaxBody').value,
        } });
        toast('设置已保存', 'ok'); return;
      }
      case 'wecom-test': { await api('/settings/test-wecom', { method: 'POST', body: {} }); toast('测试消息已发送', 'ok'); return; }
      case 'browser-download': { await api('/browser/download', { method: 'POST', body: {} }); toast('已开始下载，请稍后刷新', 'ok'); setTimeout(renderSettings, 4000); return; }
      case 'settings-refresh': await renderSettings(); return;
      case 'browser-save': {
        await api('/browser/config', { method: 'PUT', body: {
          bin_path: $('#brBin').value, download_host: $('#brHost').value, custom_host: $('#brCustom').value,
          revision: Number($('#brRev').value || 0), proxy: $('#brProxy').value,
          headless: $('#brHeadless').checked, no_sandbox: $('#brNoSandbox').checked,
        } });
        toast('浏览器配置已保存', 'ok'); await renderSettings(); return;
      }
      default: break;
    }
    if (window.handleEditorAction && await window.handleEditorAction(act, btn, ev)) return;
    if (window.handleStudioAction && await window.handleStudioAction(act, btn, ev)) return;
  } catch (e) {
    toast(e.message || String(e), 'err');
    btn.disabled = false;
    if (act === 'run-task') btn.textContent = '运行';
  }
});

document.addEventListener('change', async (e) => {
  if (e.target.id !== 'importFile') return;
  const f = e.target.files[0];
  if (!f) return;
  const form = new FormData();
  form.append('file', f);
  try {
    const res = await fetch('/api/tasks/import', { method: 'POST', body: form });
    const json = await res.json();
    if (json.code !== 0) throw new Error(json.msg);
    toast(json.msg, 'ok');
    await renderTasks();
  } catch (err) { toast(err.message, 'err'); }
  e.target.value = '';
});

async function refreshBadge() {
  try {
    const d = await api('/notifications?read=false&page_size=1');
    $('#navDot').hidden = !(d.unread > 0);
    if (d.unread > 0) $('#navDot').title = d.unread + ' 条未读';
  } catch { /* ignore */ }
}

async function checkHealth() {
  try {
    const d = await api('/health');
    $('#healthDot').className = 'status-dot ok';
    $('#healthText').textContent = `服务正常 · ${d.version} · 运行 ${d.uptime}`;
    $('#sysInfo').textContent = `版本 ${d.version} · 已运行 ${d.uptime}`;
  } catch (e) {
    $('#healthDot').className = 'status-dot bad';
    $('#healthText').textContent = '服务连接失败';
  }
}

window.addEventListener('hashchange', () => { closeModal(); render(); });

/* ============ 数据源：执行 SQL ============ */
function dsSQLForm(d) {
  return `<div class="form-grid">
    <div class="full"><label class="field">SQL（在「${esc(d.name)}」上直接执行，SELECT / INSERT / UPDATE / DELETE / DDL 均可）</label>
      <textarea id="dsSqlText" rows="8" spellcheck="false" placeholder="SELECT * FROM store LIMIT 10"></textarea></div>
  </div>
  <p class="hint" style="margin-top:8px">这里执行的是原始 SQL（不做 {{变量}} 替换），用于验证语句、查看表结构与数据。查询最多返回 200 行。</p>
  <div class="row" style="margin-top:12px;justify-content:flex-end">
    <button class="btn primary" data-act="ds-sql-run" data-id="${d.id}">执行</button>
  </div>
  <div id="dsSqlResult" style="margin-top:12px"></div>`;
}

function renderSQLResult(r) {
  const head = `<p class="hint">${r.statement === 'query' ? `返回 ${r.rows.length} 行` : `影响 ${r.affected} 行`} · 耗时 ${r.elapsed_ms} ms${r.truncated ? ' · 结果已截断（最多 200 行）' : ''}</p>`;
  if (!r.columns || !r.columns.length) return head;
  const rows = (r.rows || []).map((row) => `<tr>${row.map((v) => `<td>${esc(v === null || v === undefined ? '' : String(v))}</td>`).join('')}</tr>`);
  return head + `<div class="tbl-wrap"><table class="tbl"><thead><tr>${r.columns.map((c) => `<th>${esc(c)}</th>`).join('')}</tr></thead><tbody>${rows.join('')}</tbody></table></div>`;
}

window.addEventListener('DOMContentLoaded', async () => {
  await checkHealth();
  await refreshBadge();
  every(20000, refreshBadge);
  every(30000, checkHealth);
  if (!location.hash) location.hash = '#/dashboard';
  else await render();
});
