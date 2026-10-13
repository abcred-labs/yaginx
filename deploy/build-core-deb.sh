#!/usr/bin/env sh
set -eu

if [ "$#" -ne 3 ] && [ "$#" -ne 4 ]; then
  echo "usage: $0 <version> <amd64|arm64> <package-output-directory> [artifact-directory]" >&2
  exit 2
fi

version=$1
arch=$2
out=$3
artifact_dir=${4:-"artifacts/$version"}
deb_version=$version
case "$deb_version" in [0-9]*) ;; *) deb_version="0~$deb_version" ;; esac
case "$arch" in amd64|arm64) ;; *) echo "unsupported Debian architecture: $arch" >&2; exit 2 ;; esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
binary="$root/$artifact_dir/yaginx-core-linux-$arch"
if [ ! -f "$binary" ]; then
  echo "core release binary is missing: $binary" >&2
  exit 1
fi
if ! command -v dpkg-deb >/dev/null 2>&1; then
  echo "dpkg-deb is required to build a Debian package" >&2
  exit 1
fi

stage=$(mktemp -d "${TMPDIR:-/tmp}/yaginx-core-deb.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
pkg="$stage/yaginx-core"
mkdir -p "$pkg/DEBIAN" "$pkg/usr/bin" "$pkg/usr/share/yaginx-core" "$pkg/lib/systemd/system"
install -m 0755 "$binary" "$pkg/usr/bin/yaginx-core"
printf '%s\n' "$version" > "$pkg/usr/share/yaginx-core/version.txt"
install -m 0644 "$root/gateway/deploy/systemd/yaginx-core.service" "$pkg/lib/systemd/system/yaginx-core.service"
install -m 0755 "$root/gateway/deploy/deb/core-postinst" "$pkg/DEBIAN/postinst"
cat > "$pkg/DEBIAN/control" <<EOF
Package: yaginx-core
Version: $deb_version
Section: net
Priority: optional
Architecture: $arch
Maintainer: Yaginx Gateway <noreply@yaginx.invalid>
Description: Yaginx Core Rescue Gateway
 Database-free static rescue data plane for a Yaginx Debian installation.
EOF

mkdir -p "$out"
dpkg-deb --build --root-owner-group "$pkg" "$out/yaginx-core_${deb_version}_${arch}.deb" >/dev/null
sha256sum "$out/yaginx-core_${deb_version}_${arch}.deb"
