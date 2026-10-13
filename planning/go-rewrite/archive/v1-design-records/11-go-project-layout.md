# Go 项目目录

> **Archived v1 design record.** This document is historical evidence only;
> v2 deployment and operations must follow the current OpenAPI and database
> lifecycle documentation.

> **v1 RC 范围。** 本章的目录、Prometheus/OTLP 依赖和 package 边界记录 Go v1 设计。R7/v2 仅在[第 16 章](../../16-minimal-infrastructure-runtime.md)的实施包启动后调整 `Supervisor`、`TransportRegistry`、telemetry 和 `RuntimeStatus` 的内部布局。

Go 后端根目录固定 `gateway/`，module `yaginx-gateway`，Go directive 最低 `1.25.0`。它与 `adminui/`、`docs/`、`backend/` 平级；切换完成前不移动 legacy 目录。

## 当前实现状态

本章区分“已有实现”和“里程碑门禁”，避免把目录或局部测试存在误认为功能完成。完整状态以第 15 章为准；R0/W0 与 R1/W1-W2 已完成，R2 管理 Shadow 和功能型 RC 仍在进行。

- `serve` 已运行管理 listener、四个业务 TCP listener、8443 UDP HTTP/3、PROXY Protocol、数据库快照/LKG、React 静态资源和管理 API；新节点在数据库不可用时拒绝启动，已运行节点保留内存中的已加载状态。
- `migrate up|status` 已使用 pgx/goose、固定 session advisory lock、legacy fail-closed 检查、触发器与 00001-00011 migration；PostgreSQL 16 的空库、完整 legacy 行/ID/JSONB checksum、安全 index repair、旧版本前缀、unsafe partial、隔离 schema、管理流和三节点通知均有集成门禁；runtime role 仍须随 PostgreSQL 16 harness 作为 R1 发布证据重跑。
- `import-legacy` 已读取并幂等导入空密码 PFX、旧 LettuceEncrypt account JSON/DER 和 SMTP 文件；账户导入只接受当前 configured directory，且绝不覆盖已由 Go 注册的 account。
- `doctor` 已验证 runtime schema、四监听端口和 PostgreSQL 连通性；atomic runtime write 在 Debian WSL 已验证拒绝 symlink、0600 output 和 file/parent fsync。真实生产 login-role、PROXY trust CIDR 与安装后权限仍是 Manual Testing。
- production config loader 已 materialize defaults/env 覆盖，并在二进制内嵌与 `runtime-config.schema.json` 字节同步的 Draft 2020-12 schema；JWT 材料由 PostgreSQL 共享加载。
- `sql/queries/` 已包含业务命名 query，`internal/database/sqlc` 由固定 `sqlc v1.29.0` 生成。
- contract test 已校验 41 个 OpenAPI operation、72 个 API fixture、81 条 proxy matrix 行、runtime schema、Markdown links 和 Mermaid；R0 验收记录在 `gateway/tests/contract/r0-acceptance.md`。
- `go.mod` 已包含 chi、JWT、lego、HTTP/3、PROXY Protocol、sqlc、Prometheus、OpenTelemetry 官方 SDK/OTLP exporter 和证书格式支持；Prometheus 低基数指标及 management-only exposition 已接入。OTLP 默认关闭，启用时以脱敏 server span 导出到指定 collector。
- `go test ./...`、`go vet ./...` 和合同检查通过是基础证据；只有第 15 章的完整门禁通过才能把对应里程碑标记为 `DONE`。

## 目标目录

```text
gateway/
  cmd/yaginx/                    # thin main
  config/                        # no-secret examples
  migrations/                    # embedded Goose SQL
  sql/queries/                   # sqlc source
  internal/app/                  # serve/migrate/import/doctor/upgrade/version
  internal/bootstrap/            # dependency assembly
  internal/config/               # runtime schema-equivalent loader
  internal/auth/                 # JWT signing and verification
  internal/database/             # pgx/pool/migration/sqlc output
  internal/management/
    account/
    website/
    webdomain/
    traffic/
    history/
    report/
    settings/
    nodes/
    envelope/
    router/
  internal/auth/
  internal/nodes/
  internal/configsync/
  internal/runtimecfg/           # immutable snapshot types/owner
  internal/listeners/
  internal/proxy/
    config/
    runtime/
    transforms/
  internal/certificates/
    runtime/
    store/
  internal/acme/
  internal/telemetry/
    traffic/
    history/
    reports/
    retention/
  internal/adminui/
  internal/importlegacy/
  internal/platform/
    atomicfile/
    buildinfo/
    httpx/
    identity/
    lifecycle/
    upgrade/
  tests/
    contract/
    integration/
    e2e/
    fixtures/
  deploy/
    debian/
    docker/
    systemd/
```

## Import 边界

- `cmd/yaginx` 只 import `internal/app`；不含业务逻辑。
- management handler -> service/repository，不 import proxy runtime concrete。
- proxy runtime只读 `runtimecfg.Snapshot`，不 import management/database，不同步写 DB。
- configsync是 snapshot 唯一 writer；certificate/website builder返回 candidate fragment。
- telemetry只通过 bounded event API接入 proxy；DB flush 在后台。
- ACME通过 certificate store和 config event，不直接 mutate runtime snapshot。
- platform package不 import任何业务 package。
- 不创建公共 `pkg/`；不跨仓库 import telemetry `internal`。

循环依赖通过 interface 放在消费者包解决，不建立通用 `common/utils`。sqlc generated package只由 repository层使用。

## Generated 与手写

- `sql/queries/*.sql` 手写，`internal/database/sqlc` generated，不手改。
- OpenAPI 是合同，不生成 production handler；可生成 test request/response validator。
- runtime JSON Schema 由 config tests读取；production loader实现同一字段/default规则。
- migration SQL 手写并 code review；`contracts/database-schema.sql` 只用于设计 diff。
- embed adminui只允许 release build option；默认从 `/usr/share/yaginx/adminui` serving。

## Telemetry 复制

复用目标只放 `internal/platform`，来源/测试/license规则见第 13 章。禁止复制 processor、Redis、HLL、batch、signature、assignment和auto-update loop。

## 本地质量命令

```powershell
Set-Location gateway
go test ./...
go test -race ./...
go vet ./...
go generate ./...
go build ./cmd/yaginx
go run ./cmd/yaginx doctor --config config/yaginx.example.json
```

PostgreSQL/API/proxy/HTTP3/ACME/upgrade tests按第 14 章独立 target运行。直到 W12 完成，现有 production .NET build/deploy仍保留，不由 gateway skeleton替换。
