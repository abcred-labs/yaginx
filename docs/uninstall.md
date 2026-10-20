# Yaginx 完整卸载 Runbook

本文档适用于 Debian/Ubuntu 主机上的 Yaginx systemd 部署。它会移除 Yaginx
程序、systemd 服务、配置、运行状态、升级缓存和日志，但默认保留 PostgreSQL
数据库、数据库角色和数据库备份。

所有命令均需以 `root` 身份执行，或在命令前加 `sudo`。卸载会中断服务并删除
本机配置和运行数据；执行删除步骤前，请确认仍需要保留的文件已经备份。

当前版本已移除自动升级 worker/timer。下面涉及 `yaginx-upgrade.*` 的检查与清理
仅适用于旧包残留；卸载前先停用旧 timer 并确认 worker 已结束。

## 自动卸载脚本

仓库提供 `deploy/uninstall-yaginx.sh`，会检测并清理 `yaginx`、`yaginx-core` 两个
Debian 包（任意版本）、systemd unit（含旧版 `yaginx-upgrade.*`）、手动安装的
二进制，以及程序自动生成的文件（`/run/yaginx`、`/var/lib/yaginx/{core,upgrade,runtime}`、
`/var/log/yaginx/upgrade.log`）。

脚本使用 `dpkg --remove`，包状态会变为 `config-files`，保留的配置在重装时会被沿用；
未由 systemd 管理的 yaginx 进程不会被停止，需手动处理。

脚本**不会**删除 `/etc/yaginx` 配置、管理员 systemd drop-in、节点身份
（`/var/lib/yaginx/node`）和 PostgreSQL 数据库/角色。

```bash
sudo bash deploy/uninstall-yaginx.sh --check   # 只检查，不修改
sudo bash deploy/uninstall-yaginx.sh           # 卸载（-y 跳过确认）
```

以下手册为手动等价步骤，其中包含脚本不会执行的删除配置与数据步骤。

## 1. 卸载前确认

先确认服务、升级 worker 和升级 timer 的状态：

```bash
systemctl status yaginx.service --no-pager || true
systemctl status yaginx-upgrade.service --no-pager || true
systemctl status yaginx-upgrade.timer --no-pager || true
systemctl list-unit-files | grep -E '^yaginx(-upgrade)?\.' || true
```

确认实际 unit 文件、启动命令和工作目录。`systemctl cat` 在 unit 不存在时会
返回错误，这不影响后续检查：

```bash
systemctl cat yaginx.service || true
systemctl cat yaginx-upgrade.service || true
systemctl cat yaginx-upgrade.timer || true
systemctl show yaginx.service \
  -p FragmentPath -p ExecStart -p WorkingDirectory || true
```

确认是否通过 Debian 包安装：

```bash
dpkg-query -W -f='${Status} ${Version}\n' yaginx 2>/dev/null || true
dpkg -S /usr/bin/yaginx 2>/dev/null || true
```

如需保留配置、节点身份、运行时快照或升级记录，先打包保存到受保护的位置：

```bash
backup="/root/yaginx-uninstall-$(date +%Y%m%d-%H%M%S).tar.gz"
tar -czf "$backup" \
  --ignore-failed-read \
  /etc/yaginx \
  /var/lib/yaginx \
  /var/log/yaginx
chmod 0600 "$backup"
printf '备份文件：%s\n' "$backup"
```

备份文件可能包含 PostgreSQL DSN、证书私钥、ACME 材料或其他敏感信息，必须按
生产凭据级别保护。确认备份可读取后，再继续执行删除步骤。

## 2. 停止并禁用 Yaginx 单元

先停用旧 timer，确认 worker 已完成后停止主服务。Debian 包的 `postrm` 会停止并禁用主服务，但不会
替代对升级 worker 和 timer 的显式清理：

```bash
systemctl stop yaginx-upgrade.timer 2>/dev/null || true
systemctl status yaginx-upgrade.service --no-pager || true
# 如 worker 仍在执行，等其升级及 dpkg 子进程结束，再继续卸载。
systemctl stop yaginx.service 2>/dev/null || true

systemctl disable yaginx-upgrade.timer 2>/dev/null || true
systemctl disable yaginx-upgrade.service 2>/dev/null || true
systemctl disable yaginx.service 2>/dev/null || true

systemctl reset-failed \
  yaginx.service \
  yaginx-upgrade.service \
  yaginx-upgrade.timer 2>/dev/null || true
```

如果服务仍然有进程，先查看进程和监听端口，再结束确认属于 Yaginx 的进程：

```bash
pgrep -a -f '(^|/)yaginx( |$)' || true
ss -ltnp | grep -i yaginx || true
```

## 3. 卸载 Debian 包

如果 `dpkg-query` 显示已安装的 `yaginx` 包，执行：

```bash
apt purge yaginx
apt autoremove
```

