/* ============================================================
   任务管理（列表）
   ============================================================ */
(function () {
  'use strict';
  const C = Collect;

  C.component('page-tasks', {
    setup() {
      const q = C.reactive({ page: 1, keyword: '', mode: '' });
      const d = C.reactive({ list: [], total: 0, page: 1, page_size: 15 });
      const loading = C.ref(true);
      const runningId = C.ref(0);
      const fileInput = C.ref(null);

      async function load() {
        loading.value = true;
        try {
          const params = new URLSearchParams({ page: q.page, page_size: 15, keyword: q.keyword || '', mode: q.mode || '' });
          const data = await C.api('/tasks?' + params.toString());
          d.list = data.list || [];
          d.total = data.total || 0;
          d.page = data.page || 1;
          d.page_size = data.page_size || 15;
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);

      function search() { q.page = 1; load(); }
      function changePage(p) { q.page = p; load(); }

      async function runTask(t) {
        runningId.value = t.id;
        try {
          const run = await C.api('/tasks/' + t.id + '/run', { method: 'POST', body: {} });
          C.toast('任务已启动，运行 #' + run.id, 'ok');
          setTimeout(load, 900);
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          runningId.value = 0;
        }
      }

      async function cloneTask(t) {
        try {
          const c = await C.api('/tasks/' + t.id + '/clone', { method: 'POST', body: {} });
          C.toast('已复制为「' + c.name + '」', 'ok');
          load();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      async function delTask(t) {
        if (!confirm('确定删除该任务吗？运行记录会保留。')) return;
        try {
          await C.api('/tasks/' + t.id, { method: 'DELETE' });
          C.toast('已删除', 'ok');
          load();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      function exportTask(t) { window.open('/api/tasks/' + t.id + '/export', '_blank'); }
      function exportAll() { window.open('/api/tasks/export', '_blank'); }

      function pickImport() { if (fileInput.value) fileInput.value.click(); }

      async function onImport(ev) {
        const f = ev.target.files && ev.target.files[0];
        if (!f) return;
        try {
          const r = await C.upload('/tasks/import', f);
          C.toast(r.msg || '导入完成', 'ok');
          load();
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        }
        ev.target.value = '';
      }

      function scheduleText(t) {
        if (!t.enabled) return '已停用';
        if (t.schedule_type === 'cron') return 'Cron ' + (t.cron || '未设置');
        if (t.schedule_type === 'interval') return t.interval_sec ? '每 ' + t.interval_sec + ' 秒' : '未设置';
        return '手动';
      }

      return {
        q: q, d: d, loading: loading, runningId: runningId, fileInput: fileInput,
        load: load, search: search, changePage: changePage, runTask: runTask, cloneTask: cloneTask,
        delTask: delTask, exportTask: exportTask, exportAll: exportAll, pickImport: pickImport,
        onImport: onImport, scheduleText: scheduleText,
        fmt: C.fmt, statusTag: C.statusTag, modeTag: C.modeTag,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div>
          <h2>任务管理</h2>
          <p>共 {{ d.total }} 个任务，可导入导出 JSON 在其它设备共享</p>
        </div>
        <div class="row">
          <input ref="fileInput" type="file" accept=".json,application/json" hidden @change="onImport" />
          <button class="btn" data-act="import-task" @click="pickImport">导入</button>
          <button class="btn" data-act="export-all" @click="exportAll">导出全部</button>
          <a class="btn primary" href="#/task/new">+ 新建任务</a>
        </div>
      </div>

      <div class="card">
        <div class="card-head">
          <div class="row grow">
            <input type="text" id="taskKw" v-model="q.keyword" placeholder="搜索任务名称 / 备注" style="max-width:260px" @keyup.enter="search" />
            <select id="taskMode" v-model="q.mode" style="max-width:160px" @change="search">
              <option value="">全部模式</option>
              <option value="api">纯接口</option>
              <option value="ui">纯 UI</option>
              <option value="hybrid">UI + 接口</option>
            </select>
            <button class="btn sm" data-act="task-search" @click="search">搜索</button>
          </div>
          <button class="btn sm ghost" data-act="task-refresh" title="刷新列表" @click="load">↻ 刷新</button>
        </div>

        <div v-if="loading" class="loading">加载中…</div>
        <c-empty v-else-if="!d.list.length" text="还没有任务，点击右上角新建，或到录制工作台录制一个" />
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead><tr><th>ID</th><th>任务</th><th>模式</th><th>调度</th><th>状态</th><th>上次运行</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="t in d.list" :key="t.id">
                <td>#{{ t.id }}</td>
                <td>
                  <a :href="'#/task/' + t.id"><b>{{ t.name }}</b></a>
                  <div v-if="t.remark" class="hint">{{ t.remark }}</div>
                </td>
                <td><span class="tag" :class="modeTag(t.mode).cls">{{ modeTag(t.mode).text }}</span></td>
                <td>{{ scheduleText(t) }}<div class="hint" v-if="t.next_run">下次 {{ fmt(t.next_run) }}</div></td>
                <td>
                  <span v-if="t.running" class="tag blue">运行中</span>
                  <span v-else class="tag" :class="statusTag(t.last_status).cls">{{ statusTag(t.last_status || '').text }}</span>
                </td>
                <td>{{ fmt(t.last_run_at) }}<div class="hint">{{ t.last_message || '' }}</div></td>
                <td>
                  <div class="actions">
                    <button class="btn sm primary" data-act="run-task" :disabled="runningId === t.id" @click="runTask(t)">{{ runningId === t.id ? '启动中…' : '运行' }}</button>
                    <a class="btn sm" :href="'#/task/' + t.id">编辑</a>
                    <button class="btn sm" data-act="clone-task" @click="cloneTask(t)">复制</button>
                    <a class="btn sm" :href="'#/runs/' + t.id">记录</a>
                    <button class="btn sm" data-act="export-task" @click="exportTask(t)">导出</button>
                    <button class="btn sm danger" data-act="del-task" @click="delTask(t)">删除</button>
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
})();
