# 观测与数据兼容

> **v1 RC 范围。** 本章的 Prometheus、OTLP、`/metrics`、telemetry 和 request-history 约定保留为 Go v1 合同与验收依据。R7/v2 不新增外部观测基础设施，改用 PostgreSQL 聚合、JSON stdout/journald、`RuntimeStatus` 和受限 `/ops/status`；详见[第 16 章](16-minimal-infrastructure-runtime.md)。

## 日志

使用 `slog` JSON 到 stdout。固定公共字段：`time`、`level`、`msg`、`service=yaginx`、`version`、`git_sha`、`node_id`、`process_id`。请求日志增加 `tid`、listener、protocol、method、normalized path class、duration_ms、status_code、status_source、error_category；domain/URL 只进可采样日志，不进 Prometheus label。

禁止日志：Authorization/Cookie/JWT、password/recovery code、SMTP password、DSN、master/JWT key、certificate/account private key、ciphertext、完整 keyAuthorization 和未脱敏 body/header。

## Health JSON

health 不使用管理 API 信封，绑定独立 management listener。

`/health/live` 200：

```json
{
  "status": "live",
  "version": "1.0.0",
  "gitSha": "abc123",
  "nodeId": "00000000-0000-0000-0000-000000000000",
  "time": "2026-07-26T00:00:00Z"
}
```

`/health/ready` 200/503：

```json
{
  "status": "ready",
  "ready": true,
  "degraded": false,
  "nodeStatus": "approved",
  "database": "connected",
  "notify": "listening",
  "databaseRevision": "123",
  "appliedRevision": "123",
  "snapshotLoadedAt": "2026-07-26T00:00:00Z",
  "listeners": ["8080/tcp", "9080/tcp", "8443/tcp", "8443/udp", "9443/tcp"],
  "reasons": []
}
```

degraded + valid LKG 仍 200，database/notify 标 unavailable、degraded=true。pending/disabled/draining、无 snapshot、listener missing 或 certificate snapshot invalid 返回 503，reasons 使用稳定 token。当前运行合同只公开 `/health/live` 与 `/health/ready`；DB pool、queue depth、certificate expiry bucket 和 last errors 通过受 CIDR 限制的 `/metrics` 与结构化日志提供，不增加未经合同冻结的 detail endpoint。

## Prometheus

固定 metric：

| 名称 | 类型 | labels |
| --- | --- | --- |
| `yaginx_proxy_requests_total` | counter | listener, protocol, method, status_class, status_source, error_category |
| `yaginx_proxy_request_duration_seconds` | histogram | listener, protocol, method, status_class |
| `yaginx_proxy_bytes_total` | counter | listener, direction |
| `yaginx_proxy_active_connections` | gauge | listener, protocol |
| `yaginx_config_revision` | gauge | source=`database|applied` |
| `yaginx_config_refresh_total` | counter | mode=`incremental|full`, result |
| `yaginx_config_refresh_duration_seconds` | histogram | mode |
| `yaginx_config_notify_reconnect_total` | counter | result |
| `yaginx_db_pool_connections` | gauge | state=`acquired|idle|constructing|max` |
| `yaginx_db_operation_duration_seconds` | histogram | operation, result |
| `yaginx_queue_depth` | gauge | queue=`traffic|history` |
| `yaginx_queue_dropped_total` | counter | queue, reason |
| `yaginx_flush_total` | counter | pipeline=`traffic|history|report`, result |
| `yaginx_certificate_count` | gauge | source, state, expiry_range |
| `yaginx_acme_attempts_total` | counter | result, error_class |
| `yaginx_acme_lock_conflicts_total` | counter | 无 |
| `yaginx_node_heartbeat_age_seconds` | gauge | 无 |
| `yaginx_upgrade_total` | counter | result, stage |

method 只允许 GET/POST/PUT/PATCH/DELETE/OPTIONS/OTHER；status_class 为 1xx-6xx/none。禁止 raw path、domain、token、account/user ID、request ID、fingerprint、certificate fingerprint、error text 等高基数/敏感 label。

指标必须有真实 producer，不能只注册 collector：listener boundary 更新 active gauge；LISTEN 成功/失败写 reconnect counter；五秒 pool health ping 写 database duration；certificate reload 刷新 expiry buckets；ACME worker 写 success/failure/lock；node heartbeat 写 age。`upgrade` 是一次性 CLI，不运行可抓取 registry，因此只在 upgrade service mode 接入前保留为设计合同，不可作为当前 `/metrics` 已采集证据。

