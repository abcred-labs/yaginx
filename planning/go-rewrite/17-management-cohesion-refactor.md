# 17 — 管理面内聚重构（契约驱动 + 统计域合并）

> 注：本文记录的 `contractlint`、`contractcheck`、`proxycheck` 及 fixture 抓取工具已从仓库移除，相关命令与结果仅作历史记录。fixture 回放测试仍保留，数据与加载代码位于 `gateway/internal/testfixtures`。

状态：**已完成**。本文档既是设计记录也是落地记录；"决策记录"一节保留了与初始方案的偏离及其理由。

## 背景

管理面的功能分散在几个固定的大文件里，而不是按领域垂直切分。具体症状：

1. **统计能力被拆成 4 套平行 API。** `/api/host_traffic/*`、`/api/observability/*`、
   `/api/resource/report/*`、`/api/resource_view/*` 共 23 个端点读的是重叠的同一组表
   （`gateway_traffic_contribution`、`resource_report`、`host_traffic`），却各自定义了
   独立的 query 类型、分页约定和响应形状。`host_traffic` 用 stringified 数字加
   `*Human` 预格式化字段（遗留格式），`observability` 用 typed 数字 —— 同一功能的两代表示并存。

2. **`management.Store` 是 64 方法的上帝接口**（`internal/management/store.go:432`），
   唯一实现是 3320 行、77 方法的 `repository.Postgres`。任何改动都要碰这两个文件。

3. **组合根吞掉领域逻辑。** `internal/app/serve.go` 1429 行，`runServe` 单函数 666 行，
   内含报表物化调度、数据保留策略、节点健康监控、SPA 静态服务。

4. **两套并行的 API 栈。** 生产路径是 `router.go` 1056 行全内联闭包；
   `internal/management/generated/` 的 98k 行 ogen 代码**零生产引用**，只被 CI 做 drift 检查。
   `internal/contract/openapi.go:105` 的注释自己承认它
   "without pretending that production handlers are generated from the document"。

关键发现：**OpenAPI 契约里每个操作都已带 `x-go-handler: internal/management/<domain>.Handler.<Method>`
注解**，且 `contractlint` 已强制该注解非空（但不校验 handler 真实存在）。
也就是说目标架构早已写进契约，只是 Go 服务端从未落地。本次重构是把这个设计做完。

## 结果

| | 操作数 | 领域包数 | `Store` 接口 | `serve.go` |
|---|---|---|---|---|
| 重构前 | 68 | 13（仅存在于注解） | 1 个 × 64 方法 | 1429 行 |
| 重构后 | 56 | 11（真实存在） | 11 个 × 1–9 方法 | 677 行 |

契约成为单一来源：`contractlint` 现在校验 `x-go-handler` 指向的包**真实存在且有 Go 源文件**
（`internal/contract/openapi.go:103` `ValidateHandlerPackages`），注解与现实不可能再脱节。

## 决策记录

- **OpenAPI 定位：方案 A（用户选择）→ 落地时修正为"垂直切分 + 删除 `generated/`"。**
  原方案是让 ogen 生成码成为真实路由。实施中改为手写领域 handler、并**删除整个
  `internal/management/generated/`（98k 行）**，理由三条：
  1. 响应信封 `{code,msg,errMsg,tid,data}` 与 `ID` 的十进制字符串跨界约定是本项目的硬约束，
     ogen 的类型生成需要大量 `x-ogen-*` 标注和手写 marshaller 才能复现，收益被抵消；
  2. 真正要消除的是"两套并行栈"，而垂直切分同样消除它，且不引入 98k 行生成码的维护面；
  3. `contractlint` 的 `ValidateHandlerPackages` 以更低成本提供了同等的"契约↔代码不漂移"保证。
  `adminui` 侧的 `openapi-typescript` 生成（`src/api/generated/management.ts`）**保留** ——
  前端类型从契约生成没有信封/ID 的阻碍。
- **排序：单阶段完成（用户选择）。** 契约、Go 服务端、前端一次性改完。无中间可发布状态，
  内部严格分步并每步跑测试。
- **未登记端点（用户选择）：** `/api/node/register`、`/api/node/heartbeat` 加入契约
  （两者无鉴权，声明 `security: []`）；`/api/node/approve`、`/api/node/search` 作为
  `gateway_node` 的重复别名删除。
- **统计 API 形状：统一查询契约，7 个端点（用户选择）。** 保留按视图分的端点，但全部共用
  `AnalyticsQuery` 请求契约和 `AnalyticsBase` 响应基类。
- **统计契约收窄（落地修正）。** `AnalyticsQuery` 最终只有 `from`/`to`/`domain` 三个字段。
  原设计的 `grain`、`Subject{type,key}`、`timeseries.metrics[]` 全部移除：grain 由窗口宽度
  推导而非由客户端请求，subject 实际只有 domain 一维，metrics 的所有调用方都要全量指标。
  把未实现的能力写进契约等于制造新的"注解与现实脱节"。
