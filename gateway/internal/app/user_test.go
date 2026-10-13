package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"yaginx-gateway/internal/management"
)

type fakeUserStore struct {
	accounts []management.Account
	resetID  int64
	password string
	resetErr error
}

func (s *fakeUserStore) ListAccounts(context.Context) ([]management.Account, error) {
	return s.accounts, nil
}

func (s *fakeUserStore) GetAccountByID(_ context.Context, id int64) (management.Account, error) {
	for _, account := range s.accounts {
		if account.ID == id {
			return account, nil
		}
	}
	return management.Account{}, management.ErrNotFound
}

func (s *fakeUserStore) ResetAccountPassword(_ context.Context, id int64, password string) error {
	s.resetID, s.password = id, password
	return s.resetErr
}

type fakePasswordReader struct {
	values []string
	err    error
}

func (r *fakePasswordReader) ReadPassword(string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if len(r.values) == 0 {
		return "", errors.New("unexpected password prompt")
	}
	value := r.values[0]
	r.values = r.values[1:]
	return value, nil
}

func TestUsageIncludesUserCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "user") {
		t.Fatalf("help omitted user command: %s", stdout.String())
	}
}

func TestRunUserListPrintsOnlyIdentities(t *testing.T) {
	store := &fakeUserStore{accounts: []management.Account{{ID: 3, Email: "third@example.invalid"}, {ID: 7, Email: "seventh@example.invalid"}}}
	var stdout, stderr bytes.Buffer
	if code := runUserList(context.Background(), &stdout, &stderr, store); code != 0 {
		t.Fatalf("list exit code=%d stderr=%s", code, stderr.String())
	}
	if got, want := stdout.String(), "ID\tEmail\n3\tthird@example.invalid\n7\tseventh@example.invalid\n"; got != want {
		t.Fatalf("list output=%q want=%q", got, want)
	}
}

func TestRunUserPasswordResetUsesSelectedAccountAndKeepsPasswordHidden(t *testing.T) {
	store := &fakeUserStore{accounts: []management.Account{{ID: 7, Email: "reset@example.invalid"}}}
	passwords := &fakePasswordReader{values: []string{"new-secret", "new-secret"}}
	var stdout, stderr bytes.Buffer
	if code := runUserPasswordReset(context.Background(), 7, &stdout, &stderr, store, passwords); code != 0 {
		t.Fatalf("reset exit code=%d stderr=%s", code, stderr.String())
	}
	if store.resetID != 7 || store.password != "new-secret" {
		t.Fatalf("reset id=%d password=%q", store.resetID, store.password)
	}
	if !strings.Contains(stdout.String(), "ID=7 Email=reset@example.invalid") || strings.Contains(stdout.String(), "new-secret") || strings.Contains(stderr.String(), "new-secret") {
		t.Fatalf("sensitive or missing output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunUserPasswordResetRejectsMismatchedPassword(t *testing.T) {
	store := &fakeUserStore{accounts: []management.Account{{ID: 7, Email: "reset@example.invalid"}}}
	var stdout, stderr bytes.Buffer
	if code := runUserPasswordReset(context.Background(), 7, &stdout, &stderr, store, &fakePasswordReader{values: []string{"first", "second"}}); code != 1 {
		t.Fatalf("reset exit code=%d stderr=%s", code, stderr.String())
	}
	if store.resetID != 0 || !strings.Contains(stderr.String(), "does not match") {
		t.Fatalf("reset id=%d stderr=%s", store.resetID, stderr.String())
	}
}

func TestRunUserPasswordResetRejectsMissingAccount(t *testing.T) {
	store := &fakeUserStore{}
	var stdout, stderr bytes.Buffer
	if code := runUserPasswordReset(context.Background(), 9, &stdout, &stderr, store, &fakePasswordReader{}); code != 1 {
		t.Fatalf("reset exit code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "account 9 not found") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestRunUserRejectsInvalidSubcommandAndPasswordIDBeforeLoadingConfiguration(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"password"}, {"password", "--id", "0"}} {
		var stdout, stderr bytes.Buffer
		if code := runUser(context.Background(), args, &stdout, &stderr); code != 2 {
			t.Fatalf("args=%v exit code=%d stderr=%s", args, code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "Usage:") {
			t.Fatalf("args=%v missing usage stderr=%s", args, stderr.String())
		}
	}
}

func TestTerminalPasswordReaderRejectsNonTerminal(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "password-input")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var output bytes.Buffer
	if _, err := (terminalPasswordReader{input: file, output: &output}).ReadPassword("New password: "); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("non-terminal error=%v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("non-terminal wrote prompt: %q", output.String())
	}
}
