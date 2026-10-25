# Yaginx Go 版本：全新安装与配置 Runbook

本文档记录 Go 版本 Yaginx 在全新 Debian 主机上的安装、PostgreSQL 15 初始化、配置、迁移和验收流程。只支持 V2 schema。示例不包含真实主机名、口令、JWT 或加密密钥。

> 适用范围：Debian amd64、以单节点本机 PostgreSQL 为起点的全新安装。生产多节点、公开管理面和证书签发需要在此基础上完成网络、审批和 ACME 设计。

## 1. 安装结果与前置条件

| 项目 | 已验证值 / 要求 |
| --- | --- |
| 操作系统 | Debian 12，`amd64` |
| PostgreSQL | 最低主版本 15；本文使用本机 `127.0.0.1:5432` 的 PostgreSQL 15 |
| 发布版本 | `1.1.x`；运行时配置要求 PostgreSQL 15 |
| 运行账户 | `systemd` 以 root 运行；Debian 包不创建专用系统账户 |
| 数据库 | 登录角色 `yaginx`，数据库 `yaginx` |
| 运行配置 | `/etc/yaginx/config.json` |
| 运行时材料 | PostgreSQL 中共享的 JWT、证书、ACME 与 SMTP 材料；数据库及其备份必须受保护 |

以 root 身份执行后续命令。安装脚本会在缺少时安装 `curl` 和 `jq`；数据库服务需要预先安装并运行：

```bash
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql-15
systemctl enable --now postgresql
psql --version
```

确认 PostgreSQL 主版本不低于 15：

```bash
runuser -u postgres -- psql -Atqc 'SHOW server_version;'
```

## 2. 官方安装脚本

按服务器所在网络选择下载来源：

| 网络 | 下载来源 | 环境变量 |
| --- | --- | --- |
| 全球 | `https://github.com`、`https://release-assets.githubusercontent.com` | 无需设置 |
| 中国大陆 | `https://github-mirror.abc.red`、`https://gh-release-assets.abc.red` | `YAGINX_GITHUB_MIRROR`、`YAGINX_RELEASE_ASSETS_MIRROR` |

### 2.1 全球

安装最新公开发布包：

```bash
curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh | bash
```

也可以先下载并审阅脚本，再执行：

```bash
curl -fsSLO https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh
less install-yaginx.sh
bash install-yaginx.sh
```

### 2.2 中国大陆

先设置两个镜像环境变量，再执行与全球相同的安装脚本（以 root 执行；使用 sudo 时写成 `sudo -E bash`）：

```bash
export YAGINX_GITHUB_MIRROR=https://github-mirror.abc.red
export YAGINX_RELEASE_ASSETS_MIRROR=https://gh-release-assets.abc.red
curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh | bash
```

救援包脚本 `install-yaginx-core.sh` 和在线升级 `yaginx upgrade` 使用相同的两个环境变量。

### 2.3 脚本行为

`deploy/install-yaginx.sh` 的行为如下：

1. 拒绝非 root、非 Debian 和非 amd64 主机。
2. 确保 `curl`、`jq` 可用；缺少时通过 APT 安装。
3. 仅通过 TLS 1.2 以上 HTTPS 获取 `latest-release.json`；GitHub Release 下载地址会 302 跳转到资源存储，因此脚本逐跳跟随最多 6 次重定向，且每个重定向目标都必须是 HTTPS。下载失败会重试 5 次，任何失败都会打印失败的行号和命令。
4. 使用 `jq` 验证 manifest 的 `component`、SemVer `version`、`architecture`、`gitSha`、发布时间、64 位 SHA-256 和包 URL。
5. 仅接受 `https://github.com/abcred-labs/yaginx/releases/download/v<version>/yaginx_<version>_amd64.deb` 形式的不可变包 URL。
6. 下载 `.deb` 后以 manifest 的 SHA-256 校验，再通过 `/run/yaginx/upgrade.lock` 的 `flock` 串行执行 `dpkg -i`。
7. 临时下载目录会在脚本退出时删除；`dpkg --force-confdef --force-confold` 保留已有 `/etc/yaginx/config.json`。
8. 首次安装时会先安装独立的 `yaginx-core` 救援包；若该包已安装则不会更新它。
9. 可通过环境变量指定镜像（均必须为 `https://`，中国大陆用法见 2.2）。包 URL 仍按 GitHub 原始地址校验，并以 manifest 的 SHA-256 保证完整性：
   - `YAGINX_GITHUB_MIRROR`（如 `https://github-mirror.abc.red`）：替换 `https://github.com`，包括请求地址和重定向到 `github.com` 的地址。
   - `YAGINX_RELEASE_ASSETS_MIRROR`（如 `https://gh-release-assets.abc.red`）：替换重定向到 `https://release-assets.githubusercontent.com` 的地址，保留路径与签名参数。

`deploy/install-yaginx.sh` 和直接 `dpkg -i` 仅用于首次安装、离线安装或受控包部署；它们不会重启一个已运行的 `yaginx.service`。在线升级使用 `sudo yaginx upgrade`：当服务原本处于运行状态时，该命令会在安装完成后执行一次 `systemctl restart yaginx`，等待 `/health/ready` 返回新版本；失败时自动安装旧包、重启旧服务并验证回滚结果。需要安装某个已发布版本时使用 `sudo yaginx upgrade --version <semver>`，检查但不安装时使用 `sudo yaginx upgrade --check --version <semver>`；指定版本会读取对应的不可变版本 manifest，且拒绝降级。对于已有 V2 schema 但仍有待执行迁移的数据库，使用 `sudo yaginx upgrade --version <semver> --migrate-database`，升级器会先安装候选包，再使用候选版本执行 `database migrate`，成功后才重启服务。迁移失败时不会重启候选服务，会尝试恢复旧包；数据库迁移不是可自动回滚事务，必须由运维确认数据库状态。因此使用普通升级命令后不需要再手动执行 `systemctl restart yaginx`。

安装完成后确认版本：

