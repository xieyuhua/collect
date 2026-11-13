/* ============================================================
   定时作业：按顺序执行 HTTP 请求 / Shell 命令 / 数据库 SQL
   ============================================================ */
(function () {
  'use strict';
  const C = Collect;

  let seq = 1;
  function nextKey() { return 'jk' + (++seq); }

  const KIND = {
    http: ['blue', 'HTTP'],
    shell: ['amber', 'Shell'],
    sql: ['green', 'SQL'],
  };
  function kindTag(k) {
    const hit = KIND[k];
    return hit ? { cls: hit[0], text: hit[1] } : { cls: 'gray', text: k || '-' };
  }

  function kvLines(arr) {
    return (arr || []).filter(function (x) { return x && x.key; })
      .map(function (x) { return x.key + ': ' + (x.value || ''); }).join('\n');
  }
  function parseKvLines(text) {
    return String(text || '').split('\n').map(function (l) { return l.trim(); }).filter(Boolean).map(function (line) {
      const i = line.indexOf(':');
      if (i < 0) return { key: line, value: '' };
      return { key: line.slice(0, i).trim(), value: line.slice(i + 1).trim() };
    });
  }
  function envLines(arr) {
    return (arr || []).filter(function (x) { return x && x.key; })
      .map(function (x) { return x.key + '=' + (x.value || ''); }).join('\n');
  }
  function parseEnv(text) {
    return String(text || '').split('\n').map(function (l) { return l.trim(); }).filter(Boolean).map(function (line) {
      const i = line.indexOf('=');
      if (i < 0) return { key: line, value: '' };
      return { key: line.slice(0, i).trim(), value: line.slice(i + 1).trim() };
    });
  }

  function blankStep(type) {
    return {
      _k: nextKey(), _collapsed: false, name: '', type: type || 'http', enabled: true,
      method: 'GET', url: '', headers: [], body: '', content_type: 'application/json', _headers: '',
      extract: [], _extract: '',
      command: '', work_dir: '', env: [], _env: '',
      data_source_id: 0, sql: '',
      timeout_sec: 0, continue_on_error: false,
    };
  }

  function normalizeJob(j) {
    const out = Object.assign({
      id: 0, name: '', remark: '', enabled: true, notify: true, notify_on: 'failure',
      schedule_type: 'manual', cron: '', interval_sec: 0, timeout_sec: 60,
      last_run_at: null, last_status: '', last_message: '', next_run: '',
    }, j || {});
    out.steps = (j && j.steps) || [];
    out.steps.forEach(function (s) {
      s._k = nextKey();
      s._collapsed = true;
      s.type = s.type || 'http';
      s.method = s.method || 'GET';
      s.enabled = s.enabled !== false;
      s._headers = kvLines(s.headers);
      s._extract = kvLines(s.extract);
      s._env = envLines(s.env);
    });
    return out;
  }

  function stepPayload(s) {
    const o = {
      name: s.name || '', type: s.type || 'http', enabled: !!s.enabled,
      timeout_sec: Number(s.timeout_sec || 0), continue_on_error: !!s.continue_on_error,
    };
    if (s.type === 'http') {
      o.method = String(s.method || 'GET').toUpperCase();
      o.url = s.url || '';
      o.headers = parseKvLines(s._headers);
      o.body = s.body || '';
      o.content_type = s.content_type || '';
      o.extract = parseKvLines(s._extract);
    } else if (s.type === 'shell') {
      o.command = s.command || '';
      o.work_dir = s.work_dir || '';
      o.env = parseEnv(s._env);
    } else if (s.type === 'sql') {
      o.data_source_id = Number(s.data_source_id || 0);
      o.sql = s.sql || '';
    }
    return o;
  }

  function scheduleText(j) {
    if (!j.enabled) return '已停用';
    if (j.schedule_type === 'cron') return 'Cron ' + (j.cron || '未设置');
    if (j.schedule_type === 'interval') return j.interval_sec ? '每 ' + j.interval_sec + ' 秒' : '未设置';
    return '手动触发';
  }

  /* ---------------- 作业列表 ---------------- */
  C.component('page-jobs', {
    setup() {
      const q = C.reactive({ page: 1, keyword: '' });
      const d = C.reactive({ list: [], total: 0, page: 1, page_size: 20 });
      const loading = C.ref(true);
      const runningId = C.ref(0);

      async function load() {
        loading.value = true;
        try {
          const params = new URLSearchParams({ page: q.page, page_size: 20 });
          if (q.keyword) params.set('keyword', q.keyword);
          const data = await C.api('/jobs?' + params.toString());
          d.list = data.list || [];
          d.total = data.total || 0;
          d.page = data.page || 1;
          d.page_size = data.page_size || 20;
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);

      function search() { q.page = 1; load(); }
      function changePage(p) { q.page = p; load(); }

      async function runJob(j) {
        runningId.value = j.id;
        try {
          const run = await C.api('/jobs/' + j.id + '/run', { method: 'POST', body: {} });
          C.toast('作业已启动，执行 #' + run.id, 'ok');
          setTimeout(load, 800);
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          runningId.value = 0;
        }
      }
      async function cloneJob(j) {
        try {
          const c = await C.api('/jobs/' + j.id + '/clone', { method: 'POST', body: {} });
          C.toast('已复制为「' + c.name + '」', 'ok');
          load();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }
      async function delJob(j) {
        if (!confirm('确定删除作业「' + j.name + '」吗？执行记录会一并删除。')) return;
        try {
          await C.api('/jobs/' + j.id, { method: 'DELETE' });
          C.toast('已删除', 'ok');
          load();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      return {
        q: q, d: d, loading: loading, runningId: runningId,
        load: load, search: search, changePage: changePage,
        runJob: runJob, cloneJob: cloneJob, delJob: delJob, scheduleText: scheduleText,
        fmt: C.fmt, fmtDur: C.fmtDur, statusTag: C.statusTag, kindTag: kindTag,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div>
          <h2>定时作业</h2>
          <p>按 Cron / 固定间隔执行 HTTP 请求、Shell 命令、数据库 SQL，可用于清理、同步、巡检等自动化任务</p>
        </div>
        <div class="row">
          <a class="btn" href="#/jobruns">执行记录</a>
          <a class="btn primary" href="#/job/new">+ 新建作业</a>
        </div>
      </div>

      <div class="card">
        <div class="card-head">
          <div class="row grow">
            <input type="text" v-model="q.keyword" placeholder="搜索作业名称 / 备注" style="max-width:260px" @keyup.enter="search" />
            <button class="btn sm" @click="search">搜索</button>
          </div>
          <button class="btn sm ghost" @click="load">↻ 刷新</button>
        </div>

        <div v-if="loading" class="loading">加载中…</div>
        <c-empty v-else-if="!d.list.length" text="还没有定时作业，点击右上角新建" icon="⏱️" />
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead><tr><th>ID</th><th>作业</th><th>调度</th><th>步骤</th><th>状态</th><th>上次运行</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="j in d.list" :key="j.id">
                <td>#{{ j.id }}</td>
                <td>
                  <a :href="'#/job/' + j.id"><b>{{ j.name }}</b></a>
                  <div v-if="j.remark" class="hint">{{ j.remark }}</div>
                </td>
                <td>
                  {{ j.schedule_text || scheduleText(j) }}
                  <div class="hint" v-if="j.next_run">下次 {{ fmt(j.next_run) }}</div>
                  <div class="hint" v-if="!j.enabled">已停用</div>
                </td>
                <td>{{ (j.steps || []).length }}</td>
                <td>
                  <span v-if="j.running" class="tag blue">运行中</span>
                  <span v-else class="tag" :class="statusTag(j.last_status || '').cls">{{ statusTag(j.last_status || '').text }}</span>
                </td>
                <td>{{ fmt(j.last_run_at) }}<div class="hint">{{ j.last_message || '' }}</div></td>
                <td>
                  <div class="actions">
                    <button class="btn sm primary" :disabled="runningId === j.id || j.running" @click="runJob(j)">{{ runningId === j.id ? '启动中…' : '运行' }}</button>
                    <a class="btn sm" :href="'#/job/' + j.id">编辑</a>
                    <button class="btn sm" @click="cloneJob(j)">复制</button>
                    <a class="btn sm" :href="'#/jobruns/' + j.id">记录</a>
                    <button class="btn sm danger" @click="delJob(j)">删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="card-body" style="padding-top:0">
          <c-pager :total="d.total" :page="d.page" :size="d.page_size" @change="changePage" />
        </div>
      </div>
    </div>`,
  });

  /* ---------------- 作业编辑 ---------------- */
  C.component('page-job', {
    setup() {
      const route = C.route;
      const job = C.reactive(normalizeJob({}));
      const dsList = C.reactive([]);
      const loading = C.ref(true);
      const saving = C.ref(false);
      const dirty = C.ref(false);
      const cronPreset = C.ref('');
      let ready = false;

      function syncCronPreset() {
        const expr = String(job.cron || '').trim();
        const hit = (C.CRON_PRESETS || []).filter(function (p) { return p.expr === expr; })[0];
        cronPreset.value = hit ? hit.expr : (expr ? 'custom' : '');
      }
      function applyCronPreset(e) {
        const v = e && e.target ? e.target.value : '';
        cronPreset.value = v;
        if (v && v !== 'custom') job.cron = v;
      }
      const cronPresetLabel = C.computed(function () {
        const expr = String(job.cron || '').trim();
        const hit = (C.CRON_PRESETS || []).filter(function (p) { return p.expr === expr; })[0];
        return hit ? hit.label : '';
      });

      function hydrate(j) {
        Object.assign(job, normalizeJob(j));
        syncCronPreset();
        ready = false;
        Vue.nextTick(function () { ready = true; dirty.value = false; });
      }

      async function load() {
        loading.value = true;
        try {
          await C.ensureDatasources();
          dsList.splice(0, dsList.length, ...(C.sys.datasources || []));
          if (route.id === 'new') hydrate({});
          else hydrate(await C.api('/jobs/' + route.id));
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);
      Vue.watch(job, function () { if (ready) dirty.value = true; }, { deep: true });
      Vue.watch(function () { return job.cron; }, function () { syncCronPreset(); });

      function addStep(type) {
        const s = blankStep(type);
        job.steps.push(s);
      }
      function delStep(i) { job.steps.splice(i, 1); }
      function move(arr, i, delta) {
        const j = i + delta;
        if (j < 0 || j >= arr.length) return;
        const tmp = arr[i];
        arr.splice(i, 1);
        arr.splice(j, 0, tmp);
      }

      function body() {
        return {
          name: String(job.name || '').trim(),
          remark: job.remark || '',
          enabled: !!job.enabled,
          notify: !!job.notify,
          notify_on: job.notify_on,
          schedule_type: job.schedule_type,
          cron: job.cron || '',
          interval_sec: Number(job.interval_sec || 0),
          timeout_sec: Number(job.timeout_sec || 60),
          steps: job.steps.map(stepPayload),
        };
      }

      async function save() {
        if (!String(job.name || '').trim()) { C.toast('请填写作业名称', 'err'); return null; }
        if (!job.steps.length) { C.toast('请至少添加一个步骤', 'err'); return null; }
        saving.value = true;
        try {
          const b = body();
          const saved = job.id
            ? await C.api('/jobs/' + job.id, { method: 'PUT', body: b })
            : await C.api('/jobs', { method: 'POST', body: b });
          hydrate(saved);
          C.toast('保存成功', 'ok');
          if (!job.id) C.go('/job/' + saved.id);
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
          const run = await C.api('/jobs/' + saved.id + '/run', { method: 'POST', body: {} });
          C.toast('已启动执行 #' + run.id, 'ok');
          setTimeout(function () { C.go('/jobrun/' + run.id); }, 400);
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        }
      }

      function back() { history.back(); }

      function onKeydown(e) {
        if ((e.ctrlKey || e.metaKey) && String(e.key).toLowerCase() === 's') {
          e.preventDefault();
          save();
        }
      }
      Vue.onMounted(function () { document.addEventListener('keydown', onKeydown); });
      Vue.onUnmounted(function () { document.removeEventListener('keydown', onKeydown); });

      return {
        job: job, dsList: dsList, loading: loading, saving: saving, dirty: dirty,
        addStep: addStep, delStep: delStep, move: move, save: save, saveAndRun: saveAndRun, back: back,
        cronPreset: cronPreset, applyCronPreset: applyCronPreset, cronPresetLabel: cronPresetLabel,
        CRON_PRESETS: C.CRON_PRESETS,
        kindTag: kindTag, fmt: C.fmt, fmtDur: C.fmtDur,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div>
          <h2>
            {{ job.id ? (job.name || '作业编辑') : '新建定时作业' }}
            <span v-if="job.id" class="hint">#{{ job.id }}</span>
            <span v-else class="tag amber">未保存</span>
            <span class="tag amber" v-show="dirty">● 有未保存修改</span>
          </h2>
          <p>按顺序执行步骤；任一步骤失败默认终止（可勾选「失败继续」）</p>
        </div>
        <div class="row">
          <button class="btn" @click="back">返回</button>
          <button v-if="job.id" class="btn" :disabled="saving" @click="saveAndRun">保存并运行</button>
          <button class="btn primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存作业' }}</button>
        </div>
      </div>

      <div v-if="loading" class="loading">加载中…</div>
      <template v-else>
        <div class="card">
          <div class="card-head"><h3>基本设置</h3></div>
          <div class="card-body">
            <div class="form-grid">
              <div><label class="field">作业名称 *</label><input v-model="job.name" placeholder="如：每日清理临时表" /></div>
              <div><label class="field">备注</label><input v-model="job.remark" placeholder="可选说明" /></div>
              <div class="full row" style="flex-wrap:wrap;gap:16px">
                <label class="check"><input type="checkbox" v-model="job.enabled" /> 启用作业</label>
                <label class="check"><input type="checkbox" v-model="job.notify" /> 执行后通知</label>
                <select v-model="job.notify_on" style="max-width:180px">
                  <option value="failure">仅失败时通知</option>
                  <option value="success">仅成功时通知</option>
                  <option value="always">每次都通知</option>
                </select>
              </div>
              <div><label class="field">调度方式</label>
                <select v-model="job.schedule_type">
                  <option value="manual">手动触发</option>
                  <option value="cron">Cron 表达式</option>
                  <option value="interval">固定间隔</option>
                </select>
              </div>
              <div class="full">
                <label class="field">Cron 表达式（可直接选常用计划，也可自定义输入）</label>
                <div class="row" style="gap:8px;flex-wrap:wrap">
                  <select :value="cronPreset" @change="applyCronPreset" style="max-width:250px" data-act="cron-preset">
                    <option value="">选择常用计划…</option>
                    <option v-for="p in CRON_PRESETS" :key="p.expr" :value="p.expr">{{ p.label }}</option>
                    <option value="custom">自定义</option>
                  </select>
                  <input v-model="job.cron" placeholder="0 0 8 * * *" style="flex:1;min-width:220px" data-act="cron-expr" />
                </div>
                <div class="hint" style="margin-top:4px">
                  <template v-if="cronPresetLabel">当前计划：{{ cronPresetLabel }} → <span class="code">{{ job.cron }}</span></template>
                  <template v-else-if="job.cron">自定义表达式：<span class="code">{{ job.cron }}</span></template>
                  <template v-else>6 段格式（秒 分 时 日 月 周），如 <span class="code">0 0 8 * * *</span> 表示每天 8 点；5 段格式（分 时 日 月 周）同样支持。</template>
                </div>
              </div>
              <div><label class="field">间隔秒数（间隔模式）</label><input type="number" v-model.number="job.interval_sec" /></div>
              <div><label class="field">整体超时（秒）</label><input type="number" v-model.number="job.timeout_sec" /></div>
            </div>
            <p class="hint" style="margin-top:12px">
              Cron 支持 5 段或 6 段（秒 分 时 日 月 周），例如每天 8 点：0 0 8 * * *；每 30 分钟：0 */30 * * * *。
              通知会写入「消息中心」，并在系统设置开启企业微信机器人后推送到企业微信群。
              步骤参数支持内置变量：now、today、timestamp、uuid；HTTP 步骤可「提取变量」，
              后续步骤用双花括号引用（例如先登录提取 token，下一步请求头写 Authorization: Bearer 加双花括号 token，最后 SQL 写入数据库）。
              同一次执行内所有 HTTP 步骤共享 Cookie 会话。
            </p>
          </div>
        </div>

        <div class="card" style="margin-top:16px">
          <div class="card-head">
            <h3>执行步骤 <span class="hint">按从上到下顺序执行</span></h3>
            <div class="row">
              <button class="btn sm" data-act="add-jobstep" data-kind="http" @click="addStep('http')">+ HTTP 请求</button>
              <button class="btn sm" data-act="add-jobstep" data-kind="shell" @click="addStep('shell')">+ Shell 命令</button>
              <button class="btn sm" data-act="add-jobstep" data-kind="sql" @click="addStep('sql')">+ 执行 SQL</button>
            </div>
          </div>
          <div class="card-body">
            <c-empty v-if="!job.steps.length" text="还没有步骤，点击上方按钮添加 HTTP / Shell / SQL 步骤" icon="🧱" />
            <div v-else>
              <div class="sub-card" v-for="(s, i) in job.steps" :key="s._k" :class="{ collapsed: s._collapsed }" data-jobstep>
                <div class="sub-head">
                  <span class="sub-title">
                    <span class="step-no">{{ i + 1 }}</span>{{ s.name || kindTag(s.type).text + ' 步骤' }}
                    <span class="tag" :class="kindTag(s.type).cls">{{ kindTag(s.type).text }}</span>
                    <span v-if="s.enabled === false" class="tag gray">已禁用</span>
                  </span>
                  <div class="actions">
                    <button class="btn sm ghost" @click="s._collapsed = !s._collapsed">⌃</button>
                    <button class="btn sm" :disabled="i === 0" @click="move(job.steps, i, -1)">↑</button>
                    <button class="btn sm" :disabled="i === job.steps.length - 1" @click="move(job.steps, i, 1)">↓</button>
                    <button class="btn sm danger" @click="delStep(i)">删除</button>
                  </div>
                </div>
                <div class="form-grid">
                  <div><label class="field">步骤名称</label><input v-model="s.name" placeholder="留空自动命名" /></div>
                  <div><label class="field">步骤类型</label>
                    <select v-model="s.type">
                      <option value="http">HTTP 请求</option>
                      <option value="shell">Shell 命令</option>
                      <option value="sql">数据库 SQL</option>
                    </select>
                  </div>

                  <template v-if="s.type === 'http'">
                    <div><label class="field">请求方法</label>
                      <select v-model="s.method">
                        <option value="GET">GET</option>
                        <option value="POST">POST</option>
                        <option value="PUT">PUT</option>
                        <option value="DELETE">DELETE</option>
                        <option value="PATCH">PATCH</option>
                      </select>
                    </div>
                    <div><label class="field">Content-Type</label><input v-model="s.content_type" placeholder="application/json" /></div>
                    <div class="full"><label class="field">请求 URL *</label><input v-model="s.url" placeholder="https://api.example.com/hook" /></div>
                    <div class="full"><label class="field">请求头（每行 Key: Value）</label>
                      <textarea v-model="s._headers" rows="3" spellcheck="false" placeholder="Authorization: Bearer xxx&#10;X-Token: abc"></textarea>
                    </div>
                    <div class="full"><label class="field">请求体</label>
                      <textarea v-model="s.body" rows="4" spellcheck="false" placeholder="POST / PUT 时填写，可为 JSON 或表单文本"></textarea>
                    </div>
                    <div class="full"><label class="field">提取变量（每行 变量名=取值表达式，供后续步骤用双花括号变量名引用）</label>
                      <textarea v-model="s._extract" rows="3" spellcheck="false" placeholder="token=data.access_token&#10;csrf=data.csrf&#10;第一页ID=data.list.0.id"></textarea>
                      <div class="hint" style="margin-top:4px">
                        取值表达式：JSON 路径 data.token / data.list.0.id，响应头 header:X-Token，正文正则 regex:token=([0-9a-f]+)，状态码 status
                      </div>
                    </div>
                  </template>

                  <template v-else-if="s.type === 'shell'">
                    <div class="full"><label class="field">命令 *</label>
                      <textarea v-model="s.command" rows="3" spellcheck="false" placeholder="Windows: dir&#10;Linux: ls -al"></textarea>
                    </div>
                    <div><label class="field">工作目录</label><input v-model="s.work_dir" placeholder="留空使用平台目录" /></div>
                    <div class="full"><label class="field">环境变量（每行 KEY=VALUE）</label>
                      <textarea v-model="s._env" rows="2" spellcheck="false" placeholder="FOO=bar"></textarea>
                    </div>
                  </template>

                  <template v-else>
                    <div><label class="field">数据库 *</label>
                      <select v-model.number="s.data_source_id">
                        <option :value="0">请选择数据源</option>
                        <option v-for="ds in dsList" :key="ds.id" :value="ds.id">{{ ds.name }} ({{ ds.type }})</option>
                      </select>
                    </div>
                    <div class="full"><label class="field">SQL 语句 *</label>
                      <textarea v-model="s.sql" rows="4" spellcheck="false" placeholder="DELETE FROM tmp_log WHERE created_at &lt; current_date"></textarea>
                    </div>
                  </template>

                  <div class="full row" style="flex-wrap:wrap;gap:16px">
                    <label class="check"><input type="checkbox" v-model="s.enabled" /> 启用该步骤</label>
                    <label class="check"><input type="checkbox" v-model="s.continue_on_error" /> 失败继续执行下一步</label>
                    <label class="check">单独超时（秒）
                      <input type="number" v-model.number="s.timeout_sec" style="width:90px" placeholder="继承作业" />
                    </label>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>`,
  });

  /* ---------------- 执行记录列表 ---------------- */
  C.component('page-jobruns', {
    setup() {
      const route = C.route;
      const q = C.reactive({ page: 1, status: '' });
      const d = C.reactive({ list: [], total: 0, page: 1, page_size: 20 });
      const loading = C.ref(true);

      async function load() {
        loading.value = true;
        try {
          const params = new URLSearchParams({ page: q.page, page_size: 20 });
          if (route.id) params.set('job_id', route.id);
          if (q.status) params.set('status', q.status);
          const data = await C.api('/job-runs?' + params.toString());
          d.list = data.list || [];
          d.total = data.total || 0;
          d.page = data.page || 1;
          d.page_size = data.page_size || 20;
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);

      function filter() { q.page = 1; load(); }
      function changePage(p) { q.page = p; load(); }
      async function delRun(r) {
        if (!confirm('确定删除该执行记录吗？')) return;
        try {
          await C.api('/job-runs/' + r.id, { method: 'DELETE' });
          C.toast('已删除', 'ok');
          load();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      return {
        q: q, d: d, loading: loading, filter: filter, changePage: changePage, delRun: delRun,
        fmt: C.fmt, fmtDur: C.fmtDur, statusTag: C.statusTag,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div>
          <h2>作业执行记录</h2>
          <p>查看定时作业每次执行的步骤日志、请求响应与命令输出</p>
        </div>
        <a class="btn" href="#/jobs">返回作业列表</a>
      </div>
      <div class="card">
        <div class="card-head">
          <div class="row grow">
            <select v-model="q.status" style="max-width:160px" @change="filter">
              <option value="">全部状态</option>
              <option value="running">运行中</option>
              <option value="success">成功</option>
              <option value="failed">失败</option>
              <option value="partial">部分成功</option>
            </select>
            <button class="btn sm" @click="filter">筛选</button>
          </div>
          <button class="btn sm ghost" @click="load">↻ 刷新</button>
        </div>
        <div v-if="loading" class="loading">加载中…</div>
        <c-empty v-else-if="!d.list.length" text="还没有执行记录" />
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead><tr><th>执行</th><th>作业</th><th>触发</th><th>状态</th><th>开始时间</th><th>耗时</th><th>步骤（总/成功/失败）</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="r in d.list" :key="r.id">
                <td>#{{ r.id }}</td>
                <td><a :href="'#/jobrun/' + r.id">{{ r.job_name }}</a></td>
                <td><span class="tag" :class="r.trigger === 'cron' ? 'amber' : 'gray'">{{ r.trigger === 'cron' ? '定时' : '手动' }}</span></td>
                <td><span class="tag" :class="statusTag(r.status).cls">{{ statusTag(r.status).text }}</span></td>
                <td>{{ fmt(r.started_at) }}</td>
                <td>{{ fmtDur(r.duration_ms) }}</td>
                <td>{{ r.steps_total }} / {{ r.steps_ok }} / {{ r.steps_fail }}</td>
                <td>
                  <div class="actions">
                    <a class="btn sm" :href="'#/jobrun/' + r.id">详情</a>
                    <button class="btn sm danger" :disabled="r.status === 'running'" @click="delRun(r)">删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="card-body" style="padding-top:0">
          <c-pager :total="d.total" :page="d.page" :size="d.page_size" @change="changePage" />
        </div>
      </div>
    </div>`,
  });

  /* ---------------- 步骤详情弹窗 ---------------- */
  C.component('job-step-modal', {
    props: { step: { type: Object, required: true } },
    setup() {
      return { jsonPretty: C.jsonPretty, fmtDur: C.fmtDur, fmt: C.fmt };
    },
    template: `
    <div>
      <div class="kv-list">
        <span class="k">步骤</span><span>#{{ step.seq }} {{ step.name }}</span>
        <span class="k">类型</span><span>{{ step.kind }}</span>
        <span class="k">状态</span><span><span class="tag" :class="step.status === 'failed' ? 'red' : 'green'">{{ step.status === 'failed' ? '失败' : '成功' }}</span></span>
        <span class="k">开始时间</span><span>{{ fmt(step.created_at) }}</span>
        <span class="k">耗时</span><span>{{ fmtDur(step.duration_ms) }}</span>
        <template v-if="step.error"><span class="k">错误</span><span style="color:#c43239">{{ step.error }}</span></template>
      </div>
      <template v-if="step.kind === 'http'">
        <h4>请求</h4><div class="pre">{{ step.method }} {{ step.url }}</div>
        <h4>请求头</h4><div class="pre">{{ step.req_headers || '-' }}</div>
        <h4>请求体</h4><div class="pre">{{ jsonPretty(step.req_body) || '-' }}</div>
        <h4>响应状态 / 响应头</h4><div class="pre">{{ step.resp_status }} · {{ step.resp_headers || '-' }}</div>
        <h4>响应体</h4><div class="pre">{{ jsonPretty(step.resp_body) || '-' }}</div>
        <template v-if="step.output"><h4>提取变量</h4><div class="pre">{{ step.output }}</div></template>
      </template>
      <template v-else-if="step.kind === 'shell'">
        <h4>命令</h4><div class="pre">{{ step.url }}</div>
        <h4>输出</h4><div class="pre">{{ step.output || '-' }}</div>
      </template>
      <template v-else>
        <h4>数据库</h4><div class="pre">{{ step.url }}</div>
        <h4>SQL</h4><div class="pre">{{ step.req_body || '-' }}</div>
        <h4>执行结果</h4><div class="pre">{{ step.output || '-' }}</div>
        <h4>返回数据</h4><div class="pre">{{ jsonPretty(step.resp_body) || '-' }}</div>
      </template>
    </div>`,
  });

  /* ---------------- 执行详情 ---------------- */
  C.component('page-jobrun', {
    setup() {
      const route = C.route;
      const run = C.reactive({});
      const steps = C.reactive([]);
      const loading = C.ref(true);
      const newest = C.ref(false);

      async function load() {
        if (!route.id) { C.go('/jobruns'); return; }
        loading.value = true;
        try {
          const d = await C.api('/job-runs/' + route.id);
          Object.assign(run, d.run || {});
          steps.splice(0, steps.length, ...(d.steps || []));
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);
      let timer = 0;
      Vue.onMounted(function () {
        timer = setInterval(function () { if (run.status === 'running') load(); }, 2000);
      });
      Vue.onUnmounted(function () { clearInterval(timer); });

      const rows = C.computed(function () {
        const arr = steps.slice();
        arr.sort(function (a, b) { return Number(a.seq || 0) - Number(b.seq || 0); });
        if (newest.value) arr.reverse();
        return arr;
      });

      function showStep(s) {
        C.openModal('步骤：' + s.name, 'job-step-modal', { step: s });
      }
      function back() { history.back(); }

      return {
        run: run, steps: steps, rows: rows, loading: loading, newest: newest,
        showStep: showStep, back: back,
        fmt: C.fmt, fmtDur: C.fmtDur, statusTag: C.statusTag, shortUrl: C.shortUrl, kindTag: kindTag,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div>
          <h2>{{ run.job_name }} <span class="hint">执行 #{{ run.id }}</span></h2>
          <p>
            <span class="tag" :class="statusTag(run.status).cls">{{ statusTag(run.status).text }}</span>
            · 触发 {{ run.trigger === 'cron' ? '定时' : '手动' }}
            · 开始 {{ fmt(run.started_at) }} · 耗时 {{ fmtDur(run.duration_ms) }}
          </p>
        </div>
        <div class="row">
          <button class="btn" @click="back">返回</button>
          <a class="btn" :href="'#/jobruns/' + run.job_id">该作业记录</a>
          <a class="btn" :href="'#/job/' + run.job_id">编辑作业</a>
        </div>
      </div>

      <div v-if="run.error" class="card"><div class="card-body" style="color:#c43239"><b>错误：</b><span style="white-space:pre-wrap">{{ run.error }}</span></div></div>

      <div class="grid grid-4" style="margin-bottom:16px">
        <div class="card stat"><span class="k">步骤总数</span><span class="v">{{ run.steps_total }}</span></div>
        <div class="card stat"><span class="k">成功</span><span class="v">{{ run.steps_ok }}</span></div>
        <div class="card stat"><span class="k">失败</span><span class="v">{{ run.steps_fail }}</span></div>
        <div class="card stat"><span class="k">耗时</span><span class="v" style="font-size:20px">{{ fmtDur(run.duration_ms) }}</span></div>
      </div>

      <div class="card">
        <div class="card-head">
          <h3>执行步骤</h3>
          <label class="hint" style="cursor:pointer;white-space:nowrap">
            <input type="checkbox" v-model="newest" style="vertical-align:-2px" /> 最近的在前面
          </label>
        </div>
        <div v-if="loading" class="loading">加载中…</div>
        <c-empty v-else-if="!rows.length" text="没有步骤日志" />
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead><tr><th>#</th><th>步骤</th><th>类型</th><th>请求 / 命令</th><th>状态码</th><th>执行开始时间</th><th>耗时</th><th>结果</th><th></th></tr></thead>
            <tbody>
              <tr v-for="s in rows" :key="s.id">
                <td>{{ s.seq }}</td>
                <td>{{ s.name }}</td>
                <td><span class="tag" :class="kindTag(s.kind).cls">{{ kindTag(s.kind).text }}</span></td>
                <td><span v-if="s.method" class="tag gray">{{ s.method }}</span> <span class="hint">{{ shortUrl(s.url, 70) }}</span></td>
                <td>{{ s.resp_status || '-' }}</td>
                <td>{{ fmt(s.created_at) }}</td>
                <td>{{ fmtDur(s.duration_ms) }}</td>
                <td>
                  <span v-if="s.error" class="tag red" :title="s.error">{{ String(s.error).slice(0, 40) }}</span>
                  <span v-else-if="s.status === 'failed'" class="tag red">失败</span>
                  <span v-else-if="s.status === 'running'" class="tag blue">执行中</span>
                  <span v-else class="tag green">OK</span>
                </td>
                <td><button class="btn sm" @click="showStep(s)">详情</button></td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>`,
  });
})();