- **`Act-Trace-Id` 与 `Cache-Control` 不入契约。** 两者是传输层关注点（trace 透传、
  SPA 静态缓存），由 `transport.go` 和 `serve_static.go` 统一施加于所有响应，
  不是任何单个操作的契约。逐字节行为仍由 fixture 守住。
- **统计端点不要求 fixture。** 7 个 analytics 操作中的 5 个（overview/domains/timeseries/
  contributions/details）是 Go 原生新形状，没有既有基线可对齐，
  因此不带 `x-fixture-required`。覆盖由 `internal/management/analytics/handler_test.go`
  （270 行）承担。契约总计 56 操作 / 26 个 fixture-required。
- **`yaginx_observability_*` 指标名不改。** 词汇统一到 analytics 时，Prometheus 指标名
  `yaginx_observability_rows`、`yaginx_observability_build_duration_seconds`，以及
  `Metrics.ObserveObservabilityLoad`、`metricObservabilityOperation`、
  `Postgres.SetObservabilityObserver`、`internal/observability` 包名**全部保留** ——
  指标名是外部 dashboard 与告警规则的契约，改名是破坏性变更，与内部重构无关。

## 删除清单（非合并，纯删除）

| 端点 | 数量 | 依据 |
|---|---|---|
| `/api/resource/report/*` | 5 | 前端零调用。`adminui/src/api/resourceReport.ts` 的 3 个导出函数无任何页面引用，且 `src/pages/Home/homeDataSource.test.ts` 显式断言 Home 不得含 `resourceReport`/`getDashboardSummary`/`getAllReportData`/`getTypeReportData` —— 迁移到 observability 早已刻意完成 |
| `/api/host_traffic/*` | 7 | **全部删除，而非合并。** 见下 |
| `/api/node/approve`、`/api/node/search` | 2 | 旧别名，仅 `postgres_integration_test.go` 引用 |

**`host_traffic` 是删除而不是合并。** 原计划把它的 5 个操作映射进 analytics；
实际做法是整栈删除：它读的是同一组表，但表示法是旧式的 stringified 数字 +
`*Human` 预格式化字段，没有任何一行逻辑值得搬进新端点。前端的
`pages/Monitors/HostTraffics/*` 改为直接调用 analytics 端点。
连带删除的 Go 侧：`Store.SearchHostTraffic`、`HostTrafficChartSummary`、
`HostTrafficRequestTrend`、`HostTrafficRequestContribution`、`HostTrafficBandwidthAnalysis`。

连带删除：`Store.ResourceReports(ctx, string, map[string]any)`、`router.go` 的
`map[string]any` 动态解码器与 path→operation 循环、整个
`internal/repository/postgres_resource_reports.go` 死报表引擎
（`reportCycle`/`reportPeriod`/`reportWindowFor`/`buildAllReportData`/`buildTypeReportData`/
`buildDashboardSummary` 等）。
**保留**：`BackfillDailyResourceReports`、`resource_report` 表、`internal/app/report.go` ——
daily 物化仍在用。死引擎里的 `reportCycleMinutely/Hourly/Daily` 常量是活的列编码，
已移入 `internal/repository/traffic_contributions.go`（`reportCycleWeekly` 确为死代码，丢弃）。

前端连带删除：`src/api/resourceReport.ts`、`src/api/resourceView.ts`、
`src/pages/Home/dashboardData.ts`、`src/pages/Home/TrafficSummaryCards.tsx`。

## 统计域合并：14 → 7

合并范围 = `traffic` 5 + `observability` 6（去掉 `/database`）+ `resourceview` 3 = 14 个操作。

**不纳入合并**（语义不同，各自独立成包，提高而非降低内聚）：
- `/api/request_history/*` → `history` 包。请求级审计明细，表 `request_history`，不是聚合统计。
- `/api/observability/database` → `diagnostics` 包。读 `pg_stat_*`，是基础设施诊断。

### 统一契约

```yaml
AnalyticsQuery:          # 所有统计查询共用
  from:   int64 | null   # 窗口起（epoch），null = 服务端默认窗口
  to:     int64 | null   # 窗口止
  domain: string | null  # 收窄到单个 Host；null = 聚合全部

AnalyticsBase:           # 所有统计响应共用的基类
  asOf:     int64
  from:     int64
  to:       int64
  grain:    minute | hour | day      # 由窗口宽度推导
  source:   gateway_traffic_contribution | resource_report | 两者
  coverage: AnalyticsCoverage
```

表示法统一到 **typed 数字**（observability 那一代），废弃 stringified + `*Human`。
humanization 在前端完成（`src/utils/formatters.ts` 的 `formatBytes`/`toNumber`
已同时接受 `number | string`）。

