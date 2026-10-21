package app

import (
	"context"
	"fmt"
	"io"
)

const usage = `Usage: yaginx <command> [options]

Commands:
  serve                 Start management and four business listeners.
  database init         Initialize an empty PostgreSQL database with the v2 schema.
  database migrate      Upgrade an existing v2 schema.
  database status       Report uninitialized, current-v2, current-v2-with-pending-migrations, or unsupported.
  report backfill       Materialize daily reports for an inclusive UTC date range.
  report reset          Clear traffic statistics after Go-only writer confirmation.
  upgrade               Check, install, schedule, verify, or roll back releases.
  doctor                Validate configuration and optionally check PostgreSQL.
  user                  List accounts or reset an account password.
  version               Print build metadata.
`

// Run is the single command dispatcher used by the production binary and tests.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	case "version":
		return runVersion(stdout)
	case "serve":
		return runServe(ctx, args[1:], stdout, stderr)
	case "database":
		return runDatabase(ctx, args[1:], stdout, stderr)
	case "report":
		return runReport(ctx, args[1:], stdout, stderr)
	case "upgrade":
		return runUpgrade(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr)
	case "node":
		return runNode(ctx, args[1:], stdout, stderr)
	case "user":
		return runUser(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
