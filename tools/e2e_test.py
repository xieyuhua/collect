"""采集平台三种模式端到端自测脚本（需先启动 tools/mocksite.py 与 collect 服务）。

用法:
  python tools/e2e_test.py --base http://127.0.0.1:18080 --site http://127.0.0.1:18099
"""
import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = "http://127.0.0.1:18080"
SITE = "http://127.0.0.1:18099"
FAILS = []


def call(method, path, body=None, raw=None, ctype="application/json"):
    data = None
    if raw is not None:
        data = raw
    elif body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(BASE + path, data=data, method=method)
    if data is not None:
        req.add_header("Content-Type", ctype)
    try:
        with urllib.request.urlopen(req, timeout=180) as r:
            payload = r.read()
            if "json" in (r.headers.get("Content-Type") or ""):
                return json.loads(payload.decode("utf-8"))
            return payload
    except urllib.error.HTTPError as e:
        return json.loads(e.read().decode("utf-8"))


def check(name, cond, detail=""):
    print(("  [OK]   " if cond else "  [FAIL] ") + name + ((" -> " + str(detail)) if detail and not cond else ""))
    if not cond:
        FAILS.append(name)
    return cond


def create_task(payload):
    r = call("POST", "/api/tasks", payload)
    if r.get("code") != 0:
        raise SystemExit("创建任务失败: " + json.dumps(r, ensure_ascii=False))
    return r["data"]


def run_task(task_id, timeout=240):
    r = call("POST", "/api/tasks/%d/run" % task_id)
    if r.get("code") != 0:
        raise SystemExit("执行任务失败: " + json.dumps(r, ensure_ascii=False))
    run_id = r["data"]["id"]
    deadline = time.time() + timeout
    while time.time() < deadline:
        d = call("GET", "/api/runs/%d" % run_id)["data"]["run"]
        if d["status"] != "running":
            return d
        time.sleep(1.5)
    raise SystemExit("任务 %d 运行超时" % task_id)


def dataset(run_id, limit=50):
    d = call("GET", "/api/runs/%d/data?limit=%d" % (run_id, limit))["data"]
    return d["headers"], d["rows"], d["total"]


def dataset_source(run_id, source, limit=50):
    d = call("GET", "/api/runs/%d/data?limit=%d&source=%s"
             % (run_id, limit, urllib.parse.quote(source)))["data"]
    return d["headers"], d["rows"], d["total"]


def run_sources(run_id):
    d = call("GET", "/api/runs/%d/sources" % run_id)["data"]
    return {s["source"]: s for s in d.get("list", [])}, d.get("total", 0)


def mapping_common():
    return [
        {"source": "id", "target": "id", "type": "int"},
        {"source": "name", "target": "name", "type": "string"},
        {"source": "city", "target": "city", "type": "string"},
        {"source": "sales", "target": "sales", "type": "float"},
    ]


def test_api_mode(site):
    print("\n== 1. 纯接口模式 ==")
    t = create_task({
        "name": "E2E-纯接口采集",
        "mode": "api",
        "enabled": True,
        "notify": True,
        "notify_on": "failure",
        "vars": [{"key": "user", "value": "demo"}, {"key": "pass", "value": "123456"}],
        "http_steps": [
            {
                "name": "登录", "method": "POST", "url": site + "/api/login",
                "body": '{"username":"{{user}}","password":"{{pass}}"}',
                "content_type": "application/json",
                "extract": [
                    {"var": "token", "path": "data.access_token"},
                    {"var": "csrf", "path": "data.csrf"},
                ],
            },
            {
                "name": "门店列表", "method": "GET", "url": site + "/api/stores",
                "headers": [
                    {"key": "Authorization", "value": "Bearer {{token}}"},
                    {"key": "X-CSRF", "value": "{{csrf}}"},
                ],
                "capture": True, "records_path": "data.list", "paginate": True,
            },
        ],
        "pagination": {
            "enabled": True, "type": "page", "page_param": "page", "size_param": "pageSize",
            "page_size": 2, "start_page": 1, "max_pages": 5,
            "records_path": "data.list", "total_path": "data.total",
        },
        "mapping": mapping_common(),
        "dedupe": {"enabled": True, "keys": ["id"]},
        "sink": {"type": "table", "write_mode": "insert", "table": "demo_stores",
                 "batch_size": 200, "auto_create": True, "export_format": "xlsx"},
    })
    run = run_task(t["id"])
    print("  运行结果: status=%s pages=%s fetched=%s inserted=%s duplicated=%s export=%s" % (
        run["status"], run["pages"], run["fetched"], run["inserted"], run["duplicated"], run["export_path"]))
    check("纯接口运行成功", run["status"] == "success", run.get("error"))
    check("分页抓取 3 页", run["pages"] == 3, run["pages"])
    check("抓取 5 条", run["fetched"] == 5, run["fetched"])
    check("首次入库 5 条", run["inserted"] == 5, run["inserted"])
    check("生成表格文件", bool(run.get("export_path")), run.get("export_path"))
    heads, rows, total = dataset(run["id"])
    check("数据集 5 行且字段映射生效",
          total == 5 and heads[:4] == ["id", "name", "city", "sales"], "%s %s" % (total, heads))
    check("数值字段转换", bool(rows) and isinstance(rows[0].get("sales"), (int, float)), rows[:1])
    run2 = run_task(t["id"])
    print("  第二次运行: inserted=%s duplicated=%s" % (run2["inserted"], run2["duplicated"]))
    check("主键去重生效", run2["inserted"] == 0 and run2["duplicated"] == 5,
          "inserted=%s duplicated=%s" % (run2["inserted"], run2["duplicated"]))
    return t


def test_hybrid_mode(site):
    print("\n== 3. UI + 接口（混合模式）==")
    t = create_task({
        "name": "E2E-UI登录+接口采集",
        "mode": "hybrid",
        "enabled": True,
        "login_steps": [
            {"action": "open", "value": site + "/login.html"},
            {"action": "input", "selector": "#user", "value": "demo"},
            {"action": "input", "selector": "#pass", "value": "123456"},
            {"action": "click", "selector": "#submit"},
            {"action": "wait", "ms": 2000},
        ],
        "http_steps": [
            {
                "name": "门店列表", "method": "GET", "url": site + "/api/stores",
                "headers": [{"key": "X-CSRF", "value": "{{csrf}}"}],
                "capture": True, "records_path": "data.list", "paginate": True,
            },
        ],
        "capture": {"mode": "network", "url_contains": site + "/api/stores"},
        "pagination": {
            "enabled": True, "type": "page", "page_param": "page", "size_param": "pageSize",
            "page_size": 2, "start_page": 1, "max_pages": 5,
            "records_path": "data.list", "total_path": "data.total",
        },
        "mapping": mapping_common(),
        "dedupe": {"enabled": True, "keys": ["id"]},
        "sink": {"type": "table", "write_mode": "insert", "table": "demo_hybrid",
                 "batch_size": 200, "auto_create": True, "export_format": "xlsx"},
    })
    run = run_task(t["id"])
    print("  运行结果: status=%s pages=%s fetched=%s inserted=%s error=%s" % (
        run["status"], run["pages"], run["fetched"], run["inserted"], run["error"]))
    check("混合模式运行成功（Cookie 会话 + 存储变量）", run["status"] == "success", run.get("error"))
    check("混合模式分页 3 页", run["pages"] == 3, run["pages"])
    check("混合模式抓取 5 条", run["fetched"] == 5, run["fetched"])
    steps = call("GET", "/api/runs/%d/steps" % run["id"])["data"]
    ui_steps = [s for s in steps if s["kind"] == "ui"]
    check("记录 UI 登录步骤日志", len(ui_steps) >= 3, len(ui_steps))
    return t