### 端点映射

| 新端点 | 取代 |
|---|---|
| `POST /api/analytics/overview` | `observability/overview`、`observability/dashboard`、`host_traffic/chart_summary` |
| `POST /api/analytics/domains` | `observability/domains` |
| `POST /api/analytics/timeseries` | `observability/timeseries`、`host_traffic/request_trend`、`host_traffic/bandwidth_analysis` |
| `POST /api/analytics/contributions` | `observability/contributions`、`host_traffic/request_contribution` |
| `POST /api/analytics/details` | `observability/details`、`host_traffic/search_range` |
| `GET /api/analytics/catalog` | `resource_view/catalog` |
| `GET /api/analytics/subject` | `resource_view/domain`、`resource_view/website` |

`observability/dashboard` 的 `sections []string` 模式被 `overview` 吸收。

### 前端：Home 仪表盘分区

`ObservabilityDashboard` → `AnalyticsDashboard`。原先 3 个数据源（dashboard sections +
summary cards + trend）合为 1 个 `overview` 调用，页面分区从 3 段压到 2 段
（概览卡片 + 趋势图），`TrafficSummaryCards` 的内容内联进概览区 —— 它存在的唯一理由
是它有自己的数据源，数据源合并后它不再是一个独立关注点。

## 落地包结构

```
internal/management/
  envelope.go, pagination.go, key_value.go, availability.go,
  decode.go, transport.go, observe.go, bearer.go, read_models.go,
  store.go, mcp.go                                      共用原语
  server/server.go      组合根：109 行，挂载 11 个领域 handler
  account/      8 ops
  website/      8 ops
  webdomain/    6 ops
  settings/     6 ops
  nodes/        8 ops  (6 + register + heartbeat)
  ipblacklist/  5 ops
  credentials/  4 ops
  analytics/    7 ops  ← observability + resourceview 合并（host_traffic 删除）
  history/      2 ops
  diagnostics/  1 op
  systemerrors/ 1 op
                       = 56 ops
```

`store.go` 的 64 方法上帝接口按此边界拆成 11 个领域 `Store` 接口
（account 7、analytics 9、credentials 4、diagnostics 1、history 2、ipblacklist 5、
nodes 8、settings 6、systemerrors 1、webdomain 5、website 6），
`store.go` 本身只留下共用 DTO 与 `SystemErrorStore`。

`repository/postgres.go` 按同样边界拆成 13 个文件：`postgres.go`（类型与共用）+
`postgres_accounts.go`、`postgres_api_credentials.go`、`postgres_certificates.go`、
`postgres_domains.go`、`postgres_metric_counts.go`、`postgres_nodes.go`、
`postgres_proxy_snapshot.go`、`postgres_request_history.go`、`postgres_smtp.go`、
`postgres_untyped.go`、`postgres_websites.go`、`analytics.go`。

`internal/repository/observability.go` → `analytics.go`（含 `_test.go` 与
`_integration_test.go`），包内全部 `observability*`/`Observability*` 标识符按词边界
改名为 `analytics*`/`Analytics*`。例外见"决策记录"中的指标名条目。

## 组合根瘦身

从 `internal/app/serve.go`（1429 → 677 行）抽出：

| 目标 | 内容 |
|---|---|
| `internal/sysevent/recorder.go`（62 行） | `Recorder` 接口 + `StoreRecorder`（system_error 日志）。`WithoutMetrics()` 保留了"周期性维护 worker 记录失败但不计数"的原有语义 |
| `internal/analytics/materializer.go`（219 行） | `Schedule`/`Due`/`Settings`/`Materializer`（`Catchup`+`Run`）、`ReconciliationWindow`、`MinutelyReconciliationWindow`、`EffectiveDailyRetention` |
| `internal/retention/retention.go`（78 行） | `Telemetry.Run`、`ConfigEvents.Run`，共用 `runDaily` |
| `internal/nodes/monitor.go`（146 行） | `Monitor.Run`、`Revisions`、`WaitForApproved`；三处重复的 drain 块折成一个 `drain` 辅助函数（填掉当前只有 `.gitkeep` 的空包） |
| `internal/app/metrics.go`（101 行） | 无单一归属的运行时 gauge：连接池、telemetry 队列、证书清单 |
| `internal/app/serve_settings.go`（184 行） | `serveSettings`、`parseServeSettings`、`runtimePoolOptions`、`runtimeProxySettings` |
| `internal/app/serve_lkg.go`（103 行） | 磁盘上的 last-known-good 运行时状态 |
| `internal/app/serve_static.go`（40 行） | `spaStatic`、`spaStaticWithCache` |