OTLP v1 默认关闭；启用时由 `gateway/internal/observability/tracing.go` 使用 OpenTelemetry 官方 SDK 和 OTLP gRPC/HTTP protobuf exporter 只导出 trace span，sample ratio 默认 `0.01`，metrics 仍以 Prometheus 为权威，logs 仍 stdout。`observability.otlp.endpoint` 在启用时必须是绝对 `http(s)` URL；HTTP/protobuf 使用精确 `/v1/traces` URL，gRPC 使用 collector URL。process shutdown 在 `exportTimeout` 内 flush batch。span 只写 listener、受限 method、协议版本和 service build metadata；禁止 Host/domain、raw path/query、header、body、account、token、secret、certificate 或 key。`tracing_test.go` 以本地 collector 验证 exporter 可发送 span，且二进制 payload 不含请求 Host/path/Authorization/token。

## Traffic pipeline

代理完成后向 bounded traffic queue 写 event；queue 满 DropWrite，不阻塞请求。每个 process UUID 维护 UTC minute/hour bucket，duration bucket 固定 `<=100`、`>100 & <=500`、`>500 & <=1000`、`>1000`，UA parsing 默认 off。

每 15 秒把 process 当前绝对 snapshot UPSERT 到 `gateway_traffic_contribution`；仅在 `trafficAggregation.updateCurrentPeriod=true` 时写入未关闭 minute/hour，默认只在 closed period 写入。closed minute/hour 标 `is_final=true`。进入 traffic queue 前使用 `trafficAggregation.excludedPathPrefixes` 过滤，默认排除 `/yaginx_console`；metrics 仍观察全部请求。materializer 对 `cycle+period` 获取 transaction advisory lock，汇总所有 node/process contribution并绝对覆盖：

- minute -> `resource_report(cycle_type=5)`。
- hour -> `resource_report(cycle_type=10)` 和 `host_traffic`。
- day 完成 5 分钟后，单次 job 汇总 hour -> `resource_report(cycle_type=20)`。
- weekly legacy rows继续可查询；Go v1 不生成新的 cycle 30 row。

这样多节点和 retry 不会 last-write overwrite。`resource_monitor_info` 表和旧数据原地保留；Go 不写新 raw request batch。启动时先以加密 setting `traffic.cutover_ts` 固定边界，再由 `BackfillLegacyMonitorReports` 在 PostgreSQL 内展开该时刻前的 JSONB raw rows，补齐 cycle 5/10 的 request/status/duration/spider/browser/OS report，随后 `MaterializeTraffic` 补齐 cycle 20。成功后加密写 `traffic.legacy_monitor_backfill_complete`，同一 cutover 重启不重复扫描；任何 marker/cutover 不匹配 fail-closed。raw rows 只在 backfill 成功后才进入 48 小时 retention，避免与 contribution 重叠或先删后算。

`resourceReports.enabled=false` 不启动 contribution report writer，也不执行 legacy raw backfill 或首次 report materialization。启用时，`minutelyDelay` 在每个 UTC minute 边界后触发常规 absolute reconciliation；`hourlyDelay` 和 `dailyDelay` 分别在 UTC hour/day 边界后触发一次额外 reconciliation，确保迟到 flush 在产品可见周期前被覆盖。三个触发器共享一个 scheduler，重复 tick 不会在同一周期重复执行；启动完成 legacy backfill 后会立即补齐一次，避免首次发布等待下一周期。`leaderLockNamespace` 是所有这些多节点事务的同一 PostgreSQL advisory lock namespace。

当前实现中 `gateway/internal/telemetry.Aggregator` 在每个 process 维护 minute/hour absolute snapshot，并在启用时以有界 UA family cache 生成 browser/spider/OS 维度；duration bucket 固定为 `<=100`、`>100 & <=500`、`>500 & <=1000`、`>1000`。`repository.SaveTrafficContributions` UPSERT `(node_id,process_id,host,cycle,period)` 的 status/duration/UA JSONB，并将已结束周期标记 `is_final=true`；`MaterializeTraffic` 用 `resourceReports.leaderLockNamespace` 对应的 transaction advisory lock 将所有 process snapshot 绝对写入 `host_traffic`、cycle 5/10 `resource_report`，并从 cycle 10 加权绝对汇总 cycle 20 日报。`traffic_contribution_integration_test.go` 验证两个 node/process 合计、分钟/小时/日报、状态/时长分桶/UA 维度、重复 snapshot 重放、加密 cutover、post-cutover 排除，以及旧 raw JSON 的 status/duration/spider/browser/OS backfill。retention 与 legacy raw shadow backfill 均已实现。

