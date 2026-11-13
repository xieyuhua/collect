# 采集平台（Collect Platform）

一个自托管的**浏览器接口采集平台**：录制浏览器操作 → 自动生成采集任务 → 定时调度执行 → 字段映射去重 → 生成表格 / 写入多种数据库 → 企业微信 + 平台消息通知。

后端 Go（Gin + GORM + go-rod），前端为内嵌单页应用（无需 node 构建），单个可执行文件即可运行，支持 **Windows / Linux**。

## 一、三种采集模式（怎么选）

| 模式 | 名称 | 适用场景 | 原理 |
| --- | --- | --- | --- |
| **方案一** `api` | 纯接口采集 | 网站用明显的 `token` / `Authorization` 头，风控弱 | 录制登录与列表接口，登录响应里提取 token，后续请求自动替换 `{{token}}`，按分页参数循环抓取 |
| **方案三** `hybrid` | UI 登录 + 接口抓取 | 登录参数复杂（验证码、加密、动态签名、CSRF 每次变化） | UI 只回放登录动作，回收浏览器 Cookie / localStorage / sessionStorage 作为变量，之后的接口用这套会话直接 HTTP 请求 |
| **方案二** `ui` | 纯 UI 回放 | 风控极强，每一步都校验浏览器指纹 / 行为 | 完全回放页面动作（打开、输入、点击、滚动…），真实浏览器执行，从网络响应或页面 DOM 表格里取数据 |

选型建议：

1. 有清晰 token、接口可直连 → **方案一**（快、省资源、可分页）。
2. 登录有加密参数 / 动态签名 → **方案三**（登录交给浏览器，数据走接口）。
3. 接口带设备指纹、风控脚本、必须真实点击 → **方案二**（还原度最高，最慢）。

## 二、快速开始

### 1. 环境

- Go **1.26+**（`go.mod` 中为 1.26.5）
- 浏览器内核：系统已安装 Chrome / Edge 即可直接使用；也可在「系统设置 → 浏览器内核」一键下载 Chromium（Windows / Linux 均可）

### 2. 运行

Windows：

```bat
start.bat
```

Linux：

```sh
chmod +x start.sh && ./start.sh
```

手工编译运行：

```sh
go build -o bin/collect .        # Windows: go build -o bin\collect.exe .
./bin/collect                    # 默认 http://127.0.0.1:8686
./bin/collect -addr 0.0.0.0:8686 -open=false
```

参数：

- `-addr` 监听地址，默认取 `config.json` 的 `addr`（`127.0.0.1:8686`）
- `-open` 启动后自动打开浏览器，默认 `true`

首次启动会自动创建 `data/`（SQLite 主库 `data/app.db`、日志、导出文件、浏览器内核目录）。

> ⚠️ **只跑一个实例**。常见坑：`bin\collect.exe` 和 `dist\collect-windows-amd64.exe` 同时运行，
> 一个占 `127.0.0.1:8686`、一个占 `192.168.x.x:8686`，两边各有一份 SQLite（`data\app.db` 与 `dist\data\app.db`），
> 于是「明明改了配置/重启了却没变化」「任务列表对不上」。当前 `config.json` 与 `dist\config.json` 已统一为
> `0.0.0.0:8686` + `dist\data` 这一份数据；换版本时先停旧进程再启动新的。
> 前端静态资源已加 `Cache-Control: no-cache`，浏览器不会再拿旧脚本。

### 3. 打包

```powershell
powershell -ExecutionPolicy Bypass -File build.ps1
```

输出：`bin\collect.exe`（当前平台）、`dist\collect-linux-amd64`、`dist\collect-windows-amd64.exe`，可直接拷到其他设备运行（`data/` 与 `config.json` 自动生成）。

## 三、界面功能

| 页面 | 说明 |
| --- | --- |
| 仪表盘 | 任务数、今日运行、成功率、近 7 天趋势、最近运行 |
| 任务管理 | 列表 / 搜索 / 启停 / 复制 / 运行 / 导入导出 / 删除 |
| 任务编辑器 | 10 个配置页签：基本设置、变量、接口步骤、UI 步骤、登录步骤、抓取配置、分页、字段映射、落库、去重 |
| 录制工作台 | 启动有头浏览器 → 人工操作 → 实时查看捕获的请求 / 动作 → 查看请求详情 → 一键生成任务草稿 |
| 运行记录 | 每次执行的步骤日志（请求头/请求体/响应头/响应体）、抓取数据预览、导出 xlsx/csv |
| 定时作业 | GET/POST 请求、Shell 命令、数据库 SQL 的组合任务，支持 Cron / 固定间隔、执行记录与通知 |
| 数据源 | 管理多个数据库（MySQL / Oracle / SQLite / PostgreSQL），支持连接测试 |
| 运行日志 | 平台运行日志（按级别过滤、实时轮询） |
| 消息中心 | 平台统一消息（成功/失败/通知），可标记已读、清空 |
| 系统设置 | 运行参数（监听地址 / 数据目录 / 超时 / 代理 / 包体上限）、通知策略、浏览器内核与指纹，全部界面可改，无需手改 `config.json` |

### 界面优化说明（v1.4）

- **全局**：统一视觉规范（字体/间距/圆角/阴影）、表头吸顶、行悬停、按钮 hover/active/键盘焦点态、弹窗背景虚化、窄屏（<1000px）侧边栏自动收窄为图标模式；顶栏新增「↻ 刷新当前页」。
- **仪表盘**：统计卡强调色图标，新增「最近异常」提示条（失败 / 部分成功任务一键跳转）。
- **任务管理 / 运行记录**：状态筛选下拉即选即筛，新增「↻ 刷新」按钮；运行步骤类型显示中文标签（接口 / UI / 落库 / 登录 / 抓取）。
- **数据源**：顶部统计卡（总数 / MySQL / PostgreSQL / SQLite+Oracle），数据库类型彩色标签，连接串等宽字体展示。
- **运行日志**：支持「暂停 / 继续」轮询、「自动滚动到底部」开关，并显示已加载行数。
- **消息中心**：「全部消息 / 只看未读」筛选，未读行左侧色条并加粗标题。
- **任务编辑器**：
  - 修复「+ 添加 UI 动作 / 登录动作 / 接口步骤 / 变量 / 字段映射」点击无反应的缺陷（`rerenderEditor` 重复 `syncEditor` 覆盖了刚新增的条目）；新增后自动聚焦到新条目首个输入框并给出提示。
  - 顶部「● 有未保存修改」标记，`Ctrl/Cmd + S` 快捷保存。
  - 页签显示条目数徽标；各面板空状态带使用引导；步骤卡片可折叠（⌃）。
  - 基本设置随「采集模式」联动显示三种方案（api / hybrid / ui）的选择说明。

### 前端重构说明（v1.5：Vue 3）

