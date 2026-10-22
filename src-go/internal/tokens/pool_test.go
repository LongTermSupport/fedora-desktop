package tokens

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func pool(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("placeholder-not-a-real-token\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReadPool(t *testing.T) {
	dir := pool(t, "b.2030-01-01.token", "a.2030-01-01.token", "notes.txt", "old.token")
	if err := os.Mkdir(filepath.Join(dir, "dir.2030-01-01.token"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "a.2030-01-01.token"), filepath.Join(dir, "link.2030-01-01.token")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling.2030-01-01.token")); err != nil {
		t.Fatal(err)
	}
	p, err := ReadPool(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.2030-01-01.token", "b.2030-01-01.token", "link.2030-01-01.token", "old.token"}
	if !p.Exists || !slices.Equal(p.Files, want) {
		t.Fatalf("ReadPool = %+v, want the regular *.token files %q", p, want)
	}
	missing, err := ReadPool(filepath.Join(dir, "does-not-exist"))
	if err != nil || missing.Exists || len(missing.Files) != 0 {
		t.Fatalf("a missing pool = %+v, %v", missing, err)
	}
	if _, err := ReadPool(filepath.Join(dir, "a.2030-01-01.token")); err == nil {
		t.Fatal("a pool that is a file was read as a pool")
	}
}

func TestListNamesEachFile(t *testing.T) {
	p := Pool{Dir: "/home/u/.claude-tokens/ccy/tokens", Exists: true, Files: []string{
		"fresh.2027-10-21.token", "stale.2026-10-20.token", "today.2026-10-21.token", "old.token",
	}}
	var out bytes.Buffer
	if err := List(&out, p, "ccy", today, false); err != nil {
		t.Fatal(err)
	}
	rule := strings.Repeat("═", 80)
	want := "\n" + rule + "\nAvailable Claude Code Tokens for ccy\n" + rule + "\n\n" +
		"Token storage: /home/u/.claude-tokens/ccy/tokens\n\n" +
		"  • fresh\n    File: /home/u/.claude-tokens/ccy/tokens/fresh.2027-10-21.token\n    Expires: 2027-10-21 (✓ Valid)\n\n" +
		"  • stale\n    File: /home/u/.claude-tokens/ccy/tokens/stale.2026-10-20.token\n    Expires: 2026-10-20 (✗ EXPIRED)\n\n" +
		"  • today\n    File: /home/u/.claude-tokens/ccy/tokens/today.2026-10-21.token\n    Expires: 2026-10-21 (⚠ Expires TODAY)\n\n" +
		"  • old.token\n    File: /home/u/.claude-tokens/ccy/tokens/old.token\n    Status: ✗ INVALID FORMAT (missing expiry date)\n\n" +
		rule + "\n\n"
	if out.String() != want {
		t.Fatalf("List =\n%s\nwant\n%s", out.String(), want)
	}
}

func TestListColoursTheExpiry(t *testing.T) {
	var out bytes.Buffer
	p := Pool{Dir: "/d", Exists: true, Files: []string{"fresh.2027-10-21.token"}}
	if err := List(&out, p, "ccy", today, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Expires: \x1b[32m2027-10-21\x1b[0m (✓ Valid)") {
		t.Fatalf("List with colour = %q", out.String())
	}
}

func TestListOfNoTokens(t *testing.T) {
	for _, p := range []Pool{{Dir: "/d"}, {Dir: "/d", Exists: true}} {
		var out bytes.Buffer
		if err := List(&out, p, "ccy", today, false); !errors.Is(err, ErrNoTokens) {
			t.Fatalf("List of %+v = %v, want ErrNoTokens", p, err)
		}
		if !strings.Contains(out.String(), "No tokens found in: /d\n\nCreate a token with: ccy --create-token\n") {
			t.Fatalf("List of no tokens = %q", out.String())
		}
	}
}

func TestListReportsAFailedWrite(t *testing.T) {
	if err := List(failingWriter{}, Pool{Dir: "/d", Exists: true, Files: []string{"a.token"}}, "ccy", today, false); err == nil {
		t.Fatal("a listing nobody could read reported success")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// The pool cases of scripts/test-ccy-token-mode.bash.
func TestDecide(t *testing.T) {
	missing := Pool{Dir: "/d"}
	empty := Pool{Dir: "/d", Exists: true}
	expired := Pool{Dir: "/d", Exists: true, Files: []string{"stale.2026-10-20.token"}}
	usable := Pool{Dir: "/d", Exists: true, Files: []string{"stale.2026-10-20.token", "fresh.2027-10-21.token"}}
	cases := []struct {
		name string
		mode Mode
		pool Pool
		want Decision
	}{
		{"container: missing token dir", ModeContainer, missing, Decision{Outcome: OutcomeRefuse, Exit: term.ExitFailure}},
		{"container: empty pool", ModeContainer, empty, Decision{Outcome: OutcomeRefuse, Exit: ExitNoUsableToken}},
		{"container: expired tokens only", ModeContainer, expired, Decision{Outcome: OutcomeRefuse, Exit: ExitNoUsableToken, Expired: []string{"stale.2026-10-20.token"}}},
		{"host: missing token dir -> Desktop", ModeHost, missing, Decision{Outcome: OutcomeDesktop}},
		{"host: empty pool -> Desktop", ModeHost, empty, Decision{Outcome: OutcomeDesktop}},
		{"host: expired tokens only -> refuse", ModeHost, expired, Decision{Outcome: OutcomeRefuse, Exit: ExitNoUsableToken, Expired: []string{"stale.2026-10-20.token"}}},
		{"a usable token is offered", ModeContainer, usable, Decision{Outcome: OutcomeChoose, Valid: []string{"fresh.2027-10-21.token"}, Expired: []string{"stale.2026-10-20.token"}}},
		{"a usable token is offered on the host too", ModeHost, usable, Decision{Outcome: OutcomeChoose, Valid: []string{"fresh.2027-10-21.token"}, Expired: []string{"stale.2026-10-20.token"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Decide(c.mode, c.pool, today)
			if got.Outcome != c.want.Outcome || got.Exit != c.want.Exit ||
				!slices.Equal(got.Valid, c.want.Valid) || !slices.Equal(got.Expired, c.want.Expired) {
				t.Fatalf("Decide = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestParseMode(t *testing.T) {
	for s, want := range map[string]Mode{"container": ModeContainer, "host": ModeHost} {
		if got, err := ParseMode(s); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %v, %v", s, got, err)
		}
	}
	// The suite's usage-error case: an invalid mode is not a verdict on the pool.
	if _, err := ParseMode("nonsense"); err == nil {
		t.Fatal("an invalid mode was accepted")
	}
}

// The value cases of scripts/test-ccy-token-import-cleanup.bash.
func TestParseRemoveOthers(t *testing.T) {
	for v, want := range map[string]RemoveOthers{"": RemoveAsk, "0": RemoveNo, "1": RemoveYes} {
		if got, err := ParseRemoveOthers(v); err != nil || got != want {
			t.Errorf("ParseRemoveOthers(%q) = %v, %v", v, got, err)
		}
	}
	_, err := ParseRemoveOthers("maybe")
	if err == nil || err.Error() != "CCY_TOKEN_IMPORT_REMOVE_OTHERS must be 1 (remove), 0 (keep) or unset (ask). Nothing imported." {
		t.Fatalf("ParseRemoveOthers(maybe) = %v", err)
	}
	if ExitOf(err) != term.ExitUsage || ExitOf(nil) != term.ExitOK || ExitOf(errors.New("x")) != term.ExitFailure {
		t.Fatal("an unknown value refuses with exit 64")
	}
}
