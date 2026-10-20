# Yaginx

[English](README.md) | 简体中文

Yaginx 是一个用 Go 编写的 HTTP/HTTPS 网关，提供 React 管理控制台，并以 PostgreSQL
保存配置、证书、遥测数据和节点状态。它面向 Debian/systemd 部署，同时提供容器镜像和
不依赖数据库的救援运行时。

## 功能概览

- 支持 HTTP/1.1、HTTP/2、HTTP/3、WebSocket、SSE，以及流式上传和下载。
- 按主机和路径路由、请求/响应改写、CORS、维护模式、Proxy Protocol、访问控制，
  以及 ACME HTTP-01 证书。
- 管理控制台包括网站、域名、证书、统计分析、请求历史、IP 黑名单、节点控制、
  运行时设置和 API 凭据。
- 基于 PostgreSQL 的数据库迁移、多节点配置同步、健康检查与指标端点、OpenTelemetry
  链路追踪，并在数据库故障期间使用最后一次已知正常的代理快照。
- 提供 Debian 包、Docker 镜像、`yaginx-core` 救援程序、校验和、SPDX SBOM，以及由
  管理员执行的在线升级/回滚命令。

升级需手动执行：先运行 `yaginx upgrade --check --version <semver>`，再在维护窗口内运行
`sudo yaginx upgrade --version <semver>`。数据库迁移需要管理员显式加上
`--migrate-database`。自动升级定时器和相关 CLI 已移除；旧安装请参阅
[迁移运行手册](docs/go-version-new-install-runbook.md#removing-the-legacy-upgrade-timer)。

公开的管理接口契约见
[OpenAPI 文档](planning/go-rewrite/contracts/management-api.openapi.yaml)，
运行时配置契约见
[JSON Schema](planning/go-rewrite/contracts/runtime-config.schema.json)。

## 快速开始

环境要求：Linux amd64、PostgreSQL 15 或更高版本；需要 ACME 证书时，域名须解析到代理
监听地址。当前公开版本不提供 arm64 包，发布产物未签名。

1. 使用
   [`install-yaginx.sh`](https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh)
   安装最新的 Debian 发布包，或从源码构建。请以 root 执行；使用 sudo 时请写成
   `sudo -E bash`，以保留环境变量。

   **全球（默认）：**

   ```bash
   curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh | bash
   ```

   **中国大陆：** 先设置两个下载镜像，再执行同一个安装脚本：

   ```bash
   export YAGINX_GITHUB_MIRROR=https://github-mirror.abc.red
   export YAGINX_RELEASE_ASSETS_MIRROR=https://gh-release-assets.abc.red
   curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh | bash
   ```

   镜像只用于加速下载；包 URL 和 SHA-256 校验和仍以 GitHub Release 为准。详见
   [安装指南](docs/installation-guide.md)。
2. 创建一个空的 PostgreSQL 数据库和应用角色。首次启动后，可在管理端口（默认 9090）
   打开安装向导。
3. 将管理端口限制为仅管理员可访问，完成安装向导，并批准第一个节点。
4. 验证服务和数据库：

   ```bash
   sudo yaginx database status --config /etc/yaginx/config.json
   sudo yaginx serve --validate --config /etc/yaginx/config.json
   curl --fail http://127.0.0.1:9090/health/ready
   ```

完整流程、权限、防火墙建议和恢复命令见
[安装运行手册](docs/go-version-new-install-runbook.md)。

## 文档

- [公开文档站点](https://abcred-labs.github.io/yaginx/)
- [安装指南](docs/installation-guide.md)
- [安装与运维运行手册](docs/go-version-new-install-runbook.md)
- [迁移契约](docs/migrations.md)
- [公开迁移运行手册](docs/public-migration-runbook.md)
- [PostgreSQL 运维规范](docs/postgresql-operations-standard.md)
- [发布就绪与支持矩阵](docs/release-readiness.md)
- [发布打包与发布流程](docs/release-packaging.md)
- [贡献指南](CONTRIBUTING.md) 和 [安全策略](SECURITY.md)

管理监听端口在 `/` 提供 React 控制台，在 `/api/...` 提供 API，在 `/health/live` 和
`/health/ready` 提供健康检查，在 `/metrics` 提供指标，在 `/.well-known/mcp.json` 提供
MCP 发现。已退役的 `/yaginx_console/`、`/swagger`、`/hangfire` 和 `/helps` 路径不属于
Go 运行时。

## 开发与验证

```bash
cd gateway
go test -count=1 ./...
go vet ./...
go run ./cmd/contractlint
go run ./cmd/contractcheck
go run ./cmd/proxycheck

cd ../adminui
pnpm install --frozen-lockfile
pnpm run api:check
pnpm test -- --run
pnpm run build

cd ../yaginx-docs-site
npx --yes pnpm@8.15.9 install --frozen-lockfile
npx --yes pnpm@8.15.9 run typecheck
npx --yes pnpm@8.15.9 run build
```

PostgreSQL 集成测试、ACME/Pebble 测试、systemd 安装与升级演练、性能浸泡测试以及生产
流量演练需要隔离的 Linux 环境。验收标准记录在
[`gateway/tests/e2e/r5-functional-rc-report.md`](gateway/tests/e2e/r5-functional-rc-report.md)
和 [`gateway/tests/e2e/r6-cutover-observation.md`](gateway/tests/e2e/r6-cutover-observation.md)。

## 许可证

Yaginx 以 [GNU GPL v3](LICENSE) 发布。
