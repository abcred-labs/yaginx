# 数据库与迁移

> **Archived v1 design record.** This document is historical evidence only;
> v2 deployment and operations must follow the current OpenAPI and database
> lifecycle documentation.

## 权威合同

[PostgreSQL 16 设计 DDL](contracts/database-schema.sql) 固定 legacy 列、目标表、索引、check、trigger、role grant 和函数。该文件用于临时库创建和 schema diff，不直接嵌入生产；`gateway/migrations/` 把它拆成可审计、只前进的 Goose migration。

固定常量：

- PostgreSQL 最低 major：16。
- migration advisory lock：`pg_advisory_lock(6431500123034371919)`，hex `0x594147494E58474F`。
- Goose version table：`gateway_schema_version`。
- legacy baseline version：`2026072600`；只在完整检查通过后写入 applied row。
- config notify channel：`yaginx_config_changed`。

## 原则

- PostgreSQL 是动态配置、节点、证书、system setting 和聚合数据权威。
- `serve` 只做 schema compatibility check，永不自动 DDL。
- migration 使用 dedicated session 持有全局 lock，从读取 catalog 到 commit 全程不换连接。
- Go 稳定前禁止 DROP/RENAME legacy table/column、重写 ID、清空、改变 JSON shape 或收紧会阻断旧数据的约束。
- 原 FluentMigrator `versioninfo` 只读保留；Go 不写入。

## Legacy 基线判定

检查对象为八张表：`account`、`web_domain`、`website`、`resource_monitor_info`、`resource_report`、`account_password_recovery`、`host_traffic`、`request_history`。

1. 获取全局 migration lock，开启事务，读取 `pg_catalog` 的 table/column/type/nullability/default/PK/index 和 `versioninfo`。
2. **empty**：八表、`gateway_schema_version` 和任何 `gateway_*` 业务表均不存在。执行完整 legacy 建表，再执行 Go 表。
3. **complete legacy**：八表全部存在，列和主键兼容设计 DDL。允许额外列/索引；不允许必需列类型冲突或 PK 不同。
4. **recognized older legacy**：`versioninfo` 只包含已知版本 `2026042800`、`2026042801`、`2026042802`、`2026050703` 的有序前缀。按缺失版本执行现有等价 SQL，再重新完整检查。
5. **safe repair**：完整表存在，只缺非唯一普通 index、request_history 后加列或已知 check。migration 可以 `CREATE INDEX IF NOT EXISTS`、`ADD COLUMN IF NOT EXISTS` 或 `ADD ... CHECK NOT VALID`，并输出 repair report。
6. **unsafe partial**：核心表缺失但 `versioninfo` 不构成已知前缀、类型/nullability/PK 冲突、JSONB 被改为 text、ID 重复、域名/traffic 唯一 index 创建前存在未处理冲突。事务回滚，输出 JSON 诊断，不执行猜测修复。
7. 对 legacy IDENTITY sequence 执行只增不减的 `setval(max(id))` 检查，禁止生成小于当前最大 ID 的值。
8. 写 baseline version，再顺序应用 Go migration；commit 后释放 lock。

类型兼容只接受 PostgreSQL canonical 同义类型，例如 `timestamp with time zone`/`timestamptz`；不接受可隐式 cast 但会改变语义的类型。每个 unsafe 项包含 object、expected、actual、row sample query 和人工修复命令模板。

## 目标表

| 表 | 主键/唯一键 | 用途 |
| --- | --- | --- |
| `gateway_config_revision` | singleton `1` | 已提交 config revision |
| `gateway_config_event` | identity `id` | 可重放 resource event |
| `gateway_node` | UUID；node number partial unique | 审批、状态、revision、heartbeat、reload |
| `gateway_acme_account` | Snowflake ID；directory URL unique | 共享明文 account key/registration |
| `gateway_certificate` | Snowflake ID；SHA-256 fingerprint unique | 明文 PEM chain + PKCS#8 key |
| `gateway_certificate_order` | Snowflake ID；active domain partial unique | 单域名 order 状态与审计 |
| `gateway_system_setting` | setting key | 明文 JSON SMTP 等动态设置 |
| `gateway_traffic_contribution` | node/process/host/cycle/period | 多节点幂等 absolute contribution |

`gateway_traffic_contribution` 是报表链路的必要内部表：每个进程启动生成 process UUID，按分钟/小时把当前绝对计数 UPSERT 到自己的主键。单次 report job 汇总所有 contribution，以绝对值写 legacy `resource_report` 和 `host_traffic`，因此节点重试、重启和多节点不会互相覆盖。API 继续只读取 legacy aggregate 表。

## 外键与约束

- 新表之间使用 `ON DELETE RESTRICT`，节点、account、order、certificate 不物理删除审计记录。
- node status 只允许 pending/approved/draining/disabled；node number 0-1023。
- order 状态、error class、单域名规范化和 active-domain unique 见第 12 章。
- certificate validity 必须满足 `not_after > not_before`；数据库权限和备份权限保护明文业务材料。
- legacy password recovery status/attempt check 先 `NOT VALID`；数据审计通过后再单独 validate，不阻断旧异常行升级。

## 配置事件

`website`、`web_domain`、`gateway_certificate`、`gateway_system_setting` 的 row-level AFTER INSERT/UPDATE/DELETE 调用 SECURITY DEFINER `gateway_emit_config_event()`：

1. resource key 转 text。
2. 插入 event，operation 为 insert/update/delete。
3. singleton revision 更新为 event ID。
4. `pg_notify` payload 为 decimal event ID。

全部动作与业务写入同事务；rollback 不产生可见 event/notify。序列 gap 合法。函数固定 `search_path=pg_catalog,public` 并从 PUBLIC revoke execute，防止 search-path injection。

## 角色

- `yaginx_migrator`：部署身份，schema CREATE、table/sequence/function DDL 和 migration table。
- `yaginx_gateway`：runtime DML、sequence usage、LISTEN 和 advisory lock；无 CREATE/DROP/ALTER、无 role management。
- 实际 login role 通过 membership 获权，不直接把 password 写入普通配置。
- migration 完成后可重新执行 grant audit；runtime `doctor` 验证自己不能 `CREATE TABLE`。

## SMTP 导入

`import-legacy` 读取 `/app_data/system.json` 的 `smtp`，规范为 setting key `smtp`，JSON 直接写入 PostgreSQL 明文字段。不存在文件或 key 记 skipped；相同值记 skipped；不同值只有 `--replace-system-setting` 才覆盖。源文件永不修改。

## 回滚

- migration 只提供 forward repair，不提供删除新数据的 down。
- 回滚 .NET 时旧程序忽略 `gateway_*`；trigger 必须在 shadow 前通过旧 controller 写入测试。
- Go ACME/TLS 先停，确认无 active order/lock，再恢复归档 .NET。
- 数据库保留 Go 表和 legacy 新 index，不降低 schema version；再次前进可幂等继续。