def test_ui_mode(site):
    print("\n== 2. 纯 UI 模式（网络捕获）==")
    login_steps = [
        {"action": "open", "value": site + "/login.html"},
        {"action": "input", "selector": "#user", "value": "demo"},
        {"action": "input", "selector": "#pass", "value": "123456"},
        {"action": "click", "selector": "#submit"},
        {"action": "wait_for", "selector": "#rows tr"},
    ]
    t = create_task({
        "name": "E2E-纯UI网络捕获",
        "mode": "ui",
        "enabled": True,
        "ui_steps": login_steps,
        "capture": {"mode": "network", "url_contains": "/api/stores", "method": "GET",
                    "records_path": "data.list", "wait_ms": 1500},
        "mapping": mapping_common(),
        "dedupe": {"enabled": True, "keys": ["id"]},
        "sink": {"type": "table", "write_mode": "insert", "table": "demo_ui",
                 "batch_size": 200, "auto_create": True, "export_format": "xlsx"},
    })
    run = run_task(t["id"])
    print("  运行结果: status=%s fetched=%s inserted=%s msg=%s error=%s" % (
        run["status"], run["fetched"], run["inserted"], run["message"], run["error"]))
    check("纯 UI 网络捕获成功", run["status"] == "success", run.get("error"))
    check("UI 模式抓到 5 条", run["fetched"] == 5, run["fetched"])

    print("\n== 2b. 纯 UI 模式（DOM 表格提取）==")
    t2 = create_task({
        "name": "E2E-纯UI-DOM提取",
        "mode": "ui",
        "enabled": True,
        "ui_steps": login_steps + [
            {"action": "input", "selector": "#kw", "value": "北京"},
            {"action": "click", "selector": "#search"},
            {"action": "wait", "ms": 500},
        ],
        "capture": {"mode": "dom", "row_selector": "#rows tr", "wait_ms": 1000},
        "mapping": [
            {"source": "td:nth-child(1)", "target": "code", "type": "string"},
            {"source": "td:nth-child(2)", "target": "shop", "type": "string"},
            {"source": "td:nth-child(3)", "target": "city", "type": "string"},
            {"source": "td:nth-child(4)", "target": "addr", "type": "string"},
        ],
        "sink": {"type": "table", "write_mode": "insert", "batch_size": 200,
                 "auto_create": True, "export_format": "xlsx"},
    })
    run2 = run_task(t2["id"])
    heads, rows, total = dataset(run2["id"])
    print("  运行结果: status=%s total=%s rows=%s msg=%s" % (run2["status"], total, rows[:2], run2["message"]))
    check("DOM 提取成功", run2["status"] == "success" and total == 1, run2.get("error"))
    check("DOM 字段映射正确", bool(rows) and rows[0].get("shop") == "测试门店1", rows[:1])
    return t


def test_multi_sink(site):
    print("\n== 7. 多接口独立落库 / 单接口关闭落库 ==")
    tag = time.strftime("%H%M%S")
    r = call("POST", "/api/datasources", {"name": "E2E-Multi-" + tag, "type": "sqlite",
                                          "database": "e2e_multi.sqlite"})
    check("创建多源 SQLite 数据源", r.get("code") == 0, r.get("msg"))
    ds_id = r["data"]["id"]
    ta, tb = "multi_a_" + tag, "multi_b_" + tag
    for tbl, ddl in ((ta, "id INTEGER PRIMARY KEY, name TEXT, city TEXT, sales REAL"),
                     (tb, "id INTEGER PRIMARY KEY, name TEXT")):
        q = exec_sql(ds_id, "CREATE TABLE IF NOT EXISTS %s (%s)" % (tbl, ddl))
        check("建表 " + tbl, q.get("code") == 0, q.get("msg"))

    task = create_task({
        "name": "E2E-多接口独立落库-" + tag,
        "mode": "api", "enabled": True,
        "http_steps": [
            {"name": "登录", "method": "POST", "url": site + "/api/login",
             "body": '{"username":"demo","password":"123456"}',
             "content_type": "application/json",
             "extract": [
                 {"var": "token", "path": "data.access_token"},
                 {"var": "csrf", "path": "data.csrf"},
             ]},
            {"name": "门店列表", "method": "GET", "url": site + "/api/stores",
             "headers": [{"key": "Authorization", "value": "Bearer {{token}}"},
                         {"key": "X-CSRF", "value": "{{csrf}}"}],
             "capture": True, "records_path": "data.list", "paginate": True,
             "mapping": mapping_common(),
             "sink": {"type": "db", "data_source_id": ds_id, "table": ta,
                      "write_mode": "upsert", "key_columns": ["id"],
                      "batch_size": 100, "auto_create": True}},
            {"name": "门店首页", "method": "GET", "url": site + "/api/stores?page=1&pageSize=2",
             "headers": [{"key": "Authorization", "value": "Bearer {{token}}"},
                         {"key": "X-CSRF", "value": "{{csrf}}"}],
             "capture": True, "records_path": "data.list",
             "mapping": [{"source": "id", "target": "id", "type": "int"},
                         {"source": "name", "target": "name", "type": "string"}],
             "sink": {"type": "db", "data_source_id": ds_id, "table": tb,
                      "write_mode": "insert", "batch_size": 100, "auto_create": True}},
            {"name": "仅预览接口", "method": "GET", "url": site + "/api/stores?page=1&pageSize=2",
             "headers": [{"key": "Authorization", "value": "Bearer {{token}}"},
                         {"key": "X-CSRF", "value": "{{csrf}}"}],
             "capture": True, "records_path": "data.list",
             "pagination": {"enabled": False}, "sink": {"type": "none"}},
        ],
        "pagination": {
            "enabled": True, "type": "page", "page_param": "page", "size_param": "pageSize",
            "page_size": 2, "start_page": 1, "max_pages": 5,
            "records_path": "data.list", "total_path": "data.total",
        },
        "mapping": mapping_common(),
        "sink": {"type": "none"},
    })
    run = run_task(task["id"], timeout=120)
    print("  运行结果: status=%s pages=%s fetched=%s inserted=%s msg=%s error=%s" % (
        run["status"], run["pages"], run["fetched"], run["inserted"], run.get("message"), run.get("error")))
    check("多接口任务运行成功", run["status"] == "success", run.get("error"))

    srcs, total = run_sources(run["id"])
    print("  数据来源: %s" % srcs)
    check("按接口分组出 3 个来源", set(srcs) == {"step:2", "step:3", "step:4"}, sorted(srcs))
    check("运行数据总计 9 条", total == 9, total)
    _, _, n2 = dataset_source(run["id"], "step:2")
    _, rows3, n3 = dataset_source(run["id"], "step:3")
    _, _, n4 = dataset_source(run["id"], "step:4")
    check("接口2 分页抓取 5 条", n2 == 5, n2)
    check("接口3 单页抓取 2 条", n3 == 2, n3)
    check("接口4 关闭落库但保留预览", n4 == 2, n4)
    check("接口级字段映射生效", bool(rows3) and set(rows3[0]) == {"id", "name"}, rows3[:1])

    q = exec_sql(ds_id, "SELECT COUNT(*) AS n FROM " + ta)
    na = q["data"]["rows"][0][0] if q.get("code") == 0 and q["data"]["rows"] else -1
    q = exec_sql(ds_id, "SELECT COUNT(*) AS n FROM " + tb)
    nb = q["data"]["rows"][0][0] if q.get("code") == 0 and q["data"]["rows"] else -1
    check("接口2 写入表 A 5 行", int(na) == 5, na)
    check("接口3 写入表 B 2 行", int(nb) == 2, nb)

    raw = call("GET", "/api/runs/%d/export?format=csv&source=%s"
               % (run["id"], urllib.parse.quote("step:3")))
    check("按来源导出 CSV", isinstance(raw, (bytes, bytearray)) and b"name" in raw, len(raw) if raw else 0)

    steps = call("GET", "/api/runs/%d/steps" % run["id"])["data"]
    sink_steps = [s for s in steps if s["kind"] == "sink"]
    check("每个落库来源各一条落库日志", len(sink_steps) == 2, [s["name"] for s in sink_steps])

    for tid in (task["id"],):
        call("DELETE", "/api/tasks/%d" % tid)
    call("DELETE", "/api/datasources/%d" % ds_id)


