# Yaginx 新版本发布与打包流程

Yaginx 使用 Git tag 触发 GitHub Actions 发布。发布人只需要从已通过 CI 的 `main` 创建
`vMAJOR.MINOR.PATCH` 标签；打包、校验、GitHub Release 和 GHCR 推送由
`.github/workflows/release.yml` 完成。

## 发布前门禁

发布前必须确认：

- PR 已合并到 `main`，且没有未提交的发布相关修改。
- `main` 的 required checks 全部通过：`release-gate`、Go、PostgreSQL 集成、Admin UI、
  文档、secret/dependency/license/SBOM 和 Docker 检查。
- migration 是向前兼容的追加文件，已补充空库、当前库和中断重试测试。
- 版本说明、支持矩阵和升级/回滚说明已更新。
- 仓库不包含凭据、证书私钥、生产数据、私有域名或私有依赖。
- 目标版本没有复用已发布的 tag。

可在贡献者环境运行相关检查：

```bash
cd gateway
go mod verify
go build ./cmd/yaginx
go test -count=1 ./...
go vet ./...

cd ../adminui
pnpm install --frozen-lockfile
pnpm run api:check
pnpm test -- --run
pnpm run build
```

完整门禁以 GitHub Actions 为准；本地通过不代表可以跳过 required checks。

## 版本号和 migration 规则

- `v1.1.x`：当前稳定版本线，首个版本为 `v1.1.0`。
- `v1.x`：稳定公开版本线；只接受 V2 schema，重大不兼容变更使用 major 版本。
- 已发布的 migration 永远不能修改或删除；schema 变更必须新增 forward migration。
- 已发布版本发现问题时创建新的 patch 版本，不能移动 tag 或覆盖 Release 资产。

## 创建和推送 tag

必须从最新的 `main` 创建 tag。发布 workflow 当前只校验 tag 格式，不会自动判断 tag 是否来自
最新 `main`，因此这一步由发布人负责：

```bash
git fetch origin
git checkout main
git pull --ff-only origin main

git tag -a v1.1.0 -m "Release v1.1.0"
git push origin v1.1.0
```

触发格式必须是：

```text
v<major>.<minor>.<patch>
```

例如 `v1.1.0`。不要使用 `latest`、`release-1.1.0` 或移动现有 tag。

## GitHub Actions 自动打包

推送 tag 后，workflow 按顺序执行两个 job：

1. `release-gate` 在 PostgreSQL 服务上运行 Go、migration、Admin UI、文档和
   本地链接检查。
2. `release` 在 gate 成功后构建和发布制品。

`release` job 使用 tag 去掉前缀 `v` 的版本号，并把 GitHub commit SHA 写入 manifest。它会：

1. 构建 Admin UI。
2. 构建 Linux amd64 的 `yaginx` 和 `yaginx-core` 二进制。
3. 生成 Debian 包：

   ```text
   yaginx_<version>_amd64.deb
   yaginx-core_<version>_amd64.deb
   ```

4. 生成版本化 manifest、`latest-release.json`、`latest-core-release.json`。
5. 生成 `checksums-<version>.txt` 和 SPDX SBOM。
6. 发布版本化安装脚本、二进制和 Debian 包。
7. 创建 GitHub Release 并生成 Release Notes。
8. 构建并推送 GHCR 镜像：

   ```text
   ghcr.io/abcred-labs/yaginx:<version>
   ghcr.io/abcred-labs/yaginx:latest
   ```

workflow 使用 GitHub Actions 的 `GITHUB_TOKEN` 登录 GHCR，不保存长期发布密钥。仓库和包的
`contents: write`、`packages: write` 权限必须保持可用。

## 发布后验收

先查看 workflow：

```text
https://github.com/abcred-labs/yaginx/actions/workflows/release.yml
```

确认 GitHub Release 页面存在目标版本，并检查资产至少包括：

- `yaginx_<version>_amd64.deb`
- `yaginx-core_<version>_amd64.deb`
- `yaginx-linux-amd64`
- `yaginx-core-linux-amd64`
- `checksums-<version>.txt`
- `yaginx-<version>.spdx.json`
- `release-manifest-<version>.json`
- `latest-release.json`
- `install-yaginx.sh`
- `install-yaginx-core.sh`

验证 manifest 和校验文件：

```bash
version=1.1.0
curl -fsSL \
  "https://github.com/abcred-labs/yaginx/releases/download/v${version}/release-manifest-${version}.json" | jq .

curl -fsSL \
  "https://github.com/abcred-labs/yaginx/releases/download/v${version}/latest-release.json" | jq .

curl -fsSLO \
  "https://github.com/abcred-labs/yaginx/releases/download/v${version}/yaginx_${version}_amd64.deb"
curl -fsSLO \
  "https://github.com/abcred-labs/yaginx/releases/download/v${version}/checksums-${version}.txt"
grep "yaginx_${version}_amd64.deb" "checksums-${version}.txt" | sha256sum -c -
```

验证 GHCR：

```bash
docker pull "ghcr.io/abcred-labs/yaginx:${version}"
docker pull ghcr.io/abcred-labs/yaginx:latest
docker image inspect "ghcr.io/abcred-labs/yaginx:${version}"
```

生产部署应使用版本 tag 或 digest，不应只依赖可变的 `latest`。

再确认安装脚本和 manifest 只指向 GitHub Release：

```bash
curl -fsSL \
  https://github.com/abcred-labs/yaginx/releases/latest/download/latest-release.json | jq -e '
    .component == "yaginx" and
    (.packageUrl | startswith("https://github.com/abcred-labs/yaginx/releases/download/v")) and
    (.architecture == "amd64") and
    (.sha256 | test("^[0-9a-fA-F]{64}$"))
  '
```

## 失败处理和重试

- 如果 `release-gate` 失败，修复代码或文档后合并新的 PR，再创建新的版本 tag。
- 如果 `release` job 在推送 GHCR 或创建 Release 后失败，先查看已生成的资产，再从 Actions 页面
  重跑失败 job；不要移动或覆盖已发布 tag。
- 如果资产内容错误，使用新的 patch 版本重新发布，并在旧 Release 中说明替代版本。
- 不要使用 `git push --force` 修改发布 tag。
- 不要删除已发布 migration 或重写已经被安装的 Release 资产。

发布失败时保留 Actions 日志、manifest、checksums 和镜像 digest，作为审计记录。

## 发布记录和后续观察

发布完成后记录：

- 版本号、tag 和 Git SHA。
- GitHub Release URL。
- Debian 包和 core 包 SHA-256。
- GHCR 版本 tag、`latest` tag 和 digest。
- SBOM URL。
- 发布人、时间和已知限制。

发布后的 7 天内观察 GitHub Actions、Secret Scanning、Dependabot、Release 下载和 GHCR 拉取。
发现公开环境缺陷时发布新的 patch 版本，而不是修改已有版本。
