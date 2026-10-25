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
Git SHA 和 SHA-256。脚本会在缺少时安装 `curl` 和 `jq`，首次安装时同时安装 `yaginx-core`
救援包。它会保留已有的 `/etc/yaginx/config.json`，不会自动替换生产配置。

请根据服务器所在网络选择安装方式：

| 网络 | 下载来源 | 需要设置的环境变量 |
| --- | --- | --- |
| 全球（可正常访问 GitHub） | `https://github.com`、`https://release-assets.githubusercontent.com` | 无 |
| 中国大陆 | `https://github-mirror.abc.red`、`https://gh-release-assets.abc.red` | `YAGINX_GITHUB_MIRROR`、`YAGINX_RELEASE_ASSETS_MIRROR` |

### 全球

建议先下载并查看脚本：

```bash
curl -fsSLO https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh
less install-yaginx.sh
sudo bash install-yaginx.sh
```

也可以使用官方一行命令：

```bash
curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh | sudo bash
```

### 中国大陆

中国大陆服务器直连 GitHub 下载 `.deb` 通常很慢或被中断。安装脚本本身很小，仍从 GitHub 获取；
脚本随后下载的 manifest 和 `.deb` 通过镜像获取：

| 环境变量 | 镜像地址 | 替换的原始地址 |
| --- | --- | --- |
| `YAGINX_GITHUB_MIRROR` | `https://github-mirror.abc.red` | `https://github.com`（Release manifest 和包地址） |
| `YAGINX_RELEASE_ASSETS_MIRROR` | `https://gh-release-assets.abc.red` | `https://release-assets.githubusercontent.com`（`.deb` 实际下载地址） |

先设置两个环境变量，再执行与全球相同的安装脚本。`sudo` 默认会清除环境变量，因此请以
root 执行，或使用 `sudo -E bash`：

```bash
export YAGINX_GITHUB_MIRROR=https://github-mirror.abc.red
export YAGINX_RELEASE_ASSETS_MIRROR=https://gh-release-assets.abc.red
curl -fsSLO https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh
less install-yaginx.sh
sudo -E bash install-yaginx.sh
```

一行命令：

```bash
export YAGINX_GITHUB_MIRROR=https://github-mirror.abc.red
export YAGINX_RELEASE_ASSETS_MIRROR=https://gh-release-assets.abc.red
curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh | sudo -E bash
```

镜像只负责下载加速：脚本仍按 GitHub 原始地址校验包 URL，并用 manifest 中的 SHA-256 校验
下载的 `.deb`，镜像返回的内容被篡改会被拒绝。两个变量都必须是 `https://` 地址。

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

中国大陆服务器在线升级时，最简单的方式是给 `yaginx upgrade` 加 `--use-mirror`，自动使用内置的
国内镜像（只补全尚未设置的镜像，显式参数、环境变量和配置项优先）。也可以先在
`/etc/yaginx/config.json` 的 `upgrade` 中加入
`"githubMirror": "https://github-mirror.abc.red"` 和
`"releaseAssetsMirror": "https://gh-release-assets.abc.red"`，或执行升级命令时用
`sudo -E` 传入 `YAGINX_GITHUB_MIRROR`、`YAGINX_RELEASE_ASSETS_MIRROR` 环境变量。

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

## 强制升级（手动安装最新 .deb）

`yaginx upgrade` 不可用时（例如旧版本升级器指向其他发布源、无法访问 GitHub），可以直接下载最新
发布的 `.deb` 并安装。该方式**不校验 SHA-256、不备份、不检查数据库状态、不执行 migration**，
安装后立即重启服务；仅在确认数据库已是 `current-v2` 且无待执行迁移时使用。

在服务器上以 root 执行（使用中国大陆镜像；`jq` 不是必需的）：

```bash
GH=https://github-mirror.abc.red; AS=https://gh-release-assets.abc.red; V=$(curl -fsSL $GH/abcred-labs/yaginx/releases/latest/download/latest-release.json | grep -o '"version":"[^"]*"' | cut -d'"' -f4); U=$GH/abcred-labs/yaginx/releases/download/v$V/yaginx_${V}_amd64.deb; R=$(curl -sS -o /dev/null -w '%{redirect_url}' $U); [ -n "$R" ] && U=${R/https:\/\/release-assets.githubusercontent.com/$AS}; curl -fL --retry 5 -o /tmp/yaginx.deb "$U" && dpkg --force-confdef --force-confold -i /tmp/yaginx.deb && systemctl restart yaginx && sleep 8 && yaginx version && curl -s http://127.0.0.1:29090/health/ready; echo
```

说明：

- 命令从镜像读取最新版本号，下载对应 `.deb`，并把 `release-assets.githubusercontent.com` 的重定向改写到资产镜像。
- `--force-confold` 保留现有 `/etc/yaginx/config.json`。
- 任一步失败时后续步骤不会执行，服务保持原状。
- 可访问 GitHub 的环境把 `GH` 改为 `https://github.com`，`AS` 改为 `https://release-assets.githubusercontent.com`。
- 管理端口不是 `29090` 时，把最后的健康检查端口改为实际的 `ports.management`。
- 最新版本号可能低于已安装版本，此时是降级；安装前先确认 `yaginx database status --config /etc/yaginx/config.json` 输出 `current-v2`。
- 需要回退时重新安装旧版本 `.deb` 并重启；数据库迁移不可自动回滚，迁移后不要直接降级。

## 未知或部分迁移状态

`database status` 输出 `unsupported`，或迁移中途失败时：停止升级，保留旧实例和备份，先完成 schema 检查。

详细规则见 [`migrations.md`](migrations.md)。

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
| `database status` 显示 `unsupported` | 数据库不是受支持的 V2 schema；停止升级，保留备份并检查 schema，不要自动重建。 |
| 配置出现 unknown property | 删除或转换私有版本字段，按当前运行配置 schema 重新校验。 |
| readiness 返回 503 | 查看 `journalctl -u yaginx`，重点检查数据库、端口、证书、节点审批和快照。 |
| 升级包下载失败 | 检查 GitHub Release、HTTPS、架构和 manifest 中的 SHA-256。 |
| migration 失败 | 不要直接降级；检查 migration tracker、备份和 `upgrade.log` 后决定重试或恢复。 |

已发布的 migration 文件只能追加，不能重写。生产升级前始终保留旧包、配置备份和数据库备份。