def test_ui_multi_rule(site):
    print("\n== 8. 纯 UI 模式多抓取规则 ==")
    tag = time.strftime("%H%M%S")
    t = create_task({
        "name": "E2E-UI多规则-" + tag,
        "mode": "ui", "enabled": True,
        "ui_steps": [
            {"action": "open", "value": site + "/login.html"},
            {"action": "input", "selector": "#user", "value": "demo"},
            {"action": "input", "selector": "#pass", "value": "123456"},
            {"action": "click", "selector": "#submit"},
            {"action": "wait_for", "selector": "#rows tr"},
        ],
        "capture": {
            "wait_ms": 1200,
            "rules": [
                {"name": "门店接口", "mode": "network", "url_contains": "/api/stores",
                 "method": "GET", "records_path": "data.list",
                 "mapping": mapping_common(), "sink": {"type": "none"}},
                {"name": "页面表格", "mode": "dom", "row_selector": "#rows tr",
                 "mapping": [
                     {"source": "td:nth-child(1)", "target": "code", "type": "string"},
                     {"source": "td:nth-child(2)", "target": "shop", "type": "string"},
                 ],
                 "sink": {"type": "table", "export_format": "xlsx"}},
            ],
        },
        "sink": {"type": "none"},
    })
    run = run_task(t["id"])
    print("  运行结果: status=%s fetched=%s msg=%s error=%s" % (
        run["status"], run["fetched"], run.get("message"), run.get("error")))
    check("UI 多规则运行成功", run["status"] == "success", run.get("error"))
    srcs, total = run_sources(run["id"])
    print("  数据来源: %s" % srcs)
    check("UI 多规则来源分组", set(srcs) == {"cap:1", "dom:2"}, sorted(srcs))
    _, _, nc = dataset_source(run["id"], "cap:1")
    _, rowsd, nd = dataset_source(run["id"], "dom:2")
    check("网络规则抓到 5 条", nc == 5, nc)
    check("DOM 规则抓到 5 行", nd == 5, nd)
    check("DOM 规则独立字段映射", bool(rowsd) and "shop" in rowsd[0], rowsd[:1])

    steps = call("GET", "/api/runs/%d/steps" % run["id"])["data"]
    ui_idx = [i for i, s in enumerate(steps) if s["kind"] == "ui"]
    cap_idx = [i for i, s in enumerate(steps) if s["name"].startswith("捕获")]
    print("  步骤顺序: %s" % [s["name"] for s in steps])
    interleaved = (len(ui_idx) >= 5 and len(cap_idx) >= 1
                   and cap_idx[0] > ui_idx[0] and cap_idx[0] < ui_idx[-1])
    check("UI 操作与接口日志按时间交错", interleaved,
          "ui=%s capture=%s" % (ui_idx, cap_idx))
    call("DELETE", "/api/tasks/%d" % t["id"])


def test_ui_click_pagination(site):
    print("\n== 8b. 纯 UI 模式：点击「下一页」翻页（网络 + DOM）==")
    login_steps = [
        {"action": "open", "value": site + "/login.html"},
        {"action": "input", "selector": "#user", "value": "demo"},
        {"action": "input", "selector": "#pass", "value": "123456"},
        {"action": "click", "selector": "#submit"},
        {"action": "wait_for", "selector": "#rows tr"},
    ]
    pg_ui = {"enabled": True, "mode": "ui", "next_selector": "#next", "next_text": "下一页",
             "max_pages": 10, "wait_ms": 6000, "delay_ms": 0, "stop_when_empty": True}
    paged_steps = login_steps + [
        {"action": "open", "value": site + "/paged.html"},
        {"action": "wait_for", "selector": "#rows tr"},
    ]

    # (1) 网络规则：真实点下一页，抓每页接口响应
    t = create_task({
        "name": "E2E-UI点击翻页-网络",
        "mode": "ui", "enabled": True,
        "ui_steps": paged_steps,
        "capture": {
            "wait_ms": 1200,
            "rules": [{
                "name": "门店接口", "mode": "network", "url_contains": "/api/stores",
                "method": "GET", "records_path": "data.list",
                "mapping": mapping_common(),
                "pagination": dict(pg_ui),
                "sink": {"type": "table", "write_mode": "insert", "table": "demo_ui_click",
                         "key_columns": ["id"], "batch_size": 200, "auto_create": True,
                         "export_format": "xlsx"},
            }],
        },
        "dedupe": {"enabled": True, "keys": ["id"]},
        "sink": {"type": "none"},
    })
    run = run_task(t["id"])
    print("  运行结果: status=%s pages=%s fetched=%s inserted=%s msg=%s error=%s" % (
        run["status"], run.get("pages"), run["fetched"], run["inserted"],
        run.get("message"), run.get("error")))
    check("点击翻页（网络）运行成功", run["status"] == "success", run.get("error"))
    check("点击翻页抓到 3 页", run.get("pages") == 3, run.get("pages"))
    check("点击翻页抓到 5 条", run["fetched"] == 5, run["fetched"])
    steps = call("GET", "/api/runs/%d/steps" % run["id"])["data"]
    clicks = [s for s in steps if s["name"].startswith("UI 翻页")]
    print("  翻页步骤: %s" % [s["name"] for s in clicks])
    check("记录点击翻页步骤日志", len(clicks) == 2, len(clicks))
    call("DELETE", "/api/tasks/%d" % t["id"])

    # (2) DOM 规则：真实点下一页，每页重新读表格
    t2 = create_task({
        "name": "E2E-UI点击翻页-DOM",
        "mode": "ui", "enabled": True,
        "ui_steps": paged_steps,
        "capture": {
            "wait_ms": 1200,
            "rules": [{
                "name": "页面表格", "mode": "dom", "row_selector": "#rows tr",
                "mapping": [
                    {"source": "td:nth-child(1)", "target": "code", "type": "string"},
                    {"source": "td:nth-child(2)", "target": "shop", "type": "string"},
                    {"source": "td:nth-child(3)", "target": "city", "type": "string"},
                ],
                "pagination": dict(pg_ui),
                "sink": {"type": "none"},
            }],
        },
        "sink": {"type": "none"},
    })
    run2 = run_task(t2["id"])
    _, rows2, total2 = dataset(run2["id"])
    print("  运行结果: status=%s pages=%s total=%s msg=%s error=%s" % (
        run2["status"], run2.get("pages"), total2, run2.get("message"), run2.get("error")))
    check("点击翻页（DOM）运行成功", run2["status"] == "success", run2.get("error"))
    check("点击翻页（DOM）共 3 页", run2.get("pages") == 3, run2.get("pages"))
    check("点击翻页（DOM）抓到 5 行", total2 == 5, total2)
    check("点击翻页（DOM）字段映射正确", bool(rows2) and rows2[0].get("shop") == "测试门店1", rows2[:1])
    call("DELETE", "/api/tasks/%d" % t2["id"])


