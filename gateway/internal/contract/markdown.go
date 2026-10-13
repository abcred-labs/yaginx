package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var markdownLink = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
var mermaidBlock = regexp.MustCompile("(?s)```mermaid[\\t ]*\\r?\\n(.*?)```")

// ValidateMarkdownDocuments checks local links and makes sure every Mermaid
// fence is structurally a diagram. The pinned Mermaid parser is run separately
// by tools/contract-lint/check-mermaid.mjs in CI.
func ValidateMarkdownDocuments(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := validateMarkdownLinks(path, string(contents)); err != nil {
			return err
		}
		if err := validateMermaidStructure(path, string(contents)); err != nil {
			return err
		}
		return nil
	})
}

func validateMarkdownLinks(path, contents string) error {
	for _, match := range markdownLink.FindAllStringSubmatch(contents, -1) {
		target := strings.Trim(strings.TrimSpace(match[1]), "<>")
		if target == "" || strings.HasPrefix(target, "#") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		file := strings.SplitN(target, "#", 2)[0]
		if file == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(file))); err != nil {
			return fmt.Errorf("%s: broken Markdown link %q", path, target)
		}
	}
	return nil
}

func validateMermaidStructure(path, contents string) error {
	for _, match := range mermaidBlock.FindAllStringSubmatch(contents, -1) {
		diagram := strings.TrimSpace(match[1])
		if diagram == "" {
			return fmt.Errorf("%s: empty Mermaid block", path)
		}
		first := strings.Fields(diagram)
		if len(first) == 0 {
			return fmt.Errorf("%s: invalid Mermaid block", path)
		}
		switch first[0] {
		case "flowchart", "graph", "sequenceDiagram", "stateDiagram", "stateDiagram-v2", "classDiagram", "erDiagram", "journey", "gantt", "pie", "mindmap", "timeline", "gitGraph", "quadrantChart", "xychart-beta", "sankey-beta", "block-beta":
		default:
			return fmt.Errorf("%s: unsupported Mermaid diagram type %q", path, first[0])
		}
	}
	return nil
}
