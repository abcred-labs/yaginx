package repocheck

import (
	"os"
	"strings"
	"testing"
)

func TestGatewayDockerfilePackagesPrebuiltBinaryWithoutRemoteFrontend(t *testing.T) {
	path, err := findWorkspaceFile(".", "gateway/deploy/docker/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"alpine:3.24.2", "ARG BINARY_PATH", "COPY ${BINARY_PATH} /usr/bin/yaginx", "ca-certificates", "tzdata", "/var/lib/yaginx", "adminui/dist/", "EXPOSE 8443/udp", "adduser", "USER yaginx:yaginx"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Dockerfile missing %q", want)
		}
	}
	for _, forbidden := range []string{"# syntax=", "docker/dockerfile", "FROM golang:", "hub.", "mount=type=cache", "gcr.io/", "mcr.microsoft.com/"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Dockerfile contains forbidden base image %q", forbidden)
		}
	}
}
