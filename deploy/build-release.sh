#!/usr/bin/env bash
set -euo pipefail

version=${1:?usage: build-release.sh <version>}
case "$version" in
  [0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "version must be SemVer without the v prefix" >&2; exit 2 ;;
esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out="$root/artifacts/$version"
rm -rf "$out"
mkdir -p "$out"

export GOOS=linux GOARCH=amd64 CGO_ENABLED=0
commit=${GITHUB_SHA:-$(git rev-parse HEAD)}
build_time=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ldflags="-s -w -X yaginx-gateway/internal/app.Version=$version -X yaginx-gateway/internal/app.Commit=$commit -X yaginx-gateway/internal/app.BuildTime=$build_time"
(cd "$root/gateway" && go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$out/yaginx-linux-amd64" ./cmd/yaginx)
(cd "$root/gateway" && go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$out/yaginx-core-linux-amd64" ./cmd/yaginx-core)

"$root/deploy/build-deb.sh" "$version" amd64 "$out"
"$root/deploy/build-core-deb.sh" "$version" amd64 "$out" "artifacts/$version"
(cd "$root/gateway" && go run ./cmd/release-sbom --binary "$out/yaginx-linux-amd64" --output "$out/yaginx-$version.spdx.json" --version "$version")
(cd "$root/gateway" && go run ./cmd/release-sbom --binary "$out/yaginx-core-linux-amd64" --output "$out/yaginx-core-$version.spdx.json" --version "$version" --name yaginx-core)
cp "$root/deploy/install-yaginx.sh" "$out/install-yaginx.sh"
cp "$root/deploy/install-yaginx-core.sh" "$out/install-yaginx-core.sh"

sha256sum "$out"/* > "$out/checksums-$version.txt"
sha256=$(sha256sum "$out/yaginx_${version}_amd64.deb" | awk '{print $1}')
core_sha=$(sha256sum "$out/yaginx-core_${version}_amd64.deb" | awk '{print $1}')
published=$(date -u +%Y-%m-%dT%H:%M:%SZ)
cat > "$out/release-manifest-$version.json" <<EOF
{"component":"yaginx","version":"$version","packageUrl":"https://github.com/abcred-labs/yaginx/releases/download/v$version/yaginx_${version}_amd64.deb","sha256":"$sha256","publishedAtUtc":"$published","architecture":"amd64","gitSha":"$commit"}
EOF
cat > "$out/release-core-manifest-$version.json" <<EOF
{"component":"yaginx-core","version":"$version","packageUrl":"https://github.com/abcred-labs/yaginx/releases/download/v$version/yaginx-core_${version}_amd64.deb","sha256":"$core_sha","publishedAtUtc":"$published","architecture":"amd64","gitSha":"$commit"}
EOF
cp "$out/release-manifest-$version.json" "$out/latest-release.json"
cp "$out/release-core-manifest-$version.json" "$out/latest-core-release.json"
