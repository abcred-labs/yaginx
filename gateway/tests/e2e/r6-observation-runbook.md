# R6 观察期运行手册

本手册执行 [`r6-cutover-observation.md`](r6-cutover-observation.md) 的 14 天观察期。
它不授权生产切流、证书变更或删除 .NET；这些动作必须走既有变更审批。

## 每日检查

1. 从允许的管理网络检查每个节点的 `GET /health/live` 和 `GET /health/ready`。要求
   `ready=true`、`nodeStatus=approved`、listener 完整，且 `databaseRevision` 与
   `appliedRevision` 无持续差距。
2. 抓取受 CIDR 限制的 `/metrics`，查看 `yaginx_config_revision`、
   `yaginx_proxy_requests_total`、`yaginx_proxy_request_duration_seconds`、
   `yaginx_proxy_active_connections`、`yaginx_queue_depth`、
   `yaginx_queue_dropped_total`、`yaginx_db_pool_connections`、
   `yaginx_certificate_count`、`yaginx_acme_attempts_total`、
   `yaginx_node_heartbeat_age_seconds`、`yaginx_proxy_concurrency_rejected_total`、
   `yaginx_tls_handshake_rejected_total`、`yaginx_cgroup_memory_metrics_available`、
   `yaginx_process_resident_memory_bytes`、`yaginx_cgroup_memory_current_bytes`、
   `yaginx_cgroup_memory_peak_bytes`、`yaginx_cgroup_oom_events_total`、
   `yaginx_cgroup_oom_kills_total`、`yaginx_go_heap_alloc_bytes` 和
   `yaginx_go_heap_inuse_bytes`。
3. 对比同一 UTC 窗口的负载/访问日志真值、Prometheus 计数和 PostgreSQL
   `host_traffic`、`resource_report`、`gateway_traffic_contribution`。正常流量差异为零；
   只有明确注入 DropWrite 时才允许等于 dropped 增量。
4. 记录 P95/P99、非注入 5xx/601–610、RSS、heap、goroutine、FD 和活动连接的趋势；
   配置传播 P95 必须小于 2 秒、P99 小于 5 秒。
   1 GB 基线的 RSS 正常目标为 500-800 MiB，高峰不得超过 900 MiB；
   `oom_kill` 必须保持为零。
5. 将脱敏证据索引与结论写入 R6 观察记录。原始数据、生产地址和凭据均不进入 Git。

## 告警响应

将 [`../../deploy/monitoring/yaginx-r6-alerts.yaml`](../../deploy/monitoring/yaginx-r6-alerts.yaml)
加载到现有 Prometheus 规则组；部署前替换示例中的 `job` 标签并在 staging 验证查询。

- ready/抓取失败、revision 漂移、队列丢弃、证书临期或 heartbeat 超时：停止扩大任何变更，保留 metrics 与日志，按节点检查 LKG、数据库和 config-sync。
- 5xx/601–610 增加或 P95/P99 回退：将其与 .NET 基线及上游状态区分；连续两个五分钟窗口违反第 10 章阈值即触发回滚。
- telemetry 或报表真值不一致：停止 writer 相关变更，保留 UTC 窗口、dropped counter 和只读聚合证据；不得修正或删除生产数据以掩盖差异。

## 演练顺序

在维护窗口内依次完成数据库中断恢复、节点滚动重启、证书热加载和升级回滚。每项开始前
捕获 health/metrics，结束后核对 revision、流量、证书和队列；详细步骤、停止条件和
回滚动作沿用 [`planning/go-rewrite/10-testing-and-cutover.md`](../../../planning/go-rewrite/10-testing-and-cutover.md)。
