# 部署与升级

> **Archived v1 design record.** This document is historical evidence only;
> v2 deployment and operations must follow the current OpenAPI and database
> lifecycle documentation.

## 配置与运行时材料

普通 config 固定 `/etc/yaginx/config.json`，字段/default/env 见 [runtime schema](../../contracts/runtime-config.schema.json)。端口统一由 `ports.bindAddress`、`ports.http`、`ports.proxyHttp`、`ports.https`、`ports.proxyHttps` 和 `ports.management` 定义；HTTP/3 自动共用 HTTPS 地址与端口。优先级：CLI flag 仅选择 config/command > 兼容的 `YAGINX_` env override > JSON > schema default。`YAGINX_MANAGEMENT_ADDRESS` 已移除，管理 listener 和业务 listener 都使用 `ports.bindAddress`。动态 website/domain/certificate/SMTP 不放本地 config。

数据库 DSN 必须作为 `database.dsn` 写入受限的 `/etc/yaginx/config.json`。JWT current/previous、证书私钥、ACME 账户私钥和 SMTP 密码保存在 PostgreSQL；数据库读取权限和备份读取权限必须按这些材料的敏感级别保护。首个服务实例在 advisory lock 下创建共享 JWT 材料，配置事件触发所有实例在线重载。

Debian 首装提供 schema v1 的 `0640 root:yaginx` `/etc/yaginx/config.json`。管理员必须在启用服务前写入数据库 DSN；包不生成本地密钥，也不自动启动服务。

## 文件布局

```text
/usr/bin/yaginx
/etc/yaginx/config.json
/usr/share/yaginx/adminui/
/var/lib/yaginx/node/identity.json
/var/lib/yaginx/node/snowflake.state
/var/lib/yaginx/runtime/snapshot.bin
/var/lib/yaginx/upgrade/
/run/yaginx/upgrade.lock
```

用户/group `yaginx`，state/runtime dirs 0700，identity/state/snapshot 0600。快照为受限本地卷中的明文。默认四业务端口均 >1024，不需要 root/capability。

## 构建产物

- React 按现有 lock 构建，产物安装 adminui；runtime 不含 Node/pnpm。
- Go 1.25、`-trimpath`、ldflags 注入 version/git SHA/build UTC；仅 linux/amd64 交付。
- Debian package、OCI image、SHA-256、SPDX SBOM 和 manifest。每个版本的 deb、checksum、SBOM 与 installer 均使用带版本号的不可变对象名；`latest-release.json` 是唯一可变指针并最后发布。
- image 包含 binary、CA、tzdata、adminui、non-root user；声明 TCP 8080/9080/8443/9443 和 UDP 8443。
- CI 扫描禁止 dotnet、NuGet cache、`.dll` backend artifact、C# source、telemetry/Redis hardcode 和 secret。

## 参考实现与依赖选择

原子写入和升级 runner 使用公开依赖和可审计的文件同步、SHA-256、`dpkg` 与 readiness 测试。GitHub Actions 发布不可变 Debian 包、Docker 镜像、SHA-256、SBOM 和版本清单到 GitHub Releases；不依赖私有文件系统、私有 registry 或内部 CI 服务。

Telemetry `cmd/regional-processor/upgrade_lock_linux.go`、`upgrade_lock_other.go` 和 systemd hardening 继续作为 flock/lifecycle 的次级参考。本机路径分别为 `external source path omitted 与 `external source path omitted commit `8515413`。

## PostgreSQL runtime role

数据库 owner 在每次 `yaginx migrate up` 后执行 `gateway/deploy/postgres/roles.sql`。该脚本幂等创建 `yaginx_migrator` 与 `yaginx_gateway` 的 NOLOGIN group role，并按第 3 章授予现有表、sequence 和 config trigger 的权限；实际 login role 的 membership 由部署访问控制配置，DSN 位于受限 runtime JSON。

`yaginx_gateway` 没有 schema `CREATE`、`ALTER`、`DROP` 或角色管理权限。`yaginx_migrator` 只由人工 migration 操作使用；运行节点的 DSN 必须属于 gateway role。生产前以该 login role 执行 `yaginx doctor --check-db`，并确认 `CREATE TABLE yaginx_permission_probe (...)` 被 PostgreSQL 拒绝。

