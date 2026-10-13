# 测试与切换

> **v1 RC 范围。** 本章的 Prometheus/OTLP 观测证据和 R0-R6 切流门禁继续适用于 Go v1。R7/v2 的无外部观测组件测试、双写校验、资源稳定性和回滚条件由[第 16 章](16-minimal-infrastructure-runtime.md)定义。

## 文档与合同验证

- OpenAPI 3.1、JSON Schema 2020-12 可解析且 `$ref` 全部解析。
- OpenAPI 的 inline request、共享 401/403/500/503 examples 通过对应 schema；legacy 空属性 body 可通过模型绑定合同，新 gateway command 的 required 约束会拒绝缺字段。
- v2 baseline 在 disposable PostgreSQL 16 从空 DB commit 成功；重复 `database init` 必须安全失败，website insert/update/delete 的配置事件操作必须为 `insert`、`update`、`delete`。
- Mermaid block 可 parse；README 和章节 relative link 全存在。
- 运行模糊占位语扫描，最终设计不得留下未决标记或把实现选择推迟到编码阶段。
- OpenAPI 现有 operation 有 .NET action + Go handler；go-extension 有 origin + Go handler。

## 测试层次

### Unit/fuzz/race

覆盖 password/JWT/cookie、IDNA/route/transform、明文 LKG、revision reconcile、Snowflake rollback、601-610、ACME error/state 和 telemetry 适配组件。`internal/config/FuzzRuntimeConfigurationJSON`、`internal/configsync/FuzzLoadLKG`、`internal/certificates/FuzzCertificateDecoders`、`internal/listeners/FuzzProxyProtocolHeader`、`internal/repository/FuzzSnapshotTransformValidation` 和 `internal/management/FuzzDecodeJSONBody` 覆盖 config/JSON/LKG/certificate/PFX/PROXY/proxy transform/body decoder。RC 证据必须在 Debian WSL Go 1.25 上累计运行至少 10 分钟，例如每个目标使用 `go test -parallel=1 -fuzz=<name> -fuzztime=2m <package>`；并发包执行 `go test -race`。

除 .NET golden fixture 外，复用测试向量必须按[第 13 章](13-telemetry-component-reuse.md)登记：Edge Gateway 的 `go/internal/proxyprotocol/listener_test.go`、`go/internal/proxy/proxy_test.go`、`go/internal/tls/manager_test.go`、`go/internal/acme/{challenge,http01,renewal}_test.go` 和 `go/cmd/edge-gateway/main_test.go` 分别覆盖 PROXY、stream/trailer/WebSocket、SNI 热加载、HTTP-01、ACME 和真实 HTTP/3。迁入后测试须改为验证 PostgreSQL snapshot 与 advisory-lock 语义，而不是 filesystem/control-plane 行为。

### PostgreSQL 16

- empty、complete legacy、recognized older、safe repair、unsafe partial、repeat、concurrent migrator。
- legacy row count/ID/JSONB/checksum、identity sequence、role permission。
- trigger commit/rollback、event gap/retention、three-node LISTEN loss/reconnect/full load。
- node number competition、traffic contribution materialize、request history batch/retention。
- same-domain ACME lock、session loss、active order unique、certificate replacement。

### API golden

同一脱敏 fixture 分别运行 .NET/Go，覆盖 OpenAPI 全部现有 operation。比较 status/content type/header/envelope/data/null/order/Int64 string/中文错误。JWT 双向验证。每个 operation success、auth、parameter/business、missing、DB failure齐全。

本地已完成 `internal/management/TestAccountFixturesReplayThroughGoRouter` 的 72 份 fixture router replay、41 operation route gate、PostgreSQL management flow 和 Go JWT/cookie compatibility；它们证明 Go handler 与 fixture 的受控输入/输出边界，但不替代同时启动可写 .NET golden host 的逐 operation 双运行时比较。该 host 需要隔离 PostgreSQL fixture、.NET signing key 和可用 SMTP sink，当前列为 **Manual Testing Item: API-DUAL-RUNTIME-GOLDEN**。执行时不得使用生产账号、生产数据库或生产 SMTP；任何未批准差异按本章 rollback trigger 处理。

### Proxy E2E

代理矩阵逐行 fixture，覆盖四 TCP port + UDP 8443、PROXY v1/v2、HTTP/1.1/2/3、Host/SNI、all transforms、CORS、simple/redirect、Expect Continue、WebProxy、unsafe TLS、Next scoped asset、WebSocket、SSE、chunked、trailer、large stream、cancel、reload 和 LKG。

### ACME/import/upgrade

