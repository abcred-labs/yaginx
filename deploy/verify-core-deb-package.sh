#!/usr/bin/env sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <deb-file> <version>" >&2
  exit 2
fi

package=$1
version=$2
test -f "$package"
test "$(dpkg-deb -f "$package" Package)" = yaginx-core
test "$(dpkg-deb -f "$package" Version)" = "$version"
contents=$(dpkg-deb -c "$package")
for path in \
  usr/bin/yaginx-core \
  usr/share/yaginx-core/version.txt \
  lib/systemd/system/yaginx-core.service; do
  printf '%s\n' "$contents" | grep -F "$path" >/dev/null
done
printf '%s\n' "$contents" | grep -F './etc/yaginx/config.json' >/dev/null && {
  echo "core package must not include the main runtime configuration" >&2
  exit 1
}
dpkg-deb --ctrl-tarfile "$package" | tar -xOf - ./postinst | grep -F '/var/lib/yaginx/core' >/dev/null