release manifest 版本比较固定使用 `github.com/Masterminds/semver/v3`，不沿用 Edge Gateway 或 Telemetry 的手写三段整数比较；下载和校验实现使用标准库 SHA-256。当前发行物没有独立签名，客户端只信任 HTTPS、固定 GitHub Releases URL 和 manifest SHA-256；GitHub Releases 写权限失守时无法抵御篡改，这是已接受的 R5 供应链风险，后续版本再引入签名验证。

## systemd

`ExecStart=/usr/bin/yaginx serve --config /etc/yaginx/config.json`，`User/Group=yaginx`，`Restart=on-failure`，`TimeoutStopSec=330`，`LimitNOFILE=1048576`。hardening、ReadWritePaths 和 telemetry 模板差异见第 13 章。

部署时 migration 是独立 one-shot：

```text
yaginx migrate --config /etc/yaginx/config.json status
yaginx migrate --config /etc/yaginx/config.json up
systemctl restart yaginx
```

多个部署并发也由 fixed DB advisory lock 串行；service 启动不跑 migration。

## 人工升级

`yaginx upgrade --check` 读取 HTTPS GitHub Releases manifest，验证 component/version/arch/SHA/immutable package URL；`yaginx upgrade` 要求 Linux root，仅人工触发。`verify` 和 `rollback` 是本地兼容命令，不读取 GitHub Releases。`yaginx upgrade auto status|on|off` 管理 root `yaginx-upgrade.timer`，默认关闭，timer 固定每六小时检查且仅接受发布时间至少 24 小时的版本。网关主服务继续以 non-root `yaginx` 用户运行；手动命令和 timer 在 runner 内共同持有 `/run/yaginx/upgrade.lock`：

1. 获得 `/run/yaginx/upgrade.lock` flock。
2. 下载同 filesystem temp，禁止 redirect 并校验 SHA-256。
3. node approved -> draining，等待最多 5 分钟。
4. 从 immutable `checksums-<current>.txt` 下载、校验并原子保存当前 deb 到 `/var/lib/yaginx/upgrade/rollback/`；无法获得可信旧包时拒绝升级。
5. `dpkg --force-confdef --force-confold -i`。
6. systemd 启动，在 `readinessTimeout` 内轮询本机 `/health/ready`，要求 HTTP 200、`ready=true`、node ID 相同、version=target。
7. 成功 draining -> approved；失败安装前一 deb 并再次验证 ready。

升级不管理 PostgreSQL、Redis、LB 或其他节点。滚动 orchestration 由操作员一次一节点，保持至少 N-1 ready。

## Migration + rolling 顺序

1. 在生产 clone 跑 migration/data/API/proxy tests。
2. 生产执行只增加、向后兼容 migration；旧 .NET 继续运行。
3. 升级一个 shadow/standby Go 节点并验证。
4. 每次一个生产节点 drain、package upgrade、ready、恢复 LB。
5. 检查 applied revision、certificate、API、traffic/history 和 error metrics再下一个。

snapshot format version 1。新版本必须读上一 patch/minor 的 v1 snapshot；无法解析/验证则忽略并 full load。新节点在 DB 不可用时不启动，已运行节点保留内存中的已加载状态。

## JWT rotation

- 数据库 `gateway_runtime_material` 保存 current/previous；更新行会通过配置事件使所有实例原子重载。
- 在 session + clock-skew 窗口后清空 previous。无本地密钥文件、重加密或 snapshot 密钥轮换流程。

## .NET 下线

切流稳定一个完整发布周期后：

- 创建 migration-pre-Go Git tag，归档最后 .NET deb/image/publish package、config template、checksum 和恢复说明。
- 删除 `backend/`、solution/project、NuGet 和 .NET CI/Docker/systemd layer。
- README/docs 只保留 Go 当前运行方式；历史从 tag/Git 审计。
- 扫描当前 tree/release 确认无 runtime/build dependency。

数据库不做破坏性清理；旧列和 Go 新表继续保留，后续删除需独立版本化 migration 和数据保留评审。
