"""本地模拟站点，用于验证采集平台三种模式（测试用）。"""
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

PORT = 18099
TOKEN = "tok-demo-9f8e7d6c5b4a3210"
CSRF = "csrf-demo-556677"
SID = "sess-demo-889900"

STORES = [
    {"id": i, "name": f"测试门店{i}", "city": ["北京", "上海", "广州", "深圳", "杭州"][i - 1],
     "address": f"测试路{i}号", "sales": 1000.5 * i}
    for i in range(1, 6)
]


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        pass

    def _send(self, code, body, ctype="application/json; charset=utf-8", extra=None):
        raw = body.encode("utf-8") if isinstance(body, str) else body
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(raw)))
        for k, v in (extra or {}).items():
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(raw)

    def _json(self, code, obj, extra=None):
        self._send(code, json.dumps(obj, ensure_ascii=False), "application/json; charset=utf-8", extra)

    def _auth_ok(self):
        if self.headers.get("X-CSRF") != CSRF:
            return False
        cookie = self.headers.get("Cookie") or ""
        if SID in cookie:
            return True
        return self.headers.get("Authorization") == "Bearer " + TOKEN

    def do_POST(self):
        path = urlparse(self.path).path
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n) if n else b"{}"
        if path == "/api/login":
            try:
                data = json.loads(raw or b"{}")
            except Exception:
                data = {}
            if data.get("username") == "demo" and data.get("password") == "123456":
                self._json(200, {"code": 0, "msg": "ok", "data": {
                    "access_token": TOKEN, "csrf": CSRF, "expires_in": 7200,
                    "data": {"access_token": TOKEN},
                }}, {"Set-Cookie": f"sid={SID}; Path=/; HttpOnly"})
            else:
                self._json(200, {"code": 1, "msg": "账号或密码错误"})
            return
        self._json(404, {"code": 1, "msg": "not found"})

    def do_GET(self):
        u = urlparse(self.path)
        path, q = u.path, parse_qs(u.query)
        if path == "/api/login":
            self._json(200, {"code": 0, "data": {"access_token": TOKEN, "csrf": CSRF}})
            return
        if path == "/api/stores":
            if not self._auth_ok():
                self._json(401, {"code": 401, "msg": "未授权，请先登录"})
                return
            page = int((q.get("page") or ["1"])[0])
            size = int((q.get("pageSize") or ["2"])[0])
            start = (page - 1) * size
            items = STORES[start:start + size]
            self._json(200, {"code": 0, "msg": "ok", "data": {
                "list": items, "total": len(STORES), "page": page, "pageSize": size,
            }})
            return
        if path == "/api/echo":
            # 回显请求头，便于验证 UA / Accept-Language 指纹是否按设置生效
            self._json(200, {"code": 0, "data": {"headers": dict(self.headers), "path": self.path}})
            return
        if path == "/login.html":
            self._send(200, LOGIN_HTML, "text/html; charset=utf-8")
            return
        if path == "/autologin.html":
            self._send(200, AUTO_LOGIN_HTML, "text/html; charset=utf-8")
            return
        if path == "/stores.html":
            self._send(200, STORES_HTML, "text/html; charset=utf-8")
            return
        if path == "/paged.html":
            self._send(200, PAGED_HTML, "text/html; charset=utf-8")
            return
        if path == "/paged2.html":
            self._send(200, PAGED2_HTML, "text/html; charset=utf-8")
            return
        if path == "/spa.html":
            self._send(200, SPA_HTML, "text/html; charset=utf-8")
            return
        if path == "/outer.html":
            self._send(200, OUTER_HTML, "text/html; charset=utf-8")
            return
        if path == "/inner.html":
            self._send(200, INNER_HTML, "text/html; charset=utf-8")
            return
        if path == "/outerauto.html":
            self._send(200, OUTERAUTO_HTML, "text/html; charset=utf-8")
            return
        if path == "/innerauto.html":
            self._send(200, INNERAUTO_HTML, "text/html; charset=utf-8")
            return
        self._json(404, {"code": 1, "msg": "not found"})