- 后台管理页已全部改为 **Vue 3**（`vue.global.prod.js` 内嵌在 `web/vendor/`）重写：`go:embed` 打包进单二进制，**仍然零 Node 构建、零 npm 依赖**，解压即用。
- 目录：`web/index.html`（挂载 + 脚本加载）、`web/style.css`（视觉规范）、`web/js/core.js`（API / Toast / Modal / hash 路由 / 全局状态）、`web/js/app.js`（外壳、分页器、空状态），每个页面一个 `web/js/page-*.js`（dashboard / tasks / editor / runs / studio / systems）。
- 编辑器不再依赖手工 DOM 同步：10 个页签全部是响应式表单，「+ 添加 UI 动作 / 登录动作 / 接口步骤 / 变量 / 字段映射」等按钮点击即时渲染（此前"点了没反应"的根因已消除）。
- 路由与后端契约不变：`#/dashboard`、`#/task/new`、`#/run/:id`、`#/runs/:taskId` 等哈希路由、导入导出 JSON 格式、旧 uicheck 选择器（`#tName`、`data-act="save-task"` 等）全部保留，老脚本可继续用。
- 升级后浏览器若显示旧界面，按 **Ctrl+F5**（或 Ctrl+Shift+R）强制刷新，清掉内嵌资源的浏览器缓存即可。

### 本轮更新（v1.6）

- **录制会话**：列表新增「开始时间 / 录制时长」，录制中的会话按 5 秒自动刷新。
- **运行详情**：合并为「执行步骤 / 数据集预览」两个大页签；执行步骤默认最近的在前面，可切换为执行顺序，并新增「执行开始时间」列；数据集按来源分组预览、分来源导出。
- **任务编辑器**：按采集模式自动置灰不相关页签（见「采集模式与可编辑页签」）；步骤 / 规则卡片默认收起并支持关键词搜索。
- **纯 UI 分页抓取**：抓取规则内新增「按该接口分页抓取」，回放结束后复用登录会话逐页请求并入库。
- **生成任务草稿说明**：录制工作台内置说明，明确草稿不会自动保存，支持多接口分别生成步骤 / 规则。
- **定时作业**：新增 GET/POST、Shell、SQL 三类步骤，Cron / 固定间隔调度、执行记录、平台消息 + 企业微信通知。

### 本轮更新（v1.7）

- **作业步骤间变量**：HTTP 步骤可配置「提取变量」（如 `token=data.data.access_token`、`csrf=data.csrf`），后续 HTTP / Shell / SQL 步骤用 `{{token}}` 引用；变量值会写进步骤日志（token 等敏感变量自动打码）。
- **作业会话复用**：同一次作业执行内的所有 HTTP 步骤共用 Cookie Jar，登录接口下发的 `Set-Cookie` 可直接被后续请求使用，无需手动复制。
- **Cron 校验**：保存作业时校验 Cron 与间隔（间隔最小 5 秒），非法表达式直接报错，不再等到调度时才失败。
- **删除清理**：删除作业会同时清理执行记录与步骤日志。
- **自测覆盖**：`e2e_test.py --only jobs` 覆盖「登录提取变量 → 带 token 取数 → SQL 落库 → 非法 Cron 拒绝 → 删除清理」。

### 本轮更新（v1.8）

- **Cron 常用计划**：定时作业与采集任务的 Cron 输入框旁新增下拉选择，内置 14 个常用计划（每 5/10/30 分钟、每小时、每 2 小时、每天 00/08/09/12/18 点、工作日 9 点、每周一 9 点、每月 1 号、每年 1 月 1 日），选中即自动填入表达式；也可选「自定义」手工输入，输入框下方实时显示当前计划名称。

### 本轮更新（v1.9）

- **纯 UI「点击翻页」**：抓取规则的分页新增「翻页方式」——`页面点击翻页` 会在浏览器里真实点「下一页」，等页面自己加载出新一页，再把这页新出现的接口响应 / DOM 表格解析入库；`接口翻页` 仍是复用登录会话逐页请求。点击翻页全程没有额外构造的请求，最适合风控严格、每一步都校验的站点。按钮消失 / 置灰（`disabled`、`aria-disabled`、class 含 `disabled`）即视为最后一页。
- **DOM 规则也支持点击翻页**：DOM 表格规则勾选翻页后会自动锁定为「页面点击翻页」，配合「下一页选择器」即可整表翻页入库。
- **首屏取数修正**：点击翻页第 1 页只统计「最后一次打开页面」之后产生的接口请求，不会再把上一个页面（例如登录后跳转的门店页）的数据混进第 1 页。
- **浏览器指纹可自定义**：「系统设置 → 浏览器指纹」支持自定义 `User-Agent`、`Accept-Language`、`navigator.platform`、时区、视口宽高与额外启动参数，并提供 Windows / macOS / Linux 三个 UA 预设一键对齐；另附「检测当前内核 UA」，可直接看到内核真实上报的 UA（部分精简版内核会伪装成别的系统）。纯 UI / 混合回放、录制页与接口采集的默认请求头都会沿用这套指纹。

### 本轮更新（v1.10）

- **系统设置全部可自定义**：新增「运行参数（可自定义）」卡片，可直接在界面上修改监听地址、数据目录、单请求超时、接口采集默认代理、日志请求/响应体上限，并支持「恢复常用默认值」。超时 / 代理 / 包体上限保存后对新请求立即生效；监听地址与数据目录是启动参数，保存后卡片会标记「待重启生效」，重启平台即生效。
- **参数校验**：监听地址必须带端口（如 `127.0.0.1:8686`），代理只接受 `http:// / https:// / socks5://`，超时 `1-1800` 秒、包体上限 `8-8192` KB，非法值直接提示，不会写坏 `config.json`。
- **接口**：新增 `PUT /api/config/general`，返回保存后的完整配置与 `restart_required`，方便脚本化部署。
- **自测覆盖**：`e2e_test.py --only cfg` 覆盖保存、回读、非法参数拒绝与还原；`uicheck` 新增运行参数卡片与「恢复默认值」按钮检查。

### 本轮更新（v1.11）

