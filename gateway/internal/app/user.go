package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/repository"
)

const userUsage = `Usage:
  yaginx user [--config <path>]
  yaginx user password --id <account-id> [--config <path>]
`

type userStore interface {
	ListAccounts(context.Context) ([]management.Account, error)
	GetAccountByID(context.Context, int64) (management.Account, error)
	ResetAccountPassword(context.Context, int64, string) error
}

type passwordReader interface {
	ReadPassword(prompt string) (string, error)
}

type terminalPasswordReader struct {
	input  *os.File
	output io.Writer
}

func (r terminalPasswordReader) ReadPassword(prompt string) (string, error) {
	if r.input == nil || !term.IsTerminal(int(r.input.Fd())) {
		return "", fmt.Errorf("password input requires an interactive terminal")
	}
	fmt.Fprint(r.output, prompt)
	value, err := term.ReadPassword(int(r.input.Fd()))
	fmt.Fprintln(r.output)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(value), nil
}

func runUser(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "password" {
		return runUserPassword(ctx, args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := configPath(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, userUsage)
		return 2
	}
	return withUserStore(ctx, *path, stderr, func(store userStore) int {
		return runUserList(ctx, stdout, stderr, store)
	})
}

func runUserPassword(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("user password", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := configPath(fs)
	id := fs.Int64("id", 0, "account ID to reset")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *id < 1 {
		fmt.Fprint(stderr, userUsage)
		return 2
	}
	return withUserStore(ctx, *path, stderr, func(store userStore) int {
		return runUserPasswordReset(ctx, *id, stdout, stderr, store, terminalPasswordReader{input: os.Stdin, output: stderr})
	})
}

func withUserStore(ctx context.Context, path string, stderr io.Writer, operation func(userStore) int) int {
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		fmt.Fprintf(stderr, "load configuration: %v\n", err)
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
	return operation(repository.New(pool))
}

func runUserList(ctx context.Context, stdout, stderr io.Writer, store userStore) int {
	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "list accounts: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "ID\tEmail")
	for _, account := range accounts {
		fmt.Fprintf(stdout, "%d\t%s\n", account.ID, account.Email)
	}
	return 0
}

func runUserPasswordReset(ctx context.Context, id int64, stdout, stderr io.Writer, store userStore, passwords passwordReader) int {
	account, err := store.GetAccountByID(ctx, id)
	if errors.Is(err, management.ErrNotFound) {
		fmt.Fprintf(stderr, "account %d not found\n", id)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "get account: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Resetting account: ID=%d Email=%s\n", account.ID, account.Email)
	password, err := passwords.ReadPassword("New password: ")
	if err != nil {
		fmt.Fprintf(stderr, "password input: %v\n", err)
		return 1
	}
	if password == "" {
		fmt.Fprintln(stderr, "password cannot be empty")
		return 1
	}
	confirmation, err := passwords.ReadPassword("Confirm new password: ")
	if err != nil {
		fmt.Fprintf(stderr, "password input: %v\n", err)
		return 1
	}
	if password != confirmation {
		fmt.Fprintln(stderr, "password confirmation does not match")
		return 1
	}
	if err := store.ResetAccountPassword(ctx, account.ID, password); err != nil {
		if errors.Is(err, management.ErrNotFound) {
			fmt.Fprintf(stderr, "account %d not found\n", account.ID)
		} else {
			fmt.Fprintf(stderr, "reset password: %v\n", err)
		}
		return 1
	}
	fmt.Fprintf(stdout, "Password reset: ID=%d Email=%s\n", account.ID, account.Email)
	return 0
}
