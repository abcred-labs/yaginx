# Yaginx 安装与升级指南

本文档适用于公开发布的 Yaginx。当前公开制品面向 `linux/amd64`，推荐在
Debian amd64 + PostgreSQL 15 或更高版本上使用 systemd 部署。公开 Release、
安装脚本和容器镜像均来自
[`abcred-labs/yaginx`](https://github.com/abcred-labs/yaginx)。

## 支持范围和安装前检查

安装前确认：

- 主机是 Debian 系列 Linux，架构为 `amd64`。
- PostgreSQL 主版本至少为 15，并且应用主机可以访问数据库。
- DNS、入口防火墙和反向代理已经为业务监听器准备好。
- 管理监听器只允许运维来源访问；不要把管理端口直接开放给公网。
- 生产安装使用受控的密码、证书和备份位置，不要把它们写入 Git、工单或 shell 历史。

检查主机和数据库：

```bash
dpkg --print-architecture
psql --version
sudo systemctl status postgresql --no-pager
```

### PostgreSQL 最低版本与安装

安装脚本不会安装或初始化数据库，数据库需在安装 Yaginx 前自行准备。运行时会检查
`server_version_num`，主版本低于 15 时 `serve` 与 `doctor --check-db` 都会拒绝启动；
该下限固定为 15，不可通过配置调整。

| 系统 | 仓库自带 PostgreSQL | 是否满足 ≥ 15 |
| --- | --- | --- |
| Debian 12 | 15 | 满足 |
| Debian 13 | 17 | 满足 |
| Ubuntu 24.04 LTS | 16 | 满足 |
| Ubuntu 22.04 LTS | 14 | 不满足，需先添加 PostgreSQL 官方 PGDG 源 |
| Debian 11 / Ubuntu 20.04 | 13 / 12 | 不满足，不支持 |

Debian 12、Debian 13、Ubuntu 24.04（使用系统仓库）：

```bash
sudo apt-get update
sudo apt-get install -y postgresql
sudo systemctl enable --now postgresql
```

Ubuntu 22.04（使用 PGDG 官方源安装 PostgreSQL 15）：

```bash
sudo apt-get install -y curl ca-certificates
sudo install -d /usr/share/postgresql-common/pgdg
sudo curl -fsSL -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc https://www.postgresql.org/media/keys/ACCC4CF8.asc
echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt $(. /etc/os-release; echo $VERSION_CODENAME)-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list
sudo apt-get update
sudo apt-get install -y postgresql-15
sudo systemctl enable --now postgresql
```

确认版本不低于 15（输出应不小于 `150000`）：

```bash
sudo -u postgres psql -Atqc 'SHOW server_version_num;'
```

创建角色和数据库、写入 DSN 的步骤见[全新安装 Runbook](go-version-new-install-runbook.md) 第 3、4 节。

## 安装最新公开版本

安装脚本只接受 Debian amd64，以 HTTPS 获取 GitHub Release manifest，并校验版本、架构、
Git SHA 和 SHA-256。建议先下载并查看脚本：

```bash
curl -fsSLO https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh
less install-yaginx.sh
sudo bash install-yaginx.sh
```

脚本会在缺少时安装 `curl` 和 `jq`，首次安装时同时安装 `yaginx-core` 救援包。它会保留
已有的 `/etc/yaginx/config.json`，不会自动替换生产配置。

也可以使用官方一行命令：

```bash
curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh | sudo bash
```

生产环境应优先使用固定版本，并保存校验记录：

```bash
version=1.1.0
curl -fsSLO "https://github.com/abcred-labs/yaginx/releases/download/v${version}/yaginx_${version}_amd64.deb"
curl -fsSLO "https://github.com/abcred-labs/yaginx/releases/download/v${version}/checksums-${version}.txt"
grep "yaginx_${version}_amd64.deb" "checksums-${version}.txt" | sha256sum -c -
sudo apt-get install -y "./yaginx_${version}_amd64.deb"
```

当前公开包只发布 `linux/amd64`，没有 arm64 Debian 包。

## PostgreSQL 和运行配置

新安装时创建专用数据库角色和数据库。生产环境应由 DBA 按组织的密码策略执行，并把
密码放在 secret manager 中：

```bash
sudo -u postgres createuser --login yaginx
sudo -u postgres createdb --owner=yaginx yaginx
```

写入配置时保持 `0600 root:root`。下面的 DSN 仅是格式示例，必须替换为实际受控凭据：

```bash
sudo install -m 0600 -o root -g root /dev/stdin /etc/yaginx/config.json <<'JSON'
{
  "configVersion": 1,
  "database": {
    "dsn": "host=127.0.0.1 port=5432 dbname=yaginx user=yaginx sslmode=disable"
  },
  "ports": {
    "bindAddress": "127.0.0.1",
    "http": 18080,
    "proxyHttp": 19080,
    "https": 18443,
    "proxyHttps": 19443,
    "management": 19090
  },
  "listeners": {"http3": {"enabled": false}},
  "acme": {"enabled": false}
}
JSON
```

验证配置和数据库连接：

```bash
sudo yaginx doctor --check-db --config /etc/yaginx/config.json
sudo yaginx serve --validate --config /etc/yaginx/config.json
```

## 初始化空数据库

`database init` 只接受真正的空数据库。不要在已有业务数据库上执行它：

```bash
sudo yaginx database init --config /etc/yaginx/config.json
sudo yaginx database status --config /etc/yaginx/config.json
sudo systemctl enable --now yaginx
```

首次启动后检查健康状态。以下端口必须替换成配置中的 management 端口：

```bash
curl --fail http://127.0.0.1:19090/health/live
curl --fail http://127.0.0.1:19090/health/ready
systemctl is-active yaginx
```

如果使用默认管理端口 9090，则将上面的 `19090` 替换为 `9090`。首次 setup 页面只应在
受限维护窗口开放，完成初始化后立即关闭防火墙放行。

## 从已有公开 V2 数据库升级

自动升级已移除。迁移旧安装前，先按[旧 timer 迁移步骤](go-version-new-install-runbook.md#removing-the-legacy-upgrade-timer)
停用旧 timer，确认已开始的升级结束，再人工安装目标版本。

升级前必须创建并验证 PostgreSQL 可恢复备份：

```bash
sudo -u postgres pg_dump --format=custom \
  --file=/secure-backup/yaginx-before-upgrade.dump yaginx
```

先确认当前数据库是 V2：

```bash
sudo yaginx database status --config /etc/yaginx/config.json
```

状态应为 `current-v2` 或 `current-v2-with-pending-migrations`。检查目标版本但不安装：

```bash
sudo yaginx upgrade --check \
  --version <version> \
  --config /etc/yaginx/config.json
```

在维护窗口执行安装、重启和 readiness 验证；仅在确认存在待执行迁移时，显式添加
示例中的 `--migrate-database`（否则省略该参数）：

```bash
sudo yaginx upgrade \
  --version <version> \
  --migrate-database \
  --config /etc/yaginx/config.json
```

迁移完成后验收：

```bash
sudo yaginx database status --config /etc/yaginx/config.json
sudo yaginx doctor --check-db --config /etc/yaginx/config.json
curl --fail http://127.0.0.1:19090/health/ready
journalctl -u yaginx --since '15 minutes ago' --no-pager
```

如果旧服务本身不健康，可在确认备份后使用 `--skip-preflight-health`。只预装包而不重启时
使用 `--no-restart`。配置损坏时可以使用 `--package-only`，但该模式不会连接数据库、
执行 migration、重启或自动回滚，必须由运维单独恢复配置并验收。

## V1 和旧 .NET 数据库

- **V1 数据库**：先使用最后一个支持 V1 → V2 的 `0.1.x` 兼容版本完成迁移，再安装 1.x。
  1.x 不会猜测 V1 数据，也不会隐式重写 V1 schema。
- **旧 .NET 数据库**：不能直接让公开 1.x 二进制原地升级。使用只读源连接生成兼容报告，
  将脱敏数据导入新 V2 数据库，再切换应用配置。密码、恢复码、HTTP 正文/请求头、异常堆栈和
  私钥不会由导入器复制；切换后需要重置密码并重新配置或签发 ACME 证书。
- **未知或部分迁移状态**：停止升级，保留旧实例和备份，先完成 schema 检查。

详细规则见 [`migrations.md`](migrations.md) 和
[`public-migration-runbook.md`](public-migration-runbook.md)。

## Docker 安装

拉取固定版本：

```bash
docker pull ghcr.io/abcred-labs/yaginx:1.1.0
```

生产环境可以固定 digest：

```text
ghcr.io/abcred-labs/yaginx@sha256:aecaf1978ddb6080007134716666ecf828795aa39671fb46cdee4c92d7464ef7
```

`latest` 适合测试或受控更新：

```bash
docker pull ghcr.io/abcred-labs/yaginx:latest
```

容器必须挂载受保护的运行配置和持久化状态，并连接 PostgreSQL。不要把 DSN、JWT、证书私钥
或 SMTP 密码写入镜像层或公开 compose 文件。

## 回滚和恢复

升级器会在 readiness 失败时尝试恢复旧 Debian 包，但数据库 migration 是前向、追加式操作。
应用包回滚不等于数据库回滚。若数据库结构已经前进，必须根据变更窗口决定重试兼容版本或从
升级前备份恢复数据库，再恢复匹配的应用包。

主服务无法恢复且已有有效快照时，可使用 `yaginx-core` 救援运行时临时接管业务流量；它不连接
PostgreSQL，也不提供管理 API。恢复主服务后应立即停止核心救援服务并重新验证 readiness。

## 常见问题

| 现象 | 处理 |
| --- | --- |
| `database init` 拒绝执行 | 目标数据库不是空库；已有 V2 数据库应使用 `database migrate`。 |
| `database status` 显示 V1 | 先安装最后一个 `0.1.x` 兼容版本。 |
| 配置出现 unknown property | 删除或转换私有版本字段，按当前运行配置 schema 重新校验。 |
| readiness 返回 503 | 查看 `journalctl -u yaginx`，重点检查数据库、端口、证书、节点审批和快照。 |
| 升级包下载失败 | 检查 GitHub Release、HTTPS、架构和 manifest 中的 SHA-256。 |
| migration 失败 | 不要直接降级；检查 migration tracker、备份和 `upgrade.log` 后决定重试或恢复。 |

已发布的 migration 文件只能追加，不能重写。生产升级前始终保留旧包、配置备份和数据库备份。