- **纯 UI 点击翻页更省事**：规则里的「下一页 / 加载更多」可以**只填按钮文字**，选择器留空也能量：文字按包含匹配（「下一页 ›」「Next >」「加载更多」都能命中），并且只在「按钮语义」元素（`a` / `button` / `[role=button]` / `li` / `span`）里挑最内层那个点，避免误点分页容器导致空转。
- **从录制动作识别**：规则分页区新增「从录制动作识别」，自动从已录制的点击动作里找出「下一页 / 加载更多」并填好选择器与文字；旁边还有常见分页组件（Ant Design / Element / layui / `a[rel=next]` / `li.next a`）的一键填入。
- **一键改成点击翻页**：纯 UI 下如果规则还用「接口翻页」，会提示风控风险，并给一个「一键改成点击翻页」按钮，点了就切到真实点击翻页并自动填好按钮文字。
- **末页判断更准**：选择器命中的按钮存在但置灰（`disabled` / `aria-disabled` / class 含 `disabled` / `pointer-events:none`）时直接判定为最后一页，不再退化成文字匹配去乱点；`position:fixed` 的悬浮分页按钮也能识别。
- **零变化自动停**：点击「下一页」后既没有新接口请求、DOM 也没变化时立刻结束，只记一条 info 日志，不会把当前页数据重复追加（步骤里也不会出现「假页面」）。
- **空页提示**：翻页步骤状态新增 `warn`（琥珀色「无新数据」）；勾了「空页自动停止」立即停，没勾时连续 2 页空也会自动停，防止 SPA 按钮一直可点空转到最大页数。
- **自测覆盖**：新增 `e2e_test.py --only uipgt`（选择器留空、纯文字匹配翻页）与模拟站点 `/paged2.html`；`uicheck` 新增按钮文字输入、识别按钮、接口翻页警告与「一键改成点击翻页」检查。

### 本轮更新（v1.12）

- **iframe 内嵌页面完整支持**：当目标页面是 `<iframe>` 内嵌（含跨域 iframe）时，不再需要切换到 iframe 地址单独录制。任务编辑器里三类配置都可填 iframe 选择器：
  - UI 步骤 / 登录步骤：每个非 `open` 动作多一个「所在 iframe」输入
  - DOM 抓取规则：「表格所在 iframe」输入
  - 点击翻页：「下一页按钮所在 iframe」输入
- **嵌套 iframe**：用 `>>` 逐层进入，例如 `iframe#outer >> iframe.inner`；也可以直接写 `#mainFrame`、`iframe[name=content]`。
- **跨域 iframe 可用**：进入 iframe 走浏览器 CDP 的 frame 上下文，不依赖同源 JS，因此跨域 iframe 内的点击、输入、取数、翻页都能工作。
- **录制自动识别 iframe**：录制工作台会把录制脚本注入主页面与所有 iframe，iframe 内产生的输入 / 点击自动带上「所在 iframe」；生成任务草稿时 UI 步骤自动带入 `frame`，动作列表上会显示 `［iframe #xxx］`。
- **动态 iframe**：iframe 动态创建或内部跳转后会自动重新注入脚本；修复了“iframe 导航后复用旧 frame 导致 iframe 内动作录不到”的问题。
- **稳定性修复**：录制轮询、脚本注入、回放、DOM 提取对同一页面的求值改为串行（按 target 加锁），修复了偶发的 `panic: assignment to entry in nil map`；回放时不再同时注入动作录制脚本，避免录制线程与回放线程互相干扰。
- **自测覆盖**：新增模拟站点 `/outer.html`（iframe 登录 + 门店表格 + 点击翻页）、`/outerauto.html`（iframe 内自动动作）；`e2e_test.py --only uiframe` 覆盖 iframe 内网络规则与 DOM 规则翻页入库，`--only studioiframe` 覆盖录制动作自动标注 iframe 与草稿保留 `frame`；`uicheck` 新增 iframe 输入框检查。
- **不填 iframe 也能自动识别（v1.12.1）**：UI 步骤 / DOM 规则 / 点击翻页的 iframe 留空时，回放会先在主页面找，找不到就自动扫描所有 iframe（含嵌套）并继续操作；DOM 表格与「下一页」按钮同样适用。运行日志会写「已自动进入 iframe #xxx」，可把该值填回配置以加快后续运行。老任务不用重录。
- **模式提示更明确**：纯接口模式忽略 UI / 登录动作、纯 UI 模式忽略登录步骤、混合模式只跑登录步骤时，运行日志都会给出明确警告，不再静默跳过。
- **录制的「打开 xxx」不再丢（v1.12.2）**：录制过程中页面发生的跳转（例如登录后点菜单进到内嵌列表页）以前只显示在「页面动作」里、不会进入任务步骤；现在会自动转成 `打开页面` 步骤并按录制顺序插入，回放时才会真的先跳到那一页再点元素。与起始地址相同的跳转自动去重，不会生成两条一样的打开步骤。同时：生成草稿的「模式」默认跟随录制会话的模式（以前固定是纯接口，导致纯 UI 录完却一个 UI 步骤都没有），纯接口模式下丢弃页面动作时会在草稿面板和任务编辑器顶部给出提示。
- **「打开页面」丢登录态问题修复（v1.12.3）**：
  - UI 步骤 / 登录步骤的 `打开页面` 动作新增「**打开方式**」下拉：`整页打开（默认，重新加载页面）` / `站内跳转（不刷新页面，保留登录态）`。字段名 `open_mode`（空 = 整页打开，`spa` = 站内跳转）。
  - 「站内跳转」在同源页面上用 `history.pushState` / `location.hash` + `popstate`/`hashchange` 事件切换路由，不重新加载文档，SPA 内存里的 token、Vuex/Redux store 都保留；跨域或页面路由没响应时自动回落到整页打开，并在运行日志里写明。
  - 每次 `打开页面` 都会在运行日志里记录 Cookie / localStorage / sessionStorage 数量（`打开 xxx（Cookie 3 个 / localStorage 2 项 / sessionStorage 0 项）`），方便对比哪一步把会话清空了。
  - 打开前不是登录页、打开后被跳成登录页时，直接写一条**警告**日志：「这一步把登录态弄丢了」，并列出三种改法。
  - 打开同源 hash 路由（含 `#/`）且用的是整页打开时会先提示一句：建议改用「站内跳转」。
  - 自测：模拟站新增 `/spa.html`（登录态只存在 JS 变量里，刷新即丢），`e2e_test.py --only spaopen` 覆盖「站内跳转保会话」与「整页打开丢会话 + 告警日志」两组用例；`uicheck` 新增「打开方式」下拉检查。
- **会话共享（v1.13）**：「系统设置 → 浏览器内核 → 会话模式」新增 `每次全新会话（默认）/ 共享会话`，任务「基本设置 → 浏览器会话」可单独覆盖。共享模式下所有运行复用同一个持久化用户目录（Cookie / localStorage / IndexedDB），登录一次后续直接复用；同一会话目录自动串行排队，支持「清空共享会话」。详见 5.12。

## 四、录制 → 任务（推荐流程）

1. 打开「录制工作台」，填写起始网址（如登录页），选择模式（api / hybrid / ui），点「启动浏览器并开始录制」。
2. 在弹出的浏览器里正常操作：输入账号密码 → 登录 → 打开列表页 → 翻页 / 搜索。
3. 回到平台点「刷新」，查看捕获的请求（JSON 请求会标记）与页面动作。
4. 点某个请求可查看 **请求头 / 请求体 / 响应头 / 响应体**。
5. 点「生成任务草稿」：
   - 自动选出「数据列表」请求（JSON、响应体最大、含数组），识别记录路径（如 `data.list`）与总数路径（`data.total`）；
   - 自动识别登录请求与 token 字段（`access_token` / `token` / `Authorization` 等），生成提取规则；
   - 自动识别分页参数（`page` / `pageNo` / `pageSize` / `limit` / `offset` …）；
   - 自动按返回字段生成字段映射，并推断主键做去重；
   - hybrid / ui 模式会把录到的动作转成 UI 步骤。
