# 目标与兼容边界

> **Archived v1 design record.** This document is historical evidence only;
> v2 deployment and operations must follow the current OpenAPI and database
> lifecycle documentation.

## 目标

使用 Go 重写 Yaginx 全部后端职责，同时让现有 React 管理后台和 PostgreSQL 数据原地升级。最终系统由 `/usr/share/yaginx/adminui`、单一 Go 二进制和 PostgreSQL 16 组成，不需要 .NET Runtime。

Go v1 必须覆盖：

- 8080、9080、8443、9443，PROXY Protocol、HTTP/1.1/2/3 和动态 TLS SNI。
- 基于 `website` JSONB 的默认路由、子规则、transform、simple response、redirect、CORS 和大请求策略。
- `/yaginx_console/` React 静态资源、SPA fallback 和全部生产管理 API。
- 账户、JWT、密码找回、SMTP、网站、域名、流量、请求历史、资源报表和节点管理。
- revision/event、LISTEN/NOTIFY、last-known-good、节点审批/心跳/Snowflake。
- PFX/account import、共享加密证书、ACME HTTP-01、续期和同域名多节点互斥。
- structured logs、Prometheus、health、graceful drain、Debian/Docker/systemd 和人工滚动升级。

## 保留合同

- 管理入口 `/yaginx_console/` 和 API `/yaginx_console/api/...`。
- [OpenAPI](../../contracts/management-api.openapi.yaml) 中所有现有 method/path、参数来源、JSON 字段、Int64 string、null、分页、状态和中文错误。
- 信封 `{code,msg,errMsg,tid,data}`；参数、业务和 missing-row 异常继续通常返回 HTTP 500，而不是改为 400/404。
- `account/user_info` 继续匿名；`dev/test`、`account/log_test` 不进入生产合同。
- `account`、`website`、`web_domain`、`resource_monitor_info`、`resource_report`、`account_password_recovery`、`host_traffic`、`request_history` 表、行、ID 和 legacy JSONB。
- 现有 password salt/hash、JWT issuer/audience/claims/timing/cookie；切换不使有效 token 失效。
- `web_domain.is_use_free_cert`、`is_verified`、`free_cert_message` 和 `configurable.example.invalid` HTTP-01。
- 601-610 status、status source、error category 和 request history capture 行为。
- 动态添加、更新、删除 website/domain/setting/certificate 后多节点自动刷新，不要求重启。

## 已批准差异

- Host 和 SNI 统一 IDNA ASCII、小写、去尾点；冲突配置拒绝发布，不依赖 YARP 路由顺序。
- 8443 明确提供 HTTP/3；9443 明确不提供 UDP/QUIC。
- Go 更新密码不再写 legacy 明文 `account.password`，只写 salt/hash。
- 没有匹配 SNI 证书时握手失败，不随机选择任意证书。
- 数据库不可写且本地快照有效时继续代理，管理写 operation 返回 HTTP 503 信封。
- 新增 gateway_node API 和受保护 health/metrics；不改变现有 React URL。

这些差异必须有 fixture 或迁移前扫描报告，不允许实现阶段再扩大差异范围。

## 移除项

- `backend/` 下全部 C#、solution/project、Kestrel、YARP、LettuceEncrypt、Certes、FluentMigrator 和 AgileLabs 后端依赖。
- NuGet、`dotnet restore/test/publish`、.NET Docker layer 和旧 systemd `dotnet Yaginx.dll`。
- 本地 PFX、ACME account 和 `/app_data/system.json` 作为生产权威存储的职责；导入后只保留审计/回滚用途。
- 仅进程内 `ProxyRuleChangeNotifyService` 和固定 Snowflake worker `(1,1)`。

## Go v1 不包含

- Redis、Kafka、独立控制面或外部消息队列。
- RBAC、WAF、多 destination 治理、rate limit、配置历史产品功能和 Docker 管理。
- HTTP-01 wildcard 自动证书。
- 自动定时升级。
- 历史 React backup/restore 路径，因为当前没有活动 .NET controller；该能力进入 v1.1 新合同。

## 验收定义

- OpenAPI 全部现有 operation 在同一数据库 fixture 下与 .NET golden 对比通过；React 当前工作流无修改运行。
- 设计 DDL 在 PostgreSQL 16 可创建；完整 legacy 升级后行数、ID、关键 checksum 和旧程序写入不变。
- 三节点配置传播 P95 < 2 秒；通知丢失后 30 秒内对账；DB 中断时有效快照继续代理。
- 同一规范化域名外部 ACME order 并发恒为 1。
- 固定流量模型下 Go P95 <= .NET P95 的 110%，5xx 和 601-610 错误率不增加。
- 最终仓库当前树、CI、Debian/Docker/systemd 和生产运行无 .NET SDK/Runtime/NuGet/C# 项目。