LOGIN_HTML = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>登录</title></head>
<body>
<form id="f">
  <input id="user" placeholder="账号"><br>
  <input id="pass" type="password" placeholder="密码"><br>
  <button id="submit" type="submit">登录</button>
</form>
<script>
document.getElementById('f').addEventListener('submit', async (e) => {
  e.preventDefault();
  const r = await fetch('/api/login', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({
      username: document.getElementById('user').value,
      password: document.getElementById('pass').value,
    }),
  });
  const j = await r.json();
  if (j.code === 0) {
    localStorage.setItem('token', j.data.access_token);
    localStorage.setItem('csrf', j.data.csrf);
    location.href = '/stores.html';
  } else {
    alert(j.msg);
  }
});
</script>
</body></html>"""

AUTO_LOGIN_HTML = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>自动登录</title></head>
<body><div id="msg">登录中...</div>
<script>
(async () => {
  const r = await fetch('/api/login', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({username: 'demo', password: '123456'}),
  });
  const j = await r.json();
  localStorage.setItem('token', j.data.access_token);
  localStorage.setItem('csrf', j.data.csrf);
  location.href = '/stores.html';
})();
</script>
</body></html>"""

STORES_HTML = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>门店列表</title>
<style>table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:4px 8px}</style></head>
<body>
<input id="kw" placeholder="关键词">
<button id="search">搜索</button>
<table><thead><tr><th>ID</th><th>门店</th><th>城市</th><th>地址</th></tr></thead>
<tbody id="rows"></tbody></table>
<script>
let all = [];
function render(list) {
  document.getElementById('rows').innerHTML = list.map(s =>
    `<tr><td>${s.id}</td><td>${s.name}</td><td>${s.city}</td><td>${s.address}</td></tr>`).join('');
}
(async () => {
  const r = await fetch('/api/stores?page=1&pageSize=5', {
    headers: {
      'X-CSRF': localStorage.getItem('csrf') || '',
      'Authorization': 'Bearer ' + (localStorage.getItem('token') || ''),
    },
  });
  const j = await r.json();
  all = (j.data && j.data.list) || [];
  render(all);
})();
document.getElementById('search').addEventListener('click', () => {
  const kw = document.getElementById('kw').value.trim();
  render(all.filter(s => !kw || s.city.includes(kw) || s.name.includes(kw)));
});
</script>
</body></html>"""


PAGED_HTML = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>分页门店列表</title>
<style>table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:4px 8px}
#pager{margin-top:10px}#pager button{margin:0 4px}</style></head>
<body>
<table><thead><tr><th>ID</th><th>门店</th><th>城市</th><th>地址</th></tr></thead>
<tbody id="rows"></tbody></table>
<div id="pager">
  <button id="prev" disabled>上一页</button>
  <span id="pageInfo">1</span>
  <button id="next">下一页</button>
</div>
<script>
let page = 1, total = 0;
const size = 2;
function render(list) {
  document.getElementById('rows').innerHTML = list.map(s =>
    `<tr><td>${s.id}</td><td>${s.name}</td><td>${s.city}</td><td>${s.address}</td></tr>`).join('');
}
async function load(p) {
  const r = await fetch('/api/stores?page=' + p + '&pageSize=' + size, {
    headers: {
      'X-CSRF': localStorage.getItem('csrf') || '',
      'Authorization': 'Bearer ' + (localStorage.getItem('token') || ''),
    },
  });
  const j = await r.json();
  const list = (j.data && j.data.list) || [];
  total = (j.data && j.data.total) || 0;
  page = p;
  render(list);
  document.getElementById('pageInfo').textContent = '第 ' + p + ' 页';
  document.getElementById('prev').disabled = p <= 1;
  document.getElementById('next').disabled = p * size >= total;
}
document.getElementById('prev').addEventListener('click', () => { if (page > 1) load(page - 1); });
document.getElementById('next').addEventListener('click', () => {
  if (page * size < total) load(page + 1);
});
load(1);
</script>
</body></html>"""

