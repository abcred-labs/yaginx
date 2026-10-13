package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"yaginx-gateway/internal/management"
)

type fakeNodeApprover struct {
	id     string
	result management.NodeSummary
	err    error
}

func (s *fakeNodeApprover) ApproveNode(_ context.Context, id string) (management.NodeSummary, error) {
	s.id = id
	return s.result, s.err
}

func TestUsageOmitsNodeApproval(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "node approve") {
		t.Fatalf("help omitted node approval: %s", stdout.String())
	}
}

func TestApproveLocalNodeUsesOnlyProvidedLocalIdentity(t *testing.T) {
	nodeNumber := int16(7)
	store := &fakeNodeApprover{result: management.NodeSummary{NodeID: "local-node", NodeNumber: &nodeNumber, Status: "active"}}
	var stdout, stderr bytes.Buffer
	if code := approveLocalNode(context.Background(), "local-node", store, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code=%d stderr=%s", code, stderr.String())
	}
	if store.id != "local-node" || !strings.Contains(stdout.String(), "Number=7") || !strings.Contains(stdout.String(), "Status=active") {
		t.Fatalf("id=%q stdout=%q", store.id, stdout.String())
	}
}

func TestApproveLocalNodeReportsUnregisteredIdentity(t *testing.T) {
	store := &fakeNodeApprover{err: management.ErrNotFound}
	var stdout, stderr bytes.Buffer
	if code := approveLocalNode(context.Background(), "missing-node", store, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not registered") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestApproveLocalNodeReportsDatabaseError(t *testing.T) {
	store := &fakeNodeApprover{err: errors.New("permission denied")}
	var stdout, stderr bytes.Buffer
	if code := approveLocalNode(context.Background(), "local-node", store, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "permission denied") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunNodeApproveRequiresExplicitConfirmation(t *testing.T) {
	for _, args := range [][]string{{}, {"approve"}, {"approve", "--yes", "unexpected"}} {
		var stdout, stderr bytes.Buffer
		if code := runNode(context.Background(), args, &stdout, &stderr); code != 2 {
			t.Fatalf("args=%v exit code=%d stderr=%s", args, code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "Usage:") {
			t.Fatalf("args=%v stderr=%s", args, stderr.String())
		}
	}
}
