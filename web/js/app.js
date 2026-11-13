/* ============================================================
   应用外壳：侧边栏 / 顶栏 / 路由视图 / Toast / Modal
   ============================================================ */
(function () {
  'use strict';

  const C = Collect;

  /* 侧边栏导航项 */
  const NAV = [
    { key: 'dashboard', icon: '📊', label: '仪表盘' },
    { key: 'tasks', icon: '🧩', label: '任务管理' },
    { key: 'studio', icon: '🎬', label: '录制工作台' },
    { key: 'runs', icon: '📄', label: '运行记录' },
    { key: 'jobs', icon: '⏱️', label: '定时作业' },
    { key: 'datasources', icon: '🗄️', label: '数据源' },
    { key: 'logs', icon: '📝', label: '运行日志' },
    { key: 'notifications', icon: '🔔', label: '消息中心' },
    { key: 'settings', icon: '⚙️', label: '系统设置' },
  ];

  /* ---------------- 通用分页 ---------------- */
  C.component('c-pager', {
    props: { total: { type: Number, default: 0 }, page: { type: Number, default: 1 }, size: { type: Number, default: 20 } },
    emits: ['change'],
    setup(props, ctx) {
      const pages = C.computed(function () { return Math.max(1, Math.ceil((props.total || 0) / (props.size || 20))); });
      return { pages: pages, prev: function () { if (props.page > 1) ctx.emit('change', props.page - 1); }, next: function () { if (props.page < pages.value) ctx.emit('change', props.page + 1); } };
    },
    template: `<div class="pager" v-if="total > 0">
      <span>共 {{ total }} 条 · 第 {{ page }}/{{ pages }} 页</span>
      <button class="btn sm" :disabled="page <= 1" @click="prev">上一页</button>
      <button class="btn sm" :disabled="page >= pages" @click="next">下一页</button>
    </div>`,
  });

  /* ---------------- 空状态 ---------------- */
  C.component('c-empty', {
    props: { text: { type: String, default: '暂无数据' }, icon: { type: String, default: '📭' } },
    setup() { return {}; },
    template: `<div class="empty"><div class="big">{{ icon }}</div>{{ text }}</div>`,
  });

  /* ---------------- 应用外壳 ---------------- */
  C.component('app-shell', {
    setup() {
      const route = C.route;
      const sys = C.sys;
      const modal = C.modal;

      const pageComp = C.computed(function () { return C.pageOf(route.name); });
      const crumb = C.computed(function () { return C.crumbOf(route.name, route.id); });
      const viewKey = C.computed(function () { return route.name + '/' + route.id + '/' + route.extra + '/' + route.key; });

      function isActive(key) {
        if (key === route.name) return true;
        if (key === 'tasks' && route.name === 'task') return true;
        if (key === 'runs' && route.name === 'run') return true;
        if (key === 'jobs' && (route.name === 'job' || route.name === 'jobruns' || route.name === 'jobrun')) return true;
        return false;
      }

      let healthTimer = 0;
      let badgeTimer = 0;
      Vue.onMounted(async function () {
        await C.refreshHealth();
        await C.refreshBadge();
        healthTimer = setInterval(C.refreshHealth, 30000);
        badgeTimer = setInterval(C.refreshBadge, 20000);
      });
      Vue.onUnmounted(function () {
        clearInterval(healthTimer);
        clearInterval(badgeTimer);
      });

      function onMaskClick(ev) {
        if (ev.target && ev.target.id === 'modalMask') C.closeModal();
      }

      function onKeydown(ev) {
        if (ev.key === 'Escape' && modal.visible) C.closeModal();
      }

      Vue.onMounted(function () { document.addEventListener('keydown', onKeydown); });
      Vue.onUnmounted(function () { document.removeEventListener('keydown', onKeydown); });

      return {
        NAV: NAV, route: route, sys: sys, modal: modal,
        pageComp: pageComp, crumb: crumb, viewKey: viewKey,
        isActive: isActive, onMaskClick: onMaskClick,
        reload: C.reload, closeModal: C.closeModal,
      };
    },
    template: `
    <div class="layout">
      <aside class="sidebar">
        <div class="brand">
          <div class="logo">采</div>
          <div>
            <div class="brand-name">采集平台</div>
            <div class="brand-sub">Browser Collect Platform</div>
          </div>
        </div>
        <nav class="nav">
          <a v-for="n in NAV" :key="n.key" :href="'#/' + n.key" :class="{ active: isActive(n.key) }" :title="n.label">
            <span>{{ n.icon }}</span>{{ n.label }}
            <em v-if="n.key === 'notifications' && sys.unread > 0" class="dot" :title="sys.unread + ' 条未读'"></em>
          </a>
        </nav>
        <div class="sidebar-foot">
          <div class="mini">版本 {{ sys.version }}{{ sys.uptime ? ' · 已运行 ' + sys.uptime : '' }}</div>
          <div class="mini muted">接口 · UI · 混合 三种采集模式</div>
        </div>
      </aside>

      <main class="main">
        <header class="topbar">
          <div class="crumbs">{{ crumb }}</div>
          <div class="top-actions">
            <span class="status-dot" :class="{ ok: sys.ok, bad: !sys.ok }"></span>
            <span class="muted small">{{ sys.msg }}</span>
            <button class="icon-btn" data-act="page-reload" title="刷新当前页面数据" style="font-size:13px" @click="reload()">↻</button>
          </div>
        </header>
        <div class="view" id="view">
          <component :is="pageComp" :key="viewKey" />
        </div>
      </main>
    </div>

    <div class="toast-wrap">
      <div v-for="t in Collect.toasts" :key="t.id" class="toast" :class="t.type">{{ t.msg }}</div>
    </div>

    <div class="modal-mask" id="modalMask" :hidden="!modal.visible" @click="onMaskClick">
      <div class="modal">
        <div class="modal-head">
          <h3>{{ modal.title }}</h3>
          <button class="icon-btn" data-act="modal-close" @click="closeModal()">✕</button>
        </div>
        <div class="modal-body" id="modalBody">
          <component v-if="modal.comp" :is="modal.comp" v-bind="modal.props" />
          <div v-else v-html="modal.html"></div>
        </div>
      </div>
    </div>`,
  });
})();
