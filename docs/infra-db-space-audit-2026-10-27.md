# infra-db 空间扫描与整理建议（2026-10-27）

## 范围与结论

本次通过 SSH 对 `root@hk.niusys.net:229` 做了扫描；没有删除镜像或日志，也没有执行 `VACUUM FULL`。

随后按下方已存在的保留策略执行了小范围过期数据清理：删除 `gateway_traffic_contribution` 6,472 行、`resource_report` 分钟粒度 7,101 行、`host_traffic` 388 行，并分别执行了普通 `VACUUM (ANALYZE)`。清理条件复核为 0 行；没有对 `request_history`、`tools` 或 `arai` 中没有明确保留策略的表执行删除或 `TRUNCATE`。普通 VACUUM 只回收 PostgreSQL 内部可复用空间，数据目录大小不会立即按删除量缩小。

主机根盘 30G，已用 25G（87%），可用约 3.8G；`/data` 共约 19G。空间主要来自 PostgreSQL 业务表/索引和 Docker 镜像层，WAL 没有异常堆积。

| 路径 | 占用 | 主要内容 |
|---|---:|---|
| `/data/pgdb_local` | 5.0G | `yaginx`、`tools`、`niusys`、`gm` |
| `/data/pgdb_xian` | 6.9G | `arai` |
| `/data/docker` | 3.8G | overlay2 约 3.5G，容器 JSON 日志约 247M |
| `/data/mongo` | 3.0G | 本次未展开 Mongo 集合级扫描 |

## Docker

当前 6 个容器均在运行，没有已停止容器。Docker 的可回收镜像（当前没有容器引用）约 0.87G：

- `verdaccio/verdaccio`：约 208M
- `vaultwarden/server`：约 300M
- `hub.feinian.net/jqk/jqkai_jobhost:260812_083930`：约 311M
- `nginx:1.27-alpine`：约 48M

容器 JSON 日志约 247M，其中 `pgdb_xian` 91M、Mongo 85M、`pgdb` 77M。建议先确认上述镜像确实不再用于回滚，再执行 `docker image prune -a`；日志应配置 `max-size/max-file` 轮转，不能直接无备份清空生产日志。

## `pgdb_local`（约 5.0G）

数据库大小：`yaginx` 2.67G、`tools` 2.06G、`niusys` 95M、`gm` 66M。

### `yaginx`（2.67G）

| 表 | 总大小 | 表数据 | 索引 | 行数 | 时间范围 |
|---|---:|---:|---:|---:|---|
| `request_history` | 1.72G | 508M | 1.15G | 428,986 | 2026-10-24 至 2026-10-27 |
| `gateway_traffic_contribution` | 532M | 403M | 128M | 558,695 | 2026-09-26 至 2026-10-27 |
| `resource_report` | 390M | 303M | 87M | 450,243 | 2026-06-07 至 2026-10-27 |

`request_history` 当前只存约 3 天数据，已经小于仓库默认 7 天保留期；代码/运维规范允许生产环境收紧到 48 小时，但这会影响历史排障，需业务确认后再调配置。`gateway_traffic_contribution` 的 31 天数据与默认 31 天保留期一致，不建议直接删除。`resource_report` 按粒度分层：`cycle_type=5` 分钟数据约 31 天，`10` 小时数据约 142 天，`20` 日数据约 84 天，符合仓库默认的 30/365/730 天边界，不能按整表日期粗暴删除。

`request_history` 的 6 个索引合计约 1.15G。统计期内 `error_category` 和 `status_source` 两个索引扫描次数很少，但代码确实支持这两个过滤条件；删除前必须用生产查询计划和更长时间窗口验证，当前不建议直接删。

### `tools`（2.06G）

| 表 | 总大小 | 表数据 | 索引 | 行数 | 时间范围 |
|---|---:|---:|---:|---:|---|
| `ip_data_conflicts` | 1.05G | 522M | 528M | 3,314,144 | 2026-10-25 至 2026-10-27 |
| `ip_lookup_cache` | 497M | 476M | 21M | 321,560 | 2026-08-02 至 2026-10-27 |
| `ip_lookup_cache_changes` | 470M | 464M | 6M | 294,482 | 2026-10-25 至 2026-10-27 |
| `ip_cache_refresh_jobs` | 38M | 22M | 16M | 27,984 | 最近持续更新 |

这里是最值得先确认业务保留策略的区域：`ip_data_conflicts` 和 `ip_lookup_cache_changes` 在两天内快速增长，前者索引几乎与表数据等大，后者 JSONB 历史数据占主要空间。`ip_lookup_cache` 是当前缓存，清理前要确认缓存可重建；冲突和变更历史应增加明确的保留天数或按日期分区。

## `pgdb_xian`（约 6.9G）

数据库 `arai` 约 6.74G，主要对象如下：

