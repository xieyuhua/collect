/* ============================================================
   数据源 / 运行日志 / 消息中心 / 系统设置
   ============================================================ */
(function () {
  'use strict';
  const C = Collect;

  /* ==================== 数据源表单弹窗 ==================== */
  C.component('ds-form-modal', {
    props: { ds: { type: Object, default: function () { return {}; } }, onSaved: { type: Function, default: null } },
    setup(props) {
      const d = props.ds || {};
      const f = C.reactive({
        id: d.id || 0, name: d.name || '', type: d.type || 'mysql',
        host: d.host || '', port: d.port || 0, user: d.user || '', password: d.password || '',
        database: d.database || '', params: d.params || '', dsn: d.dsn || '',
      });
      const busy = C.ref(false);
      const TYPES = ['mysql', 'oracle', 'postgres', 'sqlite'];

      function body() {
        return {
          id: Number(f.id || 0), name: f.name, type: f.type, host: f.host,
          port: Number(f.port || 0), user: f.user, password: f.password,
          database: f.database, params: f.params, dsn: f.dsn,
        };
      }

      async function testConn() {
        busy.value = true;
        try {
          await C.api('/datasources/test', { method: 'POST', body: body() });
          C.toast('连接成功', 'ok');
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally { busy.value = false; }
      }

      async function save() {
        busy.value = true;
        try {
          const b = body();
          await C.api(b.id ? '/datasources/' + b.id : '/datasources', { method: b.id ? 'PUT' : 'POST', body: b });
          C.closeModal();
          C.toast('已保存', 'ok');
          C.ensureDatasources(true);
          if (props.onSaved) props.onSaved();
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally { busy.value = false; }
      }

      return { f: f, TYPES: TYPES, busy: busy, testConn: testConn, save: save };
    },
    template: `
    <div>
      <div class="form-grid">
        <div><label class="field">名称</label><input id="dsName" v-model="f.name" placeholder="如：业务库 MySQL" /></div>
        <div><label class="field">类型</label>
          <select id="dsType" v-model="f.type"><option v-for="t in TYPES" :key="t" :value="t">{{ t }}</option></select>
        </div>
        <div><label class="field">主机（SQLite 可留空）</label><input id="dsHost" v-model="f.host" placeholder="127.0.0.1" /></div>
        <div><label class="field">端口</label><input id="dsPort" type="number" v-model.number="f.port" placeholder="3306 / 1521 / 5432" /></div>
        <div><label class="field">用户名</label><input id="dsUser" v-model="f.user" /></div>
        <div><label class="field">密码</label>
          <input id="dsPass" type="password" v-model="f.password" :placeholder="f.id ? '留空表示不修改' : ''" />
        </div>
        <div><label class="field">数据库 / SQLite 文件路径</label><input id="dsDb" v-model="f.database" placeholder="mydb 或 ./data/local.sqlite" /></div>
        <div><label class="field">附加参数（可选）</label><input id="dsParams" v-model="f.params" placeholder="charset=utf8mb4&parseTime=True" /></div>
        <div class="full"><label class="field">DSN（可选，填写后优先使用）</label>
          <input id="dsDsn" v-model="f.dsn" placeholder="user:pass@tcp(host:port)/db?charset=utf8mb4" />
        </div>
      </div>
      <div class="row" style="margin-top:16px;justify-content:flex-end">
        <button class="btn" data-act="ds-form-test" :disabled="busy" @click="testConn">测试连接</button>
        <button class="btn primary" data-act="ds-save" :disabled="busy" @click="save">保存</button>
      </div>
    </div>`,
  });

  /* ==================== 数据源执行 SQL 弹窗 ==================== */
  C.component('ds-sql-modal', {
    props: { ds: { type: Object, default: function () { return {}; } } },
    setup(props) {
      const sql = C.ref('');
      const busy = C.ref(false);
      const result = C.ref(null);
      const error = C.ref('');

      async function exec() {
        if (!String(sql.value).trim()) { C.toast('请填写 SQL', 'err'); return; }
        busy.value = true;
        error.value = '';
        result.value = null;
        try {
          const r = await C.api('/datasources/' + props.ds.id + '/exec', {
            method: 'POST', body: { sql: sql.value, limit: 200 },
          });
          result.value = r;
          C.toast('执行完成，耗时 ' + r.elapsed_ms + ' ms', 'ok');
        } catch (e) {
          error.value = e.message || String(e);
        } finally {
          busy.value = false;
        }
      }

      return { sql: sql, busy: busy, result: result, error: error, exec: exec, name: props.ds.name || ('#' + props.ds.id), id: props.ds.id };
    },
    template: `
    <div>
      <div class="form-grid">
        <div class="full">
          <label class="field">SQL（在「{{ name }}」上直接执行，SELECT / INSERT / UPDATE / DELETE / DDL 均可）</label>
          <textarea id="dsSqlText" v-model="sql" rows="8" spellcheck="false" placeholder="SELECT * FROM store LIMIT 10"></textarea>
        </div>
      </div>
      <p class="hint" style="margin-top:8px">这里执行的是原始 SQL（不做变量替换），用于验证语句、查看表结构与数据。查询最多返回 200 行。</p>
      <div class="row" style="margin-top:12px;justify-content:flex-end">
        <button class="btn primary" data-act="ds-sql-run" :disabled="busy" @click="exec">{{ busy ? '执行中…' : '执行' }}</button>
      </div>
      <div id="dsSqlResult" style="margin-top:12px">
        <div v-if="error" class="empty"><div class="big">⚠️</div>{{ error }}</div>
        <div v-else-if="result">
          <p class="hint">
            {{ result.statement === 'query' ? '返回 ' + result.rows.length + ' 行' : '影响 ' + result.affected + ' 行' }}
            · 耗时 {{ result.elapsed_ms }} ms
            <span v-if="result.truncated">· 结果已截断（最多 200 行）</span>
          </p>
          <div v-if="result.columns && result.columns.length" class="tbl-wrap">
            <table class="tbl">
              <thead><tr><th v-for="c in result.columns" :key="c">{{ c }}</th></tr></thead>
              <tbody>
                <tr v-for="(row, i) in result.rows" :key="i">
                  <td v-for="(v, j) in row" :key="j">{{ v === null || v === undefined ? '' : String(v) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>`,
  });

  /* ==================== 数据源页面 ==================== */
  C.component('page-datasources', {
    setup() {
      const list = C.reactive([]);
      const loading = C.ref(true);

      async function load() {
        loading.value = true;
        try {
          const data = await C.ensureDatasources(true);
          list.splice(0, list.length, ...(data || []));
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);

      function newDs() { C.openModal('新增数据源', 'ds-form-modal', { ds: {}, onSaved: load }); }
      function editDs(d) { C.openModal('编辑数据源', 'ds-form-modal', { ds: d, onSaved: load }); }
      function sqlDs(d) { C.openModal('执行 SQL · ' + d.name, 'ds-sql-modal', { ds: d }); }

      async function testDs(d) {
        try {
          await C.api('/datasources/' + d.id + '/test', { method: 'POST', body: {} });
          C.toast('连接成功', 'ok');
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        }
      }

      async function delDs(d) {
        if (!confirm('确定删除该数据源吗？')) return;
        try {
          await C.api('/datasources/' + d.id, { method: 'DELETE' });
          C.toast('已删除', 'ok');
          await load();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      function connText(d) {
        if (d.dsn) return String(d.dsn).replace(/:[^:@/]+@/, ':***@');
        return (d.host || '') + (d.port ? ':' + d.port : '') + '/' + (d.database || '');
      }

      const count = C.computed(function () {
        return {
          all: list.length,
          mysql: list.filter(function (x) { return x.type === 'mysql'; }).length,
          pg: list.filter(function (x) { return x.type === 'postgres'; }).length,
          other: list.filter(function (x) { return x.type === 'sqlite' || x.type === 'oracle'; }).length,
        };
      });

      return {
        list: list, loading: loading, count: count,
        load: load, newDs: newDs, editDs: editDs, sqlDs: sqlDs, testDs: testDs, delDs: delDs, connText: connText,
        dsTypeTag: C.dsTypeTag,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div>
          <h2>数据源</h2>
          <p>支持 MySQL / Oracle / PostgreSQL / SQLite，任务可按字段映射写入目标表，也可直接执行 SQL 调试</p>
        </div>
        <div class="row">
          <button class="btn" @click="load">↻ 刷新</button>
          <button class="btn primary" data-act="ds-new" @click="newDs">+ 新增数据源</button>
        </div>
      </div>

      <div class="grid grid-4" style="margin-bottom:16px">
        <div class="card stat"><span class="k">🗄️ 数据源总数</span><span class="v">{{ count.all }}</span><span class="s">可在任务落库配置中选择</span></div>
        <div class="card stat"><span class="k">🐬 MySQL</span><span class="v">{{ count.mysql }}</span><span class="s">默认端口 3306</span></div>
        <div class="card stat"><span class="k">🐘 PostgreSQL</span><span class="v">{{ count.pg }}</span><span class="s">默认端口 5432</span></div>
        <div class="card stat"><span class="k">🪶 SQLite / Oracle</span><span class="v">{{ count.other }}</span><span class="s">SQLite 填文件路径即可</span></div>
      </div>

      <div class="card">
        <div v-if="loading" class="loading">加载中…</div>
        <c-empty v-else-if="!list.length" text="还没有数据源，点击右上角「+ 新增数据源」添加一个数据库连接" />
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead><tr><th>ID</th><th>名称</th><th>类型</th><th>连接</th><th>用户名</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="d in list" :key="d.id">
                <td>#{{ d.id }}</td>
                <td><b>{{ d.name }}</b></td>
                <td><span class="tag" :class="dsTypeTag(d.type).cls">{{ dsTypeTag(d.type).text }}</span></td>
                <td><span class="code">{{ connText(d) }}</span></td>
                <td>{{ d.user || '-' }}</td>
                <td>
                  <div class="actions">
                    <button class="btn sm" data-act="ds-test" @click="testDs(d)">测试连接</button>
                    <button class="btn sm" data-act="ds-sql" @click="sqlDs(d)">执行 SQL</button>
                    <button class="btn sm" data-act="ds-edit" @click="editDs(d)">编辑</button>
                    <button class="btn sm danger" data-act="ds-del" @click="delDs(d)">删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>`,
  });

  /* ==================== 运行日志 ==================== */
  C.component('page-logs', {
    setup() {
      const lines = C.reactive([]);
      const level = C.ref('');
      const paused = C.ref(false);
      const autoScroll = C.ref(true);
      const box = C.ref(null);
      const loading = C.ref(true);
      let lastId = 0;
      let timer = 0;

      function scrollToEnd() {
        const el = box.value;
        if (el && autoScroll.value) el.scrollTop = el.scrollHeight;
      }

      async function load(reset) {
        try {
          const d = await C.api('/logs?after=' + (reset ? 0 : lastId) + '&limit=500&level=' + encodeURIComponent(level.value || ''));
          const list = d.list || [];
          if (reset) { lines.splice(0, lines.length); lastId = 0; }
          if (list.length) {
            list.forEach(function (e) { lines.push(e); });
            lastId = list[list.length - 1].id;
            if (lines.length > 2000) lines.splice(0, lines.length - 2000);
            Vue.nextTick(scrollToEnd);
          }
        } catch (e) { /* 轮询错误忽略 */ }
        finally { loading.value = false; }
      }

      function restart() { load(true); }

      Vue.onMounted(function () {
        load(true);
        timer = setInterval(function () { if (!paused.value) load(false); }, 2500);
      });
      Vue.onUnmounted(function () { clearInterval(timer); });

      function clearScreen() { lines.splice(0, lines.length); lastId = 0; }
      function togglePause() { paused.value = !paused.value; }

      return {
        lines: lines, level: level, paused: paused, autoScroll: autoScroll, box: box, loading: loading,
        restart: restart, clearScreen: clearScreen, togglePause: togglePause,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div><h2>运行日志</h2><p>采集执行、浏览器、调度与接口调用日志，实时刷新</p></div>
        <div class="row">
          <select id="logLevel" v-model="level" style="width:130px" @change="restart">
            <option value="">全部级别</option>
            <option>INFO</option><option>WARN</option><option>ERROR</option><option>DEBUG</option>
          </select>
          <button class="btn" data-act="log-pause" :class="{ primary: paused }" @click="togglePause">{{ paused ? '继续' : '暂停' }}</button>
          <button class="btn" data-act="log-clear" @click="clearScreen">清屏</button>
        </div>
      </div>
      <div class="card" style="margin-bottom:12px">
        <div class="card-body row" style="padding:12px 16px">
          <label class="check"><input type="checkbox" id="logAutoScroll" v-model="autoScroll" /> 自动滚动到底部</label>
          <span class="hint grow" id="logStat">已加载 {{ lines.length }} 行{{ level ? ' · 级别 ' + level : '' }}{{ paused ? ' · 已暂停' : '' }}</span>
          <span class="hint">ERROR / WARN 会在平台写入通知（按设置）</span>
        </div>
      </div>
      <div class="log-box" id="logBox" ref="box">
        <div v-if="!lines.length" class="log-line"><span class="m">{{ loading ? '加载中…' : '暂无日志' }}</span></div>
        <div v-for="e in lines" :key="e.id" class="log-line" :class="e.level">
          <span class="t">{{ e.time }}</span><span class="s">[{{ e.scope }}]</span><span class="l">{{ e.level }}</span><span class="m">{{ e.msg }}</span>
        </div>
      </div>
    </div>`,
  });

  /* ==================== 消息中心 ==================== */
  C.component('page-notifications', {
    setup() {
      const d = C.reactive({ list: [], total: 0, page: 1, page_size: 20, unread: 0 });
      const page = C.ref(1);
      const unreadOnly = C.ref(false);
      const loading = C.ref(true);

      async function load() {
        loading.value = true;
        try {
          const params = '/notifications?page=' + page.value + '&page_size=20' + (unreadOnly.value ? '&read=false' : '');
          const data = await C.api(params);
          d.list = data.list || [];
          d.total = data.total || 0;
          d.page = data.page || 1;
          d.page_size = data.page_size || 20;
          d.unread = data.unread || 0;
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);

      function filter(unread) {
        unreadOnly.value = unread;
        page.value = 1;
        load();
      }
      function changePage(p) { page.value = p; load(); }

      async function refreshAll() { await load(); C.refreshBadge(); }

      async function markRead(n) {
        try {
          await C.api('/notifications/' + n.id + '/read', { method: 'POST', body: {} });
          await refreshAll();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }
      async function readAll() {
        try {
          await C.api('/notifications/read-all', { method: 'POST', body: {} });
          C.toast('已全部标记为已读', 'ok');
          await refreshAll();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }
      async function delOne(n) {
        try {
          await C.api('/notifications/' + n.id, { method: 'DELETE' });
          await refreshAll();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }
      async function clearAll() {
        if (!confirm('确定清空全部消息吗？')) return;
        try {
          await C.api('/notifications', { method: 'DELETE' });
          C.toast('已清空', 'ok');
          await refreshAll();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      function levelCls(l) { return l === 'error' ? 'red' : l === 'success' ? 'green' : 'blue'; }

      return {
        d: d, unreadOnly: unreadOnly, loading: loading,
        filter: filter, changePage: changePage, markRead: markRead, readAll: readAll, delOne: delOne, clearAll: clearAll,
        levelCls: levelCls, fmt: C.fmt,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div><h2>消息中心</h2><p>任务执行结果统一提醒，未读 {{ d.unread }} 条</p></div>
        <div class="row">
          <button class="btn" data-act="notif-read-all" @click="readAll">全部已读</button>
          <button class="btn danger" data-act="notif-clear" @click="clearAll">清空消息</button>
        </div>
      </div>
      <div class="card">
        <div class="card-head">
          <div class="chip-group">
            <button class="chip" :class="{ active: !unreadOnly }" data-act="notif-filter" data-read="all" @click="filter(false)">全部消息</button>
            <button class="chip" :class="{ active: unreadOnly }" data-act="notif-filter" data-read="unread" @click="filter(true)">只看未读</button>
          </div>
          <span class="hint">共 {{ d.total }} 条</span>
        </div>
        <div v-if="loading" class="loading">加载中…</div>
        <c-empty v-else-if="!d.list.length" text="暂无消息" />
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead><tr><th>状态</th><th>级别</th><th>标题</th><th>内容</th><th>渠道</th><th>时间</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="n in d.list" :key="n.id" :class="{ unread: !n.read }">
                <td><span class="tag" :class="n.read ? 'gray' : 'red'">{{ n.read ? '已读' : '未读' }}</span></td>
                <td><span class="tag" :class="levelCls(n.level)">{{ n.level }}</span></td>
                <td :style="n.read ? '' : 'font-weight:600'">{{ n.title }}</td>
                <td class="hint" style="max-width:520px;white-space:pre-wrap">{{ n.content }}</td>
                <td>{{ n.channel === 'wecom' ? '企业微信' : '平台' }}</td>
                <td>{{ fmt(n.created_at) }}</td>
                <td>
                  <div class="actions">
                    <button v-if="!n.read" class="btn sm" data-act="notif-read" @click="markRead(n)">标记已读</button>
                    <button class="btn sm danger" data-act="notif-del" @click="delOne(n)">删除</button>
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

  /* ==================== 系统设置 ==================== */
  C.component('page-settings', {
    setup() {
      const s = C.reactive({
        wecom_enabled: 'false', wecom_webhook: '', notify_on_failure: 'true',
        notify_on_success: 'false',
      });
      const b = C.reactive({
        bin_path: '', download_host: 'npm', custom_host: '', revision: 0, proxy: '', headless: false, no_sandbox: false,
        user_agent: '', accept_language: '', platform: '', timezone: '', width: 1440, height: 960, extra_args_text: '',
        session_mode: 'isolated', profile_dir: '',
      });
      // 运行参数（可自定义）：监听地址 / 数据目录 / 超时 / 代理 / 日志包体上限
      const g = C.reactive({ addr: '', data_dir: '', timeout_sec: 60, proxy: '', max_body_kb: 256 });
      const gp = C.reactive({ restart_required: false, saved_at: '' });
      const bs = C.reactive({
        available: false, platform: '', revision: '', path: '', source: '', download_dir: '', message: '', downloading: false,
        session_mode: 'isolated', profile_dir: '', session_exists: false, session_files: 0, session_size_mb: 0,
        session_locked: false, session_persist: false,
      });
      const configFile = C.ref('');
      const loading = C.ref(true);
      const busy = C.ref(false);

      async function load() {
        loading.value = true;
        try {
          const d = await C.api('/settings');
          const st = d.settings || {};
          Object.keys(s).forEach(function (k) { if (st[k] !== undefined && st[k] !== null) s[k] = String(st[k]); });
          const cfg = d.config || {};
          const br = cfg.browser || {};
          Object.keys(b).forEach(function (k) {
            if (k === 'extra_args_text') return;
            if (br[k] !== undefined && br[k] !== null) b[k] = br[k];
          });
          b.extra_args_text = Array.isArray(br.extra_args) ? br.extra_args.join('\n') : '';
          // 老配置里可能没有 session_mode（空串），下拉框需要落到「每次全新会话」上
          b.session_mode = b.session_mode === 'shared' ? 'shared' : 'isolated';
          // 配置里 0 表示「用内核默认」，界面上显示成常用默认值更直观
          if (!(Number(b.width) > 0)) b.width = 1440;
          if (!(Number(b.height) > 0)) b.height = 960;
          g.addr = cfg.addr || '';
          g.data_dir = cfg.data_dir || '';
          g.timeout_sec = Number(cfg.timeout_sec) > 0 ? Number(cfg.timeout_sec) : 60;
          g.proxy = cfg.proxy || '';
          g.max_body_kb = Number(cfg.max_body_kb) > 0 ? Number(cfg.max_body_kb) : 256;
          Object.assign(bs, d.browser_status || {});
          configFile.value = d.config_file || '';
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);

      async function saveSettings() {
        busy.value = true;
        try {
          await C.api('/settings', { method: 'PUT', body: {
            wecom_enabled: s.wecom_enabled === 'true' ? 'true' : 'false',
            wecom_webhook: s.wecom_webhook,
            notify_on_failure: s.notify_on_failure,
            notify_on_success: s.notify_on_success,
          } });
          C.toast('设置已保存', 'ok');
        } catch (e) { C.toast(e.message || String(e), 'err'); }
        finally { busy.value = false; }
      }

      async function saveGeneral() {
        busy.value = true;
        try {
          const d = await C.api('/config/general', { method: 'PUT', body: {
            addr: g.addr, data_dir: g.data_dir,
            timeout_sec: Number(g.timeout_sec || 0),
            proxy: g.proxy,
            max_body_kb: Number(g.max_body_kb || 0),
          } });
          gp.restart_required = !!(d && d.restart_required);
          gp.saved_at = new Date().toLocaleTimeString();
          C.toast(gp.restart_required ? '已保存，监听地址 / 数据目录改动需重启平台后生效' : '运行参数已保存', gp.restart_required ? 'warn' : 'ok');
          await load();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
        finally { busy.value = false; }
      }

      function resetGeneral() {
        g.timeout_sec = 60;
        g.max_body_kb = 256;
        g.proxy = '';
        C.toast('已恢复常用默认值，记得点保存', 'ok');
      }

      async function testWecom() {
        try {
          await C.api('/settings/test-wecom', { method: 'POST', body: {} });
          C.toast('测试消息已发送', 'ok');
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      async function downloadBrowser() {
        try {
          await C.api('/browser/download', { method: 'POST', body: {} });
          C.toast('已开始下载，请稍后刷新', 'ok');
          setTimeout(load, 4000);
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      async function saveBrowser() {
        busy.value = true;
        try {
          await C.api('/browser/config', { method: 'PUT', body: {
            bin_path: b.bin_path, download_host: b.download_host, custom_host: b.custom_host,
            revision: Number(b.revision || 0), proxy: b.proxy,
            headless: !!b.headless, no_sandbox: !!b.no_sandbox,
            user_agent: b.user_agent, accept_language: b.accept_language,
            platform: b.platform, timezone: b.timezone,
            width: Number(b.width || 0), height: Number(b.height || 0),
            extra_args: String(b.extra_args_text || '').split('\n').map(function (x) { return x.trim(); }).filter(function (x) { return x; }),
            session_mode: b.session_mode === 'shared' ? 'shared' : 'isolated',
            profile_dir: b.profile_dir || '',
          } });
          C.toast('浏览器配置已保存', 'ok');
          await load();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
        finally { busy.value = false; }
      }

      async function clearSession() {
        if (!window.confirm('确定清空共享会话吗？\n\n会删除 ' + (bs.profile_dir || '共享会话目录') + ' 下的 Cookie / localStorage / IndexedDB，下次运行需要重新登录。')) return;
        busy.value = true;
        try {
          const d = await C.api('/browser/session/clear', { method: 'POST', body: {} });
          if (d) Object.assign(bs, d);
          C.toast('共享会话已清空', 'ok');
          await load();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
        finally { busy.value = false; }
      }

      // 常用 UA 预设，一键对齐 UA / 语言 / 平台，避免三者互相矛盾被风控识别
      const UA_PRESETS = {
        win: { ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36', lang: 'zh-CN,zh;q=0.9', platform: 'Win32' },
        mac: { ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36', lang: 'zh-CN,zh;q=0.9', platform: 'MacIntel' },
        linux: { ua: 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36', lang: 'zh-CN,zh;q=0.9', platform: 'Linux x86_64' },
      };
      function applyUaPreset(key) {
        if (key === 'default') {
          b.user_agent = ''; b.accept_language = ''; b.platform = '';
          return;
        }
        const p = UA_PRESETS[key];
        if (!p) return;
        b.user_agent = p.ua; b.accept_language = p.lang; b.platform = p.platform;
      }

      async function probeUA() {
        busy.value = true;
        try {
          const d = await C.api('/browser/probe-ua', { method: 'POST', body: {} });
          bs.probe = 'UA: ' + (d.user_agent || '-') + ' | 平台: ' + (d.platform || '-') + ' | 时区: ' + (d.timezone || '-');
          C.toast('已读取当前内核指纹', 'ok');
        } catch (e) { C.toast(e.message || String(e), 'err'); }
        finally { busy.value = false; }
      }

      return {
        s: s, b: b, g: g, gp: gp, bs: bs, configFile: configFile, loading: loading, busy: busy,
        load: load, saveSettings: saveSettings, testWecom: testWecom, downloadBrowser: downloadBrowser, saveBrowser: saveBrowser,
        clearSession: clearSession,
        saveGeneral: saveGeneral, resetGeneral: resetGeneral,
        applyUaPreset: applyUaPreset, probeUA: probeUA,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div><h2>系统设置</h2><p>配置文件：<span class="code">{{ configFile }}</span>（修改 addr 需重启）</p></div>
        <button class="btn" @click="load">↻ 刷新状态</button>
      </div>

      <div class="card">
        <div class="card-head">
          <h3>运行参数（可自定义）</h3>
          <span class="tag" :class="gp.restart_required ? 'amber' : 'green'">{{ gp.restart_required ? '待重启生效' : '已生效' }}</span>
        </div>
        <div class="card-body">
          <div class="form-grid">
            <div><label class="field">监听地址</label>
              <input id="cfAddr" v-model="g.addr" placeholder="127.0.0.1:8686" />
            </div>
            <div><label class="field">数据目录</label>
              <input id="cfDataDir" v-model="g.data_dir" placeholder="E:\\workspace\\biaoge\\collect\\data" />
            </div>
            <div><label class="field">单请求超时（秒）</label>
              <input id="cfTimeout" type="number" min="1" max="1800" v-model.number="g.timeout_sec" />
            </div>
            <div><label class="field">接口采集默认代理</label>
              <input id="cfProxy" v-model="g.proxy" placeholder="http://127.0.0.1:7890（留空直连）" />
            </div>
            <div><label class="field">单次请求/响应体最大保存（KB）</label>
              <input id="cfMaxBody" type="number" min="8" max="8192" v-model.number="g.max_body_kb" />
            </div>
            <div class="full hint">
              监听地址、数据目录属于启动参数，保存后需重启平台才生效（数据目录改动会切换到新的 app.db，请谨慎）。超时、代理、包体上限保存后立即对新请求生效。
            </div>
          </div>
          <div class="row" style="margin-top:14px">
            <button class="btn" data-act="general-reset" @click="resetGeneral">恢复常用默认值</button>
            <span class="hint" style="flex:1">{{ gp.saved_at ? '上次保存：' + gp.saved_at : '' }}</span>
            <button class="btn primary" data-act="general-save" :disabled="busy" @click="saveGeneral">保存运行参数</button>
          </div>
        </div>
      </div>

      <div class="grid grid-2">
        <div class="card">
          <div class="card-head"><h3>通知设置</h3></div>
          <div class="card-body">
            <div class="form-grid">
              <div class="full">
                <label class="check">
                  <input type="checkbox" id="setWecomEnabled" :checked="s.wecom_enabled === 'true'"
                         @change="s.wecom_enabled = $event.target.checked ? 'true' : 'false'" />
                  启用企业微信机器人
                </label>
              </div>
              <div class="full"><label class="field">企业微信 Webhook 地址</label>
                <input id="setWebhook" v-model="s.wecom_webhook" placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx" />
              </div>
              <div><label class="field">默认失败通知</label>
                <select id="setNotifyFailure" v-model="s.notify_on_failure">
                  <option value="true">开启</option><option value="false">关闭</option>
                </select>
              </div>
              <div><label class="field">默认成功通知</label>
                <select id="setNotifySuccess" v-model="s.notify_on_success">
                  <option value="true">开启</option><option value="false">关闭</option>
                </select>
              </div>
            </div>
            <div class="row" style="margin-top:16px;justify-content:flex-end">
              <button class="btn" data-act="wecom-test" @click="testWecom">发送测试消息</button>
              <button class="btn primary" data-act="settings-save" :disabled="busy" @click="saveSettings">保存设置</button>
            </div>
            <p class="hint" style="margin-top:12px">任务级别还可以单独选择「通知 / 不通知」及通知时机；平台消息中心始终记录所有执行结果。</p>
          </div>
        </div>

        <div class="card">
          <div class="card-head">
            <h3>浏览器内核</h3>
            <span class="tag" :class="bs.available ? 'green' : 'amber'">{{ bs.available ? '可用' : '未就绪' }}</span>
          </div>
          <div class="card-body">
            <div class="kv-list">
              <span class="k">运行平台</span><span>{{ bs.platform }}</span>
              <span class="k">内核版本</span><span>Chromium r{{ bs.revision }}</span>
              <span class="k">当前路径</span><span class="code" style="word-break:break-all">{{ bs.path || '未找到，可点击下方下载或填写系统 Chrome/Edge 路径' }}</span>
              <span class="k">来源</span><span>{{ bs.source || '-' }}</span>
              <span class="k">下载目录</span><span class="code">{{ bs.download_dir }}</span>
              <template v-if="bs.message"><span class="k">说明</span><span class="hint">{{ bs.message }}</span></template>
            </div>
            <div class="row" style="margin-bottom:14px">
              <button class="btn" data-act="browser-download" :disabled="bs.downloading" @click="downloadBrowser">{{ bs.downloading ? '下载中…' : '下载 Chromium 内核' }}</button>
              <button class="btn" data-act="settings-refresh" @click="load">刷新状态</button>
            </div>
            <div class="form-grid">
              <div class="full"><label class="field">自定义浏览器路径（留空自动探测系统 Chrome/Edge）</label>
                <input id="brBin" v-model="b.bin_path" placeholder="C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe" />
              </div>
              <div><label class="field">下载镜像</label>
                <select id="brHost" v-model="b.download_host">
                  <option value="npm">npm</option><option value="google">google</option><option value="custom">custom</option>
                </select>
              </div>
              <div><label class="field">自定义镜像前缀</label>
                <input id="brCustom" v-model="b.custom_host" placeholder="https://npmmirror.com/mirrors/chromium-browser-snapshots" />
              </div>
              <div><label class="field">Chromium 版本号（0 默认）</label><input id="brRev" type="number" v-model.number="b.revision" /></div>
              <div><label class="field">浏览器代理</label><input id="brProxy" v-model="b.proxy" placeholder="http://127.0.0.1:7890" /></div>
              <div><label class="check"><input type="checkbox" id="brHeadless" v-model="b.headless" /> 采集时无头运行</label></div>
              <div><label class="check"><input type="checkbox" id="brNoSandbox" v-model="b.no_sandbox" /> 关闭沙箱（Linux/容器建议）</label></div>
              <div><label class="field">会话模式（任务运行时的浏览器会话）</label>
                <select id="brSession" v-model="b.session_mode" data-act="browser-session-mode">
                  <option value="isolated">每次全新会话（默认 · 无痕，跑完即删）</option>
                  <option value="shared">共享会话（复用 Cookie / 登录态）</option>
                </select>
              </div>
              <div class="full"><label class="field">共享会话目录（留空 = 数据目录\\browser\\profiles\\default）</label>
                <input id="brProfile" v-model="b.profile_dir" :disabled="b.session_mode !== 'shared'" placeholder="D:\\profiles\\gov（仅共享会话模式生效）" />
              </div>
            </div>
            <div class="row" style="margin-top:14px;justify-content:flex-end">
              <button class="btn primary" data-act="browser-save" :disabled="busy" @click="saveBrowser">保存浏览器配置</button>
            </div>
            <div class="hint" style="margin-top:12px" data-act="browser-session-info">
              当前模式：<b>{{ b.session_mode === 'shared' ? '共享会话' : '每次全新会话' }}</b>
              　会话目录：<span class="code">{{ bs.profile_dir }}</span>
              　占用：{{ bs.session_files }} 个文件 / {{ bs.session_size_mb }} MB
              <span v-if="bs.session_locked" class="tag amber">正在被任务使用</span>
            </div>
            <div class="row" style="margin-top:10px">
              <button class="btn" data-act="browser-session-clear" :disabled="busy || bs.session_locked" @click="clearSession">清空共享会话</button>
              <span class="hint" style="flex:1">共享模式下同一时间只允许一个任务使用同一个会话目录；并发任务会排队，超过 2 分钟提示稍后重试。</span>
            </div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-head">
          <h3>浏览器指纹（UA / 语言 / 平台 / 时区 / 视口）</h3>
          <span class="tag" :class="b.user_agent ? 'green' : ''">{{ b.user_agent ? '已自定义' : '使用内核默认' }}</span>
        </div>
        <div class="card-body">
          <div class="form-grid">
            <div><label class="field">UA 预设（一键对齐 UA / 语言 / 平台）</label>
              <select id="brUaPreset" data-act="ua-preset" @change="applyUaPreset($event.target.value)">
                <option value="default">浏览器默认（不覆盖）</option>
                <option value="win">Windows · Chrome</option>
                <option value="mac">macOS · Chrome</option>
                <option value="linux">Linux · Chrome</option>
              </select>
            </div>
            <div><label class="field">Accept-Language</label>
              <input id="brLang" v-model="b.accept_language" placeholder="zh-CN,zh;q=0.9（留空默认）" />
            </div>
            <div class="full"><label class="field">User-Agent（留空使用内核默认）</label>
              <textarea id="brUA" v-model="b.user_agent" rows="2" placeholder="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 ..."></textarea>
            </div>
            <div><label class="field">navigator.platform</label>
              <input id="brPlatform" v-model="b.platform" placeholder="Win32 / MacIntel / Linux x86_64" />
            </div>
            <div><label class="field">时区</label>
              <input id="brTZ" v-model="b.timezone" placeholder="Asia/Shanghai（留空默认）" />
            </div>
            <div><label class="field">视口宽</label><input id="brW" type="number" v-model.number="b.width" /></div>
            <div><label class="field">视口高</label><input id="brH" type="number" v-model.number="b.height" /></div>
            <div class="full"><label class="field">额外启动参数（每行一个，如 --disable-gpu）</label>
              <textarea id="brArgs" v-model="b.extra_args_text" rows="2" placeholder="--disable-gpu"></textarea>
            </div>
          </div>
          <div class="row" style="margin-top:14px">
            <button class="btn" data-act="browser-probe" :disabled="busy" @click="probeUA">检测当前内核 UA</button>
            <span class="hint" style="flex:1">{{ bs.probe || '' }}</span>
            <button class="btn primary" data-act="fingerprint-save" :disabled="busy" @click="saveBrowser">保存指纹配置</button>
          </div>
          <p class="hint" style="margin-top:12px">
            纯 UI / 混合模式的回放、录制，以及接口采集的默认请求头都会沿用这里的 UA 与语言，保证「浏览器里看到的样子」和「接口发出的样子」一致。
            UA、平台、语言三者要互相匹配（例如 macOS 的 UA 配 MacIntel），随意拼凑反而更容易被风控识别。修改后对新启动的浏览器生效。
          </p>
        </div>
      </div>

      <div class="card">
        <div class="card-head"><h3>关于平台</h3></div>
        <div class="card-body hint">
          <p>· 方案一「纯接口」：录制登录接口 → 自动识别 Token 并替换为 <span class="code" v-pre>{{token}}</span> → 列表接口分页抓取，适合验证参数少的站点。</p>
          <p>· 方案三「UI + 接口」：UI 只回放登录，采集 Cookie / localStorage / sessionStorage，再走接口抓取，适合动态签名参数多的站点。</p>
          <p>· 方案二「纯 UI」：回放真实页面点击/输入并捕获网络或 DOM 表格，风控严格的站点还原度最高。</p>
          <p>· 变量占位符：<span class="code" v-pre>{{token}}</span> <span class="code" v-pre>{{page}}</span>
            <span class="code" v-pre>{{pageSize}}</span> <span class="code" v-pre>{{now}}</span>
            <span class="code" v-pre>{{today}}</span> <span class="code" v-pre>{{timestamp}}</span>
            <span class="code" v-pre>{{uuid}}</span>；支持 <span class="code" v-pre>{{var|url}}</span>、<span class="code" v-pre>{{var|b64}}</span> 等修饰符。</p>
        </div>
      </div>
    </div>`,
  });
})();