def test_ui_click_text_pagination(site):
    print("\n== 8c. 纯 UI：只按按钮文字点击翻页（选择器留空）==")
    steps = [
        {"action": "open", "value": site + "/login.html"},
        {"action": "input", "selector": "#user", "value": "demo"},
        {"action": "input", "selector": "#pass", "value": "123456"},
        {"action": "click", "selector": "#submit"},
        {"action": "wait_for", "selector": "#rows tr"},
        {"action": "open", "value": site + "/paged2.html"},
        {"action": "wait_for", "selector": "#rows tr"},
    ]
    t = create_task({
        "name": "E2E-UI点击翻页-文字匹配",
        "mode": "ui", "enabled": True,
        "ui_steps": steps,
        "capture": {
            "wait_ms": 1200,
            "rules": [{
                "name": "门店接口", "mode": "network", "url_contains": "/api/stores",
                "method": "GET", "records_path": "data.list",
                "mapping": mapping_common(),
                "pagination": {"enabled": True, "mode": "ui", "next_selector": "", "next_text": "下一页",
                               "max_pages": 10, "wait_ms": 6000, "stop_when_empty": True},
                "sink": {"type": "none"},
            }],
        },
        "dedupe": {"enabled": True, "keys": ["id"]},
        "sink": {"type": "none"},
    })
    run = run_task(t["id"])
    print("  运行结果: status=%s pages=%s fetched=%s msg=%s error=%s" % (
        run["status"], run.get("pages"), run["fetched"], run.get("message"), run.get("error")))
    check("文字匹配点击翻页运行成功", run["status"] == "success", run.get("error"))
    check("文字匹配点击翻页抓到 3 页", run.get("pages") == 3, run.get("pages"))
    check("文字匹配点击翻页抓到 5 条", run["fetched"] == 5, run["fetched"])
    call("DELETE", "/api/tasks/%d" % t["id"])


def test_ui_iframe(site):
    print("\n== 8d. 纯 UI：iframe 内登录 + 列表 + 点击翻页 ==")
    IF = "#mainFrame"
    steps = [
        {"action": "open", "value": site + "/outer.html"},
        {"action": "wait_for", "selector": "#user", "frame": IF},
        {"action": "input", "selector": "#user", "value": "demo", "frame": IF},
        {"action": "input", "selector": "#pass", "value": "123456", "frame": IF},
        {"action": "click", "selector": "#submit", "frame": IF},
        {"action": "wait_for", "selector": "#next", "frame": IF},
    ]
    pg = {"enabled": True, "mode": "ui", "next_selector": "#next", "next_text": "下一页",
          "max_pages": 10, "wait_ms": 8000, "delay_ms": 0, "stop_when_empty": True,
          "frame": IF}

    # (1) iframe 内接口 + 点击翻页
    t = create_task({
        "name": "E2E-UI-iframe-网络",
        "mode": "ui", "enabled": True,
        "ui_steps": steps,
        "capture": {
            "wait_ms": 1500,
            "rules": [{
                "name": "门店接口", "mode": "network", "url_contains": "/api/stores",
                "method": "GET", "records_path": "data.list",
                "mapping": mapping_common(),
                "pagination": dict(pg),
                "sink": {"type": "table", "write_mode": "insert", "table": "demo_iframe",
                         "key_columns": ["id"], "batch_size": 200, "auto_create": True,
                         "export_format": "xlsx"},
            }],
        },
        "dedupe": {"enabled": True, "keys": ["id"]},
        "sink": {"type": "none"},
    })
    run = run_task(t["id"])
    print("  运行结果: status=%s pages=%s fetched=%s msg=%s error=%s" % (
        run["status"], run.get("pages"), run["fetched"], run.get("message"), run.get("error")))
    check("iframe 网络规则运行成功", run["status"] == "success", run.get("error"))
    check("iframe 内抓到 3 页", run.get("pages") == 3, run.get("pages"))
    check("iframe 内抓到 5 条", run["fetched"] == 5, run["fetched"])
    st = call("GET", "/api/runs/%d/steps" % run["id"])["data"]
    ui_ok = [s for s in st if s["name"].startswith("UI-") and s["status"] == "success"]
    check("iframe 内 UI 步骤全部成功", len(ui_ok) == len(steps), len(ui_ok))
    statuses = {s["name"]: s["status"] for s in st if s["name"].startswith("UI-")}
    print("  UI 步骤状态: %s" % statuses)
    call("DELETE", "/api/tasks/%d" % t["id"])

    # (2) iframe 内表格 DOM + 点击翻页
    t2 = create_task({
        "name": "E2E-UI-iframe-DOM",
        "mode": "ui", "enabled": True,
        "ui_steps": steps,
        "capture": {
            "wait_ms": 1500,
            "rules": [{
                "name": "页面表格", "mode": "dom", "row_selector": "#rows tr", "frame": IF,
                "mapping": [
                    {"source": "td:nth-child(1)", "target": "code", "type": "string"},
                    {"source": "td:nth-child(2)", "target": "shop", "type": "string"},
                    {"source": "td:nth-child(3)", "target": "city", "type": "string"},
                ],
                "pagination": dict(pg),
                "sink": {"type": "none"},
            }],
        },
        "sink": {"type": "none"},
    })
    run2 = run_task(t2["id"])
    _, rows2, total2 = dataset(run2["id"])
    print("  运行结果: status=%s pages=%s total=%s msg=%s error=%s" % (
        run2["status"], run2.get("pages"), total2, run2.get("message"), run2.get("error")))
    check("iframe DOM 规则运行成功", run2["status"] == "success", run2.get("error"))
    check("iframe DOM 翻页 3 页", run2.get("pages") == 3, run2.get("pages"))
    check("iframe DOM 共 5 行", total2 == 5, total2)
    check("iframe DOM 字段映射正确", bool(rows2) and rows2[0].get("shop") == "测试门店1", rows2[:1])
    call("DELETE", "/api/tasks/%d" % t2["id"])

    # (3) 老任务兼容：UI 步骤 / 翻页 / DOM 规则都不填 frame，应自动识别 iframe
    steps_auto = [
        {"action": "open", "value": site + "/outer.html"},
        {"action": "wait_for", "selector": "#user"},
        {"action": "input", "selector": "#user", "value": "demo"},
        {"action": "input", "selector": "#pass", "value": "123456"},
        {"action": "click", "selector": "#submit"},
        {"action": "wait_for", "selector": "#next"},
    ]
    pg_auto = dict(pg)
    pg_auto.pop("frame", None)
    t3 = create_task({
        "name": "E2E-UI-iframe-自动识别",
        "mode": "ui", "enabled": True,
        "ui_steps": steps_auto,
        "capture": {
            "wait_ms": 1500,
            "rules": [{
                "name": "门店接口", "mode": "network", "url_contains": "/api/stores",
                "method": "GET", "records_path": "data.list",
                "mapping": mapping_common(),
                "pagination": dict(pg_auto),
                "sink": {"type": "none"},
            }],
        },
        "dedupe": {"enabled": True, "keys": ["id"]},
        "sink": {"type": "none"},
    })
    run3 = run_task(t3["id"])
    print("  自动识别 iframe 运行结果: status=%s pages=%s fetched=%s msg=%s error=%s" % (
        run3["status"], run3.get("pages"), run3["fetched"], run3.get("message"), run3.get("error")))
    check("不填 frame 也能自动识别 iframe", run3["status"] == "success", run3.get("error"))
    check("自动识别后抓到 3 页", run3.get("pages") == 3, run3.get("pages"))
    check("自动识别后抓到 5 条", run3["fetched"] == 5, run3["fetched"])
    call("DELETE", "/api/tasks/%d" % t3["id"])

    t4 = create_task({
        "name": "E2E-UI-iframe-DOM-自动识别",
        "mode": "ui", "enabled": True,
        "ui_steps": steps_auto,
        "capture": {
            "wait_ms": 1500,
            "rules": [{
                "name": "页面表格", "mode": "dom", "row_selector": "#rows tr",
                "mapping": [
                    {"source": "td:nth-child(1)", "target": "code", "type": "string"},
                    {"source": "td:nth-child(2)", "target": "shop", "type": "string"},
                    {"source": "td:nth-child(3)", "target": "city", "type": "string"},
                ],
                "pagination": dict(pg_auto),
                "sink": {"type": "none"},
            }],
        },
        "sink": {"type": "none"},
    })
    run4 = run_task(t4["id"])
    _, rows4, total4 = dataset(run4["id"])
    print("  自动识别 DOM 运行结果: status=%s pages=%s total=%s msg=%s error=%s" % (
        run4["status"], run4.get("pages"), total4, run4.get("message"), run4.get("error")))
    check("DOM 不填 frame 也能自动识别", run4["status"] == "success", run4.get("error"))
    check("DOM 自动识别翻 3 页", run4.get("pages") == 3, run4.get("pages"))
    check("DOM 自动识别共 5 行", total4 == 5, total4)
    call("DELETE", "/api/tasks/%d" % t4["id"])


