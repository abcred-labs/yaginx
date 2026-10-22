# Modra 迁移：测试发现与修复方案

测试时间：2026-10-22。方法：Chrome 连真实网关 `goya.feinian.net` 逐页读取与界面操作，写操作只在测试站点 `autotest.feinian.net` 和少量可回滚的数据上进行；自动化部分是 `adminui/e2e` 的 22 条冒烟用例。

## 一、已修复

| 问题 | 原因 | 修复 |
|---|---|---|
| 暗色模式下图标几乎看不见 | Font Awesome SVG 默认填充黑色 | `ui/icons.tsx` 统一加 `fill-current` |
| 首页指标数字被截断（`30,000 / 30,…`） | 紧凑指标用了 `truncate`，真实数据更长 | 改为自动换行 |
| 切换页面时弹出"请求失败 signal is aborted" | 页面切换主动取消的请求被当成传输错误 | `FetchJson` 忽略 `AbortError` |
| 删除站点的确认框没有站点名 | 沿用旧文案"此资源" | 文案带上站点名 |
| 开发代理是占位值，任何 `/api` 请求让 Vite 崩溃 | `config/constant.ts` 里是 `https:///` | 默认 `https://goya.feinian.net/`，可用 `VITE_PROXY_HTTP` 覆盖 |
| 一个页面出错后，切到别的页面仍显示错误 | `ErrorBoundary` 不随路由复位 | 加 `resetKeys` |
| antd 全局样式盖过 Modra 按钮文字色 | reset 样式不在层里 | 已随 antd 一并移除 |

## 一（续）、已修复（第二批，2026-10-22）

| 问题 | 提交 | 核对方式 |
|---|---|---|
| 开发模式 `brotli-wasm` 报错 | `a86d758` | `optimizeDeps.exclude`；重启开发服务器后请求追踪页 wasm 文件正常加载（304），无控制台错误 |
| 首页趋势图右侧纵轴重叠 | `a86d758` | Latency 轴外移并加宽 grid；真实数据下截图确认两列刻度分开；新增单元测试 |
| 数据库性能页面包屑停在上一页 | `a86d758` | 页面发布 meta；新增冒烟用例，已验证去掉修复后该用例失败、恢复后通过 |
| `echarts` peer 警告 | `a86d758` | `peerDependencyRules.allowedVersions`；`pnpm install --lockfile-only` 不再提示，锁文件未变 |
| 后端站点搜索不认主机绑定、规则目标、上游池端点 | `f1c6317` | 在真实库上只读验证：上游过滤命中从 95 变为 98（多出的 3 个站点的上游只在规则或池里）；域名过滤对旧站点结果不变。集成用例已写，但本机没有可用的 Postgres（Docker 未启动），**集成用例尚未实际运行**，需要在 CI 或本地数据库上跑一次 |

## 二、待修复

### P1 后端：站点搜索不认新版主机绑定（已修复，见上）
- 现象：新建站点只配置了主机绑定，在站点列表按域名搜不到（搜 `autotest` 返回 0 条）；按"上游地址"搜也只认旧字段。
- 原因：`gateway/internal/repository/postgres_websites.go:489-512` 的 `websiteSearchQuery` 只查 `specifications.defaultHost` 和旧的 `hosts` 数组；上游只查 `defaultDestination`。新界面保存时 `hosts` 为空（请求体与迁移前一致，不是前端引入的）。
- 方案：
  1. 域名过滤增加对 `hostBindings[].hostname` 的 `EXISTS (jsonb_array_elements …)` 匹配；上游过滤增加 `proxyRules[].destination` 和 `upstreamPools[].endpoints[].address`。具体 JSON 路径先对照 `websiteResponseQuery` 和存储结构确认。
  2. 在 `internal/management/postgres_integration_test.go`（第 349 行附近已有域名过滤用例）补两条：只有主机绑定的站点能被搜到；只有上游池的站点能按上游地址搜到。
  3. 备选（不推荐单独采用）：保存时用主机绑定回填 `hosts`，会让两处数据长期重复。

### P2 开发模式下 `brotli-wasm` 报错（已修复）
- 现象：`pnpm dev` 下进入请求追踪页，控制台报 `WebAssembly.instantiate(): expected magic word`，Body 解压不可用；生产构建正常。
- 原因：Vite 预构建 `brotli-wasm` 后，wasm 请求落到了 `index.html`。
- 方案：`vite.config.ts` 增加 `optimizeDeps: { exclude: ['brotli-wasm'] }`，并确认 dev 下能解压后，冒烟用例就可以改跑开发服务器，省掉每次先构建。需要先验证，不要直接合并。