原计划让 `runServe` 退化为"读配置 → 建依赖 → 启动 N 个 component → 等停止"。
**未完全做到，有意停在文件级拆分**：`runServe` 仍约 580 行，剩下的体量是配置热重载循环
与 listener 组装，两者共享同一函数内约 40 个局部变量。把它们拆开需要引入一个承载这些
变量的运行时状态结构体，改动面与回归风险远超收益。worker 启动已经是
`go retention.Telemetry{...}.Run(lifecycle)` 这样的结构体字面量形式，
组合根的可读性目标已基本达成。

### 收尾审计修正

抽取完成后的复查发现并修掉三处抽取残留：

1. **重复的配置解析（真实缺陷，非仅冗余）。** `runServe` 原地解析 22 个配置项的代码块
   被保留着，同时又调用了新抽出的 `parseServeSettings(cfg)` 并把结果逐个覆盖回同名局部变量。
   两处解析覆盖的键与三处校验完全相同。更要紧的是**原地解析发生在
   `config.ApplySharedRuntime` 之前**：本地配置文件缺失或非法、而数据库中的共享运行时设置
   本应补上合法值的节点，会在 `ApplySharedRuntime` 有机会生效之前就启动失败。
   删掉原地解析块（95 行），只保留 `ApplySharedRuntime` 之后的那一次解析。
2. **错位的文档注释。** `waitForApprovedNode` 的注释被留在了 `serve_settings.go` 里、
   挂到了 `runtimePoolOptions` 头上。
3. **`waitForApprovedNode` 未按计划迁移。** 计划表要求它随 `monitorNode` 进 `internal/nodes/`；
   它的依赖（`*repository.Postgres` + `*slog.Logger`）与该包已有的完全一致，
   迁移为 `nodes.WaitForApproved` 并带回它的注释。

同时补上 `ValidateHandlerPackages` 的单元测试（`openapi_test.go`）：它是替代 ogen 漂移
检查的门禁，此前只在真实代码树上被"通过"验证过，没有任何测试证明它会在漂移时失败。
三个新测试覆盖缺失包、畸形注解（空 / 缺 Type / 缺 Method）与正常包。

## 其他清理

- 删除 `internal/legacy/`（完全空）。
- 删除 `internal/management/router.go`（1056 行）及其手写路由测试，行为测试迁移到各领域包。
- 删除 `internal/management/generated/`（98k 行 ogen 产物）。
- `internal/contract/openapi.go` 的操作数断言 68 → 56；新增 `ValidateHandlerPackages`：
  `x-go-handler` 指向的包必须真实存在且含 Go 源文件。
- `.drone.yml` 去掉 generated drift 检查；`go vet ./...` 改为
  `go vet -tags integration ./...` —— 本次重构中 `postgres_integration_test.go` 的
  编译错误被 `go vet ./...` 漏掉（build tag 使其不被编译），只在后面依赖 Postgres 容器的
  步骤才暴露，这个改动把该类错误拉回到快速门禁。

## 验证

```bash
cd gateway
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go vet -tags integration ./...      # -tags 必需，否则不编译集成测试
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=0 go run ./cmd/contractlint
CGO_ENABLED=0 go run ./cmd/contractcheck
CGO_ENABLED=0 go run ./cmd/proxycheck
gofmt -l internal cmd
```

> 本机 macOS 27 SDK 损坏（`ld: tapi error: unknown architecture arm64e.x1-macos`），
> 故 `CGO_ENABLED=0` 是必需的，且 `cmd/yaginx` 无法在本机链接。与本次重构无关。
> `gofmt -l` 的既存未格式化文件：`internal/certificates/manager_test.go`、
> `internal/contract/approval.go`（刻意不动）。

```bash
cd adminui && pnpm run build && pnpm test
```

> `pnpm api:check` = `api:generate && git diff --exit-code -- src/api/generated/management.ts`，
> 在重新生成的 `management.ts` 提交之前必然失败。

最终结果：build / vet(-tags integration) / test 全绿；
contractlint ok；contractcheck 56 操作（26 fixture-required）/ 52 fixtures /
22 approved deviations / complete；proxycheck 83 rows / 86 fixtures / complete；
adminui 28 files / 78 tests 通过。

关键回归点（已守住）：
- 响应信封（`code`/`msg`/`errMsg`/`tid`/`data`）逐字节不变。
- `ID` 类型的十进制字符串跨界约定不退化为 JSON 数字。
- `/api/node/register`、`/api/node/heartbeat` 保持无鉴权。
- `DatabaseGuard`（`availability.go`）的 503 行为在新路由下仍然生效。
- host_traffic 集成断言删除后，analytics 读模型的 Postgres 覆盖由
  `internal/repository/analytics_integration_test.go`（6 个 store 方法全覆盖）与
  `traffic_contribution_integration_test.go`（物化）承担；
  `internal/management/postgres_integration_test.go` 只保留
  `SearchRequestHistory`/`GetRequestHistory` 断言。