def test_studio_multi(site):
    print("\n== 9. 录制工作台：多接口草稿 ==")
    r = call("POST", "/api/studio/sessions", {"url": site + "/autologin.html", "mode": "api"})
    if r.get("code") != 0 or not (r.get("data") or {}).get("id"):
        check("录制会话已启动", False, r)
        return
    sid = r["data"]["id"]
    check("录制会话已启动", True)
    try:
        cands = []
        for _ in range(20):
            time.sleep(1.5)
            d = call("GET", "/api/studio/sessions/%s/capture" % sid).get("data") or {}
            cands = d.get("candidates") or []
            if cands:
                break
        print("  数据接口候选: %s" % [(c.get("method"), c.get("url"), c.get("rows")) for c in cands])
        check("自动识别数据接口候选", len(cands) >= 1, cands)
        ids = [c["id"] for c in cands[:1]]
        if ids:
            draft = call("POST", "/api/studio/sessions/%s/draft" % sid,
                         {"mode": "api", "list_req_ids": ids}).get("data") or {}
            cap_steps = [s for s in (draft.get("http_steps") or []) if s.get("capture")]
            check("纯接口草稿生成抓取步骤", len(cap_steps) == 1, draft.get("http_steps"))
            draft_ui = call("POST", "/api/studio/sessions/%s/draft" % sid,
                            {"mode": "ui", "list_req_ids": ids}).get("data") or {}
            rules = (draft_ui.get("capture") or {}).get("rules") or []
            check("纯 UI 草稿生成抓取规则", len(rules) == 1 and bool(rules[0].get("records_path")), rules)
            check("纯 UI 草稿生成 UI 步骤", len(draft_ui.get("ui_steps") or []) >= 1,
                  draft_ui.get("ui_steps"))
    finally:
        call("DELETE", "/api/studio/sessions/" + sid)


def test_studio_iframe(site):
    """录制 iframe 内嵌页面：动作应自动带上 iframe 选择器，草稿 UI 步骤要保留它。"""
    print("\n== 11. 录制工作台：iframe 内动作录制 ==")
    r = call("POST", "/api/studio/sessions", {"url": site + "/outerauto.html", "mode": "ui"})
    if r.get("code") != 0 or not (r.get("data") or {}).get("id"):
        check("iframe 录制会话已启动", False, r)
        return
    sid = r["data"]["id"]
    check("iframe 录制会话已启动", True)
    try:
        acts = []
        for _ in range(12):
            time.sleep(1.5)
            d = call("GET", "/api/studio/sessions/%s/capture" % sid).get("data") or {}
            acts = d.get("actions") or []
            framed = [a for a in acts if (a.get("frame") or "").strip()]
            if len(framed) >= 2:
                break
        framed = [a for a in acts if (a.get("frame") or "").strip()]
        print("  录制动作: %s" % [(a.get("type"), a.get("selector"), a.get("frame")) for a in acts])
        check("iframe 内动作自动带上 iframe 选择器", len(framed) >= 2, acts)
        check("iframe 选择器指向 #mainFrame",
              bool(framed) and all("#mainFrame" in (a.get("frame") or "") for a in framed), acts)
        draft = call("POST", "/api/studio/sessions/%s/draft" % sid, {"mode": "ui"}).get("data") or {}
        ui_steps = draft.get("ui_steps") or []
        framed_steps = [s for s in ui_steps if (s.get("frame") or "").strip()]
        check("草稿 UI 步骤保留 iframe 选择器", len(framed_steps) >= 2, ui_steps)
    finally:
        call("DELETE", "/api/studio/sessions/" + sid)


def test_scheduler(site):
    print("\n== 4. 定时调度（每 6 秒）==")
    t = create_task({
        "name": "E2E-定时任务",
        "mode": "api",
        "enabled": True,
        "schedule_type": "interval",
        "interval_sec": 6,
        "http_steps": [{
            "name": "门店列表", "method": "GET", "url": site + "/api/stores?page=1&pageSize=2",
            "headers": [
                {"key": "Authorization", "value": "Bearer tok-demo-9f8e7d6c5b4a3210"},
                {"key": "X-CSRF", "value": "csrf-demo-556677"},
            ],
            "capture": True, "records_path": "data.list",
        }],
        "mapping": mapping_common(),
        "sink": {"type": "table", "write_mode": "insert", "batch_size": 200,
                 "auto_create": True, "export_format": "xlsx"},
    })
    check("调度已注册", bool(t.get("next_run")), t.get("next_run"))
    print("  下次执行: %s" % t.get("next_run"))
    time.sleep(16)
    runs = call("GET", "/api/runs?task_id=%d" % t["id"])["data"]["list"]
    cron_runs = [r for r in runs if r["trigger"] == "cron"]
    print("  定时触发次数: %d" % len(cron_runs))
    check("定时触发成功", len(cron_runs) >= 1, len(cron_runs))
    m = dict(t)
    m["enabled"] = False
    call("PUT", "/api/tasks/%d" % t["id"], m)
    time.sleep(1)
    after = call("GET", "/api/tasks/%d" % t["id"])["data"]
    check("停用后移除调度", not after.get("next_run"), after.get("next_run"))


def exec_sql(ds_id, sql):
    return call("POST", "/api/datasources/%d/exec" % ds_id, {"sql": sql, "limit": 200})


