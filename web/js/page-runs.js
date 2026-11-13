/* ============================================================
   运行记录（列表 + 详情）
   ============================================================ */
(function () {
  'use strict';
  const C = Collect;

  /* ---------------- 步骤详情弹窗 ---------------- */
  C.component('step-detail-modal', {
    props: { step: { type: Object, required: true } },
    setup(props) {
      return { fmtDur: C.fmtDur, jsonPretty: C.jsonPretty };
    },
    template: `
    <div v-if="step.kind === 'sink'">
      <div class="kv-list">
        <span class="k">状态</span>
        <span><span class="tag" :class="step.status === 'failed' ? 'red' : (step.status === 'warn' ? 'amber' : 'green')">{{ step.status === 'failed' ? '失败' : (step.status === 'warn' ? '无新数据' : '成功') }}</span></span>
        <span class="k">耗时</span><span>{{ fmtDur(step.duration_ms) }}</span>
        <template v-if="step.error">
          <span class="k">错误</span><span style="color:#c43239">{{ step.error }}</span>
        </template>
      </div>
      <h4>SQL 模板 / 目标表</h4><div class="pre">{{ step.url || '-' }}</div>
      <h4>执行结果</h4><div class="pre">{{ step.resp_body || '-' }}</div>
    </div>
    <div v-else>
      <div class="kv-list">
        <span class="k">请求</span><span>{{ step.method }} <span class="code">{{ step.url }}</span></span>
        <span class="k">状态码</span><span>{{ step.resp_status }}</span>
        <span class="k">耗时</span><span>{{ fmtDur(step.duration_ms) }}</span>
        <template v-if="step.error">
          <span class="k">错误</span><span style="color:#c43239">{{ step.error }}</span>
        </template>
      </div>
      <h4>请求头</h4><div class="pre">{{ step.req_headers || '-' }}</div>
      <h4>请求体</h4><div class="pre">{{ jsonPretty(step.req_body) || '-' }}</div>
      <h4>响应头</h4><div class="pre">{{ step.resp_headers || '-' }}</div>
      <h4>响应体</h4><div class="pre">{{ jsonPretty(step.resp_body) || '-' }}</div>
    </div>`,
  });

  /* ---------------- 运行记录列表 ---------------- */
  C.component('page-runs', {
    setup() {
      const route = C.route;
      const q = C.reactive({ page: 1, task_id: '', status: '' });
      const d = C.reactive({ list: [], total: 0, page: 1, page_size: 20 });
      const options = C.reactive([]);
      const loading = C.ref(true);
      // 批量删除：勾选的运行 id（运行中的记录不可选）
      const sel = C.ref([]);
      const keepN = C.ref(20);
      const deleting = C.ref(false);

      async function loadOptions() {
        try {
          const list = await C.api('/tasks/options');
          options.splice(0, options.length, ...list);
        } catch (e) { /* ignore */ }
      }

      async function load() {
        loading.value = true;
        try {
          const params = new URLSearchParams({ page: q.page, page_size: 20 });
          if (q.task_id) params.set('task_id', q.task_id);
          if (q.status) params.set('status', q.status);
          const data = await C.api('/runs?' + params.toString());
          d.list = data.list || [];
          d.total = data.total || 0;
          d.page = data.page || 1;
          d.page_size = data.page_size || 20;
          // 翻页 / 刷新后清空勾选，避免误删看不见的记录
          sel.value = [];
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }

      Vue.onMounted(async function () {
        if (route.id) { q.task_id = route.id; q.page = 1; }
        await loadOptions();
        await load();
      });

      function filter() { q.page = 1; load(); }
      function changePage(p) { q.page = p; load(); }
      function exportRun(r, format) { window.open('/api/runs/' + r.id + '/export?format=' + format, '_blank'); }
      async function delRun(r) {
        if (!confirm('确定删除该运行记录及其数据吗？')) return;
        try {
          await C.api('/runs/' + r.id, { method: 'DELETE' });
          C.toast('已删除', 'ok');
          load();
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }

      function selectable(r) { return r.status !== 'running'; }
      function isSel(r) { return sel.value.indexOf(r.id) >= 0; }
      function toggle(r) {
        if (!selectable(r)) return;
        const i = sel.value.indexOf(r.id);
        if (i >= 0) sel.value.splice(i, 1); else sel.value.push(r.id);
      }
      const pageSelectable = C.computed(function () {
        return (d.list || []).filter(selectable);
      });
      const allSel = C.computed(function () {
        const list = pageSelectable.value || [];
        return list.length > 0 && list.every(function (r) { return isSel(r); });
      });
      function toggleAll() {
        if (allSel.value) {
          sel.value = [];
        } else {
          sel.value = pageSelectable.value.map(function (r) { return r.id; });
        }
      }

      async function postBatch(body) {
        deleting.value = true;
        try {
          const r = await C.api('/runs/batch-delete', { method: 'POST', body: body });
          const msg = (r && r.msg) || '已删除';
          C.toast(msg, 'ok');
          sel.value = [];
          await load();
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          deleting.value = false;
        }
      }

      async function delSelected() {
        const n = sel.value.length;
        if (!n) return;
        if (!confirm('确定删除选中的 ' + n + ' 条运行记录吗？\n\n会同时删除这些运行的步骤日志、采集数据集和导出文件，且不可恢复。')) return;
        await postBatch({ ids: sel.value.slice() });
      }

      async function clearFiltered() {
        const scope = (q.task_id ? '当前任务' : '全部任务') +
          (q.status ? '、状态「' + statusText(q.status) + '」' : '');
        if (!confirm('确定清空 ' + scope + ' 的运行记录吗？\n\n运行中的记录会跳过；步骤日志与数据集会一并删除，且不可恢复。')) return;
        await postBatch({ all: true, task_id: Number(q.task_id) || 0, status: q.status || '' });
      }

      async function keepLatest() {
        const n = Number(keepN.value) || 0;
        if (n <= 0) { C.toast('请填写要保留的条数（≥1）', 'warn'); return; }
        const scope = q.task_id ? '当前任务' : '全部任务';
        if (!confirm('确定清理 ' + scope + ' 的历史记录，只保留最近 ' + n + ' 条吗？\n\n超出部分会连同步骤日志与数据集一起删除，且不可恢复。')) return;
        await postBatch({ keep: n, task_id: Number(q.task_id) || 0, status: q.status || '' });
      }

      function statusText(v) {
        return ({ running: '运行中', success: '成功', failed: '失败', partial: '部分成功' })[v] || v;
      }

      return {
        q: q, d: d, options: options, loading: loading,
        load: load, filter: filter, changePage: changePage, exportRun: exportRun, delRun: delRun,
        sel: sel, keepN: keepN, deleting: deleting,
        isSel: isSel, toggle: toggle, allSel: allSel, pageSelectable: pageSelectable, toggleAll: toggleAll,
        delSelected: delSelected, clearFiltered: clearFiltered, keepLatest: keepLatest,
        fmt: C.fmt, fmtDur: C.fmtDur, statusTag: C.statusTag, modeTag: C.modeTag,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div><h2>运行记录</h2><p>查看每次执行的请求、响应、步骤日志与采集数据</p></div>
      </div>
      <div class="card">
        <div class="card-head">
          <div class="row grow">
            <select id="runTask" v-model="q.task_id" style="max-width:240px" @change="filter">
              <option value="">全部任务</option>
              <option v-for="t in options" :key="t.id" :value="String(t.id)">{{ t.name }}</option>
            </select>
            <select id="runStatus" v-model="q.status" style="max-width:150px" @change="filter">
              <option value="">全部状态</option>
              <option value="running">运行中</option>
              <option value="success">成功</option>
              <option value="failed">失败</option>
              <option value="partial">部分成功</option>
            </select>
            <button class="btn sm" data-act="run-search" @click="filter">筛选</button>
          </div>
          <button class="btn sm ghost" data-act="run-refresh" @click="load">↻ 刷新</button>
        </div>

        <div class="card-body" style="padding-bottom:0">
          <div class="row" style="flex-wrap:wrap;gap:8px;align-items:center">
            <span class="hint" style="margin:0">已选 <b>{{ sel.length }}</b> 条</span>
            <button class="btn sm danger" data-act="runs-batch-del" :disabled="deleting || !sel.length" @click="delSelected">
              删除选中
            </button>
            <span class="hint" style="margin:0 0 0 12px">仅保留最近</span>
            <input type="number" min="1" v-model.number="keepN" style="width:88px" data-act="runs-keep-n" />
            <span class="hint" style="margin:0">条</span>
            <button class="btn sm" data-act="runs-keep-latest" :disabled="deleting" @click="keepLatest">清理历史</button>
            <button class="btn sm danger ghost" data-act="runs-clear-filtered" :disabled="deleting" @click="clearFiltered">
              清空当前筛选结果
            </button>
            <span class="hint" style="margin:0;flex:1;text-align:right">运行中的记录不会被删除</span>
          </div>
        </div>

        <div v-if="loading" class="loading">加载中…</div>
        <c-empty v-else-if="!d.list.length" text="还没有运行记录" />
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead><tr>
              <th style="width:34px"><input type="checkbox" data-act="runs-sel-all" :checked="allSel" :disabled="!pageSelectable.length" @change="toggleAll" /></th>
              <th>运行</th><th>任务</th><th>模式</th><th>触发</th><th>状态</th><th>开始时间</th><th>耗时</th><th>页/抓取/新增/重复</th><th>操作</th>
            </tr></thead>
            <tbody>
              <tr v-for="r in d.list" :key="r.id">
                <td><input type="checkbox" :value="r.id" :checked="isSel(r)" :disabled="r.status === 'running'" @change="toggle(r)" /></td>
                <td>#{{ r.id }}</td>
                <td><a :href="'#/run/' + r.id">{{ r.task_name }}</a></td>
                <td><span class="tag" :class="modeTag(r.mode).cls">{{ modeTag(r.mode).text }}</span></td>
                <td><span class="tag" :class="r.trigger === 'cron' ? 'amber' : 'gray'">{{ r.trigger === 'cron' ? '定时' : '手动' }}</span></td>
                <td><span class="tag" :class="statusTag(r.status).cls">{{ statusTag(r.status).text }}</span></td>
                <td>{{ fmt(r.started_at) }}</td>
                <td>{{ fmtDur(r.duration_ms) }}</td>
                <td>{{ r.pages }} / {{ r.fetched }} / {{ r.inserted }} / {{ r.duplicated }}</td>
                <td>
                  <div class="actions">
                    <a class="btn sm" :href="'#/run/' + r.id">详情</a>
                    <button class="btn sm" data-act="run-export" @click="exportRun(r, 'xlsx')">Excel</button>
                    <button class="btn sm danger" data-act="del-run" @click="delRun(r)">删除</button>
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

  /* ---------------- 运行详情 ---------------- */
  C.component('page-run-detail', {
    setup() {
      const route = C.route;
      const run = C.reactive({});
      const steps = C.reactive([]);
      const data = C.reactive({ headers: [], rows: [], total: 0, offset: 0, limit: 20 });
      const sources = C.reactive({ list: [], total: 0, active: '' });
      const loading = C.ref(true);
      // 运行详情两个大页签：执行步骤 / 数据集预览
      const detailTab = C.ref('steps');
      const stepsNewest = C.ref(true);

      async function loadSources() {
        try {
          const d = await C.api('/runs/' + route.id + '/sources');
          sources.list.splice(0, sources.list.length, ...(d.list || []));
          sources.total = d.total || 0;
          if (!sources.active && sources.list.length) sources.active = sources.list[0].source;
        } catch (e) { /* 旧版本后端无此接口时忽略 */ }
      }

      async function loadData(offset) {
        try {
          let url = '/runs/' + route.id + '/data?limit=' + data.limit + '&offset=' + offset;
          if (sources.active) url += '&source=' + encodeURIComponent(sources.active);
          const d = await C.api(url);
          Object.assign(data, d);
        } catch (e) { C.toast(e.message || String(e), 'err'); }
      }
      function pickSource(src) {
        if (sources.active === src) return;
        sources.active = src;
        data.offset = 0;
        loadData(0);
      }

      async function load() {
        if (!route.id) { C.go('/runs'); return; }
        loading.value = true;
        try {
          const detail = await C.api('/runs/' + route.id);
          Object.assign(run, detail.run || {});
          steps.splice(0, steps.length, ...((await C.api('/runs/' + route.id + '/steps')) || []));
          await loadSources();
          await loadData(0);
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);

      function changeDataPage(offset) {
        if (offset < 0) return;
        loadData(offset);
      }
      function exportRun(format) {
        let url = '/api/runs/' + route.id + '/export?format=' + format;
        if (sources.active) url += '&source=' + encodeURIComponent(sources.active);
        window.open(url, '_blank');
      }
      function exportAll(format) { window.open('/api/runs/' + route.id + '/export?format=' + format, '_blank'); }

      function showStep(s) {
        C.openModal('步骤：' + s.name, 'step-detail-modal', { step: s });
      }

      // 执行步骤默认按开始时间倒序（最近的在前），可切换为执行顺序
      const stepRows = C.computed(function () {
        const arr = steps.slice();
        arr.sort(function (a, b) {
          const sa = Number(a.seq || 0), sb = Number(b.seq || 0);
          if (sa !== sb) return sa - sb;
          return Number(a.id || 0) - Number(b.id || 0);
        });
        if (stepsNewest.value) arr.reverse();
        return arr;
      });

      function back() { history.back(); }

      const deleting = C.ref(false);
      async function delRun() {
        if (!confirm('确定删除这条运行记录吗？\n\n步骤日志、采集数据集和导出文件会一并删除，且不可恢复。')) return;
        deleting.value = true;
        try {
          await C.api('/runs/' + route.id, { method: 'DELETE' });
          C.toast('已删除', 'ok');
          C.go('/runs');
        } catch (e) {
          C.toast(e.message || String(e), 'err');
        } finally {
          deleting.value = false;
        }
      }

      function cellText(v) {
        if (v === null || v === undefined) return '';
        const s = typeof v === 'object' ? JSON.stringify(v) : String(v);
        return s.length > 60 ? s.slice(0, 60) + '…' : s;
      }
      function cellTitle(v) {
        if (v === null || v === undefined) return '';
        return typeof v === 'object' ? JSON.stringify(v) : String(v);
      }

      return {
        run: run, steps: steps, stepRows: stepRows, data: data, sources: sources, loading: loading,
        detailTab: detailTab, stepsNewest: stepsNewest,
        changeDataPage: changeDataPage, exportRun: exportRun, exportAll: exportAll,
        pickSource: pickSource, showStep: showStep, back: back,
        deleting: deleting, delRun: delRun,
        cellText: cellText, cellTitle: cellTitle,
        fmt: C.fmt, fmtDur: C.fmtDur, statusTag: C.statusTag, modeTag: C.modeTag,
        stepKindTag: C.stepKindTag, shortUrl: C.shortUrl,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div>
          <h2>{{ run.task_name }} <span class="hint">运行 #{{ run.id }}</span></h2>
          <p>
            <span class="tag" :class="statusTag(run.status).cls">{{ statusTag(run.status).text }}</span>
            <span class="tag" :class="modeTag(run.mode).cls">{{ modeTag(run.mode).text }}</span>
            · 开始 {{ fmt(run.started_at) }} · 耗时 {{ fmtDur(run.duration_ms) }}
          </p>
        </div>
        <div class="row">
          <button class="btn" @click="back">返回</button>
          <button class="btn danger" data-act="run-del" :disabled="deleting || run.status === 'running'" @click="delRun">删除记录</button>
          <button class="btn" data-act="run-export" @click="exportAll('csv')">导出全部 CSV</button>
          <button class="btn primary" data-act="run-export" @click="exportAll('xlsx')">导出全部 Excel</button>
        </div>
      </div>

      <div v-if="run.error" class="card"><div class="card-body" style="color:#c43239"><b>错误：</b>{{ run.error }}</div></div>

      <div class="grid grid-4" style="margin-bottom:16px">
        <div class="card stat"><span class="k">页数</span><span class="v">{{ run.pages }}</span></div>
        <div class="card stat"><span class="k">抓取 / 新增</span><span class="v">{{ run.fetched }} / {{ run.inserted }}</span></div>
        <div class="card stat"><span class="k">重复跳过</span><span class="v">{{ run.duplicated }}</span></div>
        <div class="card stat"><span class="k">失败</span><span class="v">{{ run.failed }}</span></div>
      </div>

      <div class="card">
        <div class="card-head" style="align-items:flex-start;padding-bottom:0;border-bottom:0">
          <div class="tabs" style="margin:0 -16px 0 -16px;flex:1">
            <button :class="{ active: detailTab === 'steps' }" data-act="detail-tab-steps" @click="detailTab = 'steps'">
              执行步骤 <span class="tab-badge">{{ steps.length }}</span>
            </button>
            <button :class="{ active: detailTab === 'data' }" data-act="detail-tab-data" @click="detailTab = 'data'">
              数据集预览 <span class="tab-badge">{{ sources.total }}</span>
            </button>
          </div>
          <label v-if="detailTab === 'steps'" class="hint" style="white-space:nowrap;cursor:pointer">
            <input type="checkbox" v-model="stepsNewest" style="vertical-align:-2px" /> 最近的在前面
          </label>
        </div>

        <div class="pane" :class="{ active: detailTab === 'steps' }">
          <div class="card-body" style="padding-top:12px">
            <div class="row" style="flex-wrap:wrap;gap:8px">
              <span class="hint">按实际执行顺序记录，可点击「详情」查看完整请求 / 响应报文</span>
            </div>
          </div>
          <c-empty v-if="!steps.length" text="没有步骤日志" />
          <div v-else class="tbl-wrap">
            <table class="tbl">
              <thead><tr><th>#</th><th>步骤</th><th>请求</th><th>状态码</th><th>执行开始时间</th><th>耗时</th><th>结果</th><th></th></tr></thead>
              <tbody>
                <tr v-for="s in stepRows" :key="s.id">
                  <td>{{ s.seq || '-' }}</td>
                  <td><span class="tag" :class="stepKindTag(s.kind).cls">{{ stepKindTag(s.kind).text }}</span> {{ s.name }}</td>
                  <td><span v-if="s.method" class="tag gray">{{ s.method }}</span> <span class="hint">{{ shortUrl(s.url, 60) }}</span></td>
                  <td>{{ s.resp_status || '-' }}</td>
                  <td>{{ fmt(s.created_at) }}</td>
                  <td>{{ fmtDur(s.duration_ms) }}</td>
                  <td>
                    <span v-if="s.error" class="tag red">{{ String(s.error).slice(0, 40) }}</span>
                    <span v-else-if="s.status === 'failed'" class="tag red">失败</span>
                    <span v-else-if="s.status === 'warn'" class="tag amber">无新数据</span>
                    <span v-else class="tag green">OK</span>
                  </td>
                  <td><button class="btn sm" data-act="step-detail" :disabled="s.kind !== 'http' && s.kind !== 'sink'" @click="showStep(s)">详情</button></td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div class="pane" :class="{ active: detailTab === 'data' }">
          <div class="card-body" style="padding-top:12px">
            <div class="row" style="flex-wrap:wrap;gap:8px">
              <button v-for="s in sources.list" :key="s.source" class="btn sm"
                      :class="{ primary: sources.active === s.source }" :data-source="s.source"
                      @click="pickSource(s.source)">
                {{ s.label }} <span class="hint">{{ s.count }}</span>
              </button>
              <span v-if="!sources.list.length" class="hint">暂无采集数据</span>
            </div>
            <div v-if="sources.list.length" class="row" style="margin-top:10px;flex-wrap:wrap;gap:8px">
              <span class="hint">当前分组共 {{ data.total }} 条</span>
              <button class="btn sm" data-act="export-source-csv" @click="exportRun('csv')">导出当前分组 CSV</button>
              <button class="btn sm" data-act="export-source-xlsx" @click="exportRun('xlsx')">导出当前分组 Excel</button>
              <button class="btn sm ghost" data-act="export-all-xlsx" @click="exportAll('xlsx')">导出全部数据</button>
            </div>
          </div>
          <c-empty v-if="!data.rows.length" text="该分组暂无采集数据" />
          <div v-else class="tbl-wrap">
            <table class="tbl">
              <thead><tr><th>#</th><th v-for="h in data.headers" :key="h">{{ h }}</th></tr></thead>
              <tbody>
                <tr v-for="(row, ri) in data.rows" :key="ri">
                  <td>{{ data.offset + ri + 1 }}</td>
                  <td v-for="h in data.headers" :key="h" :title="cellTitle(row[h])">{{ cellText(row[h]) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div class="card-body" style="padding-top:0">
            <div class="pager">
              <span>{{ sources.active ? '当前分组 · ' : '' }}共 {{ data.total }} 条 · 第 {{ Math.floor(data.offset / (data.limit || 20)) + 1 }} / {{ Math.max(1, Math.ceil(data.total / (data.limit || 20))) }} 页</span>
              <button class="btn sm" :disabled="data.offset <= 0" @click="changeDataPage(data.offset - data.limit)">上一页</button>
              <button class="btn sm" :disabled="data.offset + data.limit >= data.total" @click="changeDataPage(data.offset + data.limit)">下一页</button>
            </div>
          </div>
        </div>
      </div>
    </div>`,
  });
})();
