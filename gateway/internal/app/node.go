package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"

	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/platform/identity"
	"yaginx-gateway/internal/repository"
)

const nodeUsage = `Usage:
  yaginx node
`

type nodeApprover interface {
	ApproveNode(context.Context, string) (management.NodeSummary, error)
}

// runNode contains host-local node administration. It deliberately has no
// node-ID flag: the command can only approve the identity stored on this host.
func runNode(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fmt.Fprint(stderr, nodeUsage)
	return 2
}

func runNodeApproveCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("node approve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := configPath(fs)
	yes := fs.Bool("yes", false, "approve this node without an interactive prompt")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || !*yes {
		fmt.Fprint(stderr, nodeUsage)
		return 2
	}

	cfg, err := config.LoadRuntime(*path)
	if err != nil {
		fmt.Fprintf(stderr, "load configuration: %v\n", err)
		return 1
	}
	node, err := identity.Load(cfg.String("node", "identityFile"))
	if err != nil {
		fmt.Fprintf(stderr, "load node identity: %v\n", err)
		return 1
	}
	poolOptions, err := runtimePoolOptions(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "database configuration: %v\n", err)
		return 1
	}
	pool, err := database.NewRuntimePool(ctx, cfg.DatabaseDSN(), poolOptions)
	if err != nil {
		fmt.Fprintf(stderr, "database pool: %v\n", err)
		return 1
	}
	defer pool.Close()
	return approveLocalNode(ctx, node.NodeID, repository.New(pool), stdout, stderr)
}

func approveLocalNode(ctx context.Context, nodeID string, store nodeApprover, stdout, stderr io.Writer) int {
	result, err := store.ApproveNode(ctx, nodeID)
	if errors.Is(err, management.ErrNotFound) {
		fmt.Fprintf(stderr, "local node %s is not registered; start yaginx serve first\n", nodeID)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "approve local node: %v\n", err)
		return 1
	}
	number := "unassigned"
	if result.NodeNumber != nil {
		number = strconv.FormatInt(int64(*result.NodeNumber), 10)
	}
	fmt.Fprintf(stdout, "Node approved: ID=%s Number=%s Status=%s\n", result.NodeID, number, result.Status)
	return 0
}
