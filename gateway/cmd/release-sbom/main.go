// Command release-sbom emits a compact SPDX document from the Go build info
// embedded in the exact binary that will be published.
package main

import (
	"debug/buildinfo"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"
)

type spdxDocument struct {
	SPDXVersion       string        `json:"spdxVersion"`
	DataLicense       string        `json:"dataLicense"`
	SPDXID            string        `json:"SPDXID"`
	Name              string        `json:"name"`
	DocumentNamespace string        `json:"documentNamespace"`
	CreationInfo      creationInfo  `json:"creationInfo"`
	Packages          []spdxPackage `json:"packages"`
}
type creationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}
type spdxPackage struct {
	SPDXID           string `json:"SPDXID"`
	Name             string `json:"name"`
	VersionInfo      string `json:"versionInfo"`
	DownloadLocation string `json:"downloadLocation"`
	FilesAnalyzed    bool   `json:"filesAnalyzed"`
}

func main() {
	binary := flag.String("binary", "", "published Go binary")
	output := flag.String("output", "", "SPDX JSON output path")
	version := flag.String("version", "", "release version")
	name := flag.String("name", "yaginx", "release component name")
	flag.Parse()
	if *binary == "" || *output == "" || *version == "" {
		fmt.Fprintln(os.Stderr, "usage: release-sbom --binary <file> --output <file> --version <version>")
		os.Exit(2)
	}
	info, err := buildinfo.ReadFile(*binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read build info: %v\n", err)
		os.Exit(1)
	}
	packages := []spdxPackage{packageRecord(info.Main.Path, info.Main.Version, 0)}
	for _, dependency := range info.Deps {
		packages = append(packages, packageRecord(dependency.Path, dependency.Version, len(packages)))
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	document := spdxDocument{SPDXVersion: "SPDX-2.3", DataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT", Name: *name, DocumentNamespace: "https://yaginx.invalid/sbom/" + *name + "/" + *version, CreationInfo: creationInfo{Created: time.Now().UTC().Format(time.RFC3339), Creators: []string{"Tool: yaginx release-sbom"}}, Packages: packages}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode SPDX: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, append(encoded, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write SPDX: %v\n", err)
		os.Exit(1)
	}
}

func packageRecord(name, version string, index int) spdxPackage {
	if version == "" || version == "(devel)" {
		version = "NOASSERTION"
	}
	return spdxPackage{SPDXID: fmt.Sprintf("SPDXRef-Package-%d", index), Name: name, VersionInfo: version, DownloadLocation: "NOASSERTION", FilesAnalyzed: false}
}
