#!/usr/bin/env python3
"""Exercise real dpkg/systemd in disposable Docker containers (Linux only).

Usage: python3 deploy/tests/upgrade-removal-smoke.py <new.deb> <systemd-image>
The image must contain systemd, dpkg and a shell. Its entrypoint must start
systemd. No host systemd socket or cgroup filesystem is mounted.
"""

import contextlib
import pathlib
import re
import subprocess
import sys
import tempfile
import time
import uuid


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout.strip()


def shell(container, script, timeout=45):
    try:
        return run("docker", "exec", "-i", container, "sh", "-se", input=script, timeout=timeout)
    except subprocess.CalledProcessError as error:
        raise RuntimeError(f"{script}\n{error.stdout}\n{error.stderr}") from error


@contextlib.contextmanager
def container(image, systemd=True):
    name = "yaginx-upgrade-removal-" + uuid.uuid4().hex[:10]
    args = ["docker", "run", "-d", "--name", name]
    if systemd:
        args += ["--privileged", "--cgroupns=private", "--tmpfs", "/run", "--tmpfs", "/run/lock", "--tmpfs", "/tmp", image]
    else:
        args += ["--entrypoint", "sleep", image, "infinity"]
    run(*args)
    try:
        if systemd:
            for _ in range(100):
                result = subprocess.run(["docker", "exec", name, "systemctl", "show", "--property=Version", "--value"], capture_output=True)
                if result.returncode == 0:
                    break
                time.sleep(0.1)
            else:
                raise RuntimeError("systemd did not start")
        yield name
    finally:
        run("docker", "rm", "-fv", name)


TIMER = """[Unit]
Description=Legacy Yaginx upgrade timer fixture
[Timer]
OnActiveSec=24h
OnUnitActiveSec=6h
[Install]
WantedBy=timers.target
"""
WORKER = """[Unit]
Description=Legacy upgrade worker fixture
[Service]
Type=oneshot
ExecStart=/bin/sleep infinity
"""


def make_old_package(new_package, directory):
    stage = directory / "old"
    run("dpkg-deb", "-R", str(new_package), str(stage))
    control = stage / "DEBIAN/control"
    control.write_text(re.sub(r"(?m)^Version:.*$", "Version: 0~legacy-auto-upgrade", control.read_text()))
    # Model a prior installed package without starting the main gateway.
    for hook in ("preinst", "postinst"):
        (stage / "DEBIAN" / hook).unlink(missing_ok=True)
    units = stage / "lib/systemd/system"
    (units / "yaginx-upgrade.timer").write_text(TIMER)
    (units / "yaginx-upgrade.service").write_text(WORKER)
    old = directory / "old.deb"
    run("dpkg-deb", "--build", "--root-owner-group", str(stage), str(old))
    return old


def copy_packages(name, package, old):
    run("docker", "cp", str(package), name + ":/new.deb")
    run("docker", "cp", str(old), name + ":/old.deb")


def assert_retired(name):
    shell(name, """
test ! -e /lib/systemd/system/yaginx-upgrade.timer
test ! -e /lib/systemd/system/yaginx-upgrade.service
test ! -L /etc/systemd/system/timers.target.wants/yaginx-upgrade.timer
! systemctl is-active --quiet yaginx-upgrade.timer
! systemctl is-enabled --quiet yaginx-upgrade.timer
""")