6. 「生成并打开编辑器」→ 检查后保存；或「直接保存任务」→ 保存并运行。

### 生成任务草稿到底做了什么

- 它把本次录制的请求 / 页面动作**整理成一份任务配置**：登录接口或登录动作、`token` / `Cookie` 引用、列表接口、分页参数、记录路径、字段映射、主键去重。
- **不会自动入库、不会自动保存**；「生成并打开编辑器」只是把配置带到 `#/task/new`，确认后点「保存任务」才真正落库。
- 数据接口支持**多选**：
  - `api`：每个接口生成一个独立「接口步骤」，可分别配置分页 / 映射 / 落库（接口 1、2、3… 互不干扰）；
  - `hybrid`：登录页面动作生成「登录步骤」，每个接口生成独立接口步骤；
  - `ui`：页面动作生成「UI 步骤」，每个接口生成一条「抓取规则」，一次回放同时采集，规则内可单独开启分页。

### 采集模式与可编辑页签

编辑器会按当前「采集模式」自动置灰不相关的页签，避免误配：

| 页签 | `api` 纯接口 | `hybrid` UI+接口 | `ui` 纯 UI |
| --- | --- | --- | --- |
| 接口步骤 / 任务级分页 | ✅ | ✅ | ❌（改用规则内分页） |
| 登录步骤 | ❌ | ✅ | ❌ |
| UI 步骤 / 抓取配置 | ❌ | ❌ | ✅ |
| 变量 / 字段映射 / 落库 / 去重 | ✅ | ✅ | ✅ |

切换模式时如果当前页签不再适用，会自动回到「基本设置」。

> 录制浏览器始终是**有头**模式（人要看得到）；采集回放默认无头，可在设置里改。

## 五、任务配置要点

### 1. 变量与占位符

在 URL、Header、Body、UI 步骤里都可以用 `{{变量名}}`：

- 任务变量：在「变量」页签定义，如 `city = 北京`
- 提取变量：上一步接口响应里提取，如 `token = data.access_token`、`csrf = header:x-csrf-token`
- hybrid 模式自动注入：`cookie`、`token`、`csrf` 等（来自 localStorage / sessionStorage，键名即变量名）
- 内置变量：`now`、`today`、`timestamp`、`ts`、`uuid`、`random`、`page`、`pageSize`、`offset`、`cursor`
- 过滤器：`{{token|url}}`、`{{name|b64}}`、`{{code|lower}}`、`{{code|upper}}`

示例：登录步骤提取 token 后，列表步骤 Header 写 `Authorization: Bearer {{token}}`。

### 2. 分页

- `page` 类型：`page_param`（如 `page`）+ `size_param`（如 `pageSize`）+ `page_size` + `start_page` + `max_pages`
- `offset` 类型：按 `offset` 递增
- `cursor` 类型：从响应 `cursor_path` 取下一页游标
- 分页参数在原 URL query 中会自动**覆盖**（录到的 `page=1` 会被替换成 `{{page}}`）；Body 为 JSON 时同样支持 `page_in_body`
- `stop_when_empty`：某页无数据即停止

#### 纯 UI 模式的分页抓取（`ui`）

纯 UI 模式回放页面时也会捕获到接口请求，因此可以「UI 负责登录 / 打开页面，接口负责翻页入库」：

1. 「UI 步骤」回放：打开页面 → 登录 → 等待列表出现（翻页按钮点一次也可，用于让录制器捕获翻页接口）。
2. 「抓取配置」添加规则：`抓取来源 = 网络请求`，填写目标接口 `URL 包含`（如 `/api/store/list`）。
3. 勾选规则内的 **「启用翻页抓取」**，选择「翻页方式」：
  - **页面点击翻页（推荐）**：填「下一页按钮选择器」（如 `.pagination .next`、`a[rel=next]`），可选填按钮文字与点击后等待毫秒。运行时会真实点击「下一页」，等页面加载出新数据后再解析这一页，风控层面最接近真人操作。列表接口是 XHR 的照常按「URL 包含」取响应，纯前端渲染的表格则用 DOM 规则。
  - **接口翻页**：填分页类型 / 页码参数 / 每页条数 / 最大页数 / 记录路径，引擎用捕获到的真实请求做模板逐页请求。
   - 选择器旁的小按钮：「从录制动作识别」会从已录制的 UI 动作里自动找出「下一页 / 下一页 › / Next / 加载更多 / 查看更多」这类点击并填入选择器与文字；下面几个是常见分页组件的快捷选择器。
   - **选择器可以留空**，只填「按钮文字」即可：文字按**包含**匹配，页面写「下一页 ›」「Next >」时填「下一页」「Next」就行（只有图标没有文字时才必须填选择器）。
   - 按钮消失 / 置灰（`disabled`、`aria-disabled`、class 含 `disabled`）视为最后一页；点击后页面既没发新请求、DOM 也没变化时也会自动停止，不会把同一页数据重复入库。
   - 「加载更多」类的按钮同样适用：每点一次新加载的数据按主键去重后追加入库，适合瀑布流 / 无限滚动列表。
4. 在规则内配置字段映射与落库目标（也可「不落库」，只做数据预览）。
5. 保存运行：回放结束后引擎会读取页面 Cookie / localStorage / sessionStorage，用捕获到的真实请求（含请求头、方法、请求体）作为模板逐页请求并入库。

> 如果提示「未捕获到接口」，说明回放期间页面没有发出该请求：检查 UI 步骤是否走到列表页、URL 包含是否写对、`wait_ms` 是否足够。
> 同一个 UI 回放可以配置**多条抓取规则**，每个接口的数据来源独立（`dom:` / `cap:` 前缀），运行详情可按来源分组预览。
> 点击翻页时，运行详情里会出现 `UI 翻页 第 N 页` 步骤，紧跟该页被捕获的接口请求，方便逐页核对。

### 3. 字段映射

| 列 | 说明 |
| --- | --- |
| 来源 source | 接口响应里的字段路径（支持 `data.list[0].name` 这类嵌套），UI-DOM 模式填 **CSS 选择器**（`td:nth-child(2)`，`a@href` 表示取属性） |
| 目标 target | 输出列名 / 数据库列名 |
| 类型 type | `string` `int` `float` `bool` `datetime` `json` |
| 默认值 | 源字段缺失时使用 |

不填映射时，自动展平全部字段。

### 4. 主键去重

「去重配置」勾选后填写去重键（如 `id`）。同一个任务、同一个键值**跨运行永久只保留一次**（去重记录存 `DedupeKey` 表），重复数据计入「重复」计数。

