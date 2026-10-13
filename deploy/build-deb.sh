#!/usr/bin/env sh
set -eu

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <version> <amd64|arm64> <artifact-directory>" >&2
  exit 2
fi

version=$1
arch=$2
out=$3
deb_version=$version
case "$deb_version" in [0-9]*) ;; *) deb_version="0~$deb_version" ;; esac
case "$arch" in amd64|arm64) ;; *) echo "unsupported Debian architecture: $arch" >&2; exit 2 ;; esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
binary="$root/artifacts/$version/yaginx-linux-$arch"
adminui="$root/adminui/dist"
if [ ! -f "$binary" ]; then
  echo "release binary is missing: $binary" >&2
  exit 1
fi
if [ ! -f "$adminui/index.html" ]; then
  echo "adminui build is missing: $adminui/index.html" >&2
  exit 1
fi
if ! command -v dpkg-deb >/dev/null 2>&1; then
  echo "dpkg-deb is required to build a Debian package" >&2
  exit 1
fi

stage=$(mktemp -d "${TMPDIR:-/tmp}/yaginx-deb.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
pkg="$stage/yaginx"
mkdir -p "$pkg/DEBIAN" "$pkg/usr/bin" "$pkg/usr/share/yaginx/adminui" "$pkg/lib/systemd/system" "$pkg/etc/yaginx"
install -m 0755 "$binary" "$pkg/usr/bin/yaginx"
printf '%s\n' "$version" > "$pkg/usr/share/yaginx/version.txt"
cp -a "$adminui/." "$pkg/usr/share/yaginx/adminui/"
find "$pkg/usr/share/yaginx/adminui" -type d -exec chmod 0755 {} +
find "$pkg/usr/share/yaginx/adminui" -type f -exec chmod 0644 {} +
install -m 0600 "$root/gateway/config/yaginx.example.json" "$pkg/etc/yaginx/config.json"
install -m 0644 "$root/gateway/deploy/deb/conffiles" "$pkg/DEBIAN/conffiles"
install -m 0644 "$root/gateway/deploy/systemd/yaginx.service" "$pkg/lib/systemd/system/yaginx.service"
install -m 0644 "$root/gateway/deploy/systemd/yaginx-upgrade.service" "$pkg/lib/systemd/system/yaginx-upgrade.service"
install -m 0644 "$root/gateway/deploy/systemd/yaginx-upgrade.timer" "$pkg/lib/systemd/system/yaginx-upgrade.timer"
install -m 0755 "$root/gateway/deploy/deb/postinst" "$pkg/DEBIAN/postinst"
install -m 0755 "$root/gateway/deploy/deb/postrm" "$pkg/DEBIAN/postrm"
cat > "$pkg/DEBIAN/control" <<EOF
Package: yaginx
Version: $deb_version
Section: net
Priority: optional
Architecture: $arch
Maintainer: Yaginx Gateway <noreply@yaginx.invalid>
Description: Yaginx Go Gateway
 Functional release-candidate gateway with separate management and business listeners.
EOF

mkdir -p "$out"
dpkg-deb --build --root-owner-group "$pkg" "$out/yaginx_${deb_version}_${arch}.deb" >/dev/null
sha256sum "$out/yaginx_${deb_version}_${arch}.deb"