def main(package, image):
    package = pathlib.Path(package).resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix="yaginx-upgrade-smoke-") as temporary:
        old = make_old_package(package, pathlib.Path(temporary))
        with container(image) as name:
            copy_packages(name, package, old)
            shell(name, "dpkg -i /new.deb\nsystemctl is-enabled --quiet yaginx\nsystemctl is-active --quiet yaginx")
            pid = shell(name, "systemctl show --property=MainPID --value yaginx")
            shell(name, "dpkg -i /new.deb")
            assert shell(name, "systemctl show --property=MainPID --value yaginx") == pid
            shell(name, """
before=$(sha256sum /etc/yaginx/config.json)
for mode in status on off; do
    if yaginx upgrade auto "$mode" --config /etc/yaginx/config.json > /run/cli.out 2>&1; then exit 1; else test "$?" -eq 2; fi
done
if yaginx upgrade --auto-worker --config /etc/yaginx/config.json > /run/cli.out 2>&1; then exit 1; else test "$?" -eq 2; fi
test ! -e /var/log/yaginx/upgrade.log
if yaginx upgrade --package-only --config /missing.json --version invalid --state-directory /run/package-repair --lock-file /run/package-repair.lock > /run/repair.out 2>&1; then exit 1; else test "$?" -eq 1; fi
! grep -E 'panic:|load upgrade configuration' /run/repair.out
test "$before" = "$(sha256sum /etc/yaginx/config.json)"
""")
            assert_retired(name)
            print("PASS fresh/repeat install; main service preserved; retired CLI exits 2; package-only CLI returns ordinary error without config", flush=True)

        with container(image) as name:
            copy_packages(name, package, old)
            shell(name, """
dpkg -i /old.deb
systemctl daemon-reload
systemctl enable --now yaginx-upgrade.timer
systemctl start --no-block yaginx-upgrade.service
mkdir -p /var/log/yaginx /var/lib/yaginx/upgrade
echo preserve > /var/log/yaginx/sentinel
echo preserve > /var/lib/yaginx/upgrade/rollback.deb
""")
            pid = shell(name, "systemctl show --property=MainPID --value yaginx-upgrade.service")
            assert pid != "0", "worker did not start"
            before = shell(name, "sha256sum /etc/yaginx/config.json")
            shell(name, "dpkg -i /new.deb\ndpkg -i /new.deb")
            assert_retired(name)
            assert shell(name, "systemctl show --property=MainPID --value yaginx-upgrade.service") == pid
            assert shell(name, "sha256sum /etc/yaginx/config.json") == before
            shell(name, "! systemctl is-active --quiet yaginx\ntest -f /var/log/yaginx/sentinel\ntest -f /var/lib/yaginx/upgrade/rollback.deb")
            print("PASS enabled legacy timer removed; running worker, inactive main service, config, logs and rollback preserved", flush=True)

        with container(image) as name:
            copy_packages(name, package, old)
            shell(name, """
dpkg -i /old.deb
systemctl daemon-reload
systemctl enable --now yaginx-upgrade.timer
rm /lib/systemd/system/yaginx-upgrade.timer /lib/systemd/system/yaginx-upgrade.service
systemctl daemon-reload
dpkg -i /new.deb
dpkg -i /new.deb
""")
            assert_retired(name)
            print("PASS missing units and dangling project enablement link", flush=True)

        with container(image) as name:
            copy_packages(name, package, old)
            shell(name, """
dpkg -i /old.deb
cat > /lib/systemd/system/yaginx-upgrade.service <<'EOF'
[Service]
Type=simple
ExecStart=/bin/sh -c 'dpkg -i /new.deb && touch /run/worker-install-complete; exec sleep infinity'
EOF
systemctl daemon-reload
systemctl enable --now yaginx-upgrade.timer
systemctl start yaginx-upgrade.service
""")
            for _ in range(100):
                if shell(name, "if test -f /run/worker-install-complete; then echo done; fi") == "done":
                    break
                time.sleep(0.1)
            else:
                raise RuntimeError("worker installation failed or deadlocked")
            shell(name, "systemctl is-active --quiet yaginx-upgrade.service")
            assert_retired(name)
            print("PASS dpkg invoked from upgrade worker completes; worker not stopped or awaited", flush=True)

        with container(image) as name:
            copy_packages(name, package, old)
            shell(name, "dpkg -i /old.deb\ncp /lib/systemd/system/yaginx-upgrade.timer /etc/systemd/system/yaginx-upgrade.timer\nsystemctl daemon-reload\nsystemctl enable --now yaginx-upgrade.timer\ndpkg -i /new.deb\ntest -f /etc/systemd/system/yaginx-upgrade.timer\nsystemctl is-active --quiet yaginx-upgrade.timer\nsystemctl is-enabled --quiet yaginx-upgrade.timer")
            print("PASS administrator-supplied same-name unit preserved", flush=True)

        with container(image) as name:
            copy_packages(name, package, old)
            shell(name, r"""
dpkg -i /old.deb
sed -i '/\[Unit\]/a RefuseManualStop=yes' /lib/systemd/system/yaginx-upgrade.timer
systemctl daemon-reload
systemctl enable --now yaginx-upgrade.timer
if dpkg -i /new.deb > /run/failed-install.log 2>&1; then exit 1; fi
grep -F 'Unable to stop legacy yaginx-upgrade.timer' /run/failed-install.log
test -f /lib/systemd/system/yaginx-upgrade.timer
""")
            print("PASS stop failure is explicit and prevents unpack", flush=True)

        with container(image) as name:
            copy_packages(name, package, old)
            shell(name, """
dpkg -i /old.deb
systemctl daemon-reload
systemctl enable --now yaginx-upgrade.timer
dpkg-deb -e /new.deb /hooks
mkdir /test-bin
cat > /test-bin/systemctl <<'EOF'
#!/bin/sh
if [ "$1" = disable ]; then exit 1; fi
exec /usr/bin/systemctl "$@"
EOF
chmod 0755 /test-bin/systemctl
if PATH=/test-bin:/usr/bin:/bin /hooks/preinst upgrade > /run/failed-disable.log 2>&1; then exit 1; fi
grep -F 'Unable to disable legacy yaginx-upgrade.timer' /run/failed-disable.log
/usr/bin/env PATH=/unavailable /hooks/preinst install
mkdir /test-bin-show
cat > /test-bin-show/systemctl <<'EOF'
#!/bin/sh
if [ "$1" = show ]; then exit 1; fi
exec /usr/bin/systemctl "$@"
EOF
chmod 0755 /test-bin-show/systemctl
PATH=/test-bin-show:/usr/bin:/bin /hooks/preinst upgrade > /run/uninspectable.log 2>&1
grep -F 'skipping its cleanup' /run/uninspectable.log
""")
            print("PASS disable failure is explicit (injected systemctl failure); missing systemctl and uninspectable systemd are tolerated", flush=True)

        with container(image, systemd=False) as name:
            copy_packages(name, package, old)
            shell(name, "dpkg -i /new.deb\ndpkg -i /new.deb")
            print("PASS install and repeat install without systemd", flush=True)


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)
    main(*sys.argv[1:])