`purge` 会删除包管理的程序、unit 文件和声明为 conffile 的
`/etc/yaginx/config.json`；如果管理员曾经创建过额外文件，仍需按第 5 节手动
清理。当前包的 `postrm` 会停止、禁用 `yaginx.service` 并执行
`systemctl daemon-reload`，但卸载前仍应按第 2 节停止升级 timer 和 worker。

如果系统没有 `apt`，可使用等价的 Debian 命令：

```bash
dpkg --purge yaginx
apt-get autoremove
```

## 4. 清理遗留的 systemd unit 和 override

卸载包后检查系统 unit 路径和管理员 override。先列出文件，确认它们属于旧版
Yaginx，再删除：

```bash
find /lib/systemd/system /usr/lib/systemd/system /etc/systemd/system \
  -maxdepth 2 -type f \
  \( -name 'yaginx.service' \
     -o -name 'yaginx-upgrade.service' \
     -o -name 'yaginx-upgrade.timer' \
     -o -path '*/yaginx.service.d/*' \
     -o -path '*/yaginx-upgrade.service.d/*' \
     -o -path '*/yaginx-upgrade.timer.d/*' \) \
  -print
```

确认无须保留后，删除标准 unit 和 drop-in：

```bash
rm -f \
  /lib/systemd/system/yaginx.service \
  /lib/systemd/system/yaginx-upgrade.service \
  /lib/systemd/system/yaginx-upgrade.timer \
  /usr/lib/systemd/system/yaginx.service \
  /usr/lib/systemd/system/yaginx-upgrade.service \
  /usr/lib/systemd/system/yaginx-upgrade.timer \
  /etc/systemd/system/yaginx.service \
  /etc/systemd/system/yaginx-upgrade.service \
  /etc/systemd/system/yaginx-upgrade.timer

rm -rf \
  /etc/systemd/system/yaginx.service.d \
  /etc/systemd/system/yaginx-upgrade.service.d \
  /etc/systemd/system/yaginx-upgrade.timer.d

systemctl daemon-reload
systemctl daemon-reexec
systemctl reset-failed
```

只删除已经确认属于 Yaginx 的文件。不要把包含其他软件配置的整个
`/etc/systemd/system`、`/lib/systemd/system` 或 `/usr/lib/systemd/system` 目录删除。

## 5. 清理本地配置、状态和日志

确认第 1 节的备份已经完成后，删除 Yaginx 本地目录：

```bash
rm -rf /etc/yaginx
rm -rf /var/lib/yaginx
rm -rf /var/log/yaginx
rm -rf /run/yaginx
```

这些目录分别可能包含运行配置、节点 identity、runtime snapshot、升级状态、
升级日志和临时锁文件。删除前如果只想清理旧版本而保留某项材料，应改为按文件
逐项删除，而不要执行整目录清理。

## 6. 处理手动安装的程序

Debian 包卸载不会删除包外手动复制的二进制文件。检查实际来源：

```bash
command -v yaginx || true
type -a yaginx || true
find /usr/local/bin /usr/bin /opt -maxdepth 3 \
  -type f -name 'yaginx' -print 2>/dev/null
```

确认文件属于旧版 Yaginx 后，再删除对应的自定义安装路径，例如：

```bash
rm -f /usr/local/bin/yaginx
rm -f /usr/bin/yaginx
```

如果二进制安装在 `/opt` 或其他自定义目录，只删除已经确认属于 Yaginx 的文件
和目录。不要根据路径猜测并删除共享目录中的其他程序。

## 7. 卸载验收

确认 systemd、包管理器、文件系统和进程中均没有 Yaginx 残留：

```bash
systemctl status yaginx.service --no-pager || true
systemctl list-unit-files | grep -E '^yaginx(-upgrade)?\.' || true
dpkg-query -W -f='${Package} ${Status}\n' yaginx 2>/dev/null || true

test ! -e /etc/yaginx && echo '/etc/yaginx: removed'
test ! -e /var/lib/yaginx && echo '/var/lib/yaginx: removed'
test ! -e /var/log/yaginx && echo '/var/log/yaginx: removed'
test ! -e /run/yaginx && echo '/run/yaginx: removed'

pgrep -a -f '(^|/)yaginx( |$)' || true
ss -ltnp | grep -i yaginx || true
```

正常情况下，`systemctl`、`dpkg-query`、`pgrep` 和 `ss` 不应再报告有效的 Yaginx
服务、安装包、进程或监听端口。旧的 journald 历史记录可能仍存在；如需按主机
日志保留制度清理，请由系统管理员单独执行 journald 保留策略，不要为此删除整个
系统日志目录。

## 8. PostgreSQL 数据保留边界

本 Runbook 不删除 PostgreSQL 数据库、`yaginx` 数据库角色、证书私钥、JWT 材料、
ACME 账户材料或数据库备份。卸载应用程序不会自动删除这些数据库对象。

如果确实需要销毁数据库或角色，必须先完成数据导出、恢复演练、业务确认和 DBA
审批，再使用独立的数据库变更流程执行。不要把 `DROP DATABASE` 或 `DROP ROLE`
加入本机 Yaginx 卸载命令中。
