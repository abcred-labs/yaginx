# Yaginx PostgreSQL 生产优化与变更标准

本文档是 Yaginx PostgreSQL 的生产运行、性能优化和数据库变更标准。它补充
[Go 版本安装与配置 Runbook](go-version-new-install-runbook.md)，不替代 DBA 审批、
维护窗口、备份恢复制度或安全流程。

本文档中的示例不包含 DSN、口令、生产主机名、业务 Host、请求正文或敏感 Header。

## 1. 适用范围与职责

| 项目 | 标准 |
| --- | --- |
| PostgreSQL 版本 | 生产最低主版本为 15；当前生产基线为 PostgreSQL 17。 |
| 运行数据库 | Yaginx 使用专用 `yaginx` 数据库和运行角色。 |
| 数据敏感性 | 数据库包含 JWT 材料、证书私钥、ACME 账户和 SMTP 密码；数据库、日志、备份与 DSN 按同等敏感等级保护。 |
| 配置权限 | PostgreSQL 全局参数、扩展安装和服务重启仅由数据库主机 root 或 DBA 超级用户执行。 |
| 应用权限 | `yaginx` 运行账号仅使用最小业务权限，不授予超级用户、`pg_read_all_settings` 或任意 DDL 权限。 |
| 变更责任 | DBA 负责数据库参数、备份和恢复；应用运维负责 Yaginx 配置、健康检查和指标；发布负责人负责维护窗口、验收和回滚决策。 |

生产角色拆分使用 `gateway/deploy/postgres/roles.sql` 作为授权资产。不得通过给
运行账号追加超级用户权限来临时解决诊断或迁移问题。

## 2. 当前生产基线

以下为截至 2026 年 8 月 7 日的只读核查结果，用于后续变更前后对比。虽然现有生产
基线仍记录为未创建扩展，`pg_stat_statements`、`shared_preload_libraries` 与
`compute_query_id` 已是 Yaginx 的默认必需生产配置；未满足时属于配置不合规，应在
维护窗口整改。

| 类别 | 当前值 | 标准结论 |
| --- | --- | --- |
| PostgreSQL | `17.10` | 满足最低版本要求。 |
| `track_io_timing` | `off` | 应在维护窗口启用。 |
| `log_min_duration_statement` | `-1` | 尚未记录慢 SQL，应启用阈值日志。 |
| `log_temp_files` | `-1` | 尚未记录临时文件，应启用。 |
| `log_statement` | `none` | 保持关闭，不记录全部 SQL。 |
| `track_activities` | `on` | 保持开启。 |
| `pg_stat_statements` | 安装包可用，扩展未创建 | 应在 preload 配置后创建。 |
| 请求历史 | `AllMetadata`、成功请求 `5%`、保留 `48h` | 保持错误请求全量、成功请求抽样。 |
| `resource_report` | 现有时间索引可用 | 未经证据不得新增索引。 |
| `host_traffic` | 当前为小表 | 顺序扫描不是当前瓶颈。 |

当前应用账号无权读取 `shared_preload_libraries` 和 `config_file`。这符合最小权限原则；
确认 PostgreSQL 全局配置必须使用 DBA 管理账号。

## 3. 统计、日志与诊断基线

### 3.1 必须启用的 PostgreSQL 参数

下列参数写入 `postgresql.conf` 或已纳入加载链的受控配置文件。修改
`shared_preload_libraries` 时必须保留既有扩展并追加 `pg_stat_statements`，不得覆盖。

```conf
# 需要 PostgreSQL 重启。
shared_preload_libraries = 'existing_extension,pg_stat_statements'
pg_stat_statements.max = 10000

# 可随维护窗口重启一并生效；单独变更时按 DBA 配置上下文 reload。
compute_query_id = on
pg_stat_statements.save = on
pg_stat_statements.track = top
pg_stat_statements.track_planning = on
track_io_timing = on

# 慢查询与临时文件证据链。
log_min_duration_statement = 500ms
log_temp_files = 0
log_statement = none
log_parameter_max_length = 0
```

| 参数 | 作用 | 风险与约束 |
| --- | --- | --- |
| `pg_stat_statements` | 聚合 SQL 的调用次数、规划/执行时间、块读写与临时块。 | 加载扩展需要重启；只保留归一化 SQL，不以业务 Host 作为指标标签。 |
| `track_io_timing` | 区分执行慢与磁盘 I/O 慢。 | 有轻微计时开销；启用后观察数据库 CPU。 |
| `compute_query_id` | 为 SQL 统计提供稳定 query ID。 | 应与 `pg_stat_statements` 同时启用。 |
| `log_min_duration_statement` | 保留超过阈值的慢 SQL。 | 初始阈值为 `500ms`；日志量过大时调整到 `1000ms`，不得关闭统计。 |
| `log_temp_files` | 发现排序、哈希聚合和报表查询落盘。 | `0` 记录全部临时文件，需保证日志轮转。 |
| `log_parameter_max_length` | 阻止绑定参数内容写入日志。 | 保持 `0`，避免泄露请求、凭证或个人数据。 |
| `log_statement` | 控制全量 SQL 日志。 | 必须保持 `none`，禁止设为 `all`。 |

