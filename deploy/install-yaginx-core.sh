#!/usr/bin/env bash
set -Eeuo pipefail
trap 'echo "install failed at line $LINENO: $BASH_COMMAND" >&2' ERR

github_base=https://github.com
download_base=${YAGINX_GITHUB_MIRROR:-$github_base}
download_base=${download_base%/}
[[ "$download_base" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?(/[A-Za-z0-9._~/-]*)?$ ]] || { echo "YAGINX_GITHUB_MIRROR must be an https:// URL" >&2; exit 1; }
assets_base=https://release-assets.githubusercontent.com
assets_mirror=${YAGINX_RELEASE_ASSETS_MIRROR:-}
assets_mirror=${assets_mirror%/}
[[ -z "$assets_mirror" || "$assets_mirror" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?(/[A-Za-z0-9._~/-]*)?$ ]] || { echo "YAGINX_RELEASE_ASSETS_MIRROR must be an https:// URL" >&2; exit 1; }
component=yaginx-core
release_url=$download_base/abcred-labs/yaginx/releases/latest/download/latest-core-release.json
package_prefix=$github_base/abcred-labs/yaginx/releases/download/v

test "$(id -u)" -eq 0 || { echo "run as root" >&2; exit 1; }
test -f /etc/debian_version || { echo "Debian is required" >&2; exit 1; }
test "$(dpkg --print-architecture)" = amd64 || { echo "Yaginx core installer supports Debian amd64 only" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { apt-get update; DEBIAN_FRONTEND=noninteractive apt-get install -y curl jq; }
command -v jq >/dev/null 2>&1 || { apt-get update; DEBIAN_FRONTEND=noninteractive apt-get install -y jq; }
work=$(mktemp -d /tmp/yaginx-core-install.XXXXXX)
trap 'rm -rf "$work"' EXIT

# Follows redirects one hop at a time so GitHub and release-asset redirects can be rewritten to their mirrors.
fetch() {
  local url=$1 out=$2 hop location
  for hop in 1 2 3 4 5 6; do
    location=$(curl --fail --silent --show-error --retry 5 --retry-delay 3 --retry-all-errors --proto '=https' --tlsv1.2 -w '%{redirect_url}' -o "$out" "$url")
    test -n "$location" || return 0
    case "$location" in https://*) ;; *) echo "refusing non-HTTPS redirect" >&2; return 1;; esac
    case "$location" in "$github_base"/*) location="$download_base${location#"$github_base"}";; esac
    if test -n "$assets_mirror"; then
      case "$location" in "$assets_base"/*) location="$assets_mirror${location#"$assets_base"}";; esac
    fi
    url=$location
  done
  echo "too many redirects" >&2
  return 1
}
fetch "$release_url" "$work/latest.json"
test "$(jq -er '.component' "$work/latest.json")" = "$component"
version=$(jq -er '.version' "$work/latest.json")
url=$(jq -er '.packageUrl' "$work/latest.json")
sha=$(jq -er '.sha256' "$work/latest.json")
architecture=$(jq -er '.architecture' "$work/latest.json")
published=$(jq -er '.publishedAtUtc' "$work/latest.json")
git_sha=$(jq -er '.gitSha' "$work/latest.json")
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]] || { echo "invalid SemVer version" >&2; exit 1; }
test "$architecture" = amd64 || { echo "invalid release architecture" >&2; exit 1; }
test -n "$git_sha" || { echo "missing git SHA" >&2; exit 1; }
case "$url" in "$package_prefix${version}/yaginx-core_${version}_amd64.deb") ;; *) echo "invalid immutable package URL" >&2; exit 1;; esac
[[ "$sha" =~ ^[[:xdigit:]]{64}$ ]] || { echo "invalid SHA-256" >&2; exit 1; }
published_epoch=$(date -u -d "$published" +%s 2>/dev/null) || { echo "invalid publishedAtUtc" >&2; exit 1; }
test "$published_epoch" -le "$(( $(date -u +%s) + 300 ))" || { echo "release publishedAtUtc is in the future" >&2; exit 1; }
fetch "$download_base${url#"$github_base"}" "$work/yaginx-core.deb"
printf '%s  %s\n' "$sha" "$work/yaginx-core.deb" | sha256sum --check --status
mkdir -p /run/yaginx
flock -n /run/yaginx/upgrade.lock dpkg --force-confdef --force-confold -i "$work/yaginx-core.deb"