def test_sql_sink(site):
    print("\n== 6. 自定义 SQL 落库（占位符 / 变量替换）==")
    tag = time.strftime("%H%M%S")
    r = call("POST", "/api/datasources", {"name": "E2E-SQLite-" + tag, "type": "sqlite",
                                          "database": "e2e_sqlsink.sqlite"})
    check("创建 SQLite 数据源", r.get("code") == 0, r.get("msg"))
    ds_id = r["data"]["id"]

    t1 = "store_sql_" + tag
    t2 = "store_sql_native_" + tag
    for tbl, ddl in ((t1, "id INTEGER PRIMARY KEY, name TEXT, city TEXT, sales REAL, src TEXT, updated_at TEXT"),
                     (t2, "id INTEGER PRIMARY KEY, name TEXT, city TEXT")):
        r = exec_sql(ds_id, "CREATE TABLE IF NOT EXISTS %s (%s)" % (tbl, ddl))
        check("执行建表 SQL %s" % tbl, r.get("code") == 0, r.get("msg"))

    def base_task(name, mapping, sink):
        return {
            "name": name, "mode": "api", "enabled": True,
            "vars": [{"key": "src_tag", "value": "e2e-sql"}],
            "http_steps": [{
                "name": "门店列表", "method": "GET",
                "url": site + "/api/stores?page=1&pageSize=10",
                "headers": [
                    {"key": "Authorization", "value": "Bearer tok-demo-9f8e7d6c5b4a3210"},
                    {"key": "X-CSRF", "value": "csrf-demo-556677"},
                ],
                "capture": True, "records_path": "data.list",
            }],
            "mapping": mapping,
            "dedupe": {"enabled": True, "keys": ["id"]},
            "sink": sink,
        }

    # 1) {{字段}} 绑定 + {{变量}} 内联
    tpl = ("INSERT INTO %s (id, name, city, sales, src, updated_at) "
           "VALUES ({{id}}, {{name}}, {{city}}, {{sales}}, '{{src_tag}}', '{{now}}')" % t1)
    task = create_task(base_task("E2E-SQL落库-" + tag, mapping_common(), {
        "type": "db", "data_source_id": ds_id, "write_mode": "sql",
        "batch_size": 100, "sql_text": tpl,
    }))
    run = run_task(task["id"], timeout=120)
    print("  运行结果: status=%s inserted=%s error=%s" % (run["status"], run["inserted"], run.get("error")))
    check("SQL 落库运行成功", run["status"] == "success", run.get("error"))
    check("SQL 落库写入 5 条", run["inserted"] == 5, run["inserted"])

    q = exec_sql(ds_id, "SELECT COUNT(*) AS n FROM " + t1)
    n = q["data"]["rows"][0][0] if q.get("code") == 0 and q["data"]["rows"] else -1
    check("目标表实际 5 行", int(n) == 5, n)
    q = exec_sql(ds_id, "SELECT DISTINCT src FROM " + t1)
    src = q["data"]["rows"][0][0] if q.get("code") == 0 and q["data"]["rows"] else None
    check("{{变量}} 内联生效", src == "e2e-sql", src)
    q = exec_sql(ds_id, "SELECT updated_at FROM " + t1 + " LIMIT 1")
    upd = q["data"]["rows"][0][0] if q.get("code") == 0 and q["data"]["rows"] else ""
    check("{{now}} 内联生效", bool(upd) and str(upd)[:4].isdigit(), upd)

    steps = call("GET", "/api/runs/%d/steps" % run["id"])["data"]
    sink_steps = [s for s in steps if s["kind"] == "sink"]
    check("记录落库步骤日志", len(sink_steps) == 1 and sink_steps[0]["status"] == "success", sink_steps)

    # 2) 原生占位符 ? 按字段映射顺序绑定
    task2 = create_task(base_task("E2E-SQL原生占位符-" + tag, [
        {"source": "id", "target": "id", "type": "int"},
        {"source": "name", "target": "name", "type": "string"},
        {"source": "city", "target": "city", "type": "string"},
    ], {
        "type": "both", "data_source_id": ds_id, "write_mode": "sql",
        "batch_size": 100, "sql_text": "INSERT INTO %s (id, name, city) VALUES (?, ?, ?)" % t2,
        "export_format": "xlsx",
    }))
    run2 = run_task(task2["id"], timeout=120)
    check("原生占位符运行成功", run2["status"] == "success", run2.get("error"))
    q = exec_sql(ds_id, "SELECT COUNT(*) AS n FROM " + t2)
    n2 = q["data"]["rows"][0][0] if q.get("code") == 0 and q["data"]["rows"] else -1
    check("原生占位符写入 5 行", int(n2) == 5, n2)
    check("SQL 模式仍可导出表格", bool(run2.get("export_path")), run2.get("export_path"))

    # 3) 模板写错字段名要给出明确错误
    task3 = create_task(base_task("E2E-SQL错误模板-" + tag, mapping_common(), {
        "type": "db", "data_source_id": ds_id, "write_mode": "sql",
        "batch_size": 100, "sql_text": "INSERT INTO %s (id) VALUES ({{not_exists}})" % t1,
    }))
    run3 = run_task(task3["id"], timeout=120)
    err = (run3.get("error") or "") + (run3.get("message") or "")
    check("未知字段报错可读", run3["status"] in ("failed", "partial") and "not_exists" in err, err[:120])
    for tid in (task["id"], task2["id"], task3["id"]):
        call("DELETE", "/api/tasks/%d" % tid)
    call("DELETE", "/api/datasources/%d" % ds_id)


def wait_job_run(run_id, timeout=120):
    deadline = time.time() + timeout
    while time.time() < deadline:
        d = call("GET", "/api/job-runs/%d" % run_id)["data"]
        if d["run"]["status"] != "running":
            return d
        time.sleep(1)
    raise SystemExit("作业 %d 执行超时" % run_id)


def test_jobs(site):
    print("\n== 10. 定时作业（HTTP / Shell / SQL 变量链式）==")
    tag = time.strftime("%H%M%S")
    r = call("POST", "/api/datasources", {"name": "E2E-JobSQLite-" + tag, "type": "sqlite",
                                          "database": "e2e_jobs.sqlite"})
    check("创建作业用 SQLite 数据源", r.get("code") == 0, r.get("msg"))
    ds_id = r["data"]["id"]
    table = "job_chain_" + tag

    job = {
        "name": "E2E-变量链式作业-" + tag,
        "enabled": False,
        "notify": False,
        "schedule_type": "manual",
        "timeout_sec": 60,
        "steps": [
            {"name": "登录", "type": "http", "method": "POST", "url": site + "/api/login",
             "content_type": "application/json",
             "body": json.dumps({"username": "demo", "password": "123456"}),
             "extract": [{"key": "token", "value": "data.data.access_token"},
                         {"key": "csrf", "value": "data.csrf"}]},
            {"name": "门店列表", "type": "http", "method": "GET",
             "url": site + "/api/stores?page=1&pageSize=2",
             "headers": [{"key": "X-CSRF", "value": "{{csrf}}"},
                         {"key": "Authorization", "value": "Bearer {{token}}"}],
             "extract": [{"key": "total", "value": "data.total"}]},
            {"name": "建表", "type": "sql", "data_source_id": ds_id,
             "sql": "CREATE TABLE IF NOT EXISTS %s (id INTEGER PRIMARY KEY AUTOINCREMENT, token TEXT, total TEXT, at TEXT)" % table},
            {"name": "写入", "type": "sql", "data_source_id": ds_id,
             "sql": "INSERT INTO %s (token, total, at) VALUES ('{{token}}', '{{total}}', '{{now}}')" % table},
        ],
    }
    r = call("POST", "/api/jobs", job)
    check("创建定时作业", r.get("code") == 0, r.get("msg"))
    job_id = r["data"]["id"]

    r = call("POST", "/api/jobs/%d/run" % job_id)
    check("手动触发作业", r.get("code") == 0, r.get("msg"))
    d = wait_job_run(r["data"]["id"])
    run = d["run"]
    print("  作业结果: status=%s 成功=%s 失败=%s msg=%s" % (
        run["status"], run["steps_ok"], run["steps_fail"], run.get("message")))
    check("作业四个步骤全部成功", run["status"] == "success" and run["steps_ok"] == 4, run.get("error"))
    check("步骤顺序与日志完整", [s["seq"] for s in d["steps"]] == [1, 2, 3, 4], d["steps"])
    check("HTTP 步骤记录响应报文", d["steps"][0]["resp_status"] == 200 and bool(d["steps"][0]["resp_body"]),
          d["steps"][0])
    check("提取变量写入步骤日志", "token" in (d["steps"][0].get("output") or ""), d["steps"][0].get("output"))

    q = exec_sql(ds_id, "SELECT token, total FROM %s LIMIT 1" % table)
    row = q["data"]["rows"][0] if q.get("code") == 0 and q["data"]["rows"] else None
    check("变量进入 SQL 并成功落库", bool(row) and row[0].startswith("tok-") and str(row[1]) == "5", row)

    # Cron 校验
    bad = dict(job)
    bad.update({"name": "E2E-坏Cron-" + tag, "enabled": True, "schedule_type": "cron", "cron": "not-a-cron"})
    r = call("POST", "/api/jobs", bad)
    check("非法 Cron 被拒绝", r.get("code") != 0 and "Cron" in (r.get("msg") or ""), r.get("msg"))

    good = dict(job)
    good.update({"name": "E2E-Cron作业-" + tag, "enabled": True, "schedule_type": "cron", "cron": "0 0 8 * * *"})
    r = call("POST", "/api/jobs", good)
    check("合法 Cron 创建成功", r.get("code") == 0, r.get("msg"))
    cron_id = r["data"]["id"]
    check("Cron 作业注册下次执行时间", bool(r["data"].get("next_run")), r["data"].get("next_run"))

    # 删除作业应同时清理执行记录与步骤日志
    call("DELETE", "/api/jobs/%d" % job_id)
    runs = call("GET", "/api/job-runs?job_id=%d" % job_id)["data"]
    check("删除作业后执行记录清空", not runs.get("list"), runs.get("list"))
    call("DELETE", "/api/jobs/%d" % cron_id)
    call("DELETE", "/api/datasources/%d" % ds_id)


