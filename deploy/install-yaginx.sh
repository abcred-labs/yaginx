#!/usr/bin/env bash
set -euo pipefail

component=yaginx
release_url=https://github.com/abcred-labs/yaginx/releases/latest/download/latest-release.json
package_prefix=https://github.com/abcred-labs/yaginx/releases/download/v
core_component=yaginx-core
core_release_url=https://github.com/abcred-labs/yaginx/releases/latest/download/latest-core-release.json
core_package_prefix=https://github.com/abcred-labs/yaginx/releases/download/v

test "$(id -u)" -eq 0 || { echo "run as root" >&2; exit 1; }
test -f /etc/debian_version || { echo "Debian is required" >&2; exit 1; }
test "$(dpkg --print-architecture)" = amd64 || { echo "Yaginx installer supports Debian amd64 only" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { apt-get update; DEBIAN_FRONTEND=noninteractive apt-get install -y curl jq; }
command -v jq >/dev/null 2>&1 || { apt-get update; DEBIAN_FRONTEND=noninteractive apt-get install -y jq; }
work=$(mktemp -d /tmp/yaginx-install.XXXXXX)
trap 'rm -rf "$work"' EXIT

install_core_if_missing() {
  dpkg-query -W -f='${db:Status-Status}' yaginx-core 2>/dev/null | grep -qx installed && return 0
  curl --fail --silent --show-error --max-redirs 0 --proto '=https' --tlsv1.2 -o "$work/core-latest.json" "$core_release_url"
  test "$(jq -er '.component' "$work/core-latest.json")" = "$core_component"
  local core_version core_url core_sha core_architecture core_published core_git_sha
  core_version=$(jq -er '.version' "$work/core-latest.json")
  core_url=$(jq -er '.packageUrl' "$work/core-latest.json")
  core_sha=$(jq -er '.sha256' "$work/core-latest.json")
  core_architecture=$(jq -er '.architecture' "$work/core-latest.json")
  core_published=$(jq -er '.publishedAtUtc' "$work/core-latest.json")
  core_git_sha=$(jq -er '.gitSha' "$work/core-latest.json")
  [[ "$core_version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]] || { echo "invalid core SemVer version" >&2; exit 1; }
  test "$core_architecture" = amd64 || { echo "invalid core release architecture" >&2; exit 1; }
  test -n "$core_git_sha" || { echo "missing core git SHA" >&2; exit 1; }
  case "$core_url" in "$core_package_prefix${core_version}/yaginx-core_${core_version}_amd64.deb") ;; *) echo "invalid immutable core package URL" >&2; exit 1;; esac
  [[ "$core_sha" =~ ^[[:xdigit:]]{64}$ ]] || { echo "invalid core SHA-256" >&2; exit 1; }
  local core_published_epoch
  core_published_epoch=$(date -u -d "$core_published" +%s 2>/dev/null) || { echo "invalid core publishedAtUtc" >&2; exit 1; }
  test "$core_published_epoch" -le "$(( $(date -u +%s) + 300 ))" || { echo "core release publishedAtUtc is in the future" >&2; exit 1; }
  curl --fail --silent --show-error --max-redirs 0 --proto '=https' --tlsv1.2 -o "$work/yaginx-core.deb" "$core_url"
  printf '%s  %s\n' "$core_sha" "$work/yaginx-core.deb" | sha256sum --check --status
  mkdir -p /run/yaginx
  flock -n /run/yaginx/upgrade.lock dpkg --force-confdef --force-confold -i "$work/yaginx-core.deb"
}

install_core_if_missing
curl --fail --silent --show-error --max-redirs 0 --proto '=https' --tlsv1.2 -o "$work/latest.json" "$release_url"
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
case "$url" in "$package_prefix${version}/yaginx_${version}_amd64.deb") ;; *) echo "invalid immutable package URL" >&2; exit 1;; esac
[[ "$sha" =~ ^[[:xdigit:]]{64}$ ]] || { echo "invalid SHA-256" >&2; exit 1; }
published_epoch=$(date -u -d "$published" +%s 2>/dev/null) || { echo "invalid publishedAtUtc" >&2; exit 1; }
test "$published_epoch" -le "$(( $(date -u +%s) + 300 ))" || { echo "release publishedAtUtc is in the future" >&2; exit 1; }
curl --fail --silent --show-error --max-redirs 0 --proto '=https' --tlsv1.2 -o "$work/yaginx.deb" "$url"
printf '%s  %s\n' "$sha" "$work/yaginx.deb" | sha256sum --check --status
mkdir -p /run/yaginx
flock -n /run/yaginx/upgrade.lock dpkg --force-confdef --force-confold -i "$work/yaginx.deb"
