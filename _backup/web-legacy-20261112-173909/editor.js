/* ================= 任务编辑器 ================= */

function blankTask() {
  return {
    id: 0, name: '', mode: 'api', remark: '', enabled: true, notify: true,
    notify_on: 'failure', schedule_type: 'manual', cron: '', interval_sec: 0,
    timeout_sec: 60, vars: [], http_steps: [], ui_steps: [], login_steps: [],
    capture: { mode: 'network', url_contains: '', method: 'GET', records_path: '', wait_ms: 3000, row_selector: '' },
    pagination: { enabled: false, type: 'page', page_param: 'page', size_param: 'pageSize', page_size: 20, start_page: 1, max_pages: 100, offset_param: 'offset', cursor_param: 'cursor', cursor_path: '', records_path: '', total_path: '', stop_when_empty: true, delay_ms: 0, page_in_body: false },
    mapping: [],
    sink: { type: 'table', data_source_id: 0, table: '', write_mode: 'insert', key_columns: [], batch_size: 200, auto_create: true, export_format: 'xlsx', sql_text: '' },
    dedupe: { enabled: false, keys: [] },
  };
}

function normalizeTask(t) {
  const b = blankTask();
  const out = Object.assign(b, t || {});
  out.vars = t?.vars || [];
  out.http_steps = t?.http_steps || [];
  out.ui_steps = t?.ui_steps || [];
  out.login_steps = t?.login_steps || [];
  out.mapping = t?.mapping || [];
  out.capture = Object.assign(b.capture, t?.capture || {});
  out.pagination = Object.assign(b.pagination, t?.pagination || {});
  out.sink = Object.assign(b.sink, t?.sink || {});
  out.dedupe = Object.assign(b.dedupe, t?.dedupe || {});
  return out;
}

async function renderTaskPage(id) {
  let task;
  if (id === 'new') {
    task = normalizeTask(state.pendingTask || blankTask());
  } else {
    task = normalizeTask(await api('/tasks/' + id));
  }
  state.editingTask = task;
  state.editorDirty = false;
  if (!state.datasources.length) {
    try { state.datasources = await api('/datasources'); } catch { state.datasources = []; }
  }
  if (!state.editorTab) state.editorTab = 'basic';
  $('#view').innerHTML = taskEditorHTML(task);
  switchEditorTab(state.editorTab);
}

function headersText(arr) {
  return (arr || []).filter((h) => h.key).map((h) => `${h.key}: ${h.value || ''}`).join('\n');
}
function parseHeaders(text) {
  return String(text || '').split('\n').map((l) => l.trim()).filter(Boolean).map((line) => {
    const i = line.indexOf(':');
    if (i < 0) return { key: line, value: '' };
    return { key: line.slice(0, i).trim(), value: line.slice(i + 1).trim() };
  });
}