retention：contribution 2 天、resource_monitor_info 48 小时、host_traffic 默认 90 天；`resource_report` 按粒度独立保留分钟 30 天、小时 365 天、日 730 天。`reportRetentionDays` 保留为旧配置兼容字段，并作为日报保留的下限；新的部署应只配置三个粒度字段。`repository.PruneTelemetry` 与 `PruneReportCycles` 每批删除 5000 行并单独 commit，`serve` 在启动和每 24 小时执行；PostgreSQL 16 integration 以 batch=2 验证所有过期 rows 清理、保留未过期 row，并覆盖分钟/小时清理不影响日报。

## host_traffic

保留列和 unique `(host_name,period)`。period 为 UTC hour epoch seconds。materializer 写 absolute aggregate，不做节点覆盖；ID 首次 insert 用 Snowflake，conflict 保留原 ID。search/range/chart 的 order、page 和 byte human string 按 OpenAPI fixture。

## request_history

- 默认 enabled、ErrorsOnly、success sampling 0、retention 168h（7 天）。
- body max 32 KiB、header max 16 KiB、queue 10000、DropWrite、flush 5s、batch 500、max retry 3、delete batch 5000。
- `/yaginx_console` 排除；body content type allowlist 保持 text/json/xml/+json/+xml。
- capture 只复制 bounded bytes，不改变 stream/backpressure；headers/body 使用现有 JSON/Base64。
- batch transaction；超过 retry 丢 batch并计 metric，不阻塞代理。
- 601-610 映射以代理矩阵为准；真实 upstream reserved status 通过 upstreamStatusCode/source 区分。

当前 queue 在 sink 临时失败时保留失败 batch 并在下一 flush tick 重试；traffic 保持无限重试直到 queue 的显式 backpressure，history 使用 `maxFlushRetryCount`，超过次数丢弃整个 batch 并计数。`gateway/internal/telemetry/queue_test.go` 覆盖首次失败后的重试、有限 retry drop、ErrorsOnly/AllMetadata sampling 与 console path exclusion。proxy 使用 bounded read/write mirror，不改变 stream/backpressure；PostgreSQL 16 管理 integration 验证 headers、Base64 bodies、截断位和 upstream status 的持久化/读取。history、retention、materializer 和 legacy raw shadow backfill 均已落地；W7 的剩余门禁是第 10 章定义的真实流量、性能和长期稳定证据。

Runtime 当前可直接识别并记录：请求 context cancel 为 601、`net.Error.Timeout` 为 602、无 destination 为 603、round-trip failure 为 604、raw upstream 在 response header 前 EOF 为 605、上游 response body read failure 为 606、client request body read failure 为 607、client response write failure 为 608、recovered pipeline/transport exception 为 609、注入的非 network round-trip error 为 610。101 WebSocket upgrade 不包裹 response body，以免破坏 hijack；故障注入必须保留 typed/context/EOF 路径，不能用错误文本猜测分类。

## Snowflake

ID bit layout：

```text
0 | 41-bit (UTC milliseconds since 2026-01-01T00:00:00Z) | 10-bit node | 12-bit sequence
```

公式 `((millis-epoch)<<22) | (nodeNumber<<12) | sequence`，node 0-1023，每毫秒 0-4095。approved 才能生成。

sequence 用尽等待下一毫秒。clock 小于 memory/persisted high-water 且差 <=5ms 时等待；差 >5ms 时 generator unavailable，API 新增写返回 503，traffic/history 丢弃需新 ID 的 row并报警，代理仍可转发。高水位每秒和 shutdown 原子写 `/var/lib/yaginx/node/snowflake.state` 0600；时间追平后自动恢复。禁止回拨时切换 node number或使用随机 ID掩盖冲突。

当前实现位于 `gateway/internal/platform/snowflake`：新进程只在 node approved 且取得 `node_number` 后启用，request-history ID 不再依赖 PostgreSQL sequence；state 使用 atomic write 每秒及 shutdown 持久化。`>5ms` 回拨返回明确 generator error，代理请求路径仍不进行同步数据库写。其余 telemetry materializer/retry 行为继续按本章工作包门禁验收。