### 5. 落库

「落库配置」支持：

- `table`：只生成表格（xlsx / csv）
- `db`：只写数据库
- `both`：两者都做
- `none`：只留平台数据预览

写入方式三选一：

| 写入方式 | 说明 |
| --- | --- |
| `insert` | 按字段映射拼 `INSERT INTO 表 (...) VALUES (...)`，支持 `batch_size` 多值批量 |
| `upsert` | 存在则更新，需要填「冲突主键列」且目标表有唯一索引（MySQL `ON DUPLICATE KEY`、PG/SQLite `ON CONFLICT`、Oracle `MERGE`） |
| `sql` | **自定义 SQL 模板**，每行数据执行一次，支持占位符与变量替换（见下） |

`auto_create` 只在 `insert` / `upsert` 下生效（按映射自动建表）。

#### 自定义 SQL 模板（`write_mode = sql`）

在「落库配置 → 写入方式」选「自定义 SQL 语句」，编辑器里点「按字段映射生成模板」可一键生成草稿再改。模板支持三种取值方式：

```sql
-- ① {{字段}}：取字段映射的「目标字段」，生成绑定参数（推荐，防注入）
INSERT INTO store (id, name, city, sales) VALUES ({{id}}, {{name}}, {{city}}, {{sales}})

-- ② {{变量}}：运行期变量（token / page / pageSize / now / today / uuid / 自定义变量），
--    自动判断类型：数字/布尔/NULL 原样，字符串自动加单引号并转义；也支持 |url |b64 |lower |upper
INSERT INTO store (id, name, src, updated_at)
VALUES ({{id}}, {{name}}, '{{src_tag}}', '{{now}}')

-- ③ 原生占位符：模板里不写 {{字段}} 时，按字段映射顺序逐列绑定
INSERT INTO store (id, name, city) VALUES (?, ?, ?)       -- MySQL / SQLite
INSERT INTO store (id, name, city) VALUES ($1, $2, $3)    -- PostgreSQL
INSERT INTO store (id, name, city) VALUES (:1, :2, :3)    -- Oracle
```

要点：

- 每行 SQL 在同一事务里批量执行（按 `batch_size` 提交），失败整批回滚，运行详情的步骤日志里会有 `sink` 记录（可直接看 SQL 与结果）
- 同一模板里 **不要混用** `{{字段}}` 和原生 `?` / `$1`
- 模板引用了不存在且不是变量的 `{{xxx}}` 会直接报错，避免写入脏数据
- 可以用「数据源」页的 **执行 SQL** 按钮先把语句跑通（原始 SQL，不做变量替换）

### 6. 定时调度

- `manual` 手动
- `cron` 表达式：支持 5 段或 6 段（含秒），如 `0 0 8 * * *`（每天 8 点）、`0 */30 * * * *`（每 30 分钟）
- `interval` 固定间隔（秒，最小 5）

同一任务不会并发执行（`SkipIfStillRunning`）。任务列表会显示「下次执行」时间。

### 7. 通知

- 每个任务的「执行后通知」开关 + 通知时机（仅失败 / 仅成功 / 每次都通知）
- 平台消息中心始终记录每条运行结果；企业微信机器人只在开启且配置 Webhook 后推送
- 任务未单独设置时，回落平台级策略（设置页里的「成功/失败通知」）

企业微信配置：群机器人 → 复制 Webhook → 填到「系统设置 → 企业微信机器人」→ 保存 → 点「发送测试消息」。

### 8. 导入导出 / 多设备共享

- 单任务导出：任务列表「导出」→ 生成 JSON 文件
- 全部任务导出：`GET /api/tasks/export`
- 导入：`POST /api/tasks/import`（支持带 BOM 的 JSON，重名自动加后缀）

### 9. 定时作业（HTTP / Shell / SQL）

「定时作业」与「采集任务」是两套独立能力：采集任务负责抓数据，定时作业负责**任意自动化动作**（调用接口、跑脚本、执行 SQL）。入口：左侧「⏱️ 定时作业」。

一个作业由多个步骤组成，按顺序执行，每个步骤可以是：

| 步骤类型 | 配置 | 说明 |
| --- | --- | --- |
| `http` | 方法（GET/POST/PUT/DELETE/PATCH）、URL、请求头、请求体、Content-Type | 适合调 Webhook、内部 API、登录刷新等；请求 / 响应完整写入步骤日志 |
| `shell` | 命令、工作目录、环境变量 | Windows 用 `cmd /C`，Linux 用 `sh -c`；有超时保护，输出写入日志 |
| `sql` | 数据源、SQL | 复用「数据源」里的 MySQL / Oracle / SQLite / PostgreSQL 连接；查询返回行/列，写入返回影响行数 |

**步骤间传递数据（登录 → 取数 → 入库）**

1. HTTP 步骤配「提取变量」，每行 `变量名=取值表达式`：
   - JSON 路径：`token=data.data.access_token`、`firstId=data.list.0.id`
   - 响应头：`sid=header:Set-Cookie`
   - 正文正则：`csrf=regex:csrf=([0-9a-zA-Z]+)`
   - 状态码：`code=status`
2. 后续步骤用 `{{变量名}}` 引用，例如请求头 `Authorization: Bearer {{token}}`、SQL `INSERT ... VALUES ('{{token}}', '{{now}}')`。
3. 同一次执行内所有 HTTP 步骤共享 Cookie 会话，登录返回的 Cookie 自动带到后续请求。

变量只在**本次执行**内有效；内置变量（`now/today/timestamp/ts/uuid`）名字不能复用。

通用能力：

- **调度**：手动 / Cron（5 段或 6 段，如 `0 0 8 * * *`）/ 固定间隔（秒，最小 5）；列表显示「下次执行」。
- **变量**：URL / Header / Body / Shell 命令 / SQL 里可用内置变量 `{{now}}`、`{{today}}`、`{{timestamp}}`、`{{ts}}`、`{{uuid}}`。
- **失败控制**：单个步骤可设「失败继续执行下一步」，否则失败即终止；每个步骤可单独设置超时（留空继承作业）。
- **步骤开关**：可临时禁用某一步而不删除。
- **通知**：作业级「执行后通知 + 通知时机（仅失败 / 仅成功 / 每次）」；平台消息中心始终记录，开启企业微信机器人后按策略推送。
- **执行记录**：作业列表「记录」→ 每次执行的步骤日志（请求头 / 请求体 / 响应头 / 响应体 / 命令输出 / SQL 结果），支持状态筛选和删除。
- **并发保护**：同一作业不会并发执行（手动 / 定时同时触发时后一次会提示「正在执行中」）。

示例 1：每天 8 点先 GET 一个刷新接口，再执行 SQL 清理临时表 —— 添加两个步骤（HTTP + SQL），调度选 Cron `0 0 8 * * *`，通知选「仅失败时通知」。