`pg_stat_statements.track = top` 是默认生产值：记录业务入口 SQL，避免统计嵌套语句带来的
额外开销。只有在已证明函数内部 SQL 是瓶颈时，才可经 DBA 审批改为 `all`。

### 3.2 扩展安装与验证

以下操作需要超级用户。在 PostgreSQL 重启并成功加载 preload 扩展后，于目标数据库执行：

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

SELECT extname, extversion
FROM pg_extension
WHERE extname = 'pg_stat_statements';
```

变更后验证：

```sql
SHOW shared_preload_libraries;
SHOW compute_query_id;
SHOW track_io_timing;
SHOW log_min_duration_statement;
SHOW log_temp_files;
SHOW log_parameter_max_length;

SELECT current_user,
       has_table_privilege(current_user, 'pg_stat_statements', 'SELECT') AS can_read_top_sql;

SELECT count(*) AS statements,
       count(*) FILTER (WHERE queryid IS NULL) AS statements_without_query_id
FROM pg_stat_statements;
```

如果 Console 的“数据库性能”页面提示 `Top SQL result could not be read`，先用上面的查询
确认 `queryid` 是否为空以及运行账号是否有读取权限。`queryid` 为空时，Yaginx 仍会展示
其他 Top SQL 统计，但 Query ID 会显示为“未生成”；生产环境仍应启用 `compute_query_id`。

当扩展未加载或查询标识未启用时，由 DBA 在维护窗口执行相应修复。保留现有
`shared_preload_libraries` 内容并追加扩展，重启 PostgreSQL 后再创建扩展：

```sql
-- postgresql.conf：不要覆盖既有扩展。
shared_preload_libraries = 'existing_extension,pg_stat_statements'
compute_query_id = on

-- PostgreSQL 重启后，在目标数据库执行。
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- 仅在权限核查失败时执行。
GRANT SELECT ON pg_stat_statements TO yaginx_gateway;
```

`pg_stat_statements` 不应被应用运行账号重置。需要新的诊断窗口时，由 DBA 记录当前
快照后再执行受控的 `pg_stat_statements_reset()`。

Yaginx Console 的“数据库性能”页面只读取固定统计视图并检测这些能力，不会更改
PostgreSQL 参数或创建扩展。未启用 `pg_stat_statements` 时，页面仍显示连接、锁、
I/O、缓存和表维护数据，同时提示 SQL 统计不可用。

## 4. 常规诊断标准

所有生产诊断 SQL 默认只读，并应在受控工单或事件记录中保存脱敏结果。结果中不得复制
完整请求参数、请求正文或 DSN。

### 4.1 每日或性能事件后的 SQL 快照

Top SQL：

```sql
SELECT queryid, calls, total_exec_time, mean_exec_time, max_exec_time,
       rows, shared_blks_read, shared_blks_hit, temp_blks_written, query
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 30;
```

活跃会话与等待：

```sql
SELECT pid, usename, state, wait_event_type, wait_event,
       now() - query_start AS running_for, query
FROM pg_stat_activity
WHERE datname = current_database()
  AND state <> 'idle'
ORDER BY query_start;
```

未授予的锁等待：

```sql
SELECT locktype, mode, granted, pid, relation::regclass
FROM pg_locks
WHERE NOT granted;
```

数据库级 I/O 与临时文件：

```sql
SELECT datname, blks_read, blks_hit, temp_files, temp_bytes,
       blk_read_time, blk_write_time