```bash
dpkg -s yaginx | grep -E '^(Status|Version):'
/usr/bin/yaginx version
dpkg -s yaginx-core | grep -E '^(Status|Version):'
/usr/bin/yaginx-core --version
```

### 2.1 仓库与包内配置资产

| 资产 | 安装时的关系 | 使用说明 |
| --- | --- | --- |
| `deploy/install-yaginx.sh` | 发布为版本化和 latest 安装脚本 | 第 2 节描述其验证与安装行为 |
| `deploy/install-yaginx-core.sh` | 发布为独立核心救援安装脚本 | 仅在受控维护窗口显式更新 `yaginx-core`；普通 `yaginx upgrade` 不会修改它 |
| `gateway/config/yaginx.example.json` | 构建 Debian 包时复制为初始 `/etc/yaginx/config.json` | 仅为包内初始模板；全新 Go 安装按第 4 节改为已验证配置 |
| `gateway/deploy/deb/conffiles` | 声明 `/etc/yaginx/config.json` 为 Debian conffile | 升级时保留本机配置；不应依赖包升级覆盖密钥或端口 |
| `gateway/deploy/deb/postinst` | Debian 安装后执行 | 创建 root 专用的数据/日志目录，并执行 systemd daemon-reload |
| `gateway/deploy/deb/preinst` | Debian 解包前执行 | 停止并禁用项目旧版升级 timer，不停止或等待升级 worker |
| `gateway/deploy/systemd/yaginx.service` | 复制到 `/lib/systemd/system/yaginx.service` | 主服务 unit |
| `gateway/deploy/systemd/yaginx-core.service` | 由 `yaginx-core` 包复制到 `/lib/systemd/system/` | 手工救援 unit，与主服务互斥 |
| `gateway/deploy/postgres/roles.sql` | 不会由 Debian 包或 migration 自动执行 | 可选的生产职责分离授权资产；应在 migration 后由数据库所有者执行，并在文件外受控授予登录角色成员资格 |

本 runbook 的单节点测试使用数据库所有者 `yaginx` 执行 migration 和运行时连接，因此不需要执行 `roles.sql`。需要 migrator/runtime 分离的生产部署应采用该授权资产，并审计每个 `GRANT` 后再执行。

## 3. PostgreSQL 角色和数据库

以下命令生成只在本机保留的随机口令，创建专用登录角色和空数据库。命令不会输出口令：

```bash
set -euo pipefail

db_password="$(openssl rand -hex 32)"
runuser -u postgres -- psql \
  --set=ON_ERROR_STOP=1 \
  --set=app_password="$db_password" \
  --quiet --tuples-only --no-align -d postgres >/dev/null <<'SQL'
SELECT format('CREATE ROLE yaginx LOGIN PASSWORD %L', :'app_password');
\gexec
SQL
runuser -u postgres -- createdb --owner=yaginx yaginx
```

上述流程只适用于不存在同名 role/database 的全新测试环境。已有生产数据库不得重新创建；应使用受控变更为现有数据库配置专用最小权限角色，并先备份。

## 4. 运行配置

Debian 包的 `postinst` 会创建 root 拥有的 `/var/lib/yaginx` 和 `/var/log/yaginx`。以下脚本写入已验证的本机配置；它以回环地址监听，避免测试端口暴露到公网。