### P2 首页趋势图右侧两个纵轴标签重叠（已修复）
- 现象：首页"请求与延迟趋势"图里，延迟轴（Latency）和 RPS 的右轴刻度叠在一起。迁移前的图表配置就是这样（`DashboardPanels.tsx` 的 `buildTrendWithLatency` 在已有的两个 y 轴之外又加了第三个）。
- 方案：给第三个轴设置 `offset`（约 56px），并给图表 `grid.right` 留出空间；补一个 `buildTrendWithLatency` 的单元测试，断言三个轴的 `offset` 不重复。

### P2 数据库性能页面包屑停在上一页（已修复）
- 现象：进入"数据库诊断"后，顶栏仍显示上一个页面的标题和路径。
- 原因：该页没有调用 `usePublishPageMeta`，也没有用 `WorkbenchPage`（迁移前就如此）。
- 方案：在 `Monitors/DatabasePerformance/index.tsx` 调 `usePublishPageMeta({ trail: 'SYSTEM / DATABASE', title: '数据库性能' })`，并在冒烟用例里断言面包屑末段。

### P2 主入口 CSS 约 525 KB（gzip 67 KB）
- 原因：Modra 的预设把三个品牌（ocean、forest、ids）和三种密度的全部令牌变量都打进来了，约占 75%。
- 方案：
  1. 优先请 Modra 提供只含默认品牌和默认密度的令牌入口（例如 `@modra/tokens/css/default`），yaginx 改引该入口。
  2. 在这之前不要手工裁剪 `node_modules`；如果要先压体积，可以自建 `styles/tokens.css` 只导入需要的变量，但升级 Modra 时要人工同步，不建议。

### P3 `echarts` peer 警告（已修复）
- 现象：`@modra/react` 声明 `echarts@^5.5.0`，yaginx 用 6.1.0，pnpm 报 unmet peer。
- 事实：yaginx 没有使用 Modra 的 `EChart`，图表走自己的 `EChartPanel`，所以不受影响。
- 方案：`package.json` 加 `pnpm.peerDependencyRules.allowedVersions: { "@modra/react>echarts": "6" }`，或请 Modra 把 peer 放宽为 `^5.5.0 || ^6`。

### P3 CI 里新增的 Playwright 步骤未在 GitHub 上跑过
- 方案：推送分支后先看第一次 CI 结果。如果 `playwright install --with-deps` 太慢，缓存 `~/.cache/ms-playwright`。冒烟用例只在 Chromium 上跑，暂不扩到 Firefox 和 WebKit。

## 三、需要产品确认的行为变化

- 站点列表去掉了行号列；行操作从拆分按钮改为"查看分析""配置"直接显示，"查看规则""删除"放进更多菜单。
- Top SQL 只有摘要单元格能点开详情，不再整行可点。
- 代理转换的键名补全改用 `<datalist>`；开关的状态文字放在旁边；Select 清除改为"全部/不选择"选项或清除按钮。
- 时间范围选择器没有秒。
- 实心的 Font Awesome 图标比原来的线性图标更粗。

## 四、Modra 侧的缺口（可提给 Modra 仓库）

| 缺口 | 现在的绕法 |
|---|---|
| 没有 AutoComplete | 输入框加 `<datalist>` |
| `Switch` 没有内部文字 | 文字放在旁边 |
| `Select` 没有干净的清除能力 | `SelectClearButton` 或"全部"选项 |
| `Statistic` 数值字号偏大 | 自建 `MonStat` |
| `Progress` 不支持颜色 | 用选择器覆盖指示条背景 |
| `ToggleGroup` 的 `variant` 在子项上 | 每个子项单独传 |
| `DateRangePicker` 没有时间部分 | 用 `DateTimeRangePicker`（无秒） |
| 令牌入口包含所有品牌与密度 | 见 P2 |
| `echarts` peer 只声明 5.x | 见 P3 |

## 五、没有测的部分和建议

线上环境不适合做破坏性操作，以下没有验证：保存运行配置（会触发滚动重启）、ACME 重试/取消/证书申请、恢复备份、节点重启/禁用/删除，以及状态码气泡的悬停交互。
- 建议用仓库里的 `docker-compose` 起一套本地网关（`yaginx-compose` 分支已有），在上面把这些操作各点一遍。
- 其余界面部分已有 22 条冒烟用例覆盖。

## 六、建议的处理顺序

1. 先推送分支，让 CI 跑一次（P3）。
2. 前端小修一起做：P2 的 wasm、图表轴、面包屑、echarts peer（一个提交，半天内）。
3. 后端搜索修复单独一个 PR（P1），带集成测试。
4. 向 Modra 提令牌入口和缺口清单（P2 体积、第四节）。
5. 在本地 compose 环境补测第五节的破坏性操作。