示例 2：登录后把数据写入数据库 —— 四个步骤：`POST 登录`（提取 `token`、`csrf`）→ `GET 列表`（请求头 `X-CSRF: {{csrf}}`、`Authorization: Bearer {{token}}`，提取 `total`）→ `SQL 建表` → `SQL 写入`（`INSERT ... VALUES ('{{token}}', '{{total}}', '{{now}}')`）。

### 10. iframe 内嵌页面（v1.12）

很多后台把业务页面放在 `<iframe>` 里（菜单在外壳、表格在内层页面），这时**不要**把起始网址改成 iframe 的地址，仍然用外壳地址即可，把 iframe 选择器填到对应位置。

| 配置位置 | 字段 | 填什么 |
| --- | --- | --- |
| UI 步骤（非 `open` 动作） | 所在 iframe | iframe 的 CSS 选择器，如 `#mainFrame`、`iframe[name=content]` |
| 登录步骤（非 `open` 动作） | 所在 iframe | 同上（登录表单在 iframe 里时必须填，否则找不到元素） |
| DOM 抓取规则 | 表格所在 iframe | iframe 选择器；默认跟随表格所在 frame，留空即主文档 |
| 点击翻页 | 下一页按钮所在 iframe | 按钮在 iframe 内时填写；DOM 规则默认自动沿用「表格所在 iframe」 |

要点：

1. **嵌套 iframe** 用 `>>` 分层：`iframe#outer >> iframe.inner`，从左到右逐层进入。
2. **跨域 iframe 没问题**：引擎通过 CDP 进入 frame 上下文执行点击 / 输入 / 取值，不要求同源。
3. **不知道 iframe 选择器也能跑**：留空即可，回放会自动在主页面找不到后扫描所有 iframe；日志里的「已自动进入 iframe #xxx」就是可用选择器，建议之后填回配置。
4. **等 iframe 加载完再操作**：iframe 还没挂载时会报「未找到 iframe」。在动作前加一个 `wait_for` 步骤，选择器填 iframe 元素本身（如 `#mainFrame`）或 iframe 内的列表容器，等待时间按站点给 1-5 秒。
5. **iframe 内点击失败会自动兜底**：真实点击在 iframe 上下文不可用时改用 JS 点击（`element.click()`），不会用顶层坐标误点。
6. **会话采集覆盖 iframe**：回放结束读取 Cookie / localStorage / sessionStorage 时会遍历所有 iframe，内层页面写入的 token 也能被带到后续接口请求。
7. **录制**：录制工作台会自动识别 iframe 内动作并标注 `frame`，草稿直接可用；若手工新增步骤，记得填「所在 iframe」。

排查「新增的打开页面步骤不执行」：

- 任务模式是 `hybrid`：只执行「登录步骤」，不执行「UI 步骤」；要跑页面动作请用 `ui` 模式，或把动作放到登录步骤。
- 任务模式是 `api`：完全不启动页面。
- 网址填错栏位：`open` 动作要填「网址」，不要填到「选择器」。
- 步骤顺序：`open` 应排在需要它的动作之前，可用步骤行的 `＋` 在指定位置插入。
- 页面慢：`open` 后加 `wait_for` 或调大动作等待时间。
- 保存时会做 UI 步骤定位校验（选择器为空、iframe 填错都会提示）。

「启用翻页抓取 → 页面点击翻页」里的**规则**指的是一条**数据来源**，不是某一次点击：

- 网络规则：按「URL 包含」匹配一个接口，每页新产生的该接口响应都归这条规则。
- DOM 规则：按行选择器匹配页面表格，每页新渲染的行都归这条规则。
- 「页面点击翻页」只是这条规则的翻页方式；翻页后数据仍走这条规则的字段映射、主键去重、独立落库配置，不会变成新规则。
- 不同接口要分别落不同数据库时，请拆成多条规则，每条规则单独选表 / 单独关闭落库。

### 11. 「打开页面」为什么会丢登录态（v1.12.3）

现象：登录成功后，在 UI 步骤里再加一条「打开页面」跳到别的页面，后面的操作全部失败，或者页面直接跳回登录页。

**同一个任务里 Cookie 是共享的**：一次运行只启动一个浏览器、一个 Page，所有 `open` 步骤都在同一个页面上执行，Cookie 和同源 localStorage 一直带着。丢掉的通常是下面这几种：

1. **整页打开会重新加载文档**：`open` 默认是真导航（相当于按 F5）。很多后台把 token 只放在 JS 内存里（`let token`、Vuex / Redux store），刷新就没了；路由守卫发现没 token，直接把你踢回登录页。
2. **localStorage / sessionStorage 按源隔离**：换子域、换端口、换协议都是另一个源，存储读不到；`sessionStorage` 连新标签页都不共享。
3. **iframe 内部地址不能当顶层地址打开**：直接打开 iframe 的 URL 会丢掉父页面上下文（父页传入的 token、postMessage、Referer、路由守卫），页面可能空白或跳登录。
4. **默认每次运行都是全新的无痕浏览器**：上一次运行登录过，这次运行不会继承 Cookie（避免脏会话）。如果需要跨运行复用登录态，把「系统设置 → 浏览器内核 → 会话模式」改成**共享会话**，或在任务「基本设置 → 浏览器会话」里单独指定，见 5.12。
5. **步骤顺序**：`open` 排在登录步骤之前（或排在错误位置），等于还没登录就先跳走，自然「会话没了」。

三种改法：

| 改法 | 怎么做 | 适用 |
| --- | --- | --- |
| 站内跳转（首选） | 该 `open` 步骤的「打开方式」选 **站内跳转（不刷新页面，保留登录态）** | 同源 SPA，hash 路由或 `pushState` 路由 |
| 用页面里的入口进 | 删掉 `open`，改成 `click` 点菜单 / 链接 / 按钮进入目标页 | 菜单能点到；最贴近真实操作，风控最友好 |
| 先 UI 登录再接口抓 | 任务模式用 `hybrid`：登录步骤只放 UI 登录动作，数据由接口步骤带着会话请求 | 接口好抓、但登录必须走页面 |

### 12. 会话共享：让每次运行复用登录态（v1.13.0）

默认 `session_mode = isolated`：每次运行都开一个全新的临时用户目录 + 无痕，跑完即删，任务之间互相隔离。这是最安全的默认值。

有些站点每次登录都要短信 / 滑块 / 图形验证码，或者登录后还要人工确认，希望**登录一次，后面每次运行直接复用**。这时用共享会话：

**界面配置**

1. 「系统设置 → 浏览器内核 → 会话模式」选择 **共享会话（复用 Cookie / 登录态）**；
2. 「共享会话目录」留空即用 `<数据目录>/browser/profiles/default`，也可以指向别的盘（如 `D:\profiles\gov`）；
3. 点「保存浏览器配置」，再点一次任务运行即可。第一次运行会正常登录，Cookie / localStorage / IndexedDB 写进该目录；之后的运行启动浏览器时直接带着这些数据。
4. 需要重新登录（换账号、会话过期、被风控标记）时，点「清空共享会话」，目录会被重置。