```bash
set -euo pipefail

# 使用第 3 节创建角色时的同一个 db_password；不要把它写入 shell 历史。
: "${db_password:?run this in the same root shell that created the database}"

install -m 0600 -o root -g root /dev/stdin /etc/yaginx/config.json <<JSON
{
  "configVersion": 1,
  "database": {
    "dsn": "postgres://yaginx:${db_password}@127.0.0.1:5432/yaginx?sslmode=disable",
    "maxConnections": 4,
    "minConnections": 0
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

运行配置会补齐未显式给出的默认值。`database.dsn` 是必需的内嵌连接串。首次成功执行 `serve --validate` 或 `serve` 时，服务会在 PostgreSQL advisory lock 保护下生成一份共享 JWT 材料；多实例会读取同一记录。

### 4.1 安装后文件、权限和用途

| 路径 | 来源 / 生命周期 | 权限与所有者 | 用途 |
| --- | --- | --- | --- |
| `/etc/yaginx/config.json` | Debian conffile；首次安装后由管理员写入 | `0600 root:root` | 含 PostgreSQL DSN 的运行配置；升级默认保留现有文件 |
| `/var/lib/yaginx/node/identity.json` | 首次成功 `serve` 自动创建 | 仅 root 可写 | 持久节点 UUID；不得手动复制到其他节点 |
| `/var/lib/yaginx/runtime/snapshot.bin` | 运行时创建 | 仅 root 可写 | 明文最近已知配置快照；底层受限本地卷必须受保护 |
| `/var/lib/yaginx/core/runtime.json` | 每次成功加载或重载规则/证书后原子更新 | `0600 root:root` | 核心救援模式唯一输入，包含规则、监听计划和 TLS 私钥；不得手工编辑 |
| `/var/lib/yaginx/snowflake.state` | 节点批准后创建 | 仅 root 可写 | 节点编号的 ID 生成器状态 |
| `/var/lib/yaginx/upgrade/` | 升级功能按需创建 | 仅 root/服务按需写入 | 升级下载状态目录 |
| `/var/log/yaginx/` | `postinst` 创建 | `0700 root:root` | 服务日志目录；`upgrade.log` 为 root-only 升级诊断日志 |
| `/run/yaginx/upgrade.lock` | 安装/升级时创建 | 运行时文件 | 防止并发安装与升级 |

`config.json` 包含 DSN，必须维持 `0600 root:root`。PostgreSQL 保存 JWT、证书私钥、ACME 账户私钥和 SMTP 密码，因此数据库连接、数据库存储和备份必须按同等敏感级别保护。

### 4.2 包安装的 systemd 文件

| 文件 | 默认状态 | 作用 |
| --- | --- | --- |
| `/lib/systemd/system/yaginx.service` | systemd 主机首次安装自动启用并启动；升级保留原状态 | 以 root 执行 `yaginx serve --config /etc/yaginx/config.json`；启用 `NoNewPrivileges`、私有 `/tmp`、只读系统目录和有限写入路径 |
| `/lib/systemd/system/yaginx-core.service` | 不可 enable，只能手工 start | 以 root 执行 `yaginx-core --snapshot /var/lib/yaginx/core/runtime.json`；不连接数据库、不提供管理/健康端口，并与 `yaginx.service` 互斥 |
| `/etc/yaginx/config.json` | Debian conffile | `dpkg --force-confold` 下升级保留本机版本 |

首次安装自动执行 `systemctl enable --now yaginx`；升级不会额外启用或重启主服务。在线升级的重启由管理员发起的 `yaginx upgrade` 负责。自动升级已移除，不再安装升级 worker/timer。

如果没有预先写入有效的 `config.json`，启动服务会进入一次性 setup mode。setup
默认监听 `0.0.0.0:9090`，页面本身可以远程打开，但所有 setup API 都必须携带服务
启动时生成的一次性 token。token 只写入 root 可读的 systemd journal，有效期 15 分钟，
完成初始化后立即失效。由于 setup 会接收数据库凭据并执行 schema 变更，必须先在云安全组
或主机防火墙中把 9090 限制为运维来源地址，完成后删除该放行规则：

```bash
journalctl -u yaginx -n 50 --no-pager
# 浏览器打开 http://<server>:9090/，输入日志中的 setup token
# 实时查看 setup 日志：journalctl -u yaginx -f
# token 过期时：systemctl restart yaginx && journalctl -u yaginx -n 50 --no-pager
# 查看服务状态：systemctl status yaginx --no-pager
# ufw allow from <admin-ip> to any port 9090 proto tcp
# setup 完成后：ufw delete allow from <admin-ip> to any port 9090 proto tcp
```

云安全组入站规则同样只允许 TCP/9090，来源为运维公网 IP 的 `/32`，而非 `0.0.0.0/0`。
HTTP 不加密 token 和数据库凭据；跨不可信网络时请使用 SSH 隧道或 HTTPS 入口。
token 过期后执行 `systemctl restart yaginx` 并重新查看 journal 获取新 token。
正常管理入口由配置中的 management listener 提供，与临时 setup 的端口和访问策略独立。

setup 成功后会写回完整且权限为 `0600` 的 `/etc/yaginx/config.json`，并请求服务重启。
无人值守部署应跳过页面，按第 5 节先写入配置，再执行 `doctor`、`database init` 或
`database migrate` 和 `serve --validate`。

向导不会要求粘贴完整 DSN，而是分别收集 PostgreSQL 主机、端口、数据库名、用户名、
密码和 SSL 模式，由服务端组装连接串。数据库账号必须具备初始化 schema 的 DDL 权限。
随后可选择首节点的监听端口、HTTP/3 和 ACME，最后设置首个管理员邮箱和至少 12 位密码。
数据库已有账号时不会覆盖账号。完成后页面等待 `/health/ready` 成功，再跳转到 management
首页的登录页；管理员密码不会保存在 URL 或浏览器自动提交状态中。

### 4.3 升级日志与故障取证

`yaginx upgrade` 默认以 UTC 时间戳输出检查、下载、校验、安装、重启、就绪轮询、回滚等阶段信息；同一内容会写入 `/var/log/yaginx/upgrade.log`。该文件为 `0600`，达到 10 MiB 时轮转，保留最近 5 个历史文件。日志文件不可写不会阻止升级或回滚，终端输出仍会继续并给出警告。

管理员手动升级的阶段输出会同时写入日志，可用以下命令查看：

```bash
tail -n 200 /var/log/yaginx/upgrade.log
```

若候选版本在重启后未就绪，升级器会把候选运行窗口内 `yaginx.service` 的最后 100 条 journal 一并写入 `upgrade.log`，然后执行既有自动回滚。手工检查相同窗口时使用：

```bash
journalctl -u yaginx --since '<upgrade restart UTC time>' --no-pager
```

## 5. 迁移、节点批准与服务启动

由 DBA 预先创建空数据库后，先用 `doctor` 验证配置和数据库连接，再执行 v2 初始化、角色授权和状态校验；`serve --validate` 必须在初始化完成后执行。对于已经是 V2 的既有环境，先完成可恢复备份，再使用 `database migrate`。`database` 动作位于前，随后可传入 `--config`：

```bash
/usr/bin/yaginx doctor --check-db --config /etc/yaginx/config.json
/usr/bin/yaginx database init --config /etc/yaginx/config.json
psql --set=ON_ERROR_STOP=1 -d yaginx -f /usr/share/yaginx/deploy/postgres/roles.sql
/usr/bin/yaginx database status --config /etc/yaginx/config.json
/usr/bin/yaginx serve --validate --config /etc/yaginx/config.json

systemctl enable --now yaginx
systemctl status yaginx --no-pager
curl --fail --silent --show-error http://127.0.0.1:19090/health/live
```

`database init` 只接受真正空的数据库。`database migrate` 只接受 V2 schema；部分 schema 或未知版本均会拒绝。不能通过 repair/baseline 自动修复。

`upgrade --migrate-database` 只在候选包已安装后执行迁移，默认升级不会自动修改数据库。迁移失败后程序包可以回滚，但数据库结构可能已经前进，不能把包回滚视为数据库回滚；重试前应检查迁移跟踪表和备份。

标准 `yaginx upgrade` 需要读取有效的 `/etc/yaginx/config.json`，即使数据库当前不可用也一样：
升级器必须知道发布源、锁文件、状态目录以及升级后的管理就绪地址，才能安全重启并验证
服务。缺失配置时拒绝升级是有意的安全失败；升级器不会自动选择 `.bak`，因为备份可能
包含过期的 DSN、端口或状态路径。应先人工审核并恢复配置，或显式指定备份副本：

```bash
install -m 0600 -o root -g root /etc/yaginx/config.json.bak /etc/yaginx/config.json
/usr/bin/yaginx serve --validate --config /etc/yaginx/config.json
/usr/bin/yaginx upgrade --config /etc/yaginx/config.json
```

只想查询发布版本时可使用 `yaginx upgrade --check`，但当前实现同样要求配置存在；这
保证检查结果使用与实际升级完全一致的发布源和策略。没有配置时应先从备份恢复，不能
把包升级当作配置恢复工具。

当旧服务已经不健康但需要切换到新程序时，使用
`yaginx upgrade --skip-preflight-health`。它跳过旧服务 readiness 检查，
但重启后仍验证新版本 readiness。维护窗口中可以使用
`yaginx upgrade --no-restart` 只预装程序包，不重启服务。

如果配置文件缺失或损坏，可使用 package-only 模式更新程序包：

```bash
/usr/bin/yaginx upgrade --package-only --version <semver> \
  --manifest-url https://github.com/abcred-labs/yaginx/releases/latest/download/latest-release.json \
  --state-directory /var/lib/yaginx/upgrade \
  --lock-file /run/yaginx/upgrade.lock
