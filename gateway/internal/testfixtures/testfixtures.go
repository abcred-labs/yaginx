// Package testfixtures holds the redacted management API and proxy fixtures
// that the Go router and proxy runtime replay in their tests. It is imported
// only from _test.go files, so the embedded data never reaches the binary.
package testfixtures

import "embed"

//go:embed data/management/*.json data/proxy/*.json
var data embed.FS

// Management returns every validated management API fixture.
func Management() ([]Fixture, error) {
	return loadManagement(data, "data/management")
}

// Proxy returns every validated proxy fixture.
func Proxy() ([]ProxyFixture, error) {
	return loadProxy(data, "data/proxy")
}