**任务级覆盖**：任务编辑器「基本设置 → 浏览器会话」可选 `跟随系统设置 / 每次全新会话 / 共享会话`。例如只有某个必须过验证码的任务用共享会话，其余任务仍然无痕，就把它单独设为 `shared`。纯接口（api）模式不启动浏览器，该项不生效。

**注意**

- 同一个会话目录同一时刻只能被一个 Chromium 进程使用。共享模式下并发任务会自动排队等待（最多 2 分钟），超时会提示「共享会话正被另一个任务使用」。需要并行跑多个任务时，请给它们配不同的 `profile_dir`，或改用隔离模式。
- 共享会话目录是明文 Cookie，等同于「已登录的浏览器配置」，注意机器权限与备份安全。
- 共享模式下「打开页面」丢登录态的判定仍然适用：如果站点把 token 只放在 JS 内存里，跨运行复用只能恢复 Cookie / localStorage，内存态依旧需要重新走登录步骤。
- 录制工作台（有头浏览器）也遵循该设置：共享模式下录制时登录一次，后续运行可直接复用；不想污染共享会话时，把会话模式临时切回「每次全新会话」再录制。

## 六、配置文件 `config.json`

```json
{
  "addr": "127.0.0.1:8686",
  "data_dir": "./data",
  "browser": {
    "bin_path": "",
    "headless": true,
    "proxy": "",
    "download_host": "npm",
    "custom_host": "",
    "revision": 0,
    "no_sandbox": false,
    "session_mode": "isolated",
    "profile_dir": "",
    "user_agent": "",
    "accept_language": "",
    "platform": "",
    "timezone": "",
    "width": 1440,
    "height": 960,
    "extra_args": []
  },
  "max_body_kb": 256,
  "proxy": "",
  "timeout_sec": 60,
  "run_workers": 2
}
```

- `browser.bin_path`：自定义 Chrome/Chromium 路径；留空则依次使用：下载的内核 → 系统 Chrome / Edge / Chromium / Brave
- `browser.headless`：采集回放是否无头（录制始终有头）
- `browser.download_host`：`npm`（默认，国内较快）/ `google` / `custom`
- `browser.no_sandbox`：Linux / 容器环境建议 `true`
- `browser.user_agent`：自定义 UA，留空用内核默认（也可在界面「设置 → 浏览器指纹」里改，带预设与「检测当前内核 UA」）
- `browser.accept_language`：`Accept-Language` 请求头，如 `zh-CN,zh;q=0.9`；留空用内核默认
- `browser.platform`：`navigator.platform`，如 `Win32` / `MacIntel`；需与 UA 对应，不要随意拼凑
- `browser.timezone`：时区，如 `Asia/Shanghai`
- `browser.width` / `browser.height`：视口尺寸（也是浏览器窗口尺寸）
- `browser.extra_args`：额外 Chromium 启动参数数组，如 `["--disable-gpu"]`
- `browser.session_mode`：`isolated`（默认，每次全新无痕）/ `shared`（共享会话，复用 Cookie、localStorage，登录一次后续复用）
- `browser.profile_dir`：共享会话的用户目录，留空用 `data/browser/profiles/default`
- `max_body_kb`：单个请求/响应体在日志中保存的最大体积

以上参数除了直接改文件，也可以在「系统设置」里改：`addr` / `data_dir` / `timeout_sec` / `proxy` / `max_body_kb` 在「运行参数（可自定义）」卡片，浏览器相关在「浏览器内核」与「浏览器指纹」卡片。界面保存会做合法性校验（地址必须带端口、代理只接受 http/https/socks5 等），`addr` 与 `data_dir` 改完需重启进程生效。

浏览器内核下载：界面「系统设置 → 浏览器内核 → 下载内核」，下载到 `data/browser`，自动按 平台/架构 选择（`Win_x64`、`Win`、`Linux_x64`、`Linux_arm64`、`Mac`、`Mac_Arm`）。

## 七、API 一览

基础路径 `/api`，返回 `{code, data, msg}`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/health` | 健康检查 |
| GET | `/dashboard` | 仪表盘统计 |
| GET | `/logs?level=&limit=` | 运行日志 |
| GET/POST | `/tasks` | 任务列表 / 新建 |
| GET/PUT/DELETE | `/tasks/:id` | 任务详情 / 更新 / 删除 |
| POST | `/tasks/:id/run` | 立即执行 |
| POST | `/tasks/:id/clone` | 复制任务 |
| GET | `/tasks/:id/runs` | 任务运行记录 |
| GET | `/tasks/export`、`/tasks/:id/export` | 导出全部 / 单个任务 |
| POST | `/tasks/import` | 导入任务（JSON 文件体） |
| GET | `/runs?task_id=&status=&page=` | 运行记录列表 |
| GET | `/runs/:id`、`/runs/:id/steps`、`/runs/:id/data` | 运行详情 / 步骤日志 / 抓取数据 |
| GET | `/runs/:id/export?format=xlsx|csv` | 导出运行数据 |
| POST | `/studio/sessions` | 开始录制（`{url, mode}`） |
| GET | `/studio/sessions`、`/studio/sessions/:id/capture` | 会话列表 / 录制结果 |
| GET | `/studio/sessions/:id/requests/:reqId` | 请求详情（含请求/响应体） |
| GET/PUT | `/settings` | 平台设置（通知、请求体大小等） |
| PUT | `/config/general` | 运行参数（监听地址、数据目录、超时、代理、包体上限），返回 `restart_required` |
| GET | `/browser/status`、`PUT /browser/config` | 内核状态 / 浏览器与指纹配置 |
| POST | `/browser/probe-ua` | 启动一次无头浏览器，回报内核真实的 UA / 平台 / 时区 |
| POST | `/browser/download` | 下载 Chromium 内核 |
| POST | `/studio/sessions/:id/draft` | 生成任务草稿 |
| POST | `/studio/sessions/:id/stop`、DELETE `/studio/sessions/:id` | 停止 / 删除会话 |
| GET/POST | `/datasources`、PUT/DELETE `/datasources/:id` | 数据源管理 |
| POST | `/datasources/:id/test`、`/datasources/test` | 连接测试 |
| POST | `/datasources/:id/exec` | 在数据源上执行 SQL（`{sql, limit}`，返回列/行/影响行数），用于验证 SQL 模板 |
| GET/POST | `/jobs` | 定时作业列表 / 新建 |
| GET/PUT/DELETE | `/jobs/:id` | 作业详情 / 更新 / 删除 |
| POST | `/jobs/:id/run`、`/jobs/:id/clone` | 立即执行 / 复制作业 |
| GET | `/jobs/:id/runs`、`/job-runs?job_id=&status=` | 作业执行记录 |
| GET/DELETE | `/job-runs/:id` | 执行详情（含步骤日志） / 删除 |
| GET/PUT | `/settings` | 平台设置 |
| POST | `/settings/test-wecom` | 企业微信测试消息 |
| GET | `/browser/status`、POST `/browser/download`、PUT `/browser/config` | 浏览器内核状态 / 下载 / 配置 |
| POST | `/browser/session/clear` | 清空共享会话目录（Cookie / localStorage / IndexedDB） |
| GET | `/notifications`、POST `/notifications/read-all`、DELETE `/notifications` | 消息中心 |

## 八、目录结构

```
collect/
├─ main.go                 启动入口（-addr / -open）
├─ config.json             运行配置（首次启动自动生成）
├─ internal/
│  ├─ api/                 Gin 路由与接口
│  ├─ browser/             浏览器内核管理、录制器、回放器、DOM 提取
│  ├─ collector/           采集引擎（三种模式）、变量替换、分页、映射去重、落库（表/SQL）、导出
│  ├─ jobs/                定时作业执行器（HTTP / Shell / SQL）
│  ├─ notify/              平台消息 + 企业微信机器人
│  ├─ scheduler/           cron / interval 调度
│  ├─ studio/              录制会话与任务草稿生成
│  ├─ store/               SQLite 主库（GORM）
│  └─ model/               数据模型
├─ web/                    内嵌前端（Vue 3，零 Node 构建）
│  ├─ index.html           页面骨架 + Vue 挂载 + 脚本加载
│  ├─ style.css            视觉规范（配色 / 组件 / 表格 / 表单）
│  ├─ vendor/              vue.global.prod.js（内嵌，不联网）
│  └─ js/                  core.js（API/路由/Toast/Modal）、app.js（外壳），page-*.js（各页面）
├─ tools/
│  ├─ mocksite.py          本地模拟站点（登录 + 门店列表 + DOM 表格）
│  ├─ e2e_test.py          三种模式 + 调度 + 导入导出 + SQL 落库 + 定时作业 端到端自测
│  ├─ rectest/main.go      录制工作台自测（录制 → 抓包 → 生成草稿）
│  └─ uicheck/main.go      前端冒烟测试（路由渲染 / 编辑器保存 / JS 异常）
├─ data/                   运行数据（app.db、日志、导出、浏览器内核）
├─ bin/                    编译产物
└─ dist/                   跨平台打包产物
```

## 九、自测

```powershell
# 1) 启动模拟站点（另开窗口）
python tools\mocksite.py                # http://127.0.0.1:18099