function extractText(arr) {
  return (arr || []).filter((r) => r.var).map((r) => {
    let right = '';
    if (r.header) right = 'header:' + r.header;
    else right = r.path || '';
    if (r.regex) right = (right || '') + ' | ' + r.regex;
    return `${r.var} = ${right}`.trim();
  }).join('\n');
}
function parseExtract(text) {
  return String(text || '').split('\n').map((l) => l.trim()).filter(Boolean).map((line) => {
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

function httpStepCard(st, i) {
  return `<div class="sub-card" data-step>
    <div class="sub-head">
      <span class="sub-title"><span class="step-no">${i + 1}</span>${esc(st.name || '接口请求')} ${st.method ? `<span class="tag gray">${esc(st.method)}</span>` : ''} ${st.capture ? '<span class="tag green">抓取</span>' : ''} ${st.paginate ? '<span class="tag blue">分页</span>' : ''}</span>
      <div class="actions">
        <button class="btn sm ghost" data-act="card-toggle" title="折叠/展开">⌃</button>
        <button class="btn sm" data-act="step-up" data-idx="${i}">↑</button>
        <button class="btn sm" data-act="step-down" data-idx="${i}">↓</button>
        <button class="btn sm danger" data-act="del-step" data-idx="${i}">删除</button>
      </div>
    </div>
    <div class="form-grid">
      <div><label class="field">名称</label><input data-f="name" value="${esc(st.name || '')}" placeholder="如：登录 / 门店列表" /></div>
      <div><label class="field">请求方法</label><select data-f="method">${['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].map((m) => `<option ${String(st.method || 'GET').toUpperCase() === m ? 'selected' : ''}>${m}</option>`).join('')}</select></div>
      <div class="full"><label class="field">URL（支持 {{变量}}）</label><input data-f="url" value="${esc(st.url || '')}" placeholder="https://api.example.com/store/list?page={{page}}&size={{pageSize}}" /></div>
      <div class="full"><label class="field">请求头（每行 Key: Value，如 Authorization: Bearer {{token}}）</label><textarea data-f="headers" rows="3">${esc(headersText(st.headers))}</textarea></div>
      <div class="full"><label class="field">请求体（JSON，占位符不加引号，如 {"page":{{page}}}）</label><textarea data-f="body" rows="4">${esc(st.body || '')}</textarea></div>
      <div><label class="field">Content-Type</label><input data-f="content_type" value="${esc(st.content_type || '')}" placeholder="application/json / application/x-www-form-urlencoded" /></div>
      <div><label class="field">请求前延迟 (ms)</label><input data-f="delay_ms" type="number" value="${st.delay_ms || 0}" /></div>
      <div class="full"><label class="field">提取变量（每行：变量 = JSON路径，或 变量 = header:响应头名，可加 | 正则）</label>
        <textarea data-f="extract" rows="3" placeholder="token = data.access_token&#10;csrf = header:x-csrf-token">${esc(extractText(st.extract))}</textarea></div>
      <div class="full row">
        <label class="check"><input type="checkbox" data-f="capture" ${st.capture ? 'checked' : ''} /> 该步骤为数据抓取步骤</label>
        <label class="check"><input type="checkbox" data-f="paginate" ${st.paginate ? 'checked' : ''} /> 该步骤按分页配置循环</label>
      </div>
      <div><label class="field">记录列表路径</label><input data-f="records_path" value="${esc(st.records_path || '')}" placeholder="data.list / data.records / result" /></div>
      <div><label class="field">总数字段路径</label><input data-f="total_path" value="${esc(st.total_path || '')}" placeholder="data.total" /></div>
    </div>
  </div>`;
}

function uiStepCard(st, i, kind) {
  const acts = [['open', '打开页面'], ['click', '点击'], ['input', '输入'], ['select', '下拉选择'], ['press', '按键'], ['wait', '等待'], ['wait_for', '等待元素'], ['scroll', '滚动'], ['eval', '执行 JS']];
  const actLabel = (acts.find(([v]) => v === st.action) || ['', st.action || '动作'])[1];
  return `<div class="sub-card" data-ui="${kind}">
    <div class="sub-head">
      <span class="sub-title"><span class="step-no">${i + 1}</span>${esc(actLabel)} ${st.selector ? `<span class="hint">${esc(st.selector)}</span>` : ''}</span>
      <div class="actions">
        <button class="btn sm ghost" data-act="card-toggle" title="折叠/展开">⌃</button>
        <button class="btn sm" data-act="ui-up" data-idx="${i}">↑</button>
        <button class="btn sm" data-act="ui-down" data-idx="${i}">↓</button>
        <button class="btn sm danger" data-act="del-ui" data-idx="${i}">删除</button>
      </div>
    </div>
    <div class="form-grid">
      <div><label class="field">动作</label><select data-f="action">${acts.map(([v, t]) => `<option value="${v}" ${st.action === v ? 'selected' : ''}>${t}</option>`).join('')}</select></div>
      <div><label class="field">选择器(CSS)</label><input data-f="selector" value="${esc(st.selector || '')}" placeholder="#username / button.login" /></div>
      <div><label class="field">值 / 网址 / JS</label><input data-f="value" value="${esc(st.value || '')}" placeholder="open 填网址；input 填内容；eval 填 JS" /></div>
      <div><label class="field">按键（press）</label><input data-f="key" value="${esc(st.key || '')}" placeholder="Enter / Tab" /></div>
      <div><label class="field">等待毫秒 / 超时</label><input data-f="ms" type="number" value="${st.ms || 0}" /></div>
      <div><label class="field">文本（点击兜底 / wait_for 文本）</label><input data-f="text" value="${esc(st.text || '')}" /></div>
      <div class="full"><label class="field">说明</label><input data-f="desc" value="${esc(st.desc || '')}" /></div>
    </div>
  </div>`;
}

function taskEditorHTML(t) {
  const isNew = !t.id;
  const counts = {
    vars: (t.vars || []).length,
    steps: (t.http_steps || []).length,
    ui: (t.ui_steps || []).length,
    login: (t.login_steps || []).length,
    mapping: (t.mapping || []).length,
  };
  return `
  <div class="page-head">
    <div>
      <h2>${isNew ? '新建任务' : esc(t.name || '任务编辑')} ${t.id ? `<span class="hint">#${t.id}</span>` : '<span class="tag amber">未保存</span>'} <span class="tag amber" id="dirtyTag" hidden>● 有未保存修改</span></h2>
      <p>${modeTag(t.mode)} 配置采集步骤、字段映射与落库规则</p>
    </div>
    <div class="row">
      <button class="btn" data-act="task-back">返回</button>
      ${!isNew ? `<button class="btn" data-act="task-run">保存并运行</button>` : ''}
      <button class="btn primary" data-act="save-task">保存任务</button>
    </div>
  </div>
  <div class="card">
    <div class="tabs">
      ${[['basic', '基本设置'], ['vars', '变量'], ['steps', '接口步骤'], ['ui', 'UI 步骤'], ['login', '登录步骤'], ['capture', '抓取配置'], ['pagination', '分页配置'], ['mapping', '字段映射'], ['sink', '落库配置'], ['dedupe', '去重配置']]
        .map(([k, label]) => `<button data-act="task-tab" data-tab="${k}">${label}${counts[k] ? `<span class="tab-badge">${counts[k]}</span>` : ''}</button>`).join('')}
    </div>
    <div class="card-body">
      <div class="pane" id="pane-basic">
        <div class="form-grid">
          <div><label class="field">任务名称 *</label><input id="tName" value="${esc(t.name)}" placeholder="如：门店列表采集" /></div>
          <div><label class="field">采集模式 *</label><select id="tMode">
            <option value="api" ${t.mode === 'api' ? 'selected' : ''}>方案一 · 纯接口（Token 替换）</option>
            <option value="hybrid" ${t.mode === 'hybrid' ? 'selected' : ''}>方案三 · UI 登录 + 接口抓取</option>
            <option value="ui" ${t.mode === 'ui' ? 'selected' : ''}>方案二 · 纯 UI 回放</option>
          </select></div>
          <div class="full"><p class="hint" id="modeHint">${modeHintText(t.mode)}</p></div>
          <div class="full"><label class="field">备注</label><input id="tRemark" value="${esc(t.remark)}" /></div>
          <div class="full row">
            <label class="check"><input type="checkbox" id="tEnabled" ${t.enabled ? 'checked' : ''} /> 启用任务</label>
            <label class="check"><input type="checkbox" id="tNotify" ${t.notify ? 'checked' : ''} /> 执行后通知</label>
            <select id="tNotifyOn" style="max-width:170px">
              <option value="failure" ${t.notify_on === 'failure' ? 'selected' : ''}>仅失败时通知</option>
              <option value="success" ${t.notify_on === 'success' ? 'selected' : ''}>仅成功时通知</option>
              <option value="always" ${t.notify_on === 'always' ? 'selected' : ''}>每次都通知</option>
            </select>
          </div>
          <div><label class="field">调度方式</label><select id="tSchedule">
            <option value="manual" ${t.schedule_type === 'manual' ? 'selected' : ''}>手动触发</option>
            <option value="cron" ${t.schedule_type === 'cron' ? 'selected' : ''}>Cron 表达式</option>
            <option value="interval" ${t.schedule_type === 'interval' ? 'selected' : ''}>固定间隔</option>
          </select></div>
          <div><label class="field">Cron / 间隔</label><input id="tCron" value="${esc(t.cron)}" placeholder="0 0 8 * * * 或 3600" /></div>
          <div><label class="field">间隔秒数（间隔模式）</label><input id="tInterval" type="number" value="${t.interval_sec || 0}" /></div>
          <div><label class="field">单次超时（秒）</label><input id="tTimeout" type="number" value="${t.timeout_sec || 60}" /></div>
        </div>
        <p class="hint" style="margin-top:14px">Cron 支持 5 段或 6 段（秒 分 时 日 月 周）。示例：<span class="code">0 0 8 * * *</span> 每天 8 点；<span class="code">0 */30 * * * *</span> 每 30 分钟。</p>
      </div>

      <div class="pane" id="pane-vars">
        <p class="hint">任务级变量，可在 URL / Header / Body 中以 <span class="code">{{变量名}}</span> 引用。内置变量：now、today、timestamp、ts、uuid、random、page、pageSize、offset、cursor。</p>
        <div id="varsBox" style="margin-top:12px">
          ${(t.vars || []).map((v, i) => `<div class="kv-row" data-kv>
            <input data-k="key" value="${esc(v.key)}" placeholder="变量名" />
            <input data-k="value" value="${esc(v.value)}" placeholder="值" />
            <button class="btn sm danger" data-act="del-var" data-idx="${i}">删除</button>
          </div>`).join('')}
        </div>
        ${(t.vars || []).length ? '' : `<div class="pane-empty">还没有任务变量。常用示例：<span class="code">user = demo</span>、<span class="code">pass = 123456</span>，在请求体里用 <span class="code">{{user}}</span> 引用。</div>`}
        <button class="btn sm" data-act="add-var">+ 添加变量</button>
      </div>

      <div class="pane" id="pane-steps">
        <p class="hint">按顺序执行。典型配置：① 登录接口（提取 token）② 列表接口（勾选数据抓取 + 分页）。Token 会自动替换为 <span class="code">{{token}}</span>。</p>
        <div id="stepsBox" style="margin-top:12px">
          ${(t.http_steps || []).map((s, i) => httpStepCard(s, i)).join('')}
        </div>
        ${(t.http_steps || []).length ? '' : `<div class="pane-empty">还没有接口步骤。方案一：加「登录接口」提取 token，再加「列表接口」勾选分页抓取；也可以到录制工作台录制后自动生成。</div>`}
        <button class="btn sm" data-act="add-step">+ 添加接口步骤</button>
      </div>

      <div class="pane" id="pane-ui">
        <p class="hint">按顺序回放页面动作。open 动作的「值」填网址；click 填选择器；input 填选择器与内容；wait 填毫秒。</p>
        <div id="uiBox" style="margin-top:12px">
          ${(t.ui_steps || []).map((s, i) => uiStepCard(s, i, 'ui')).join('')}
        </div>
        ${(t.ui_steps || []).length ? '' : `<div class="pane-empty">还没有 UI 动作。点击下方「+ 添加 UI 动作」手工添加，或在「录制工作台」用方案二录制页面操作后自动生成。</div>`}
        <button class="btn sm" data-act="add-ui" data-kind="ui">+ 添加 UI 动作</button>
      </div>

      <div class="pane" id="pane-login">
        <p class="hint">方案三专用：只回放登录相关的 UI 动作，采集 Cookie / localStorage / sessionStorage 后交给接口步骤。若使用方案一可留空。</p>
        <div id="loginBox" style="margin-top:12px">
          ${(t.login_steps || []).map((s, i) => uiStepCard(s, i, 'login')).join('')}
        </div>
        ${(t.login_steps || []).length ? '' : `<div class="pane-empty">还没有登录动作。典型顺序：打开登录页 → 输入用户名 → 输入密码 → 点击登录 → 等待列表元素出现。</div>`}
        <button class="btn sm" data-act="add-ui" data-kind="login">+ 添加登录动作</button>
      </div>

      <div class="pane" id="pane-capture">
        <p class="hint">仅纯 UI 模式使用：回放完成后从网络请求或页面 DOM 中抓取数据。</p>
        <div class="form-grid" style="margin-top:12px">
          <div><label class="field">抓取来源</label><select id="capMode">
            <option value="network" ${t.capture.mode !== 'dom' ? 'selected' : ''}>网络请求（JSON）</option>
            <option value="dom" ${t.capture.mode === 'dom' ? 'selected' : ''}>DOM 表格</option>
          </select></div>
          <div><label class="field">目标接口 URL 包含</label><input id="capUrl" value="${esc(t.capture.url_contains || '')}" placeholder="/api/store/list" /></div>
          <div><label class="field">请求方法（留空不限）</label><input id="capMethod" value="${esc(t.capture.method || '')}" placeholder="GET / POST" /></div>
          <div><label class="field">记录列表路径</label><input id="capRecords" value="${esc(t.capture.records_path || '')}" placeholder="data.list（留空自动识别数组）" /></div>
          <div><label class="field">等待数据加载（毫秒）</label><input id="capWait" type="number" value="${t.capture.wait_ms || 3000}" /></div>
          <div><label class="field">DOM 行选择器</label><input id="capRow" value="${esc(t.capture.row_selector || '')}" placeholder="table tbody tr" /></div>
        </div>
      </div>

      <div class="pane" id="pane-pagination">
        <label class="check" style="margin-bottom:12px"><input type="checkbox" id="pgEnabled" ${t.pagination.enabled ? 'checked' : ''} /> 启用分页抓取</label>
        <div class="form-grid">
          <div><label class="field">分页类型</label><select id="pgType">
            <option value="page" ${t.pagination.type === 'page' ? 'selected' : ''}>页码 page</option>
            <option value="offset" ${t.pagination.type === 'offset' ? 'selected' : ''}>偏移量 offset</option>
            <option value="cursor" ${t.pagination.type === 'cursor' ? 'selected' : ''}>游标 cursor</option>
          </select></div>
          <div><label class="field">页码参数名</label><input id="pgPageParam" value="${esc(t.pagination.page_param || 'page')}" /></div>
          <div><label class="field">每页条数参数名</label><input id="pgSizeParam" value="${esc(t.pagination.size_param || 'pageSize')}" /></div>
          <div><label class="field">每页条数</label><input id="pgPageSize" type="number" value="${t.pagination.page_size || 20}" /></div>
          <div><label class="field">起始页</label><input id="pgStart" type="number" value="${t.pagination.start_page || 1}" /></div>
          <div><label class="field">最大页数</label><input id="pgMax" type="number" value="${t.pagination.max_pages || 100}" /></div>
          <div><label class="field">offset 参数名</label><input id="pgOffsetParam" value="${esc(t.pagination.offset_param || 'offset')}" /></div>
          <div><label class="field">cursor 参数名</label><input id="pgCursorParam" value="${esc(t.pagination.cursor_param || 'cursor')}" /></div>
          <div><label class="field">cursor 取值路径</label><input id="pgCursorPath" value="${esc(t.pagination.cursor_path || '')}" placeholder="data.next_cursor" /></div>
          <div><label class="field">记录列表路径（可覆盖步骤）</label><input id="pgRecords" value="${esc(t.pagination.records_path || '')}" /></div>
          <div><label class="field">总数字段路径</label><input id="pgTotal" value="${esc(t.pagination.total_path || '')}" placeholder="data.total" /></div>
          <div><label class="field">每页间隔 (ms)</label><input id="pgDelay" type="number" value="${t.pagination.delay_ms || 0}" /></div>
          <div class="row"><label class="check"><input type="checkbox" id="pgInBody" ${t.pagination.page_in_body ? 'checked' : ''} /> 分页参数放在 Body(JSON)</label></div>
          <div class="row"><label class="check"><input type="checkbox" id="pgStopEmpty" ${t.pagination.stop_when_empty !== false ? 'checked' : ''} /> 空页自动停止</label></div>
        </div>
      </div>

      <div class="pane" id="pane-mapping">
        <p class="hint">来源支持 JSON 路径（如 <span class="code">store.name</span>、<span class="code">list[0].id</span>），DOM 模式下填单元格 CSS 选择器（<span class="code">td:nth-child(2)</span>，<span class="code">a@href</span> 取属性）。目标字段即表格列名/数据库列名。</p>
        <div id="mapBox" style="margin-top:12px">
          ${(t.mapping || []).map((m, i) => `<div class="step-row" data-map>
            <input data-m="source" value="${esc(m.source)}" placeholder="来源字段" />
            <input data-m="target" value="${esc(m.target)}" placeholder="目标字段" />
            <select data-m="type">${['string', 'int', 'float', 'bool', 'datetime', 'json'].map((x) => `<option ${m.type === x ? 'selected' : ''}>${x}</option>`).join('')}</select>
            <input data-m="default" value="${esc(m.default || '')}" placeholder="默认值" />
            <button class="btn sm danger" data-act="del-map" data-idx="${i}">删</button>
          </div>`).join('')}
        </div>
        ${(t.mapping || []).length ? '' : `<div class="pane-empty">还没有字段映射。至少配置一列目标字段，才能导出表格或写入数据库。</div>`}
        <button class="btn sm" data-act="add-map">+ 添加字段映射</button>
      </div>

      <div class="pane" id="pane-sink">
        <div class="form-grid">
          <div><label class="field">落库方式</label><select id="sinkType">
            <option value="table" ${t.sink.type === 'table' ? 'selected' : ''}>仅生成表格（Excel/CSV）</option>
            <option value="db" ${t.sink.type === 'db' ? 'selected' : ''}>仅写入数据库</option>
            <option value="both" ${t.sink.type === 'both' ? 'selected' : ''}>表格 + 数据库</option>
            <option value="none" ${t.sink.type === 'none' ? 'selected' : ''}>不保留（仅测试）</option>
          </select></div>
          <div><label class="field">目标数据源</label><select id="sinkDs">
            <option value="0">请选择数据源</option>
            ${state.datasources.map((d) => `<option value="${d.id}" ${Number(t.sink.data_source_id) === d.id ? 'selected' : ''}>${esc(d.name)} (${esc(d.type)})</option>`).join('')}
          </select></div>
          <div><label class="field">目标表名</label><input id="sinkTable" value="${esc(t.sink.table || '')}" placeholder="collect_store" /></div>
          <div><label class="field">写入方式</label><select id="sinkMode">
            <option value="insert" ${t.sink.write_mode !== 'upsert' && t.sink.write_mode !== 'sql' ? 'selected' : ''}>插入 insert（按表批量）</option>
            <option value="upsert" ${t.sink.write_mode === 'upsert' ? 'selected' : ''}>存在则更新 upsert</option>
            <option value="sql" ${t.sink.write_mode === 'sql' ? 'selected' : ''}>自定义 SQL 语句（占位符）</option>
          </select></div>
          <div><label class="field">冲突主键列（逗号分隔，upsert 用）</label><input id="sinkKeys" value="${esc((t.sink.key_columns || []).join(','))}" placeholder="id" /></div>
          <div><label class="field">批量条数</label><input id="sinkBatch" type="number" value="${t.sink.batch_size || 200}" /></div>
          <div><label class="field">表格导出格式</label><select id="sinkFormat">
            <option value="xlsx" ${t.sink.export_format !== 'csv' ? 'selected' : ''}>Excel (xlsx)</option>
            <option value="csv" ${t.sink.export_format === 'csv' ? 'selected' : ''}>CSV</option>
          </select></div>
          <div class="row"><label class="check"><input type="checkbox" id="sinkAuto" ${t.sink.auto_create !== false ? 'checked' : ''} /> 表不存在时自动建表（仅 insert/upsert 生效）</label></div>
        </div>
        <div id="sinkSqlWrap" ${t.sink.write_mode === 'sql' ? '' : 'hidden'} style="margin-top:14px">
          <div class="row" style="justify-content:space-between;align-items:center">
            <label class="field" style="margin:0">SQL 模板（选择「自定义 SQL 语句」时生效，每行数据执行一次）</label>
            <button class="btn sm" data-act="sink-sql-gen">按字段映射生成模板</button>
          </div>
          <textarea id="sinkSql" rows="8" spellcheck="false" placeholder="INSERT INTO store (id, name, city, sales, updated_at) VALUES ({{id}}, {{name}}, {{city}}, {{sales}}, '{{now}}')">${esc(t.sink.sql_text || '')}</textarea>
          <p class="hint" style="margin-top:8px">取值方式：<br />
            · <span class="code">{{字段}}</span> 取字段映射的「目标字段」，生成绑定参数（安全，推荐）<br />
            · <span class="code">{{变量}}</span> 取运行期变量（<span class="code">token</span> <span class="code">page</span> <span class="code">now</span> <span class="code">uuid</span> 等），支持 <span class="code">|url</span> <span class="code">|b64</span> <span class="code">|upper</span>，会自动按类型加引号转义<br />
            · 原生占位符 <span class="code">?</span> / <span class="code">:1</span> / <span class="code">$1</span>：模板里不写 <span class="code">{{字段}}</span> 时，按字段映射顺序逐列绑定<br />
            · 每条 SQL 在事务内批量执行，失败整批回滚；可用数据源页「执行 SQL」按钮先验证语句
          </p>
        </div>
        <p class="hint" style="margin-top:12px">写入数据库时，字段映射的「目标字段」就是数据库列名。首次可勾选自动建表，后续会按主键 upsert 更新；需要写入多表 / 复杂语句时改用「自定义 SQL 语句」。</p>
      </div>

      <div class="pane" id="pane-dedupe">
        <label class="check" style="margin-bottom:12px"><input type="checkbox" id="ddEnabled" ${t.dedupe.enabled ? 'checked' : ''} /> 启用主键去重（跨运行永久生效）</label>
        <div class="form-grid">
          <div class="full"><label class="field">去重字段（逗号分隔，映射后的目标字段名）</label><input id="ddKeys" value="${esc((t.dedupe.keys || []).join(','))}" placeholder="store_id 或 store_id,date" /></div>
        </div>
        <p class="hint" style="margin-top:12px">去重基于字段值生成 SHA1 指纹存储，重复数据不会写入数据集与目标表，并在运行记录里统计「重复」数量。</p>
      </div>
    </div>
  </div>`;
}

function switchEditorTab(tab) {
  state.editorTab = tab;
  $$('.tabs button').forEach((b) => b.classList.toggle('active', b.dataset.tab === tab));
  $$('.pane').forEach((p) => p.classList.toggle('active', p.id === 'pane-' + tab));
}

function modeHintText(mode) {
  switch (mode) {
    case 'hybrid':
      return '方案三：在「登录步骤」里只录制登录页面动作，采集 Cookie / localStorage / sessionStorage，后续「接口步骤」复用该会话抓数据。适合动态签名参数多的站点。';
    case 'ui':
      return '方案二：在「UI 步骤」里回放真实页面操作，并在「抓取配置」选择网络请求或 DOM 表格提取数据。适合风控严格、每一步都校验的站点。';
    default:
      return '方案一：在「接口步骤」录制登录接口提取 token，再配置列表接口分页抓取，后续请求头用 {{token}} 自动替换。适合接口稳定、验证参数少的站点。';
  }
}

document.addEventListener('change', (e) => {
  if (e.target && e.target.id === 'tMode') {
    const hint = $('#modeHint');
    if (hint) hint.textContent = modeHintText(e.target.value);
  }
});

function toggleSinkSQL() {
  const wrap = $('#sinkSqlWrap');
  if (!wrap) return;
  wrap.hidden = elVal('#sinkMode') !== 'sql';
}

document.addEventListener('change', (e) => {
  if (e.target && e.target.id === 'sinkMode') toggleSinkSQL();
});

function elVal(sel) { const e = $(sel); return e ? e.value : ''; }
function elNum(sel) { const n = Number(elVal(sel)); return isNaN(n) ? 0 : n; }
function elChk(sel) { const e = $(sel); return e ? e.checked : false; }

// 把当前编辑器 DOM 内容同步回 state.editingTask
function syncEditor() {
  const t = state.editingTask;
  if (!t || !$('#tName')) return t;
  t.name = elVal('#tName');
  t.mode = elVal('#tMode');
  t.remark = elVal('#tRemark');
  t.enabled = elChk('#tEnabled');
  t.notify = elChk('#tNotify');
  t.notify_on = elVal('#tNotifyOn');
  t.schedule_type = elVal('#tSchedule');
  const cronRaw = elVal('#tCron');
  const interval = elNum('#tInterval');
  if (t.schedule_type === 'interval') {
    t.interval_sec = interval || Number(cronRaw) || 0;
    t.cron = '';
  } else if (t.schedule_type === 'cron') {
    t.cron = cronRaw;
    t.interval_sec = 0;
  } else {
    t.cron = cronRaw;
    t.interval_sec = interval || 0;
  }
  t.timeout_sec = elNum('#tTimeout');

  t.vars = $$('#varsBox [data-kv]').map((row) => ({
    key: row.querySelector('[data-k=key]').value,
    value: row.querySelector('[data-k=value]').value,
  })).filter((v) => v.key);

  t.http_steps = $$('#stepsBox [data-step]').map((card) => {
    const v = (f) => card.querySelector(`[data-f="${f}"]`)?.value || '';
    const c = (f) => !!card.querySelector(`[data-f="${f}"]`)?.checked;
    return {
      name: v('name'), method: v('method'), url: v('url'),
      headers: parseHeaders(v('headers')), body: v('body'), content_type: v('content_type'),
      extract: parseExtract(v('extract')), capture: c('capture'), paginate: c('paginate'),
      records_path: v('records_path'), total_path: v('total_path'),
      delay_ms: Number(v('delay_ms') || 0),
    };
  });

  const collectUI = (boxSel) => $$(boxSel + ' [data-ui]').map((card) => {
    const v = (f) => card.querySelector(`[data-f="${f}"]`)?.value || '';
    return {
      action: v('action'), selector: v('selector'), value: v('value'), key: v('key'),
      ms: Number(v('ms') || 0), text: v('text'), desc: v('desc'),
    };
  });
  t.ui_steps = collectUI('#uiBox');
  t.login_steps = collectUI('#loginBox');

  t.capture = {
    mode: elVal('#capMode'), url_contains: elVal('#capUrl'), method: elVal('#capMethod'),
    records_path: elVal('#capRecords'), wait_ms: elNum('#capWait'), row_selector: elVal('#capRow'),
  };
  t.pagination = {
    enabled: elChk('#pgEnabled'), type: elVal('#pgType'),
    page_param: elVal('#pgPageParam'), size_param: elVal('#pgSizeParam'),
    page_size: elNum('#pgPageSize'), start_page: elNum('#pgStart'), max_pages: elNum('#pgMax'),
    offset_param: elVal('#pgOffsetParam'), cursor_param: elVal('#pgCursorParam'), cursor_path: elVal('#pgCursorPath'),
    records_path: elVal('#pgRecords'), total_path: elVal('#pgTotal'),
    stop_when_empty: elChk('#pgStopEmpty'), delay_ms: elNum('#pgDelay'), page_in_body: elChk('#pgInBody'),
  };
  t.mapping = $$('#mapBox [data-map]').map((row) => ({
    source: row.querySelector('[data-m=source]').value,
    target: row.querySelector('[data-m=target]').value,
    type: row.querySelector('[data-m=type]').value,
    default: row.querySelector('[data-m=default]').value,
  })).filter((m) => m.source || m.target);
  t.sink = {
    type: elVal('#sinkType'), data_source_id: Number(elVal('#sinkDs') || 0), table: elVal('#sinkTable'),
    write_mode: elVal('#sinkMode'),
    key_columns: elVal('#sinkKeys').split(',').map((s) => s.trim()).filter(Boolean),
    batch_size: elNum('#sinkBatch'), auto_create: elChk('#sinkAuto'), export_format: elVal('#sinkFormat'),
    sql_text: elVal('#sinkSql'),
  };
  t.dedupe = {
    enabled: elChk('#ddEnabled'),
    keys: elVal('#ddKeys').split(',').map((s) => s.trim()).filter(Boolean),
  };
  return t;
}

async function saveTask() {
  const t = syncEditor();
  if (!t.name.trim()) { toast('请填写任务名称', 'err'); switchEditorTab('basic'); return; }
  const dbSink = t.sink.type === 'db' || t.sink.type === 'both';
  if (t.sink.write_mode === 'sql') {
    if (!dbSink) { toast('写入方式选了「自定义 SQL」，请把落库方式改为「仅写入数据库」或「表格 + 数据库」', 'err'); switchEditorTab('sink'); return; }
    if (!String(t.sink.sql_text || '').trim()) { toast('请填写 SQL 模板', 'err'); switchEditorTab('sink'); return; }
  }
  const body = {
    name: t.name.trim(), mode: t.mode, remark: t.remark, enabled: t.enabled, notify: t.notify,
    notify_on: t.notify_on, schedule_type: t.schedule_type, cron: t.cron,
    interval_sec: t.interval_sec, timeout_sec: t.timeout_sec,
    vars: t.vars, http_steps: t.http_steps, ui_steps: t.ui_steps, login_steps: t.login_steps,
    capture: t.capture, pagination: t.pagination, mapping: t.mapping, sink: t.sink, dedupe: t.dedupe,
  };
  const saved = t.id ? await api('/tasks/' + t.id, { method: 'PUT', body }) : await api('/tasks', { method: 'POST', body });
  state.pendingTask = null;
  state.editorDirty = false;
  const dirtyTag = $('#dirtyTag');
  if (dirtyTag) dirtyTag.hidden = true;
  toast('保存成功', 'ok');
  if (!t.id) {
    location.hash = '#/task/' + saved.id;
  } else {
    state.editingTask = normalizeTask(saved);
    $('#crumbs').textContent = '任务编辑 #' + saved.id;
  }
  return saved;
}

function rerenderEditor() {
  $('#view').innerHTML = taskEditorHTML(state.editingTask);
  switchEditorTab(state.editorTab);
}

function markEditorDirty() {
  state.editorDirty = true;
  const tag = $('#dirtyTag');
  if (tag) tag.hidden = false;
}

document.addEventListener('input', (e) => {
  if (!state.editingTask || !$('#tName')) return;
  const view = $('#view');
  if (view && view.contains(e.target)) markEditorDirty();
});

document.addEventListener('keydown', (e) => {
  if ((e.ctrlKey || e.metaKey) && String(e.key).toLowerCase() === 's') {
    if (!state.editingTask || !$('#tName')) return;
    e.preventDefault();
    saveTask().catch((err) => toast(err.message || String(err), 'err'));
  }
});

// 渲染后把焦点落到指定容器的最后一个输入框（新增条目时使用）
function focusLastInput(boxSel) {
  const box = $(boxSel);
  if (!box) return;
  const cards = box.querySelectorAll('[data-step],[data-ui],[data-map],[data-kv]');
  const last = cards[cards.length - 1];
  if (!last) return;
  const input = last.querySelector('input,select,textarea');
  if (input) {
    input.focus();
    if (input.select) input.select();
  }
}

window.handleEditorAction = async function (act, btn) {
  if (!state.editingTask || !$('#tName')) return false;
  const idx = Number(btn.dataset.idx || -1);
  switch (act) {
    case 'task-tab': switchEditorTab(btn.dataset.tab); return true;
    case 'task-back': history.back(); return true;
    case 'save-task': await saveTask(); return true;
    case 'task-run': { const saved = await saveTask(); const run = await api(`/tasks/${saved.id}/run`, { method: 'POST', body: {} }); toast('已启动运行 #' + run.id, 'ok'); location.hash = '#/run/' + run.id; return true; }
    case 'add-var': syncEditor(); state.editingTask.vars.push({ key: '', value: '' }); state.editorTab = 'vars'; rerenderEditor(); focusLastInput('#varsBox'); return true;
    case 'del-var': syncEditor(); state.editingTask.vars.splice(idx, 1); state.editorTab = 'vars'; rerenderEditor(); return true;
    case 'add-step': syncEditor(); state.editingTask.http_steps.push({ name: '', method: 'GET', url: '', headers: [], body: '', content_type: 'application/json', extract: [], capture: false, paginate: false, records_path: '', total_path: '', delay_ms: 0 }); state.editorTab = 'steps'; rerenderEditor(); focusLastInput('#stepsBox'); return true;
    case 'del-step': syncEditor(); state.editingTask.http_steps.splice(idx, 1); state.editorTab = 'steps'; rerenderEditor(); return true;
    case 'step-up': case 'step-down': {
      syncEditor();
      const arr = state.editingTask.http_steps;
      const j = act === 'step-up' ? idx - 1 : idx + 1;
      if (j >= 0 && j < arr.length) { [arr[idx], arr[j]] = [arr[j], arr[idx]]; }
      state.editorTab = 'steps'; rerenderEditor(); return true;
    }
    case 'add-ui': {
      syncEditor();
      const kind = btn.dataset.kind;
      const list = kind === 'login' ? state.editingTask.login_steps : state.editingTask.ui_steps;
      list.push({ action: 'click', selector: '', value: '', key: '', ms: 0, text: '', desc: '' });
      state.editorTab = kind === 'login' ? 'login' : 'ui';
      rerenderEditor();
      focusLastInput(kind === 'login' ? '#loginBox' : '#uiBox');
      toast(kind === 'login' ? '已添加登录动作，选择动作类型并填写选择器' : '已添加 UI 动作，选择动作类型并填写选择器', 'ok');
      return true;
    }
    case 'del-ui': {
      syncEditor();
      const kind = btn.closest('[data-ui]')?.dataset.ui || 'ui';
      const list = kind === 'login' ? state.editingTask.login_steps : state.editingTask.ui_steps;
      list.splice(idx, 1);
      state.editorTab = kind === 'login' ? 'login' : 'ui'; rerenderEditor(); return true;
    }
    case 'ui-up': case 'ui-down': {
      syncEditor();
      const kind = btn.closest('[data-ui]')?.dataset.ui || 'ui';
      const arr = kind === 'login' ? state.editingTask.login_steps : state.editingTask.ui_steps;
      const j = act === 'ui-up' ? idx - 1 : idx + 1;
      if (j >= 0 && j < arr.length) { [arr[idx], arr[j]] = [arr[j], arr[idx]]; }
      state.editorTab = kind === 'login' ? 'login' : 'ui'; rerenderEditor(); return true;
    }
    case 'add-map': syncEditor(); state.editingTask.mapping.push({ source: '', target: '', type: 'string', default: '' }); state.editorTab = 'mapping'; rerenderEditor(); focusLastInput('#mapBox'); return true;
    case 'del-map': syncEditor(); state.editingTask.mapping.splice(idx, 1); state.editorTab = 'mapping'; rerenderEditor(); return true;
    case 'sink-sql-gen': {
      syncEditor();
      const t = state.editingTask;
      const cols = (t.mapping || []).map((m) => (m.target || '').trim()).filter(Boolean);
      if (!cols.length) { toast('请先在「字段映射」里配置目标字段', 'err'); return true; }
      const table = (t.sink.table || '').trim() || 'your_table';
      const vals = cols.map((c) => `{{${c}}}`).join(', ');
      const box = $('#sinkSql');
      if (box) box.value = `INSERT INTO ${table} (${cols.join(', ')}) VALUES (${vals})`;
      const mode = $('#sinkMode');
      if (mode) mode.value = 'sql';
      toggleSinkSQL();
      toast('已生成 SQL 模板，可再手工调整', 'ok');
      return true;
    }
    case 'card-toggle': {
      const card = btn.closest('[data-step],[data-ui]');
      if (card) card.classList.toggle('collapsed');
      return true;
    }
    default: return false;
  }
};

/* ================= 录制工作台 ================= */

async function renderStudio() {
  const sessions = await api('/studio/sessions');
  $('#view').innerHTML = `
  <div class="page-head">
    <div><h2>录制工作台</h2><p>打开真实浏览器，录制登录与列表操作，自动生成采集任务</p></div>
  </div>
  <div class="grid grid-2">
    <div class="card">
      <div class="card-head"><h3>开始录制</h3></div>
      <div class="card-body">
        <div class="form-grid">
          <div class="full"><label class="field">网址</label><input id="studioUrl" placeholder="https://example.com/login" /></div>
          <div class="full"><label class="field">录制模式</label><select id="studioMode">
            <option value="api">方案一 · 纯接口（录制登录 + 列表接口）</option>
            <option value="hybrid">方案三 · UI 登录 + 接口抓取</option>
            <option value="ui">方案二 · 纯 UI（录制页面操作）</option>
          </select></div>
        </div>
        <div class="row" style="margin-top:16px">
          <button class="btn primary" data-act="studio-start">启动浏览器并开始录制</button>
        </div>
        <p class="hint" style="margin-top:12px">会打开一个独立 Chrome 窗口（有头模式）。请在窗口里完成登录、翻页等操作，本页面会实时显示捕获的请求与页面动作。完成后点击「生成任务草稿」。</p>
      </div>
    </div>
    <div class="card">
      <div class="card-head"><h3>录制会话</h3><button class="btn sm" data-act="studio-refresh-sessions">刷新</button></div>
      <div class="card-body" id="studioSessions">${studioSessionsHTML(sessions)}</div>
    </div>
  </div>
  <div class="card" id="studioCaptureCard" style="margin-top:16px" ${state.studioId ? '' : 'hidden'}>
    <div class="card-head">
      <h3>抓包与动作 <span class="hint" id="studioSessInfo"></span></h3>
      <div class="row">
        <label class="check"><input type="checkbox" id="reqJsonOnly" checked /> 只看 JSON</label>
        <input id="reqKw" placeholder="过滤 URL" style="max-width:180px" />
        <button class="btn sm" data-act="studio-refresh">刷新</button>
      </div>
    </div>
    <div class="card-body">
      <div class="grid grid-2">
        <div>
          <h4 style="margin:0 0 8px">网络请求 <span class="hint" id="reqCount"></span></h4>
          <div id="studioReqs" style="max-height:460px;overflow:auto"></div>
          <p class="hint">点击请求可查看完整请求头、请求体、响应头与响应体。</p>
        </div>
        <div>
          <h4 style="margin:0 0 8px">页面动作</h4>
          <div id="studioActs" class="timeline" style="max-height:460px;overflow:auto"></div>
        </div>
      </div>
    </div>
  </div>
  <div class="card" id="studioDraftCard" style="margin-top:16px" ${state.studioId ? '' : 'hidden'}>
    <div class="card-head"><h3>生成任务草稿</h3><span class="hint">选择登录接口和列表接口，自动识别 Token、分页与字段映射</span></div>
    <div class="card-body">
      <div class="form-grid">
        <div><label class="field">任务名称</label><input id="draftName" placeholder="留空自动生成" /></div>
        <div><label class="field">模式</label><select id="draftMode">
          <option value="api">纯接口</option><option value="hybrid">UI + 接口</option><option value="ui">纯 UI</option>
        </select></div>
        <div class="full"><label class="field">登录请求（可选，方案一用于提取 Token）</label><select id="draftLogin"><option value="">自动识别</option></select></div>
        <div class="full"><label class="field">列表/数据请求（方案二、三可留空）</label><select id="draftList"><option value="">自动选择最像数据列表的请求</option></select></div>
        <div class="full"><label class="field">起始网址（UI 回放用）</label><input id="draftStart" placeholder="默认使用会话网址" /></div>
      </div>
      <div class="row" style="margin-top:16px;justify-content:flex-end">
        <button class="btn" data-act="studio-draft-save">直接保存任务</button>
        <button class="btn primary" data-act="studio-draft">生成并打开编辑器</button>
      </div>
    </div>
  </div>`;
  bindStudioFilter();
  if (state.studioId) {
    await refreshStudioCapture();
    every(1600, refreshStudioCapture);
  }
}

function studioSessionsHTML(sessions) {
  if (!sessions || !sessions.length) return emptyBox('还没有录制会话，先在上方启动一个', '🎬');
  return `<div class="tbl-wrap"><table class="tbl"><thead><tr><th>会话</th><th>模式</th><th>状态</th><th>请求</th><th>动作</th><th>操作</th></tr></thead><tbody>
  ${sessions.map((s) => `<tr>
    <td class="code">${esc(s.id.slice(0, 8))}</td>
    <td>${modeTag(s.mode)}</td>
    <td>${s.status === 'recording' ? '<span class="tag green">录制中</span>' : `<span class="tag gray">${esc(s.status)}</span>`}</td>
    <td>${s.requests || 0} <span class="hint">JSON ${s.json_requests || 0}</span></td>
    <td>${s.actions || 0}</td>
    <td><div class="actions">
      <button class="btn sm primary" data-act="studio-open" data-id="${s.id}">查看</button>
      ${s.status === 'recording' ? `<button class="btn sm" data-act="studio-stop" data-id="${s.id}">停止</button>` : ''}
      <button class="btn sm danger" data-act="studio-remove" data-id="${s.id}">删除</button>
    </div></td>
  </tr>`).join('')}
  </tbody></table></div>`;
}

function bindStudioFilter() {
  ['#reqJsonOnly', '#reqKw'].forEach((sel) => {
    const e = $(sel);
    if (e) e.addEventListener('input', () => refreshStudioCapture(true));
  });
}

async function refreshStudioCapture(keepSel) {
  if (!state.studioId || !$('#studioReqs')) return;
  let d;
  try { d = await api(`/studio/sessions/${state.studioId}/capture`); } catch (e) {
    $('#studioReqs').innerHTML = emptyBox(e.message, '⚠️');
    return;
  }
  const s = d.session || {};
  $('#studioSessInfo').textContent = `${s.id ? s.id.slice(0, 8) : ''} · ${s.status} · 请求 ${s.requests || 0} / JSON ${s.json_requests || 0} · 动作 ${s.actions || 0}`;
  const jsonOnly = $('#reqJsonOnly')?.checked;
  const kw = ($('#reqKw')?.value || '').toLowerCase();
  const reqs = (d.requests || []).filter((r) => (!jsonOnly || r.is_json) && (!kw || (r.url || '').toLowerCase().includes(kw)));
  $('#reqCount').textContent = `(${reqs.length}/${(d.requests || []).length})`;
  $('#studioReqs').innerHTML = reqs.length ? `<div class="tbl-wrap"><table class="tbl"><thead><tr><th>#</th><th>方法</th><th>URL</th><th>状态</th><th>大小</th><th>耗时</th></tr></thead><tbody>
    ${reqs.map((r) => `<tr class="req-row ${state.selReq === r.id ? 'selected' : ''}" data-act="studio-req" data-id="${r.id}">
      <td>${r.seq}</td>
      <td><span class="tag ${r.method === 'GET' ? 'green' : 'blue'}">${esc(r.method)}</span></td>
      <td><div class="req-url" title="${esc(r.url)}">${esc(shortUrl(r.url, 70))}</div>${r.is_json ? '<span class="tag blue">JSON</span>' : ''}${r.failed ? ' <span class="tag red">失败</span>' : ''}</td>
      <td>${r.status || '-'}</td>
      <td>${bytes(r.size)}</td>
      <td>${fmtDur(r.duration_ms)}</td>
    </tr>`).join('')}
  </tbody></table></div>` : emptyBox('暂无匹配的请求，请在浏览器里操作页面', '🕸️');

  const acts = d.actions || [];
  $('#studioActs').innerHTML = acts.length ? acts.map((a) => {
    const t = a.at ? new Date(a.at) : null;
    const hh = t ? `${String(t.getHours()).padStart(2, '0')}:${String(t.getMinutes()).padStart(2, '0')}:${String(t.getSeconds()).padStart(2, '0')}` : '';
    const label = a.type === 'click' ? `点击 ${a.selector || a.text || ''}` : a.type === 'input' ? `输入 ${a.selector || ''} = ${a.value || a.text || ''}` : a.type === 'navigate' ? `打开 ${a.url || ''}` : `${a.type} ${a.selector || ''}`;
    return `<div class="tl-item ${esc(a.type)}"><span class="tl-time">${hh}</span>${esc(label)}</div>`;
  }).join('') : '<div class="hint">暂无页面动作</div>';

  // 更新草稿下拉
  const opts = reqs.filter((r) => r.is_json).map((r) => `<option value="${r.id}">${esc(r.method)} ${esc(shortUrl(r.url, 60))}</option>`).join('');
  const loginSel = $('#draftLogin');
  const listSel = $('#draftList');
  if (loginSel) {
    const v = loginSel.value;
    loginSel.innerHTML = '<option value="">自动识别</option>' + opts;
    loginSel.value = v;
  }
  if (listSel) {
    const v = listSel.value;
    listSel.innerHTML = '<option value="">自动选择</option>' + opts;
    listSel.value = v;
  }
  if (s.status && s.status !== 'recording') {
    $('#studioSessInfo').textContent += ' · 已停止录制';
  }
}

async function studioBuildDraft(save) {
  if (!state.studioId) { toast('请先查看一个录制会话', 'err'); return; }
  const body = {
    name: $('#draftName')?.value || '',
    mode: $('#draftMode')?.value || 'api',
    login_req_id: $('#draftLogin')?.value || '',
    list_req_id: $('#draftList')?.value || '',
    start_url: $('#draftStart')?.value || '',
  };
  const draft = await api(`/studio/sessions/${state.studioId}/draft`, { method: 'POST', body });
  state.pendingTask = draft;
  if (save) {
    const saved = await api('/tasks', { method: 'POST', body: draft });
    state.pendingTask = null;
    toast('任务已保存：' + saved.name, 'ok');
    location.hash = '#/task/' + saved.id;
    return;
  }
  toast('草稿已生成，请检查后保存', 'ok');
  if (location.hash === '#/task/new') await renderTaskPage('new');
  else location.hash = '#/task/new';
}

window.handleStudioAction = async function (act, btn) {
  switch (act) {
    case 'studio-start': {
      const url = $('#studioUrl')?.value || '';
      if (!url.trim()) { toast('请填写网址', 'err'); return true; }
      btn.disabled = true; btn.textContent = '正在启动浏览器…';
      const s = await api('/studio/sessions', { method: 'POST', body: { url, mode: $('#studioMode').value } });
      state.studioId = s.id;
      toast('录制已开始，请在弹出的浏览器窗口操作', 'ok');
      await renderStudio();
      return true;
    }
    case 'studio-open': state.studioId = btn.dataset.id; state.selReq = ''; await renderStudio(); return true;
    case 'studio-stop': await api(`/studio/sessions/${btn.dataset.id}/stop`, { method: 'POST', body: {} }); toast('已停止录制', 'ok'); await renderStudio(); return true;
    case 'studio-remove': {
      if (!confirm('确定删除该会话吗？')) return true;
      await api(`/studio/sessions/${btn.dataset.id}`, { method: 'DELETE' });
      if (state.studioId === btn.dataset.id) state.studioId = '';
      await renderStudio(); return true;
    }
    case 'studio-refresh-sessions': await renderStudio(); return true;
    case 'studio-refresh': await refreshStudioCapture(); return true;
    case 'studio-req': {
      state.selReq = btn.dataset.id;
      const r = await api(`/studio/sessions/${state.studioId}/requests/${btn.dataset.id}`);
      const hdrs = (o) => Object.entries(o || {}).map(([k, v]) => `${k}: ${v}`).join('\n') || '-';
      openModal(`${r.method} ${shortUrl(r.url, 70)}`, `
        <div class="kv-list">
          <span class="k">状态码</span><span>${r.status} ${r.failed ? '<span class="tag red">失败</span>' : ''}</span>
          <span class="k">资源类型</span><span>${esc(r.resource_type || '-')}</span>
          <span class="k">耗时</span><span>${fmtDur(r.duration_ms)}</span>
          <span class="k">大小</span><span>${bytes(r.size)} ${r.body_truncated ? '（已截断）' : ''}</span>
          <span class="k">请求时间</span><span>${fmt(r.started_at)}</span>
        </div>
        <h4>请求头</h4><div class="pre">${esc(hdrs(r.req_headers))}</div>
        <h4>请求体</h4><div class="pre">${esc(jsonPretty(r.req_body) || '-')}</div>
        <h4>响应头</h4><div class="pre">${esc(hdrs(r.resp_headers))}</div>
        <h4>响应体</h4><div class="pre">${esc(jsonPretty(r.resp_body) || '-')}</div>`);
      await refreshStudioCapture(true);
      return true;
    }
    case 'studio-draft': await studioBuildDraft(false); return true;
    case 'studio-draft-save': await studioBuildDraft(true); return true;
    default: return false;
  }
};