def test_general_config():
    print("\n== 8. 系统设置：运行参数自定义 ==")
    d = call("GET", "/api/settings")["data"]
    cfg = d.get("config") or {}
    old_timeout = int(cfg.get("timeout_sec") or 60)
    old_body = int(cfg.get("max_body_kb") or 256)
    old_proxy = cfg.get("proxy") or ""

    r = call("PUT", "/api/config/general", {
        "addr": cfg.get("addr"), "data_dir": cfg.get("data_dir"),
        "timeout_sec": 75, "proxy": "", "max_body_kb": 512,
    })
    check("保存运行参数成功", r.get("code") == 0, r.get("msg"))
    got = ((r.get("data") or {}).get("config") or {})
    check("超时已更新", got.get("timeout_sec") == 75, got.get("timeout_sec"))
    check("包体上限已更新", got.get("max_body_kb") == 512, got.get("max_body_kb"))
    check("地址未变无需重启", ((r.get("data") or {}).get("restart_required") is False), r.get("data"))

    d2 = call("GET", "/api/settings")["data"]
    check("设置页回读包体上限", (d2.get("settings") or {}).get("max_body_kb") == "512", d2.get("settings"))

    bad = call("PUT", "/api/config/general", {"timeout_sec": 0})
    check("超时非法被拒绝", bad.get("code") != 0, bad)
    bad = call("PUT", "/api/config/general", {"proxy": "ftp://127.0.0.1:21"})
    check("非法代理被拒绝", bad.get("code") != 0, bad)
    bad = call("PUT", "/api/config/general", {"addr": "127.0.0.1"})
    check("缺少端口的地址被拒绝", bad.get("code") != 0, bad)

    # 还原，避免影响后续用例
    r = call("PUT", "/api/config/general", {
        "addr": cfg.get("addr"), "data_dir": cfg.get("data_dir"),
        "timeout_sec": old_timeout, "proxy": old_proxy, "max_body_kb": old_body,
    })
    check("还原运行参数", r.get("code") == 0, r.get("msg"))


def test_import_export():
    print("\n== 5. 导入导出 / 通知 ==")
    data = call("GET", "/api/tasks/export")
    if isinstance(data, (bytes, bytearray)):
        data = json.loads(bytes(data).decode("utf-8"))
    check("导出包含任务", len(data.get("tasks", [])) > 0, len(data.get("tasks", [])))
    payload = b"\xef\xbb\xbf" + json.dumps({"tasks": data["tasks"][:1]}, ensure_ascii=False).encode("utf-8")
    r = call("POST", "/api/tasks/import", raw=payload)
    print("  导入结果: %s" % r.get("msg"))
    check("带 BOM 的 JSON 可导入", r.get("code") == 0, r.get("msg"))

    t = create_task({
        "name": "E2E-失败通知",
        "mode": "api",
        "enabled": True,
        "notify": True,
        "notify_on": "failure",
        "http_steps": [{"name": "错误地址", "method": "GET", "url": "http://127.0.0.1:18099/not-exist"}],
        "sink": {"type": "none"},
    })
    run = run_task(t["id"], timeout=60)
    check("失败任务状态正确", run["status"] in ("failed", "partial"), run["status"])
    time.sleep(1)
    notes = call("GET", "/api/notifications")["data"]
    items = notes["list"] if isinstance(notes, dict) else notes
    hit = [n for n in items if n.get("run_id") == run["id"] and n.get("level") == "error"]
    check("平台消息已产生", len(hit) >= 1, items[:1])


def test_open_keeps_session(site):
    """打开页面会不会把登录态弄丢：整页打开会刷新页面（SPA 内存里的 token 丢失），
    「站内跳转」只改路由不刷新，登录态保留。"""
    print("\n== 12. 打开页面与登录态（SPA 内存 token）==")
    login_steps = [
        {"action": "open", "value": site + "/spa.html"},
        {"action": "wait", "ms": 900},
        {"action": "input", "selector": "#user", "value": "demo"},
        {"action": "input", "selector": "#pass", "value": "123456"},
        {"action": "click", "selector": "#login"},
        {"action": "wait_for", "selector": "#rows tr"},
    ]
    cap = {
        "wait_ms": 1200,
        "rules": [{
            "name": "门店接口", "mode": "network", "url_contains": "/api/stores",
            "method": "GET", "records_path": "data.list",
            "mapping": mapping_common(),
            "sink": {"type": "none"},
        }],
    }

    # (1) 站内跳转：不刷新页面，内存里的登录态还在，回到列表还能继续抓
    steps_spa = login_steps + [
        {"action": "open", "value": site + "/spa.html#/detail", "open_mode": "spa"},
        {"action": "wait_for", "selector": "#backList"},
        {"action": "click", "selector": "#backList"},
        {"action": "wait_for", "selector": "#rows tr"},
    ]
    t = create_task({
        "name": "E2E-站内跳转保会话", "mode": "ui", "enabled": True,
        "ui_steps": steps_spa, "capture": cap,
        "dedupe": {"enabled": True, "keys": ["id"]}, "sink": {"type": "none"},
    })
    run = run_task(t["id"])
    print("  站内跳转: status=%s fetched=%s msg=%s error=%s" % (
        run["status"], run["fetched"], run.get("message"), run.get("error")))
    check("站内跳转运行成功", run["status"] == "success", run.get("error"))
    check("站内跳转后登录态保留（还能抓到数据）", run["fetched"] >= 5, run["fetched"])
    call("DELETE", "/api/tasks/%d" % t["id"])

    # (2) 整页打开：页面刷新 → 内存登录态丢失 → 被路由守卫踢回登录页
    steps_nav = login_steps + [
        {"action": "open", "value": site + "/spa.html?r=2#/detail"},
        {"action": "wait_for", "selector": "#login"},
    ]
    t2 = create_task({
        "name": "E2E-整页打开丢会话", "mode": "ui", "enabled": True,
        "ui_steps": steps_nav, "capture": cap,
        "dedupe": {"enabled": True, "keys": ["id"]}, "sink": {"type": "none"},
    })
    run2 = run_task(t2["id"])
    steps_log = call("GET", "/api/runs/%d/steps" % run2["id"])["data"]
    last = steps_log[-1] if steps_log else {}
    print("  整页打开: status=%s last_step=%s/%s" % (run2["status"], last.get("name"), last.get("status")))
    check("整页打开后确实回到登录页（登录态丢失已复现）", last.get("status") == "success", last.get("error"))
    logs = call("GET", "/api/logs?level=warn&limit=400").get("data", {}).get("list", [])
    hit = [l for l in logs if "登录态" in json.dumps(l, ensure_ascii=False)]
    check("运行日志提示「打开后跳到登录页」", len(hit) >= 1, hit[:1])
    call("DELETE", "/api/tasks/%d" % t2["id"])