# 2) 启动平台
.\bin\collect.exe -addr 127.0.0.1:18080 -open=false

# 3) 三种模式 / 调度 / 导入导出
python tools\e2e_test.py
python tools\e2e_test.py --only api|hybrid|ui|scheduler|import|sqlsink|jobs|cfg
python tools\e2e_test.py --only uipg|uipgt|uiframe|studio|studioiframe|spaopen

# 4) 录制工作台
go run ./tools/rectest

# 5) 前端冒烟
go run ./tools/uicheck
```

模拟站点账号：`demo / 123456`。

## 十、常见问题

**Q：录制不到请求？**
确认浏览器是平台拉起的那个窗口，操作后点「刷新」；页面若是 iframe 内部接口，也会被记录（按 session 过滤）。

**Q：页面是 iframe 内嵌，回放点不到元素 / 录不到 iframe 内动作？**
任务编辑器里给对应 UI 步骤填「所在 iframe」；DOM 规则填「表格所在 iframe」；点击翻页填「下一页按钮所在 iframe」。嵌套用 `>>`。如果 iframe 是异步挂载的，先在动作前加 `wait_for` 等 iframe 出现。

**Q：不知道 iframe 地址，能不能直接把 iframe 的 URL 当成一个「打开页面」步骤来操作？**
不推荐。外层页面才是真实入口：Cookie 虽然在同一个浏览器里共享，但直接把 iframe 地址开到顶层会丢掉父页面上下文（父页传入的 token、postMessage、Referer、路由守卫都可能失效），页面可能跳登录或空白，录到的选择器也可能对不上。正确做法是：`open` 仍填外层地址，后续动作留空「所在 iframe」让引擎自动识别，或把日志里提示的 `#xxx` 填进去。这样可以完整保留 iframe 内会话。

**Q：纯 UI 任务里新增的「打开页面」步骤不执行？**
先看任务模式：`hybrid` 只跑登录步骤，`api` 不跑页面；`open` 动作要填在「网址」栏；确认步骤顺序与等待时间。详情见「五、10. iframe 内嵌页面」的排查清单。

**Q：录制工作台里明明有「打开 xxx」的页面动作，生成任务后步骤里却没有？**
两种原因：① 生成草稿的「模式」选的是**纯接口**，这种模式只用请求、不会生成 UI 步骤；② 老版本不会把录制中的页面跳转转成步骤。现在（v1.12.2）已修：模式默认跟随录制会话，跳转自动变成 `打开页面` 步骤；如果仍有问题，草稿面板与任务编辑器顶部会出现黄色提示说明原因。

**Q：hybrid 模式接口 401？**
检查登录步骤是否成功（运行记录的「登录」步骤日志），会话变量名是否与 Header 中 `{{变量}}` 一致；部分站点要求同时带 `Cookie` 与 `X-CSRF`。

**Q：同一个任务里，在 UI 步骤新增（或重新打开）一个页面后，会话就没了？**
不是「不是一个会话」，而是**整页打开把页面刷新了**：一次运行只有一个浏览器、一个 Page，Cookie 一直是同一份；丢的是只存在 JS 内存里的登录态（`let token`、Vuex / Redux store），刷新即失效，路由守卫随后把你踢回登录页。另外换域名 / 端口 / 协议就是换了源，读不到原源的 localStorage；每次运行也是全新的无痕浏览器，不继承上次运行的 Cookie。
处理办法：① 把这条 `open` 步骤的「打开方式」改成 **站内跳转（不刷新页面，保留登录态）**（同源 SPA 必选）；② 或者删掉这条 `open`，改成 `click` 点页面里的菜单 / 链接进入；③ 确认这条打开步骤排在登录步骤之后。运行日志现在会写「打开后页面跳到了登录页 —— 这一步把登录态弄丢了」，直接指出是哪一步。详见「五、11」。

**Q：Linux 上浏览器起不来？**
`config.json` 里把 `browser.no_sandbox` 设为 `true`，并确保安装了 Chromium 依赖（`libnss3`、`libatk`、`libgbm` 等）。

**Q：抓取数据量很大？**
调大 `max_pages`、`page_size`；落库用 `db` 模式 + `batch_size`（默认 200）；表格导出为 xlsx 时每行一条记录。

**Q：如何多台设备共享任务？**
任务列表「导出」→ 另一台设备「导入」；或直接调用 `GET /api/tasks/export`。