```

该模式不读取配置、不连接数据库、不重启服务，也不执行自动回滚或数据库迁移；
安装成功后必须由运维单独恢复配置并重启、验收服务。

`00012_resource_report_time_repair.sql` 会将旧报表的 `report_time` 镜像字段按权威 `report_ts` 修复。完成迁移后，如需补齐历史日报，按 UTC 日期范围运行无监听器的可重试任务：

```bash
/usr/bin/yaginx report backfill --config /etc/yaginx/config.json \
  --from 2026-01-01 --to 2026-08-03 --statement-timeout 5m
```

首次启动时节点会注册为 `pending`。`/health/live` 已返回成功，但 `/health/ready` 会返回 HTTP 503，直到节点被审批并加载配置快照。已验证的单节点测试环境可直接以数据库管理员身份批准唯一待审批节点：

```bash
runuser -u postgres -- psql --set=ON_ERROR_STOP=1 -d yaginx <<'SQL'
WITH pending AS (
  SELECT node_id FROM gateway_node WHERE status = 'pending' ORDER BY created_at LIMIT 1
)
UPDATE gateway_node
SET status = 'approved', node_number = 0,
    approved_at = clock_timestamp(), state_changed_at = clock_timestamp()
WHERE node_id = (SELECT node_id FROM pending);
SQL
```

> **生产边界：** 上述 SQL 仅用于隔离的单节点安装测试。生产节点可通过受控管理面审批，或在该待批准主机上以 root 执行 `/usr/bin/yaginx node approve --yes --config /etc/yaginx/config.json`。本机 CLI 只批准已有的本机 identity，不接受任意 node ID，并由数据库事务分配唯一节点编号；不得批量批准或覆盖已有节点。

等待就绪并检查监听端口：

```bash
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if curl --fail --silent --show-error http://127.0.0.1:19090/health/ready; then
    echo
    break
  fi
  sleep 1
done

ss -ltn | grep -E '127\.0\.0\.1:(18080|19080|18443|19090|19443)\b'
```

## 6. 验收与重启检查

在已验证环境中，数据库状态为 `current-v2`，服务重启后仍保持 ready。执行以下检查：

```bash
set -euo pipefail

systemctl restart yaginx
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if curl --fail --silent --show-error http://127.0.0.1:19090/health/ready >/tmp/yaginx-ready.json; then
    break
  fi
  sleep 1
done

systemctl is-active --quiet yaginx
systemctl is-enabled --quiet yaginx
/usr/bin/yaginx doctor --check-db --config /etc/yaginx/config.json
/usr/bin/yaginx database status --config /etc/yaginx/config.json | grep -x 'current-v2'
jq -e '.status == "ready" and .ready and .nodeStatus == "approved" and .database == "connected"' \
  /tmp/yaginx-ready.json