FROM pg_stat_database
WHERE datname = current_database();
```

PostgreSQL 17 I/O 分项：

```sql
SELECT backend_type, object, context, reads, read_time, writes, write_time
FROM pg_stat_io
ORDER BY read_time DESC NULLS LAST, write_time DESC NULLS LAST;
```

### 4.2 采集频率与证据保留

| 场景 | 最少采集项 | 保留要求 |
| --- | --- | --- |
| 日常巡检 | Top SQL、连接数、锁等待、`pg_stat_database`、磁盘容量。 | 每日汇总保留 30 天。 |
| 发布前后 | 发布前基线与发布后 15 分钟、1 小时、24 小时快照。 | 与发布记录共同保留。 |
| 性能事件 | 上述 SQL、慢 SQL 日志、临时文件日志、Yaginx `/metrics` 与数据库主机资源。 | 保留至事件关闭后至少 90 天。 |
| 容量事件 | 表/索引大小、autovacuum、归档或清理作业结果。 | 保留至容量风险解除。 |

## 5. 应用连接、超时与数据生命周期

Yaginx 的应用级默认数据库配置以
[安装 Runbook](go-version-new-install-runbook.md#71-databasesecuritynode) 为准：

| 配置 | 标准 |
| --- | --- |
| `database.maxConnections` | 默认 `40`；先结合 PostgreSQL `max_connections`、节点数和其他客户端容量核算，再调整。 |
| `database.minConnections` | 默认 `4`；不得大于最大连接数。 |
| `database.statementTimeout` | 默认 `15s`；常规管理读写不得通过无限超时掩盖慢 SQL。 |
| `resourceReports.materializationStatementTimeout` | 默认 `2m`；仅用于报表物化，不替代常规 SQL 优化。 |
| `database.healthQueryTimeout` | 默认 `2s`；健康检查超时是告警信号，不应通过放宽阈值隐藏故障。 |
| 连接生命周期 | 默认最长 `30m`、空闲 `5m`；修改前必须观察连接池和 PostgreSQL 连接峰值。 |

请求历史与报表生命周期标准：

| 数据 | 标准 |
| --- | --- |
| `request_history` | 全局默认错误请求完整记录；成功请求仅在 `AllMetadata` 下按 `successSamplingRate=0.05` 采样。网站可覆盖为 `errorsOnly` 或 `all`（站点 `all` 默认成功采样率为 1）；显式全量策略会保留普通 404 和客户端取消的可观测结果。生产保留 `48h`。 |
| `gateway_traffic_contribution` | 按运行时 `resourceReports.contributionRetention` 保留；清理前确认报表已覆盖历史窗口。 |
| `resource_report` | 分别保留分钟、小时、日报；默认 `30/365/730` 天。 |
| `host_traffic` | 按 `trafficAggregation.retentionDays` 保留；变更保留期前评估磁盘、索引和查询窗口。 |
| 清理作业 | 使用现有分批删除机制；禁止在线执行无条件的大范围 `DELETE`。 |

## 6. SQL、索引与表维护规则

### 6.1 优化前证据

新增索引、重写 SQL、调大内存、拆表或扩容前，必须具备以下至少一项证据：

```sql
EXPLAIN (ANALYZE, BUFFERS) SELECT ...;
```

- `pg_stat_statements` 显示该 SQL 在总耗时、均值、最大耗时、块读取或临时块中占比异常。
- 慢 SQL 或 `log_temp_files` 显示相同归一化查询持续出现。
- 锁、I/O 或 autovacuum 证据排除了其他根因。

`EXPLAIN (ANALYZE, BUFFERS)` 只能在低峰、受控事务和代表性参数下执行；不得对未知的大写入
语句或生产全表 DML 直接执行。

### 6.2 索引变更

- 线上新增索引必须使用 `CREATE INDEX CONCURRENTLY`，并在独立事务执行。
- 线上删除索引前必须确认至少一个完整业务周期内无有效使用证据。
- 禁止为“可能有用”新增 JSONB、表达式或重复复合索引。
- 当前 `resource_report` 的时间查询使用既有索引；当前小规模 `host_traffic` 的顺序扫描不是
  单独加索引的理由。
- 索引变更后验证执行计划、写入开销、索引大小和查询 P95；未达到目标则回滚或记录例外。

### 6.3 DDL、autovacuum 与膨胀

- 禁止未经审批的全表 DDL、长事务、直接修改历史报表或手工重算生产聚合数据。
- 大表结构变更应使用分批迁移、可恢复任务或维护窗口，禁止长时间持有排他锁。
- 当表或索引容量持续增长、死元组比例异常、autovacuum 延迟或查询计划恶化时，先采集
  `pg_stat_user_tables`、`pg_stat_user_indexes` 和 `pg_stat_progress_vacuum`。
- 表级 autovacuum 参数调整必须基于表规模、更新率和死元组证据；不得全局盲目降低阈值。

## 7. 维护窗口、备份与回滚

### 7.1 变更前置条件

1. 记录 PostgreSQL 版本、实例规格、可用磁盘、连接数、慢 SQL 与 Top SQL 基线。
2. 验证最近一次可恢复备份，并记录恢复点、备份位置和责任人。
3. 备份 `postgresql.conf`、`postgresql.auto.conf` 及相关受控配置文件。
4. 明确变更参数、预期影响、成功标准、回滚触发条件和通讯窗口。
5. 在非生产环境先验证同主版本 PostgreSQL 和相近数据规模。

### 7.2 重启与健康检查

涉及 `shared_preload_libraries` 或其他 restart 级参数时：

```bash
systemctl restart postgresql
runuser -u postgres -- pg_isready -d yaginx
systemctl is-active postgresql
```

应用侧同时确认：

```bash
curl -fsS http://127.0.0.1:29090/health/ready
```

实际端口和访问控制以受控 Yaginx 配置为准；文档、工单和 shell history 不得记录 DSN。

### 7.3 参数回滚

使用 `ALTER SYSTEM` 变更时，先恢复已备份的 `postgresql.auto.conf`，或按变更项执行：

```sql
ALTER SYSTEM RESET track_io_timing;
ALTER SYSTEM RESET log_min_duration_statement;
ALTER SYSTEM RESET log_temp_files;
ALTER SYSTEM RESET pg_stat_statements.track;
ALTER SYSTEM RESET pg_stat_statements.track_planning;
```

涉及 `shared_preload_libraries` 或 `pg_stat_statements.max` 的回滚必须重启 PostgreSQL。
已创建的 `pg_stat_statements` 扩展通常保留；除非有明确兼容性或安全原因，不执行
`DROP EXTENSION`。

### 7.4 日志、备份与恢复演练

- PostgreSQL 慢 SQL 和临时文件日志至少保留 14 天，并通过文件轮转或 journald 容量限制防止
  占满磁盘。
- 数据库备份、WAL 归档和恢复测试必须覆盖含运行时材料的敏感数据保护要求。
- 至少每季度演练一次恢复到隔离环境，并验证 schema、Yaginx 连接、readiness 和报表读取。

## 8. 告警与发布验收

| 类别 | 触发条件 | 首要动作 |
| --- | --- | --- |
| 数据库 CPU/I/O | CPU、磁盘等待或 I/O 时间持续高于基线。 | 查看 `pg_stat_io`、Top SQL 和主机资源。 |
| 连接池 | 活跃连接接近容量、等待连接增加或健康检查超时。 | 核对 Yaginx 池配置、长事务和 `pg_stat_activity`。 |
| 慢 SQL | 新增或持续的超过 `500ms` 查询。 | 对照 `pg_stat_statements`、执行计划和临时文件。 |
| 锁等待 | 未授予锁持续存在或业务请求超时。 | 定位阻塞会话，按变更流程处理，禁止直接杀会话作为常规修复。 |
| 临时文件 | `temp_bytes` 或慢 SQL 临时文件日志持续增长。 | 分析排序/聚合计划和工作内存需求，不先盲目调参。 |
| autovacuum | 死元组、膨胀或 vacuum 延迟影响查询。 | 收集表级证据，制定表级维护方案。 |
| 请求历史增长 | `request_history` 增长率偏离 5% 成功采样预期。 | 核对采样、错误率、保留清理和大字段采集。 |
| 管理页面性能 | `yaginx_management_request_duration_seconds` 或页面 API P95 异常。 | 核对 observability 行数、报表覆盖和 Top SQL。 |
| 报表完整性 | `contribution_report_mismatch` 或 `missing_report_bucket` 非零或增长。 | 作为数据完整性事件处理，不以索引或扩容替代修复。 |

发布后的验收窗口至少包含 15 分钟、1 小时和 24 小时三个快照。观察项目包括数据库 CPU、
临时文件、连接池、慢 SQL、管理接口 P95、报表完整性和 `request_history` 增长率。

## 9. 禁止事项

- 禁止在生产数据库执行未审查的全表 DDL、无条件大范围 `DELETE`、全量历史重算或 down migration。
- 禁止开启 `log_statement = all`、在日志中记录 DSN、口令、JWT、证书私钥、请求正文或敏感 Header。
- 禁止为解决慢查询而直接放宽应用 `statementTimeout`、关闭日志或增加连接数。
- 禁止未经 `EXPLAIN (ANALYZE, BUFFERS)`、SQL 统计或慢日志证据新增索引。
- 禁止把数据库超级用户权限授予 Yaginx 运行账号。

## 10. 文档维护

每次 PostgreSQL 主版本升级、数据模型重大变更、连接池默认值调整、保留策略调整、索引变更或
性能事件关闭后，必须更新本文档的基线、策略或诊断证据链接。生产真实数据、凭证和完整 SQL
日志保存在受控的非公开系统，仓库仅保留脱敏规则和操作标准。
