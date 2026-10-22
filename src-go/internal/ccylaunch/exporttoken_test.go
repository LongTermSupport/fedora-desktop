package ccylaunch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// exportHead is the import script's start for a token, before the quoted body.
func exportHead(name, date, token string) string {
	return "# CCY Token: " + name + " | Expires: " + date + " | Generated: 2030-01-15 12:00:00\n" +
		"# Paste into terminal on target machine, or save in LastPass/1Password.\n" +
		"# Other files of this name: asks before removing them; CCY_TOKEN_IMPORT_REMOVE_OTHERS=1 removes, =0 keeps.\n" +
		"bash <<'CCYTOKEN'\nset -eo pipefail\nname='" + name + "'\ndate='" + date + "'\ntoken='" + token + "'\n"
}

// F13: --export-token NAME prints the import script of the name's latest file, after the
// prelude; the token is printed because that is the flag's purpose, and goes nowhere else.
func TestExportTokenIsAnswered(t *testing.T) {
	r := repoRun(t, []string{"work.2030-06-01.token", "work.2030-09-01.token", "other.2030-06-01.token"}, "--export-token", "work")
	r.answered(t, term.ExitOK)
	want := creatingGitignore + exportHead("work", "2030-09-01", "secret-work.2030-09-01.token") + exportBody
	if r.stdout.String() != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", r.stdout, want)
	}
	if r.stderr.String() != writingDist {
		t.Fatalf("stderr %q", r.stderr)
	}
	for _, c := range r.runner.Calls() {
		if strings.Contains(c.Line(), "secret-") || c.Env != nil {
			t.Fatalf("the token reached a command: %+v", c)
		}
	}
}

// F13's refusals, each the bash's words and exit 1; the error's red only on a terminal (D10).
func TestExportTokenRefusals(t *testing.T) {
	cases := map[string]struct {
		files       []string
		content     string
		name        string
		err, stdout string
	}{
		"no such name": {
			[]string{"work.2030-06-01.token"},
			"", "nobody", "ERROR: No token found with name: nobody\n",
			"\nAvailable tokens:\n\n" + strings.Repeat("═", 80) + "\nAvailable Claude Code Tokens for ccy\n",
		},
		"no tokens at all": {nil, "", "nobody", "ERROR: No token found with name: nobody\n", "\nAvailable tokens:\n\n"},
		"expired":          {[]string{"old.2030-01-14.token"}, "", "old", "ERROR: Token 'old' is expired (2030-01-14)\n", "Create a new token first: ccy --create-token\n"},
		"expires today":    {[]string{"old.2030-01-15.token"}, "", "old", "ERROR: Token 'old' is expired (2030-01-15)\n", "Create a new token first: ccy --create-token\n"},
		"empty":            {[]string{"work.2030-06-01.token"}, "\n\n", "work", "ERROR: Token file is empty: {pool}/work.2030-06-01.token\n", ""},
		"holding a quote":  {[]string{"work.2030-06-01.token"}, "a'b", "work", "ERROR: Token contains unexpected characters\n", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := repoRun(t, c.files, "--export-token", c.name)
			if c.content != "" {
				if err := os.WriteFile(filepath.Join(r.pool(), c.files[0]), []byte(c.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			r.answered(t, term.ExitFailure)
			if want := writingDist + strings.ReplaceAll(c.err, "{pool}", r.pool()); r.stderr.String() != want {
				t.Fatalf("stderr %q, want %q", r.stderr, want)
			}
			if out := strings.TrimPrefix(r.stdout.String(), creatingGitignore); !strings.HasPrefix(out, c.stdout) {
				t.Fatalf("stdout %q, want it to start %q", out, c.stdout)
			}
		})
	}
	r := repoRun(t, []string{"work.2030-06-01.token"}, "--export-token", "nobody")
	r.front.IsTerminal = func(any) bool { return true }
	r.answered(t, term.ExitFailure)
	if !strings.HasSuffix(r.stderr.String(), "\x1b[0;31mERROR:\x1b[0m No token found with name: nobody\n") {
		t.Fatalf("stderr on a terminal %q", r.stderr)
	}
}

// F13's F4: what the bash reads otherwise, and the interactive export, are the bash's.
func TestExportTokenHandsOver(t *testing.T) {
	cases := map[string]struct {
		files  []string
		name   string
		change func(t *testing.T, r *run)
	}{
		"the separator":                           {nil, "--", nil},
		"a name outside the grammar":              {nil, "a.b", nil},
		"a file the glob matches of another name": {[]string{"work.2030-06-01.token", "work.x.2030-01-01.token"}, "work", nil},
		"an undated file the glob matches":        {[]string{"work.2030-06-01.token", "work.old.token"}, "work", nil},
		"a token holding a NUL": {[]string{"work.2030-06-01.token"}, "work", func(t *testing.T, r *run) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(r.pool(), "work.2030-06-01.token"), []byte("a\x00b"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		"an unreadable token": {[]string{"work.2030-06-01.token"}, "work", func(t *testing.T, r *run) {
			t.Helper()
			if os.Geteuid() == 0 {
				t.Skip("root reads a file of mode 0")
			}
			if err := os.Chmod(filepath.Join(r.pool(), "work.2030-06-01.token"), 0); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := repoRun(t, c.files, "--export-token", c.name)
			if c.change != nil {
				c.change(t, r)
			}
			r.handsOverUntouched(t)
		})
	}
	for _, args := range [][]string{{"--export-token"}, {"--export-token", "work", "--no-ssh"}, {"--prevent", "--no-ssh"}} {
		r := repoRun(t, []string{"work.2030-06-01.token"}, args...)
		r.start(t)
		r.handedOver(t, filepath.Dir(r.front.Self))
	}
}
