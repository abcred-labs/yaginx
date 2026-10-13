# ACME 与证书

> **Archived v1 design record.** This document is historical evidence only;
> v2 deployment and operations must follow the current OpenAPI and database
> lifecycle documentation.

## 固定参数

- `lego/v4`、HTTP-01、EC P-256/ES256、email `operator@example.com`。
- production directory `https://acme-v02.api.letsencrypt.org/directory`；staging 通过 config 显式替换。
- 证书缺失或 notAfter 距当前 <= 840 小时进入扫描。
- 首版一个 normalized domain 一个 order，不自动组合 SAN。
- approved 节点均可扫描；pending/draining/disabled 不创建 order。
- wildcard 分类为 permanent，不能 HTTP-01 自动申请。

## 参考实现与适配边界

ACME 协议实现以公开的 `lego/v4` 和 RFC 测试向量为参考；任何外部适配代码都必须经过许可证和来源审查：

| Yaginx 目标 | 参考文件 | 本机绝对路径 | 复用方式 |
| --- | --- | --- | --- |
| lego account/client/ES256 | `go/internal/acme/account.go`、`renewal.go` | `external source path omitted | 复制 lego account、client、renew/obtain orchestration 与对应测试向量；账号和证书由 PostgreSQL 明文 repository 保存。 |
| HTTP-01 provider/response | `go/internal/acme/http01.go`、`challenge.go` | `external source path omitted | 复制 lego provider 接口、token/path 校验、精确 Content-Length 与 fall-through 测试；challenge backend 固定改为 `configurable.example.invalid` set/get。 |
| 热加载证书验证 | `go/internal/tls/manager.go` | `external source path omitted | 迁入 chain/key 验证与 SNI hot-load 测试向量；实际发布通过 immutable snapshot。 |

Edge 的 `sync.Map` domain mutex 只用于单进程重复扫描，Yaginx 可以用 `singleflight` 做本地合并，但绝不能以其代替 PostgreSQL dedicated-session advisory lock。Edge 的 filesystem account/certificate/challenge store、Console lease 和控制面上传均不迁入。

## 域名与互斥

域名 trim、去尾点、IDNA ASCII、小写；拒绝 scheme、port、path、空 label、label >63 和总长 >253。

每次申请使用 dedicated pgx session：

```sql
SELECT pg_try_advisory_lock(
  hashtextextended('yaginx:acme:' || $1, 0)
);
```

只有成功持锁后才创建外部 CA order。锁从 DB order 进入 locked、challenge set/validation、finalize、证书明文入库、旧证书替换和 event commit 全程保持。session 断开立即 cancel；未获锁不调用 CA/set API，60 秒 + 0..15 秒 jitter 后重试。

ACME account 首次注册使用 dedicated lock `hashtextextended('yaginx:acme-account:' || directory_url,0)`；directory URL unique constraint 作为第二道保护。状态、timeout 和 crash recovery 见第 12 章。

### 当前实现证据

- `gateway/internal/acme/worker.go` 已实现 approved node 的周期扫描、单域名 `lego` 签发、account load/create/registration、temporary/permanent 分类和 worker 生命周期；`gateway/internal/repository/acme.go` 持久化 account、order、retry 与替换事务。
- account private key/registration JSON 与 certificate chain/PKCS#8 key 均以明文存入 PostgreSQL，不写独立本地文件；数据库权限和备份必须受保护。
- `gateway/internal/acme/worker_integration_test.go` 在 PostgreSQL 16 中启动三个已审批节点：同一域名只进入一次 external issuance callback，成功后 order 为 `valid`、残留 retry row 被取消、SNI manager 可加载新证书。
- 真实 HTTP-01/DNS callback、真实 CA production key 和生产域名授权仍是第 10 章的 Manual Testing Item；本地 Pebble 验证不能替代它们。

## configurable.example.invalid

设置：

```text
GET https://configurable.example.invalid/api/key_value/set?key=<urlencoded token>&value=<urlencoded keyAuthorization>
```

读取：

```text
GET https://configurable.example.invalid/api/key_value/get?key=<urlencoded token>
```

- 使用 query encoder，禁止字符串拼接未编码值。
- no redirect、10 秒总 timeout、64 KiB response limit。
- 读取只接受 HTTP 200、JSON、信封 `code=200`、非空 string data。
- 日志只记录 token SHA-256 前 12 位，不记录 keyAuthorization。
- set/get network、timeout、5xx 和 malformed envelope 属 temporary，不关闭 free cert。

所有业务 listener 在路由前处理 `/.well-known/acme-challenge/{token}`。命中返回 `application/octet-stream`、UTF-8 精确 body 和字节 Content-Length；未命中或读取失败继续 simple/redirect/proxy，不固定 404。

## 存储与替换

`gateway_certificate` 保留 fingerprint SHA-256、dns names、source、validity、active/replacement、timestamps，以及明文 PEM full chain 和 PKCS#8 key。数据库权限和备份必须保护这些材料。

`gateway_acme_account` 保存明文 account private key 与 registration JSON；数据库读取权限和备份必须受保护。成功签发事务：

1. 解析 chain、SAN、validity，验证 private key match。
2. fingerprint 幂等插入新证书。
3. 同域名旧 active 证书设 inactive/replaced_by。
4. order -> valid，清空 `free_cert_message`。
5. commit 后 trigger notify；各节点热加载。

无证书空窗，旧记录不删除。SNI selection 见代理矩阵。

## Legacy import

`yaginx import-legacy` 只读：

- `/app_data/certs/*.pfx`：空 password，验证 key/SAN/chain，source `legacy_pfx`，fingerprint 幂等。
- `/app_data/accounts/**`：识别现有 account JSON/DER，按 directory URL 幂等。
- `/app_data/system.json`：smtp -> 明文 `gateway_system_setting('smtp')`。

当前实现已覆盖 `system.json` 的 `smtp` root member，兼容 Newtonsoft 常见 PascalCase/camelCase 字段。仅当 PostgreSQL 不存在 `smtp` setting 时导入，既有 Go 管理 API 设置绝不被 legacy 文件覆盖；`internal/repository/smtp_import_integration_test.go` 在 PostgreSQL 16 中验证该幂等行为。`internal/app/import_legacy.go` 也识别旧 LettuceEncrypt 的 `accounts/<authority>/<directory>/<id>.json`：从 base64 DER 转为 PKCS#8 PEM，并只导入与当前 `acme.directoryURL` 相同的账户。`ImportACMEAccountIfAbsent` 不会覆盖已被 lego 注册的 account URL/registration；其单元和 PostgreSQL 16 integration 测试覆盖 DER 解析、明文落库和重复导入。

报告为 JSON + human summary：source、imported/skipped/failed、fingerprint/account URL 和脱敏原因。任何失败不删除、rename 或 chmod 源文件；command exit code 非零只表示存在 failed item，已成功项保持提交。

## 错误分类

- `lock_conflict`：不创建外部 order，短退避。
- `temporary`：DB/network/configurable.example.invalid/CA 429/5xx/timeout，保持 `is_use_free_cert=true`，1m/5m/30m/2h/6h 退避。
- `permanent`：invalid/wildcard、CA 明确拒绝、chain/key mismatch、不可解析 certificate。在同一事务 order failed、`is_use_free_cert=false`、写稳定中文 `free_cert_message`。
- operator 关闭免费证书：pending/retry order canceled，不影响已导入 certificate，未来不续期。

PostgreSQL 共享 JWT 材料轮换、LKG 格式和状态恢复见第 12 章。
