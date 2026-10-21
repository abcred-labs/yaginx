package repocheck

import (
	"os"
	"strings"
	"testing"
)

func TestDebBuildScriptCreatesConfigurationDirectory(t *testing.T) {
	path, err := findWorkspaceFile(".", "deploy/build-deb.sh")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	if !strings.Contains(text, `"$pkg/etc/yaginx"`) {
		t.Fatal("Debian build script must create /etc/yaginx before installing config.json")
	}
	if !strings.Contains(text, `install -m 0600 "$root/gateway/config/yaginx.example.json" "$pkg/etc/yaginx/config.json"`) {
		t.Fatal("Debian build script must package config.json with mode 0600")
	}
}

func TestDebPostInstallSecuresConfigurationForRootService(t *testing.T) {
	path, err := findWorkspaceFile(".", "gateway/deploy/deb/postinst")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, want := range []string{
		"install -d -o root -g root -m 0700 /var/lib/yaginx /var/log/yaginx",
		"chown root:root /etc/yaginx/config.json",
		"chmod 0600 /etc/yaginx/config.json",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Debian post-install hook is missing %q", want)
		}
	}
	for _, forbidden := range []string{"groupadd --system yaginx", "useradd --system --gid yaginx", "root:yaginx"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Debian post-install hook must not contain %q", forbidden)
		}
	}
}

func TestDebBuildScriptInstallsPostRemoveHook(t *testing.T) {
	buildPath, err := findWorkspaceFile(".", "deploy/build-deb.sh")
	if err != nil {
		t.Fatal(err)
	}
	buildContents, err := os.ReadFile(buildPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buildContents), `install -m 0755 "$root/gateway/deploy/deb/postrm" "$pkg/DEBIAN/postrm"`) {
		t.Fatal("Debian build script must package the post-remove hook")
	}

	postrmPath, err := findWorkspaceFile(".", "gateway/deploy/deb/postrm")
	if err != nil {
		t.Fatal(err)
	}
	postrmContents, err := os.ReadFile(postrmPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(postrmContents)
	for _, want := range []string{
		"#!/bin/sh",
		"remove|purge)",
		"systemctl stop yaginx.service",
		"systemctl disable yaginx.service",
		"systemctl daemon-reload",
		"|| true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Debian post-remove hook is missing %q", want)
		}
	}
}

func TestDebVerificationChecksConfigurationMode(t *testing.T) {
	path, err := findWorkspaceFile(".", "deploy/verify-deb-package.sh")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `-rw-------`) {
		t.Fatal("Debian package verification must require mode 0600 for config.json")
	}
}

func TestSystemdUnitRunsGatewayAsRoot(t *testing.T) {
	path, err := findWorkspaceFile(".", "gateway/deploy/systemd/yaginx.service")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, want := range []string{"User=root", "Group=root"} {
		if !strings.Contains(text, want) {
			t.Fatalf("systemd unit is missing %q", want)
		}
	}
}
