# 自动升级移除验收记录

日期：2026-10-18。范围：移除自动升级，保留管理员手动升级及原有保护流程。

## 已执行验证

| 范围 | 验证方式 | 结果 |
| --- | --- | --- |
| 旧 CLI | `TestRetiredAutomaticUpgradeEntrypointsHaveNoSideEffects` 覆盖 `auto`、`status/on/off`、worker flag 及其组合 | 退出码 2，配置原字节与目录内容不变 |
| 手动 CLI 参数解析 | `TestManualUpgradeFlagsStillParse` | 检查、指定版本、迁移、不重启及跳过旧检查参数可解析 |
| package-only CLI | 入口回归及安装包中真实 CLI 执行 | 配置缺失时不再 panic，返回普通错误且不读取配置或连接数据库 |
| 跳过旧健康检查 | 健康目标、不健康目标、已知 node ID 不匹配回归 | 跳过旧检查后仍验证新服务；失败回滚；已知节点身份仍严格验证 |
| 本地配置兼容 | `upgrade_compat_test.go` | 剔除旧字段并警告，保留手动配置和 configVersion，读取不写文件 |
| 共享配置兼容 | 配置边界单测及 `TestRetiredUpgradeSettingsReadApplyAndSave` PostgreSQL 集成测试 | 旧字段读取、应用正常；数据库读取不改写，保存与导出不含旧字段 |
| 未知字段 | 本地、共享校验回归 | 其他未知 upgrade 字段仍失败 |
| 升级保护 | upgrade runner 单测 | 同版本不安装、拒绝降级、SHA-256 包损坏拒绝、迁移先于重启、就绪失败回滚通过 |
| 发布时间 | 成功安装测试使用发布约一分钟的版本 | 手动安装不受旧最小发布时间限制 |
| 并发锁 | Linux 原生测试二进制执行 `TestUpgradeLockConflictAndRelease`，及 runner 锁冲突测试 | 同一路径 flock 冲突明确报错，退出后锁可再次获取 |
| 包内容 | `build-deb.sh` 和 `verify-deb-package.sh` | 新包不含两个旧 unit，preinst/postinst/postrm 均为可执行 hook |
| 常规检查 | `go test -count=1 ./...`、`go vet ./...`、`go mod verify` | 通过 |
| PostgreSQL 集成 | database、configsync、repository、management、app、acme | 隔离 PostgreSQL 18 容器全部通过 |
| 文档 | 本地链接检查、Docusaurus production build | 通过 |

## 一次性 Debian/systemd 验证

使用 Debian 13、systemd 257 的一次性 Docker 容器，未挂载宿主机 systemd socket 或
cgroup 文件系统。测试结束销毁容器。旧包是由新包构造的迁移夹具，含旧 unit 名称与
一个持续运行的 worker；不下载或运行旧版本自动升级二进制。

已通过的场景：

1. 全新安装与重复安装：主服务按现有策略启用、启动，重复安装不重启主服务。
   包内旧 CLI 返回 2，配置与升级日志不变；package-only CLI 在缺少配置时正常返回错误。
2. 旧 timer 已启用且 worker 正在执行：timer 停用、链接与旧文件移除，worker PID
   不变；原本未启动的主服务保持未启动，配置、日志和回滚包保留。
3. 已启用的旧 unit 文件缺失、残留启用链接：安装和重复安装通过，残留链接被清理。
4. worker 内执行 `dpkg -i`：安装完成，无 worker 停止或等待死锁。
5. 管理员在 `/etc/systemd/system` 提供的同名 timer：文件、运行和启用状态保留。
6. 旧 timer 拒绝手动停止：安装明确失败并阻止解包，旧 unit 仍在。
7. 注入 systemctl disable 失败：preinst 明确失败；systemctl 不存在，或 `systemctl show` 无法查询
   （注入失败）时，preinst 输出警告并跳过清理，不阻止安装。
8. 无 systemd 的容器：全新安装和重复安装通过。

可在支持特权容器与 cgroup v2 的 Linux Docker 环境复现：

```bash
docker build -f deploy/tests/Dockerfile.systemd \
  -t yaginx-upgrade-removal-systemd deploy/tests
python3 deploy/tests/upgrade-removal-smoke.py \
  artifacts/<version>/yaginx_<version>_amd64.deb yaginx-upgrade-removal-systemd
```

`BASE_IMAGE` 构建参数可改用已有的 Debian 13 镜像；本次验证使用已有的 Debian 13
基础镜像安装 systemd 257，避免依赖公开 registry 的实时可达性。

该脚本只用于打包验收，不提供运行时升级入口或调度。

## 独立风险

本次不扩展 `dpkg` 安装失败后的恢复策略；数据库迁移也不由包回滚撤销。
验收中发现的两处既有手动参数缺陷已获确认并修复，详情与独立风险见
[升级器独立风险](upgrade-known-risks.md)。
