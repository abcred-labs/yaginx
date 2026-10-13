# 预生产稳定性压测演练手册

本手册是 Go gateway 发布前的人工演练门禁，不替代
[`planning/go-rewrite/10-testing-and-cutover.md`](../../../planning/go-rewrite/10-testing-and-cutover.md)
中的合同、阈值或生产灰度流程。执行对象只能是隔离的预生产环境；禁止使用
生产数据库、真实用户、生产域名、生产证书私钥或生产流量。

演练结果填写到同目录的
[`report-template.md`](report-template.md)。未完成演练只能标记为 Manual Testing
Item，不能作为发布通过的证据。

## 1. 准入前置条件

- 至少三台 gateway 节点，拓扑、实例规格、kernel/ulimit、PostgreSQL 16、证书
  类型、网络路径和运行配置与目标生产环境一致。每个节点必须已通过
  `GET /health/ready`，且 `ready=true`、`degraded=false`、database/applied revision
  一致。
- 负载发生器与被测网关分离；其 CPU、内存、网卡和 socket 上限足以成为客户端，
  而不是压测瓶颈。记录所有节点和发生器的硬件、OS、内核、镜像及工具版本。
- 为 .NET 基线与 Go 候选运行分别准备可重置的合成数据库和相同路由数据；两者使用
  同一批负载发生器、上游、证书和网络 namespace。不得在同一轮混用两个运行时。
- Prometheus 至少每 10 秒抓取 gateway `/metrics`，并同时保留 gateway JSON 日志、
   主机 CPU/RSS/FD/网络数据、PostgreSQL `pg_stat_activity`、慢查询和数据库主机资源。
  1 GB 基线必须额外保留 `yaginx_process_resident_memory_bytes`、
  `yaginx_cgroup_memory_current_bytes`、`yaginx_cgroup_memory_peak_bytes`、
  `yaginx_cgroup_memory_metrics_available`、`yaginx_tls_handshake_rejected_total`、
  `yaginx_cgroup_oom_events_total`、`yaginx_cgroup_oom_kills_total`、
  `yaginx_proxy_concurrency_rejected_total`、`yaginx_go_heap_alloc_bytes` 和
  `yaginx_go_heap_inuse_bytes`。
  所有导出文件须放入受控、非公开的本次运行目录。
- 运行开始前和结束后保存 `/health/live`、`/health/ready`、`/metrics` 原文，以及
  `gateway_config_revision`、`gateway_node`、`host_traffic`、`resource_report` 与
  `gateway_traffic_contribution` 的只读查询结果。查询输出不得包含 credential、请求
  body 或敏感 header。

## 2. 隔离 fixture upstream

在独立主机或容器启动本仓库提供的 fixture。它不是生产服务，且只用于合成流量：

```powershell
Set-Location gateway
go run ./cmd/proxyfixtureupstream -listen 0.0.0.0:20180 -mode echo
```

将专用的预生产 Host 路由到该 upstream。以下 `/perf/` 路径为本手册新增的确定性
压测端点，普通 `/sse`、`/websocket` 与 `/large-download` 的 golden-fixture 行为不变。

| 场景 | 上游路径 | 预期 |
| --- | --- | --- |
| 1 KiB 短读 | `GET /perf/fixed/1024` | 精确 1,024-byte 二进制响应 |
| JSON POST | `POST /perf/echo/32768` | 读取请求体，精确 32 KiB JSON 响应 |
| 下载 | `GET /perf/download/<bytes>` | 流式精确长度，最大 1 GiB |
| SSE | `GET /perf/sse?events=300&payloadBytes=1024&interval=1s` | 300 个已 flush 的事件，每秒一个 |
| WebSocket | `GET /perf/websocket?echoes=3000` | 升级后回显 3,000 个文本 frame |
| 上游超时 | `GET /perf/delay?duration=30s` | 延迟响应；用低于该值的 gateway upstream timeout 注入 602 |
| 上游 4xx/5xx | `GET /status/404`、`GET /status/503` | 原样上游状态 |
| 连接提前关闭/短响应 | 独立 fixture 使用 `-mode raw-close` 或 `-mode raw-short-body` | 分别用于 605/606 故障注入 |

压测前先对每条路径直连 upstream 和经网关各验证一次。`/perf/` 参数越界会返回 400，
以避免错误参数意外产生无界流量。

## 3. 正常负载与基线对比

使用已在预生产镜像中批准、并能导出原始延迟和完成计数的客户端。一个客户端可负责
HTTP/1.1/2 短请求与上传下载；HTTP/3 客户端必须显式支持 QUIC；WebSocket 客户端必须
保留连接并按指定频率发送 frame。每轮开始前通过客户端协商日志和 gateway 指标确认
HTTP/1.1、HTTP/2、HTTP/3 均实际产生流量，不能仅依据客户端配置声明。

每个运行时按相同参数执行三轮：预热 5 分钟、正式采样 30 分钟、轮次间恢复到空闲和
稳定指标。最终比较三轮正式采样的中位数。