| 表 | 总大小 | 表数据 | 索引 | 行数 | 时间范围 |
|---|---:|---:|---:|---:|---|
| `api_request_history` | 1.82G | 552M | 1.27G | 515,197 | 2026-08-08 至 2026-10-27 |
| `usage_trend_bucket` | 1.42G | 194M | 1.22G | 834,861 | 2026-04-30 至 2026-10-27 |
| `upstream_key_usage_snapshot` | 913M | 273M | 640M | 901,117 | 2026-04-30 至 2026-10-27 |
| `api_key_usage_fact` | 408M | 217M | 191M | 981,398 | — |
| `user_subscription_usage_receipt` | 400M | 159M | 241M | 948,775 | — |
| `daily_quota_usage_fact` | 362M | 169M | 193M | 981,135 | 2026-07-08 至 2026-10-27 |
| `model_output_latency_bucket` | 295M | 58M | 237M | 400,273 | 2026-08-05 至 2026-10-27 |

`api_request_history` 已设置较积极的 autovacuum 参数（vacuum scale factor 0.02、analyze 0.01）；其他大表大多仍使用实例默认 0.2/0.1。统计显示部分表死元组比例达到约 8–14%，建议先做普通 `VACUUM (ANALYZE)`，再针对高写入表设置更低的表级阈值。`usage_trend_bucket`、`upstream_key_usage_snapshot` 等表索引占比很高，是否能删历史数据取决于报表查询窗口；应先确定 30/90/180 天的业务保留目标，再按时间列分批删除。

## 建议的执行顺序

1. **先释放 Docker 的低风险空间**：确认 4 个无容器引用的镜像不需要回滚后，在远端执行 `docker image prune -a`（按现场估算约 0.87G）；同时为 Docker JSON 日志设置轮转。
2. **先做数据库备份/恢复可用性确认**，再对高写入表分批清理。每批提交后观察锁等待、复制/备份任务和应用错误。
3. **优先处理 `tools` 的历史策略**：明确 `ip_data_conflicts`、`ip_lookup_cache_changes` 的保留天数；缓存表确认可重建后再清理旧记录。
4. **`arai` 先定保留窗口再清理**：建议分别评估 `api_request_history`、各类 `*_bucket`、`*_usage_fact` 的 30/90/180 天方案，不按整库或整表直接删除。
5. **清理后执行普通 `VACUUM (ANALYZE)`**。普通 VACUUM 主要回收可复用空间，不一定立即缩小文件；只有确认磁盘余量、维护窗口和锁影响后，才考虑单表 `VACUUM FULL`/`pg_repack`。
6. **不要手工删除 `pg_wal` 或 PostgreSQL 数据目录文件**。本次 WAL 约 81M（local）和 241M（xian），没有发现复制槽滞留或 WAL runaway。

## 可选的激进清理档位

`arai` 没有在本仓库发现明确的历史保留配置，因此本次没有按猜测删除。若业务确认只保留 90 天，预计可删除：`usage_trend_bucket` 约 406,707 行、`upstream_key_usage_snapshot` 约 100,509 行、`daily_quota_usage_fact` 约 316,886 行，按当前表大小粗估可回收约 0.9G 的逻辑空间；`api_request_history` 和 `model_output_latency_bucket` 当前没有超过 90 天的数据。删除后要分批提交并执行 `VACUUM (ANALYZE)`；要把文件大小还给操作系统，需要单表维护窗口执行 `VACUUM FULL` 或使用 `pg_repack`，并预留额外临时空间。

`tools.ip_lookup_cache` 约 497M，理论上是可重建缓存，但本次没有直接 `TRUNCATE`。只有确认服务能在清空后自动回填、且能承受回填流量时，才执行 `TRUNCATE public.ip_lookup_cache`；`ip_data_conflicts` 和 `ip_lookup_cache_changes` 属于历史/诊断数据，不能按缓存表处理。

## 可直接用于复核的只读命令

```sh
# Docker
ssh -p 229 root@hk.niusys.net "docker system df -v"

# 表/索引空间
ssh -p 229 root@hk.niusys.net 'docker exec pgdb psql -U postgres -d yaginx -c "select schemaname,relname,pg_size_pretty(pg_total_relation_size(relid)),pg_size_pretty(pg_indexes_size(relid)),n_live_tup,n_dead_tup from pg_stat_user_tables order by pg_total_relation_size(relid) desc;"'

# 普通维护（需在维护窗口执行；不改变业务数据）
ssh -p 229 root@hk.niusys.net "docker exec pgdb psql -U postgres -d yaginx -c 'VACUUM (ANALYZE) public.gateway_traffic_contribution;'"
```

上面的最后一条命令只是示例；本次已对三个符合既有保留策略的表执行过普通 `VACUUM (ANALYZE)`。
