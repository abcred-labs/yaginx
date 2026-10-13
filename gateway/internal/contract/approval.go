package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ApprovedDeviation is an explicitly reviewed R0 exception. It is deliberately
// narrower than a blanket ignore: every exception identifies one coverage
// requirement and an evidence fixture that later Go acceptance tests replay.
type ApprovedDeviation struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	OperationID       string `json:"operationId,omitempty"`
	Requirement       string `json:"requirement,omitempty"`
	MatrixLine        int    `json:"matrixLine,omitempty"`
	Reason            string `json:"reason"`
	DotNetObservation string `json:"dotnetObservation"`
	TargetGoBehavior  string `json:"targetGoBehavior"`
	Owner             string `json:"owner"`
	Evidence          string `json:"evidence"`
}

type ApprovedDeviationManifest struct {
	Version    int                 `json:"version"`
	Approvals  []ApprovedDeviation `json:"approvals"`
}

func LoadApprovedDeviations(path, workspaceRoot string) ([]ApprovedDeviation, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest ApprovedDeviationManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return nil, fmt.Errorf("parse approved deviations: %w", err)
	}
	if manifest.Version != 1 {
		return nil, fmt.Errorf("approved deviation manifest version = %d, want 1", manifest.Version)
	}
	seen := make(map[string]struct{}, len(manifest.Approvals))
	for _, approval := range manifest.Approvals {
		if err := approval.Validate(workspaceRoot); err != nil {
			return nil, err
		}
		if _, ok := seen[approval.ID]; ok {
			return nil, fmt.Errorf("duplicate approved deviation %q", approval.ID)
		}
		seen[approval.ID] = struct{}{}
	}
	return manifest.Approvals, nil
}

func (approval ApprovedDeviation) Validate(workspaceRoot string) error {
	for field, value := range map[string]string{
		"id": approval.ID, "reason": approval.Reason, "dotnetObservation": approval.DotNetObservation,
		"targetGoBehavior": approval.TargetGoBehavior, "owner": approval.Owner, "evidence": approval.Evidence,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("approved deviation %q %s is required", approval.ID, field)
		}
	}
	switch approval.Kind {
	case "api":
		if approval.OperationID == "" || (approval.Requirement != "success" && approval.Requirement != "failure") || approval.MatrixLine != 0 {
			return fmt.Errorf("approved API deviation %q must identify operationId and success/failure requirement only", approval.ID)
		}
	case "proxy":
		if approval.MatrixLine < 1 || approval.OperationID != "" || approval.Requirement != "" {
			return fmt.Errorf("approved proxy deviation %q must identify matrixLine only", approval.ID)
		}
	default:
		return fmt.Errorf("approved deviation %q has unsupported kind %q", approval.ID, approval.Kind)
	}
	evidencePath := filepath.Clean(filepath.Join(workspaceRoot, filepath.FromSlash(approval.Evidence)))
	root := filepath.Clean(workspaceRoot) + string(os.PathSeparator)
	if !strings.HasPrefix(evidencePath, root) {
		return fmt.Errorf("approved deviation %q evidence escapes workspace", approval.ID)
	}
	if info, err := os.Stat(evidencePath); err != nil || info.IsDir() {
		return fmt.Errorf("approved deviation %q evidence %q is unavailable", approval.ID, approval.Evidence)
	}
	return nil
}

func approvalIndex(approvals []ApprovedDeviation, kind string) map[string]ApprovedDeviation {
	result := make(map[string]ApprovedDeviation, len(approvals))
	for _, approval := range approvals {
		if approval.Kind == kind {
			result[approval.ID] = approval
		}
	}
	return result
}

func sortedUnusedApprovals(approvals []ApprovedDeviation, used map[string]struct{}) []string {
	unused := make([]string, 0)
	for _, approval := range approvals {
		if _, ok := used[approval.ID]; !ok {
			unused = append(unused, approval.ID)
		}
	}
	sort.Strings(unused)
	return unused
}