| 负载类型 | 并发/比例 | 请求与连接定义 |
| --- | ---: | --- |
| 短请求 | 400 workers | 70% `GET /perf/fixed/1024`；15% 向 `/perf/echo/32768` 发送 16 KiB 有效 JSON；15% 上传下载 |
| 上传下载 | 上述 15% 中 | 90% 10 MiB、9% 100 MiB、1% 1 GiB；上传由 echo 请求或等效 consume-body 路由承载，下载使用 `/perf/download/<bytes>` |
| SSE | 50 连接 | 每条 5 分钟、`events=300`、`interval=1s`、1 KiB payload |
| WebSocket | 50 连接 | 每条 5 分钟、1 KiB 文本 frame、10 msg/s、`echoes=3000` |

短请求协议分布为 HTTP/1.1 40%、HTTP/2 50%、HTTP/3 10%；监听器分布为 8080 35%、
9080 15%、8443（TCP/UDP 合计）35%、9443 15%。开启 keep-alive/连接复用。运行期间每秒
提交一次无害 website revision，记录提交时间与各节点观察到 applied revision 的时间。

正常流量期间不得注入故障。统计时把业务上游返回的 4xx/5xx 与 gateway 生成的
601–610 分开，并排除明确标记为故障注入的请求。

## 4. 稳定性与故障演练

1. **24 小时耐久。** 以已通过的正常轮次吞吐的 60% 连续运行，保留完整时序数据。
   SSE/WebSocket 按五分钟周期重新建立，避免把客户端自然到期误判为泄漏。
2. **配置同步。** 维持每秒 revision 更新，断开一个节点 PostgreSQL `LISTEN/NOTIFY`
   连接一次；记录每次 revision 提交、三个节点 applied 时间和 reconnect 指标。通知
   无法送达时，revision poll 仍须在 30 秒内追平。
3. **数据库中断。** 保持业务流量，阻断所有 gateway 到 PostgreSQL 的链路 10 分钟。
   已加载 LKG 的代理必须继续响应；管理写必须为 503。恢复连接后记录恢复时间，30 秒
   内确认 ready、heartbeat、database/applied revision 和异步 flush 恢复。
4. **滚动重启。** 每次仅排空并重启一台节点，等待其 `/health/ready` 正常且 revision
   追平后再处理下一台；全程保留多数节点服务。记录排空前后活跃连接、客户端重连和
   任意失败。
5. **上游故障。** 在独立的、明确标记的流量分支中按既定比例注入 4xx/5xx、raw close、
   short body、超时和客户端取消；核对 601–610、request_history、队列和 Prometheus
   的分类。注入流量不计入正常用户成功率。

任何阶段触发中止条件时，立即停止扩大负载，保存当前指标/日志/数据库快照，按
`10-testing-and-cutover.md` 的回滚动作恢复上一稳定阶段；不得尝试 down migration。

## 5. 验收与核对

- Go 短请求 P95 不高于 .NET 110%，P99 不高于 120%，完成吞吐不少于 95%。任意连续
  两个五分钟窗口 P95 超限或非注入 5xx/601–610 高于基线，即中止。
- 稳态 RSS 不高于 .NET 120%；对于 Yaginx 1 GB 基线，正常 RSS 目标为 500-800 MiB，
  高峰不得超过 900 MiB，且 cgroup `oom_kill` 为零。24 小时内 heap、goroutine、活跃连接、FD、CPU、网络、
  PostgreSQL pool 和队列深度不得呈持续单调增长。正常流量下
  `yaginx_queue_dropped_total` 的增量必须为零；并发拒绝必须单独通过
  `yaginx_proxy_concurrency_rejected_total` 统计。
- 配置传播 P95 小于 2 秒、P99 小于 5 秒；数据库中断时代理成功率下降不得超过
  0.01 percentage point，恢复后 30 秒内追平。
- 使用 Prometheus 的 `yaginx_proxy_requests_total`、
  `yaginx_proxy_request_duration_seconds`、`yaginx_proxy_bytes_total`、
  `yaginx_proxy_active_connections`、`yaginx_config_revision`、
  `yaginx_queue_depth`、`yaginx_queue_dropped_total`、`yaginx_flush_total` 和
  `yaginx_node_heartbeat_age_seconds` 与负载发生器真值交叉核对。
- 以相同 UTC 窗口汇总 `host_traffic` 的 `request_counts`、`inbound_bytes`、
  `outbound_bytes`，并将 `resource_report.request_qty`、状态分桶及
  `gateway_traffic_contribution` 相加结果与发生器真值比对。差异必须为零；仅在明确的
  DropWrite 注入中，差异可等于对应 queue 的 dropped 增量。
- ready 变 503、snapshot 拒绝、证书/SNI 错误、数据库恢复超时、长期 revision 不一致、
  telemetry 真值不一致或日志/指标泄露敏感信息，均为立即失败。