SPA_HTML = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>SPA 单页（登录态只在内存里）</title>
<style>table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:4px 8px}</style></head>
<body>
<div id="app">loading</div>
<script>
// 登录态只放在内存里：页面一刷新就丢（很多后台管理系统的真实做法）
let token = '', csrf = '';
const app = document.getElementById('app');
function login() {
  app.innerHTML = '<h3>登录</h3><input id="user" placeholder="账号"><br>'
    + '<input id="pass" placeholder="密码"><br><button id="login">登录</button>';
  document.getElementById('login').onclick = function () {
    fetch('/api/login', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({
        username: document.getElementById('user').value,
        password: document.getElementById('pass').value,
      }),
    }).then(function (r) { return r.json(); }).then(function (j) {
      if (j.code !== 0) { alert(j.msg || '登录失败'); return; }
      // 只存在内存里：不写 localStorage / sessionStorage，刷新页面即丢
      token = j.data.access_token;
      csrf = j.data.csrf;
      location.hash = '#/list';
    });
  };
}
function list() {
  fetch('/api/stores?page=1&pageSize=5', {
    headers: {'Authorization': 'Bearer ' + token, 'X-CSRF': csrf},
  })
    .then(function (r) { return r.json(); })
    .then(function (j) {
      app.innerHTML = '<h3>门店列表</h3><table><tbody id="rows"></tbody></table>'
        + '<button id="toDetail">查看详情</button>';
      document.getElementById('rows').innerHTML = (j.data.list || []).map(function (s) {
        return '<tr><td>' + s.id + '</td><td>' + s.name + '</td></tr>';
      }).join('');
      document.getElementById('toDetail').onclick = function () { location.hash = '#/detail'; };
    });
}
function detail() {
  app.innerHTML = '<h3>门店详情</h3><button id="backList">返回列表</button>';
  document.getElementById('backList').onclick = function () { location.hash = '#/list'; };
}
function render() {
  const route = location.hash || '#/login';
  if (!token && route !== '#/login') { location.hash = '#/login'; return; }
  if (route === '#/list') { list(); return; }
  if (route === '#/detail') { detail(); return; }
  login();
}
window.addEventListener('hashchange', render);
window.addEventListener('popstate', render);
render();
</script>
</body></html>"""

PAGED2_HTML = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>分页门店列表（文字按钮）</title>
<style>table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:4px 8px}
.pager{margin-top:10px}.pager button{margin:0 4px}</style></head>
<body>
<table><thead><tr><th>ID</th><th>门店</th><th>城市</th><th>地址</th></tr></thead>
<tbody id="rows"></tbody></table>
<div class="pager">
  <button class="pg-prev" disabled>上一页</button>
  <span id="info">第 1 页</span>
  <button class="pg-next">下一页 ›</button>
</div>
<script>
let page = 1, total = 0;
const size = 2;
function render(list) {
  document.getElementById('rows').innerHTML = list.map(s =>
    `<tr><td>${s.id}</td><td>${s.name}</td><td>${s.city}</td><td>${s.address}</td></tr>`).join('');
}
async function load(p) {
  const r = await fetch('/api/stores?page=' + p + '&pageSize=' + size, {
    headers: {
      'X-CSRF': localStorage.getItem('csrf') || '',
      'Authorization': 'Bearer ' + (localStorage.getItem('token') || ''),
    },
  });
  const j = await r.json();
  total = (j.data && j.data.total) || 0;
  page = p;
  render((j.data && j.data.list) || []);
  document.getElementById('info').textContent = '第 ' + p + ' 页';
  document.querySelector('.pg-prev').disabled = p <= 1;
  document.querySelector('.pg-next').disabled = p * size >= total;
}
document.querySelector('.pg-prev').addEventListener('click', () => { if (page > 1) load(page - 1); });
document.querySelector('.pg-next').addEventListener('click', () => {
  if (page * size < total) load(page + 1);
});
load(1);
</script>
</body></html>"""