rm -f /tmp/yaginx-ready.json
```

预期结果：服务为 `active` 和 `enabled`，`doctor` 输出 `database connectivity: ok`，数据库状态为 `current-v2`，ready 响应包含 `ready=true`、`nodeStatus=approved`、`database=connected`。

### 6.1 核心救援接管

核心模式仅在主服务无法恢复时手工启用。它只读取最近一次成功写入的
`/var/lib/yaginx/core/runtime.json`，静态提供五个业务入口；不读取
`/etc/yaginx/config.json`，不连接 PostgreSQL，也不提供管理 listener。

```bash
systemctl stop yaginx
/usr/bin/yaginx-core --validate --snapshot /var/lib/yaginx/core/runtime.json
systemctl start yaginx-core
systemctl is-active --quiet yaginx-core
```

用负载均衡已有的业务域名探针确认实际转发。不要对核心模式使用
`/health/ready`，该路径属于主服务管理端口且核心模式不会绑定它。恢复主服务时：

```bash
systemctl stop yaginx-core
systemctl start yaginx
curl --fail --silent --show-error http://127.0.0.1:19090/health/ready
```

`yaginx-core.service` 与 `yaginx.service` 使用 `Conflicts=` 防止端口争用，但仍须按上述顺序操作。核心包升级只能通过独立的受控安装器执行：

```bash
curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx-core.sh | bash
```

中国大陆服务器先设置镜像环境变量再执行：

```bash
export YAGINX_GITHUB_MIRROR=https://github-mirror.abc.red
export YAGINX_RELEASE_ASSETS_MIRROR=https://gh-release-assets.abc.red
curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx-core.sh | bash
```

## 7. Runtime 配置参考

`/etc/yaginx/config.json` 只能使用以下顶层配置域；未知字段会被拒绝。未写入的字段使用当前版本的默认值。

### 7.1 database、security、node

| 配置项 | 默认值 | 环境变量 | 约束 / 说明 |
| --- | --- | --- | --- |
| `database.dsn` | — | — | 必需、非空的 pgx PostgreSQL URI 或 keyword DSN |
| `database.minimumServerMajor` | `15` | — | 固定为 PostgreSQL 15 |
| `database.maxConnections` | `40` | `YAGINX_DATABASE_MAX_CONNECTIONS` | 4–512 |
| `database.minConnections` | `4` | `YAGINX_DATABASE_MIN_CONNECTIONS` | 0–64，且不大于 max |
| `database.connectTimeout` | `10s` | — | 连接超时 |
| `database.statementTimeout` | `15s` | — | PostgreSQL `statement_timeout` |
| `database.healthQueryTimeout` | `2s` | — | 健康检查超时 |
| `database.maxConnectionLifetime` | `30m` | — | 连接最长寿命 |
| `database.maxConnectionIdleTime` | `5m` | — | 空闲连接回收 |
| `security.jwtIssuer` | `Yaginx Proxy Server` | — | 固定值 |
| `security.jwtAudience` | `Yaingx Clients` | — | 固定值（拼写保持兼容） |
| `security.sessionSeconds` | `600` | — | 固定值 |
| `security.clockSkewSeconds` | `30` | — | 固定值 |
| `node.identityFile` | `/var/lib/yaginx/node/identity.json` | `YAGINX_NODE_IDENTITY_FILE` | 绝对路径，首次运行创建 |
| `node.snapshotFile` | `/var/lib/yaginx/runtime/snapshot.bin` | `YAGINX_SNAPSHOT_FILE` | 绝对路径，受限本地卷中的明文快照 |
| `node.heartbeatInterval` | `15s` | — | 节点心跳间隔 |
| `node.heartbeatStaleAfter` | `45s` | — | 节点失联阈值 |
| `node.shutdownGracePeriod` | `30s` | — | 关闭宽限期 |
| `node.drainTimeout` | `5m` | — | drain 超时 |

### 7.2 ports、listeners、admin

| 配置项 | 默认值 | 环境变量 | 约束 / 说明 |
| --- | --- | --- | --- |
| `ports.bindAddress` | `0.0.0.0` | — | 五个 TCP 监听器的共用绑定地址 |
| `ports.http` / `ports.proxyHttp` | `8080` / `9080` | `YAGINX_HTTP_PORT` / `YAGINX_PROXY_HTTP_PORT` | 1–65535，五个 TCP 端口必须各不相同 |
| `ports.https` / `ports.proxyHttps` | `8443` / `9443` | `YAGINX_HTTPS_PORT` / `YAGINX_PROXY_HTTPS_PORT` | 同上；HTTP/3 使用 `ports.https` 的 UDP 端口 |
| `ports.management` | `9090` | `YAGINX_MANAGEMENT_PORT` | AdminUI、API、health、metrics |
| `listeners.http3.enabled` | `true` | `YAGINX_HTTP3_ENABLED` | HTTP/3 开关 |
| `listeners.http3.maxIdleTimeout` | `30s` | — | QUIC 空闲超时 |
| `listeners.proxyProtocol.headerTimeout` | `30s` | — | PROXY protocol header 超时 |
| `listeners.proxyProtocol.trustedCIDRs` | `0.0.0.0/0`, `::/0` | `YAGINX_PROXY_TRUSTED_CIDRS` | 生产必须缩小可信代理 CIDR；逗号分隔环境变量 |
| `listeners.proxyProtocol.requiredOnProxyListeners` | `true` | — | 固定为 true |
| `admin.basePath` | `/` | — | 固定管理 UI 路径 |
| `admin.uiDirectory` | `/usr/share/yaginx/adminui` | `YAGINX_ADMIN_UI_DIR` | 绝对路径 |
| `admin.apiBodyMaxBytes` | `1048576` | — | 1024–16777216 |
| `admin.indexCacheControl` | `no-cache, no-store, must-revalidate` | — | 固定值 |
| `admin.hashedAssetCacheControl` | `public, max-age=31536000, immutable` | — | 固定值 |

### 7.3 proxy、configSync、acme

| 配置项 | 默认值 | 环境变量 | 约束 / 说明 |
| --- | --- | --- | --- |
| `proxy.activityTimeout` / `proxy.responseHeaderTimeout` | `2h` / `60s` | — | 代理活动/响应头超时 |
| `proxy.maxRequestBodyBytes` | `0` | — | 0 表示无限制；大于 0 时按字节限制 |
| `proxy.maxRequestHeaderBytes` | `1048576` | — | 8192–1048576 |
| `proxy.maxIdleConnections` / `proxy.maxIdleConnectionsPerHost` | `4096` / `256` | — | 连接池上限 |
| `proxy.idleConnectionTimeout` / `proxy.expectContinueTimeout` | `90s` / `1s` | — | HTTP transport 超时 |
| `proxy.upstreamExpectContinue.enabled` | `true` | — | 上游 Expect: 100-continue 开关 |
| `proxy.upstreamExpectContinue.minContentLengthBytes` | `1048576` | — | 最小内容长度 |
| `proxy.upstreamExpectContinue.includedHosts` | `["ai.abc.red"]` | — | 适用上游 host 列表 |
| `proxy.upstreamExpectContinue.includedPathPrefixes` | 三个图像 API 前缀 | — | 必须以 `/` 开头的路径列表 |
| `proxy.ipBlacklistCIDRs` | `[]` | — | 拒绝访问 CIDR 列表 |
| `configSync.channel` | `yaginx_config_changed` | — | 固定 PostgreSQL 通知通道 |
| `configSync.revisionPollInterval` / `configSync.notifyDebounce` | `30s` / `100ms` | — | 配置同步轮询与防抖 |
| `configSync.reconnectInitialBackoff` / `configSync.reconnectMaximumBackoff` | `250ms` / `30s` | — | 通知重连退避 |
| `configSync.eventRetention` / `configSync.inactiveNodeAfter` | `24h` / `168h` | — | 事件、非活动节点保留时间 |
| `configSync.incrementalBatchSize` | `500` | — | 1–10000 |
| `acme.enabled` | 始终启用 | — | 旧配置字段仅为兼容保留 |
| `acme.directoryURL` | Let's Encrypt production directory | — | ACME directory URI |
| `acme.email` | `operator@example.com` | — | ACME 账户邮箱，可按部署修改 |
| `acme.keyType` / `acme.challengeType` / `acme.orderMode` | `EC256` / `http-01` / `single-domain` | — | 固定值 |
| `acme.scanIntervalMinimum` / `acme.scanIntervalMaximum` | `30s` / `120s` | — | 扫描间隔范围 |
| `acme.renewBefore` | `360h` | — | 续期提前时间 |
| `acme.lockRetryBase` / `acme.lockRetryJitterMaximum` / `acme.temporaryRetryMaximum` | `60s` / `15s` / `6h` | — | 锁与临时失败重试 |
| `acme.keyValueEndpointURL` | 未填写时使用本机管理地址 | — | 可选；填写后必须是同集群其他 Yaginx 管理 endpoint，并通过 `/api/identity` 握手 |
| `acme.keyValueSetPath` / `acme.keyValueGetPath` | `/api/key_value/set` / `/api/key_value/get` | — | 固定键值 API 路径 |
| `acme.keyValueRequestTimeout` / `acme.followRedirects` | `10s` / `false` | — | 请求超时；禁止重定向 |
| `acme.keyValueServer.enabled` | 始终启用 | — | 旧配置字段仅为兼容保留；每个节点始终提供本机接口 |

默认情况下，每个节点使用自己的管理端 Key/Value 接口，challenge 数据仍写入共享 PostgreSQL：

```json
{
  "acme": {
    "keyValueEndpointURL": ""
  }
}
```

如果需要使用单独的 provider 节点，也可以只选择一个 provider 节点开启服务端：

```json
{
  "acme": {
    "keyValueEndpointURL": "http://provider-node:9090"
  }
}
```

其他节点将 `keyValueEndpointURL` 配置为同一个 provider 地址；每个节点的本机
Key/Value endpoint 始终启动。provider 必须返回同集群的 `/api/identity` 身份，
并且 provider 与其他节点之间的管理端口通信网段必须包含在
`management.allowedCIDRs` 中。挑战值存储在共享 PostgreSQL，写入后保留 10 分钟。

### 7.4 trafficAggregation、requestHistory、resourceReports

| 配置项 | 默认值 | 环境变量 | 约束 / 说明 |
| --- | --- | --- | --- |
| `trafficAggregation.enabled` | `true` | — | 流量聚合开关 |
| `trafficAggregation.flushInterval` | `15s` | — | 刷新间隔 |
| `trafficAggregation.queueCapacity` / `trafficAggregation.batchSize` | `100000` / `1000` | — | 队列 1000–1000000；批次 1–10000 |
| `trafficAggregation.enableUAParsing` / `trafficAggregation.uaCacheSize` | `false` / `10000` | — | UA 解析与缓存 |
| `trafficAggregation.retentionDays` | `90` | — | 1–3650 |
| `trafficAggregation.updateCurrentPeriod` | `true` | — | 是否更新当前周期；管理台会明确标记未完成的当前桶 |
| `trafficAggregation.excludedPathPrefixes` | `[]` | — | 不聚合的路径前缀 |
| `requestHistory.enabled` | `true` | — | 默认记录错误请求的审计元数据 |
| `requestHistory.recordMode` | `ErrorsOnly` | — | `ErrorsOnly` 或 `AllMetadata`；成功请求采样仅在 `AllMetadata` 下生效 |
| `requestHistory.successSamplingRate` | `0` | — | 0–1；错误请求始终完整记录，例如错误全量加成功 5% 使用 `AllMetadata` 和 `0.05` |
| `requestHistory.retentionHours` | `168` | — | 默认 7 天；1–8760 |
| `requestHistory.bodyCaptureMaxBytes` / `requestHistory.maxHeaderBytes` | `32768` / `16384` | — | body 0–10485760；header 0–1048576 |
| `requestHistory.queueCapacity` / `requestHistory.batchSize` | `10000` / `500` | — | 队列 1–1000000；批次 1–10000 |
| `requestHistory.fullQueueBehavior` | `DropWrite` | — | 固定值 |
| `requestHistory.flushInterval` / `requestHistory.maxFlushRetryCount` | `5s` / `3` | — | 刷新间隔；重试 0–20 |
| `requestHistory.retentionDeleteBatchSize` | `5000` | — | 1–100000 |
| `requestHistory.excludedPathPrefixes` | `[]` | — | 排除路径 |
| `requestHistory.bodyCaptureContentTypes` | `text/`, JSON/XML/+json/+xml | — | 可捕获 body 的 content-type 前缀 |

| `resourceReports.enabled` | `true` | — | 资源报告开关 |
| `resourceReports.minutelyDelay` / `resourceReports.hourlyDelay` / `resourceReports.dailyDelay` | `10s` / `1m` / `5m` | — | 聚合延迟 |
| `resourceReports.minutelyReconciliationLookback` | `10m` | — | 分钟报表回看窗口；按分钟边界，不重算整天数据 |
| `resourceReports.reconciliationLookback` | `48h` | — | 在线报表重算窗口；按 UTC 自然日扩展 |
| `resourceReports.materializationStatementTimeout` | `2m` | — | 仅报表物化事务的 PostgreSQL 语句超时；不改变常规请求的数据库超时 |
| `resourceReports.monitorInfoRetention` | `48h` | — | 原始监控信息保留 |
| `resourceReports.reportRetentionDays` | `365` | — | 旧版统一报表保留期；作为日报保留的下限（90–3650），新部署应配置下列三个粒度字段 |
| `resourceReports.minutelyRetentionDays` / `hourlyRetentionDays` / `dailyRetentionDays` | `30` / `365` / `730` | — | 分别保留分钟、小时、日报表；小时/日数据不会因分钟清理而删除 |
| `resourceReports.leaderLockNamespace` | `yaginx:resource-report` | — | 固定 leader lock 命名空间 |

网站可以在编辑页的“请求跟踪”中覆盖全局历史策略。`inherit` 使用上述全局配置，
`errorsOnly` 只保留错误请求，`all` 默认记录该站点所有已完成请求（成功采样率默认
为 1，也可以按站点设置 0–1）。全局排除路径、队列容量、数据库写入和保留时间仍然
生效；全局关闭时，显式配置了站点模式的站点仍可记录。

### 7.5 analytics、upgrade

管理端提供已认证的 analytics 接口。`POST /api/analytics/overview`、
`/api/analytics/domains`、`/api/analytics/timeseries`、`/api/analytics/contributions`
和 `/api/analytics/details` 使用 JSON 请求体；请求体共享 `from`、`to` 和可选的
`domain` 字段。`overview` 还必须提供 `sections`（`overview`、`domains`、
`timeseries` 的子集），`details` 还支持 `pageIndex` 和 `pageSize` 分页字段。
资源查询使用 `GET /api/analytics/catalog` 和 `GET /api/analytics/subject`。
这些 analytics 接口已经取代旧的 `/api/observability/*` 路由；排障和部署验证时必须
使用新路径和对应的请求方法。它们返回低延迟的持久化域名报表，不向 Prometheus 增加
Host、URL 或客户端 IP 标签。延迟 P50/P95/P99 来自固定直方图，是估算值；返回的
`isPartial` 和 `dataLagSeconds` 必须在使用当前周期数据前一并检查。

管理台对观测、流量和资源视图中的小数统一显示为最多 4 位：按四舍五入处理，不补齐尾随
零，例如 `1.23456` 显示为 `1.2346`、`1.2` 保持为 `1.2`，比例 `0.12345678` 显示为
`12.3457%`。该规则仅作用于展示层；数据库原始值、聚合计算和管理 API 响应保持原始精度，
不得为满足显示格式而在存储或计算中提前舍入。请求数、原始字节、毫秒、时间戳和倒计时仍按
整数语义显示。

请求审计会脱敏 Authorization、Cookie、Token、Secret、Password 与 API Key 相关的
Header、Query 参数和 JSON 字段；只有错误请求持久化受大小限制的正文。数据库、备份和
管理端访问控制仍须按敏感日志处理。

| 配置项 | 默认值 | 环境变量 | 约束 / 说明 |
| --- | --- | --- | --- |
| `observability.logLevel` / `observability.logFormat` | `info` / `json` | `YAGINX_LOG_LEVEL` / `YAGINX_LOG_FORMAT` | level: debug/info/warn/error；format: json/text |
| `observability.metricsPath` | `/metrics` | — | 固定 metrics 路径 |
| `observability.livePath` / `observability.readyPath` | `/health/live` / `/health/ready` | — | 固定健康检查路径 |
| `management.allowedCIDRs` | `0.0.0.0/0`, `::/0` | — | 当前节点管理端口允许来源；修改本机配置后重启 |
| `observability.otlp.enabled` | `false` | — | OpenTelemetry 开关 |
| `observability.otlp.endpoint` | 空 | — | 启用时必须为绝对 http(s) URL；http/protobuf 使用准确的 `/v1/traces` 地址 |
| `observability.otlp.protocol` / `insecure` | `grpc` / `false` | — | `grpc` 或 `http/protobuf` |
| `observability.otlp.sampleRatio` / `exportTimeout` | `0.01` / `5s` | — | 采样率 0–1；导出超时 |
| `upgrade.manifestURL` | 当前公开 latest manifest | `YAGINX_UPGRADE_MANIFEST_URL` | HTTPS release manifest |
| `upgrade.githubMirror` | 空 | `YAGINX_GITHUB_MIRROR`、`--github-mirror` | 可选 HTTPS 镜像，替换 `https://github.com`；中国大陆用 `https://github-mirror.abc.red` |
| `upgrade.releaseAssetsMirror` | 空 | `YAGINX_RELEASE_ASSETS_MIRROR`、`--release-assets-mirror` | 可选 HTTPS 镜像，替换 `https://release-assets.githubusercontent.com`；中国大陆用 `https://gh-release-assets.abc.red` |
| `upgrade.component` / `upgrade.packageName` | `yaginx` / `yaginx` | — | 固定值 |
| `upgrade.stateDirectory` / `upgrade.lockFile` | `/var/lib/yaginx/upgrade` / `/run/yaginx/upgrade.lock` | — | 绝对路径 |
| `upgrade.downloadTimeout` / `upgrade.readinessTimeout` | `30m` / `90s` | — | 下载与升级就绪超时 |
| `upgrade.supportedOS` / `upgrade.supportedArchitectures` | `linux` / `["amd64"]` | — | 固定发布平台约束 |

## 8. 共享运行时材料与环境变量

JWT current/previous 由 PostgreSQL 的 `gateway_runtime_material` 单例保存。修改该记录会发出配置事件，在线实例在后续请求中原子切换材料；证书私钥、ACME 账户私钥和 SMTP 密码同样由 PostgreSQL 保存。不存在本地 JWT、主密钥或密钥环文件。`config.json` 中的 DSN、数据库本身及其备份都必须受同等级访问控制保护，且不得写入仓库、shell history、日志或工单。

环境变量只覆盖其明确声明的字段；systemd unit 默认未设置这些变量。若使用 systemd override，请在修改后执行 `systemctl daemon-reload` 并重启服务。可覆盖的变量为：

```text
YAGINX_DATABASE_MAX_CONNECTIONS
YAGINX_DATABASE_MIN_CONNECTIONS
YAGINX_NODE_IDENTITY_FILE
YAGINX_SNAPSHOT_FILE
YAGINX_HTTP_PORT
YAGINX_PROXY_HTTP_PORT
YAGINX_HTTPS_PORT
YAGINX_PROXY_HTTPS_PORT
YAGINX_MANAGEMENT_PORT
YAGINX_HTTP3_ENABLED
YAGINX_PROXY_TRUSTED_CIDRS
YAGINX_ADMIN_UI_DIR
YAGINX_ACME_ENABLED
YAGINX_LOG_LEVEL
YAGINX_LOG_FORMAT
YAGINX_UPGRADE_MANIFEST_URL
YAGINX_GITHUB_MIRROR
YAGINX_RELEASE_ASSETS_MIRROR
```

## 9. 常见失败与恢复方向

| 现象 | 检查与处理 |
| --- | --- |
| 安装脚本的 `jq` 报 manifest 解析错误 | 查看公开 `latest-release.json` 是否为合法 JSON；不得手工将多行 checksum 直接写入 `sha256` 字段 |
| `serve --validate` 失败 | 确认 PostgreSQL 可连接、已完成 migration，且运行账户能读取 `config.json` |
| `doctor --check-db` 失败 | 检查 PostgreSQL 是否运行、DSN、数据库角色、端口、`minimumServerMajor=15` |
| `database` 显示 usage | 使用 `yaginx database init --config /etc/yaginx/config.json`、`database migrate` 或 `database status`，不要使用已移除的根级 `migrate` |
| ready 为 503 且 `node_not_approved` | 按受控审批流程批准节点；隔离测试可按第 5 节 SQL 处理 |
| ready 为 503 且 `snapshot_unavailable` | 查看 `journalctl -u yaginx` 中的配置快照加载或数据库错误；保留当前 LKG，修复后再重启/升级 |
| ready 为 503 且 `listeners_unavailable` | 查看 `journalctl -u yaginx` 中端口、证书或监听器启动错误；解决冲突或配置错误后再重试 |
| service 未监听或重启失败 | `journalctl -u yaginx -n 100 --no-pager`；检查端口冲突、配置、数据库和密钥 |
| database migration failed | 检查 `upgrade.log` 中的 `database.migrate.failed`；确认数据库备份、迁移跟踪表和目标版本兼容性，再决定重试或人工恢复 |
| 主服务无法升级或回滚且业务必须恢复 | 按第 6.1 节停止主服务、验证并启动 `yaginx-core`；核心快照缺失、损坏或证书过期时会严格拒绝接管 |
| 升级后配置未变化 | 这是预期行为：`/etc/yaginx/config.json` 是 conffile，安装脚本保留本机配置 |


## MCP 自动导入

MCP 复用现有管理端口，不新增端口。配置发现地址为：

```text
http(s)://<管理地址>:<管理端口>/.well-known/mcp.json
```

它返回 `/mcp` 的客户端配置，不包含认证 token。客户端导入后使用：

```text
Authorization: Bearer <在管理 UI“API 凭据”页面创建的 token>
```

管理 UI 位于 `/`，在“系统 → API 凭据”中创建用途为 `mcp` 的凭据并保存一次性返回的 token。管理 API 位于 `/api/...`，MCP endpoint 位于 `/mcp`。旧 `/yaginx_console/` 路径已移除。

<a id="removing-the-legacy-upgrade-timer"></a>

## 移除旧自动升级 timer

升级时机和多节点执行顺序由管理员安排。迁移旧安装前，先检查 unit 来源，确认是项目旧包
提供的 `/lib/systemd/system/yaginx-upgrade.timer`（或 `/usr/lib` 下的同一 unit），然后停用：

```bash
systemctl show yaginx-upgrade.timer -p FragmentPath
sudo systemctl disable --now yaginx-upgrade.timer
systemctl status yaginx-upgrade.service --no-pager
```

确认已经开始的 worker 和其 `dpkg` 子进程结束后，再人工安装审核过的目标版本。
不要通过停止 worker 来结束升级。首次迁移可下载、校验目标 `.deb` 后，在持有
`/run/yaginx/upgrade.lock` 的情况下执行 `dpkg -i`，或使用旧版手动升级命令。
新包的 `preinst` 在解包删除旧 unit 前停止并禁用项目 timer；缺少 unit、重复安装、
无 systemd 或 systemd 无法查询均可通过（无法查询时输出警告并跳过清理），旧 timer
停用失败则明确失败。安装脚本不会停止或等待升级 worker，
允许已开始的旧升级完成，并在安装后执行 `daemon-reload`。兼容清理不删除管理员自建
unit、配置、日志或回滚包，也不改变首次安装和主服务启停策略。

旧配置的 `upgrade.enabled`、`upgrade.minPublishedAge`、`upgrade.checkInterval`
会在本地和共享运行配置校验前被剔除，并通过配置警告说明自动升级已移除；包括
`enabled=true` 在内的旧值均无效。`configVersion` 仍为 `1`。读取配置不因此改写文件；
新生成、保存和导出的配置不再包含这些字段，其他未知字段仍按原规则校验。
旧 `upgrade auto status|on|off` 和 `--auto-worker` 返回用法错误（退出码 `2`），不执行
升级或修改配置。

后续操作推荐先检查目标版本，再在维护窗口执行同一版本：

```bash
sudo yaginx upgrade --check --version <semver>
sudo yaginx upgrade --version <semver>
# 只有管理员确认需要数据库迁移时，才添加 --migrate-database
```

中国大陆服务器应在 `/etc/yaginx/config.json` 的 `upgrade` 中持久配置镜像，之后每次升级都会自动使用：

```json
"upgrade": {
  "githubMirror": "https://github-mirror.abc.red",
  "releaseAssetsMirror": "https://gh-release-assets.abc.red"
}
```

也可以临时通过环境变量指定（`sudo -E` 保留环境变量；`--package-only` 模式不读取配置文件，只读取环境变量）：

```bash
export YAGINX_GITHUB_MIRROR=https://github-mirror.abc.red
export YAGINX_RELEASE_ASSETS_MIRROR=https://gh-release-assets.abc.red
sudo -E yaginx upgrade --check --version <semver>
```

升级器逐跳跟随最多 5 次 HTTPS 重定向，并按上述镜像改写 GitHub 与 release-assets 地址；manifest 中的包 URL 仍按 GitHub 原始地址校验，下载后校验 SHA-256。指定 `--version` 时读取不可变的 `releases/download/v<semver>/release-manifest-<semver>.json`。

包校验、并发锁、拒绝降级、健康检查和失败回滚继续保护手动升级。现有修复参数
`--skip-preflight-health`、`--no-restart`、`--package-only` 保持原语义。

已知独立风险：`dpkg` 安装失败后的恢复策略不在本次移除范围内；包回滚也不能撤销
已执行的数据库迁移。发生这类失败时应保留日志、回滚包和数据库备份，由管理员检查状态。

验收结果见[自动升级移除验收记录](upgrade-removal-validation.md)，独立问题见
[升级器独立风险](upgrade-known-risks.md)。