使用 Pebble/ACME staging + controllable configurable.example.invalid mock；逐字节 challenge；三节点 concurrency；temporary/permanent；PFX/account/SMTP idempotency。发布测试必须覆盖版本化 deb/checksum/SBOM/installer、manifest 最后发布、禁止覆盖和禁止 HTTP redirect；Debian fresh/upgrade/rollback、共享 flock、`ready=true` + target version + stable node ID、amd64、adminui、config conffile 与 non-root service。GHCR/GitHub Releases 和真实 systemd 演练仍是 Manual Testing Item。arm64 不属于本 RC 发布范围。

## 固定性能模型

使用同一物理/VM、kernel、PostgreSQL、上游 mock、TLS certificate 和 network namespace。先 .NET 后 Go，各 5 分钟 warm-up + 30 分钟 measurement，运行 3 次，比较三次中位值。

并发：400 个短请求 worker、50 条 SSE、50 条 WebSocket、8 个 upload；连接复用开启。短请求分布：

| 比例 | 场景 |
| --- | --- |
| 70% | GET，1 KiB response，default/child 各半 |
| 15% | JSON POST，16 KiB request、32 KiB response |
| 5% | upload/download：90% 10 MiB、9% 100 MiB、1% 1 GiB |
| 5% | SSE，1 event/s，连接 5 分钟 |
| 5% | WebSocket，1 KiB echo，10 msg/s，连接 5 分钟 |

短请求 protocol：HTTP/1.1 40%、HTTP/2 50%、HTTP/3 10%。listener：8080 35%、9080 15%、8443 TCP/UDP 合计 35%、9443 15%。测试同时每秒提交一次 website harmless revision，观察 reload。

## 量化门槛

- Go short-request P95 <= .NET P95 110%；P99 <= 120%。
- Go completed request throughput >= .NET 95%。
- 非注入故障的 5xx/601-610 rate 不高于 .NET；按 4 位小数比较，expected cancel 场景单独统计。
- steady-state RSS <= .NET 120%，30 分钟后 heap/goroutine/connection无持续单调增长。
- config propagation P95 < 2 秒、P99 < 5 秒；notify完全丢失仍 <=30 秒追平。
- DB outage 10 分钟：已有 snapshot proxy success rate不下降超过 0.01 percentage point，管理写 100% 503；恢复 30 秒内追平。`internal/app/TestServeRetainsLKGAndRejectsManagementWritesDuringDatabaseOutage` 已在 PostgreSQL 16 harness 通过真实 TCP 连接中断验证管理 503 信封与 HTTP LKG 同时存活；10 分钟持续流量和恢复 30 秒 SLO 仍是 Manual Testing Item。
- same normalized domain 外部 ACME concurrent order maximum=1；different domain 可并行。
- traffic/report count、bytes、status bucket 与 generated request truth 差异=0；明确 DropWrite fault case只允许等于 dropped metric。

## 切换

1. 冻结合同、fixture、.NET 76 tests、生产 config/schema snapshot 和 migration-pre-Go tag。
2. clone 上跑 W0-W11 全部验证和 legacy import report。
3. 生产只增加 migration；.NET 正常运行 24 小时，确认 trigger/DB metrics。
4. approved Go shadow：LB 无业务流量、ACME off；比较 config build、API shadow、PFX/account/SMTP import 和 health。
5. 停止全部 .NET ACME/TLS writer，确认无 active order；Go 单节点启用 TLS/ACME。
6. canary 流量 1% 30 分钟、10% 2 小时、50% 24 小时；每级满足量化门槛才升级。
7. 逐节点 rolling 到 100%，.NET 只作无 ACME 的短期 LB fallback。
8. 一个完整发布周期无 rollback trigger 后停止 .NET并执行第 9 章清理。

## Rollback trigger

任一项立即停止扩大并切回上一阶段：

- API golden 出现未批准 status/shape/error/null 差异。
- 两个连续 5 分钟窗口 P95 >110% 或非注入 5xx/601-610 高于 .NET。
- config P95 >=2 秒、snapshot拒绝率 >0 或 revision长期不一致。
- SNI certificate missing/错误证书、ACME same-domain concurrent >1。
- traffic/report checksum不一致、migration改变 legacy row/ID。
- secret 泄露、PROXY trust bypass、管理 listener 公开。

## 回滚动作

稳定期内：LB 切归档 .NET -> 停 Go ACME -> 等待/取消 order 并确认 advisory lock释放 -> 恢复 .NET ACME/TLS -> 保留 Go 新表/trigger。不得 down migration。

删除 .NET 后：从 migration-pre-Go tag 和归档 package恢复；数据库仍不删除 Go 表。所有回滚均记录起止、触发 metric、DB revision、active order、certificate fingerprint 和后续修复。
