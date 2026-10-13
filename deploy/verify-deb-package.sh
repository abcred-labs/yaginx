#!/usr/bin/env sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <deb-file> <version>" >&2
  exit 2
fi

package=$1
version=$2
test -f "$package"
test "$(dpkg-deb -f "$package" Package)" = yaginx
test "$(dpkg-deb -f "$package" Version)" = "$version"
contents=$(dpkg-deb -c "$package")
for path in \
  usr/bin/yaginx \
  usr/share/yaginx/version.txt \
  usr/share/yaginx/adminui/index.html \
  etc/yaginx/config.json \
  lib/systemd/system/yaginx.service \
  lib/systemd/system/yaginx-upgrade.service \
  lib/systemd/system/yaginx-upgrade.timer; do
  printf '%s\n' "$contents" | grep -F "$path" >/dev/null
done
printf '%s\n' "$contents" | awk '$1 == "-rw-------" && $NF == "./etc/yaginx/config.json" { found=1 } END { exit !found }'
dpkg-deb --ctrl-tarfile "$package" | tar -xOf - ./conffiles | grep -Fx '/etc/yaginx/config.json' >/dev/null
