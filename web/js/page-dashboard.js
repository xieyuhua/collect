/* ============================================================
   仪表盘
   ============================================================ */
(function () {
  'use strict';
  const C = Collect;

  C.component('page-dashboard', {
    setup() {
      const d = C.reactive({ tasks: 0, tasks_enabled: 0, runs_today: 0, runs_total: 0, running: 0, success_rate: 0, unread: 0, recent: [] });
      const loading = C.ref(true);
      const error = C.ref('');

      async function load() {
        loading.value = true;
        error.value = '';
        try {
          const data = await C.api('/dashboard');
          Object.assign(d, data);
          if (!Array.isArray(d.recent)) d.recent = [];
        } catch (e) {
          error.value = e.message || String(e);
        } finally {
          loading.value = false;
        }
      }
      Vue.onMounted(load);

      const failed = C.computed(function () {
        return (d.recent || []).filter(function (r) { return r.status === 'failed' || r.status === 'partial'; });
      });
      const rateHint = C.computed(function () {
        if (d.success_rate >= 95) return '整体稳定';
        if (d.success_rate >= 80) return '存在少量失败';
        return '建议排查失败任务';
      });

      return {
        d: d, loading: loading, error: error, failed: failed, rateHint: rateHint,
        fmt: C.fmt, fmtDur: C.fmtDur, statusTag: C.statusTag, modeTag: C.modeTag, load: load,
      };
    },
    template: `
    <div>
      <div class="page-head">
        <div>
          <h2>仪表盘</h2>
          <p>三种采集模式：纯接口 · 纯 UI · UI 登录 + 接口抓取</p>
        </div>
        <div class="row">
          <button class="btn" @click="load()">↻ 刷新</button>
          <a class="btn" href="#/tasks">任务管理</a>
          <a class="btn primary" href="#/studio">开始录制</a>
        </div>
      </div>

      <div v-if="error" class="card"><div class="card-body" style="color:#c43239">⚠️ {{ error }}</div></div>

      <div class="grid grid-4">
        <div class="card stat accent">
          <span class="k">🧩 任务总数</span><span class="v">{{ d.tasks }}</span>
          <span class="s">启用 {{ d.tasks_enabled }} 个 · <a href="#/tasks">去管理</a></span>
        </div>
        <div class="card stat accent">
          <span class="k">📄 今日运行</span><span class="v">{{ d.runs_today }}</span>
          <span class="s">累计 {{ d.runs_total }} 次 · 运行中 {{ d.running }} 个</span>
        </div>
        <div class="card stat accent">
          <span class="k">✅ 成功率</span><span class="v">{{ Number(d.success_rate || 0).toFixed(1) }}%</span>
          <span class="s">{{ rateHint }}</span>
        </div>
        <div class="card stat accent">
          <span class="k">🔔 未读消息</span><span class="v">{{ d.unread }}</span>
          <span class="s"><a href="#/notifications">查看消息中心</a></span>
        </div>
      </div>

      <div v-if="failed.length" class="card" style="margin-top:16px;border-color:#f6d7d8;background:#fffafa">
        <div class="card-body row" style="gap:10px;padding:14px 18px">
          <span class="tag red">最近异常</span>
          <span class="hint grow">最近 {{ failed.length }} 次运行未完全成功，可到运行记录查看请求与响应报文。</span>
          <a class="btn sm" href="#/runs">查看运行记录</a>
        </div>
      </div>

      <div class="card" style="margin-top:16px">
        <div class="card-head"><h3>最近运行</h3><a class="btn sm" href="#/runs">全部记录</a></div>
        <c-empty v-if="!d.recent.length" text="还没有运行记录，去任务管理里执行一个任务吧" />
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead><tr><th>运行</th><th>任务</th><th>模式</th><th>状态</th><th>开始时间</th><th>耗时</th><th>抓取/新增/重复</th></tr></thead>
            <tbody>
              <tr v-for="r in d.recent" :key="r.id">
                <td>#{{ r.id }}</td>
                <td><a :href="'#/run/' + r.id">{{ r.task_name }}</a></td>
                <td><span class="tag" :class="modeTag(r.mode).cls">{{ modeTag(r.mode).text }}</span></td>
                <td><span class="tag" :class="statusTag(r.status).cls">{{ statusTag(r.status).text }}</span></td>
                <td>{{ fmt(r.started_at) }}</td>
                <td>{{ fmtDur(r.duration_ms) }}</td>
                <td>{{ r.fetched }} / {{ r.inserted }} / {{ r.duplicated }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card" style="margin-top:16px">
        <div class="card-head"><h3>模式选型建议</h3></div>
        <div class="card-body grid grid-3">
          <div>
            <div class="tag blue">方案一 · 纯接口</div>
            <p class="hint">网站使用明显的 Token（Authorization / access_token），直接录制登录接口，提取 Token 后替换后续请求头，分页拉取列表。<br /><b>推荐：</b>接口稳定、验证参数少。</p>
          </div>
          <div>
            <div class="tag amber">方案三 · UI + 接口</div>
            <p class="hint">UI 只回放登录页面，收集 Cookie / localStorage 会话，后续列表接口用 HTTP 直接抓。<br /><b>推荐：</b>动态签名参数多、接口本身可复用。</p>
          </div>
          <div>
            <div class="tag purple">方案二 · 纯 UI</div>
            <p class="hint">浏览器真实操作：打开、输入、点击、滚动，再从页面网络或 DOM 表格中抓数据。<br /><b>推荐：</b>风控严格、每一步都校验的站点。</p>
          </div>
        </div>
      </div>
    </div>`,
  });
})();