def test_session_shared(site):
    """共享会话：第一次运行登录后，Cookie 落到持久化用户目录；第二次运行即使不登录，
    也能复用该 Cookie 调通需要会话的接口。"""
    print("\n== 13. 共享会话（运行之间复用登录态）==")
    cfg = call("GET", "/api/settings")["data"]["config"]
    br = cfg.get("browser") or {}
    orig_mode = br.get("session_mode") or ""
    orig_dir = br.get("profile_dir") or ""
    prof = os.path.join(cfg.get("data_dir") or ".", "browser", "profiles", "e2e-shared")
    t1 = t2 = None
    try:
        r = call("PUT", "/api/browser/config", {"session_mode": "shared", "profile_dir": prof})
        check("切换到共享会话模式", r.get("code") == 0, r.get("msg"))
        st = r.get("data") or {}
        check("会话状态回报共享模式", st.get("session_mode") == "shared", st.get("session_mode"))
        call("POST", "/api/browser/session/clear", {})

        http_step = {
            "name": "门店列表", "method": "GET", "url": site + "/api/stores",
            "headers": [{"key": "X-CSRF", "value": "csrf-demo-556677"}],
            "capture": True, "records_path": "data.list",
        }
        t1 = create_task({
            "name": "E2E-共享会话-首次登录", "mode": "hybrid", "enabled": True,
            "login_steps": [
                {"action": "open", "value": site + "/login.html"},
                {"action": "input", "selector": "#user", "value": "demo"},
                {"action": "input", "selector": "#pass", "value": "123456"},
                {"action": "click", "selector": "#submit"},
                {"action": "wait", "ms": 2000},
            ],
            "http_steps": [{
                "name": "门店列表", "method": "GET", "url": site + "/api/stores",
                "headers": [{"key": "X-CSRF", "value": "{{csrf}}"}],
                "capture": True, "records_path": "data.list",
            }],
            "capture": {"mode": "network", "url_contains": "/api/stores"},
            "mapping": mapping_common(),
            "dedupe": {"enabled": True, "keys": ["id"]},
            "sink": {"type": "none"},
        })
        run1 = run_task(t1["id"])
        print("  首次登录: status=%s fetched=%s error=%s" % (run1["status"], run1["fetched"], run1.get("error")))
        check("共享会话首次运行成功", run1["status"] == "success", run1.get("error"))
        check("首次运行抓到数据", run1["fetched"] >= 2, run1["fetched"])

        info = call("GET", "/api/settings")["data"]["browser_status"]
        check("会话文件已落盘", bool(info.get("session_exists")) and (info.get("session_files") or 0) > 0, info.get("session_files"))

        # 第二次运行：登录步骤只打开登录页，不做任何登录动作；靠共享会话里的 Cookie 调接口
        t2 = create_task({
            "name": "E2E-共享会话-复用登录态", "mode": "hybrid", "enabled": True,
            "login_steps": [
                {"action": "open", "value": site + "/login.html"},
                {"action": "wait", "ms": 600},
            ],
            "http_steps": [http_step],
            "capture": {"mode": "network", "url_contains": "/api/stores"},
            "mapping": mapping_common(),
            "dedupe": {"enabled": True, "keys": ["id"]},
            "sink": {"type": "none"},
        })
        run2 = run_task(t2["id"])
        print("  复用会话: status=%s fetched=%s error=%s" % (run2["status"], run2["fetched"], run2.get("error")))
        check("未重新登录也能调通接口（Cookie 已复用）", run2["status"] == "success", run2.get("error"))
        check("复用会话抓到数据", run2["fetched"] >= 2, run2["fetched"])

        c = call("POST", "/api/browser/session/clear", {})
        check("清空共享会话成功", c.get("code") == 0, c.get("msg"))
        after = call("GET", "/api/settings")["data"]["browser_status"]
        check("清空后会话目录已重置", not after.get("session_exists"), after.get("session_files"))
    finally:
        for t in (t1, t2):
            if t:
                call("DELETE", "/api/tasks/%d" % t["id"])
        call("PUT", "/api/browser/config", {"session_mode": orig_mode or "isolated", "profile_dir": orig_dir})


def main():
    global BASE, SITE
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=BASE)
    ap.add_argument("--site", default=SITE)
    ap.add_argument("--only", default="", help="api|hybrid|ui|scheduler|import|sqlsink|jobs|cfg|uipg|uipgt|uiframe|studio|studioiframe|spaopen|shared")
    args = ap.parse_args()
    BASE, SITE = args.base.rstrip("/"), args.site.rstrip("/")
    only = args.only

    health = call("GET", "/api/health")
    print("服务状态:", json.dumps(health, ensure_ascii=False))
    if health.get("code") != 0:
        raise SystemExit("服务不可用，请先启动 collect")

    if not only or only == "api":
        test_api_mode(SITE)
    if not only or only == "hybrid":
        test_hybrid_mode(SITE)
    if not only or only == "ui":
        test_ui_mode(SITE)
    if not only or only == "multi":
        test_multi_sink(SITE)
    if not only or only == "uimulti":
        test_ui_multi_rule(SITE)
    if not only or only == "uipg":
        test_ui_click_pagination(SITE)
    if not only or only == "uipgt":
        test_ui_click_text_pagination(SITE)
    if not only or only == "uiframe":
        test_ui_iframe(SITE)
    if not only or only == "studio":
        test_studio_multi(SITE)
    if not only or only == "studioiframe":
        test_studio_iframe(SITE)
    if not only or only == "spaopen":
        test_open_keeps_session(SITE)
    if not only or only == "shared":
        test_session_shared(SITE)
    if not only or only == "scheduler":
        test_scheduler(SITE)
    if not only or only == "sqlsink":
        test_sql_sink(SITE)
    if not only or only == "jobs":
        test_jobs(SITE)
    if not only or only == "import":
        test_import_export()
    if not only or only == "cfg":
        test_general_config()

    print("\n" + ("全部通过" if not FAILS else "失败项 %d: %s" % (len(FAILS), FAILS)))
    sys.exit(0 if not FAILS else 1)


if __name__ == "__main__":
    main()