OUTER_HTML = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>iframe 外壳页</title></head>
<body style="margin:0;font-family:sans-serif">
<h3 style="margin:6px 10px">外层页面（列表在下面的 iframe 里）</h3>
<iframe id="mainFrame" name="mainFrame" src="/inner.html" style="width:100%;height:560px;border:0"></iframe>
</body></html>"""


INNER_HTML = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>iframe 内登录 + 列表</title>
<style>table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:4px 8px}
#pager{margin-top:10px}#pager button{margin:0 6px}</style></head>
<body>
<div id="loginBox">
  <form id="f">
    <input id="user" placeholder="账号"><br>
    <input id="pass" type="password" placeholder="密码"><br>
    <button id="submit" type="submit">登录</button>
  </form>
</div>
<div id="listBox" style="display:none">
  <table><thead><tr><th>ID</th><th>门店</th><th>城市</th><th>地址</th></tr></thead>
  <tbody id="rows"></tbody></table>
  <div id="pager">
    <button id="prev" disabled>上一页</button>
    <span id="pageInfo">第 1 页</span>
    <button id="next">下一页</button>
  </div>
</div>
<script>
let page = 1, total = 0;
const size = 2;
function render(list) {
  document.getElementById('rows').innerHTML = list.map(s =>
    `<tr><td>${s.id}</td><td>${s.name}</td><td>${s.city}</td><td>${s.address}</td></tr>`).join('');
}
async function load(p) {
  const r = await fetch('/api/stores?page=' + p + '&pageSize=' + size, {
    headers: {
      'X-CSRF': localStorage.getItem('csrf') || '',
      'Authorization': 'Bearer ' + (localStorage.getItem('token') || ''),
    },
  });
  const j = await r.json();
  const list = (j.data && j.data.list) || [];
  total = (j.data && j.data.total) || 0;
  page = p;
  render(list);
  document.getElementById('pageInfo').textContent = '第 ' + p + ' 页';
  document.getElementById('prev').disabled = p <= 1;
  document.getElementById('next').disabled = p * size >= total;
}
document.getElementById('f').addEventListener('submit', async (e) => {
  e.preventDefault();
  const r = await fetch('/api/login', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({
      username: document.getElementById('user').value,
      password: document.getElementById('pass').value,
    }),
  });
  const j = await r.json();
  if (j.code !== 0) { alert(j.msg); return; }
  localStorage.setItem('token', j.data.access_token);
  localStorage.setItem('csrf', j.data.csrf);
  document.getElementById('loginBox').style.display = 'none';
  document.getElementById('listBox').style.display = '';
  await load(1);
});
document.getElementById('prev').addEventListener('click', () => { if (page > 1) load(page - 1); });
document.getElementById('next').addEventListener('click', () => {
  if (page * size < total) load(page + 1);
});
</script>
</body></html>"""


OUTERAUTO_HTML = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>iframe 外壳页（自动）</title></head>
<body style="margin:0;font-family:sans-serif">
<h3 style="margin:6px 10px">外层页面（动作都在 iframe 里自动执行，用于 e2e 录制测试）</h3>
<iframe id="mainFrame" name="mainFrame" src="/innerauto.html" style="width:100%;height:320px;border:0"></iframe>
</body></html>"""


INNERAUTO_HTML = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>iframe 内自动操作</title></head>
<body>
<input id="user" placeholder="账号">
<input id="pass" type="password" placeholder="密码">
<button id="submit" type="button">登录</button>
<div id="ok"></div>
<script>
function fill(id, v) {
  const el = document.getElementById(id);
  el.value = v;
  el.dispatchEvent(new Event('input', { bubbles: true }));
}
// 等录制脚本注入后再动作，模拟「用户在 iframe 里输入并点击」
const waitRec = setInterval(() => {
  if (!window.__recordInstalled) return;
  clearInterval(waitRec);
  setTimeout(() => {
    fill('user', 'demo');
    fill('pass', '123456');
    setTimeout(() => {
      document.getElementById('submit').click();
      document.getElementById('ok').textContent = '已提交';
    }, 400);
  }, 500);
}, 200);
</script>
</body></html>"""


if __name__ == "__main__":
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    print(f"mock site on http://127.0.0.1:{PORT}", flush=True)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    try:
        threading.Event().wait()
    except KeyboardInterrupt:
        srv.shutdown()
