package vmtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The port of scripts/test-vmtest-host-only-gate.bash: the acceptance case first, so every
// refusal is a change from a known-good baseline, then one spoiled input per case. Every case
// of the suite is here; B6 (a literal line match) and B7 (a regular file) add two.

const (
	hostOnlyID      = "server-github-token"
	plantedToken    = "planted-UNIQUE-value-0123456789"
	passphraseValue = "correct-horse-battery-staple-UNIQUE"
)

type gateCase struct {
	k    *kit
	l    *lab
	cr   credentials
	from string
}

func newGateCase(t *testing.T) *gateCase {
	t.Helper()
	k := newKit(t)
	k.write(allowlistFile, "server-fast-provision\n")
	k.write(hostOnlyFile, hostOnlyID+"\n")
	dir := t.TempDir()
	g := &gateCase{k: k, l: &lab{paths: newPaths(k.environment())}, cr: credentials{
		account:   "throwaway-lab-user",
		tokenFile: filepath.Join(dir, "gh-token"),
		ppFile:    filepath.Join(dir, "ssh-pass"),
	}}
	for path, body := range map[string]string{g.cr.tokenFile: plantedToken + "\n", g.cr.ppFile: passphraseValue + "\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

func (g *gateCase) check() error {
	g.k.env["VMTEST_FROM_BRIDGE"] = g.from
	return newCLI(g.k.environment()).hostOnlyPreflight(g.l, hostOnlyID, g.cr)
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// E30: the acceptance case, first.
func TestE30APreparedHostOnlyRunIsPermitted(t *testing.T) {
	g := newGateCase(t)
	if err := g.check(); err != nil {
		t.Fatalf("refused: %v", err)
	}
	chmod(t, g.cr.tokenFile, 0o400)
	if err := g.check(); err != nil {
		t.Fatalf("a 0400 token was refused: %v", err)
	}
}

// E30: every spoiled input refuses, with a message that names it and never a secret.
func TestE30HostOnlyRefusals(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(t *testing.T, g *gateCase)
		want  string
	}{
		{"launched by the bridge", func(_ *testing.T, g *gateCase) { g.from = "1" }, "launched by the bridge"},
		{"absent from the host-only list", func(_ *testing.T, g *gateCase) { g.k.write(hostOnlyFile, "something-else\n") }, "is not in the deployed host-only list"},
		{"a missing host-only list", func(t *testing.T, g *gateCase) {
			t.Helper()
			if err := os.Remove(g.k.path(hostOnlyFile)); err != nil {
				t.Fatal(err)
			}
		}, "is not in the deployed host-only list"},
		{"a pattern, not the id (B6)", func(_ *testing.T, g *gateCase) { g.k.write(hostOnlyFile, "server-github-.*\n") }, "is not in the deployed host-only list"},
		{"on both lists", func(_ *testing.T, g *gateCase) {
			g.k.write(allowlistFile, "server-fast-provision\n"+hostOnlyID+"\n")
		}, "on both the bridge allowlist and the host-only list"},
		{"no account", func(_ *testing.T, g *gateCase) { g.cr.account = "" }, "needs --github-account"},
		{"no token file", func(_ *testing.T, g *gateCase) { g.cr.tokenFile = "" }, "needs --github-token-file"},
		{"no passphrase file", func(_ *testing.T, g *gateCase) { g.cr.ppFile = "" }, "needs --github-ssh-passphrase-file"},
		{"an unreadable token file", func(_ *testing.T, g *gateCase) { g.cr.tokenFile += "-absent" }, "is not readable"},
		{"an empty token file", func(t *testing.T, g *gateCase) {
			t.Helper()
			if err := os.WriteFile(g.cr.tokenFile, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "is empty"},
		{"an empty passphrase file", func(t *testing.T, g *gateCase) {
			t.Helper()
			if err := os.WriteFile(g.cr.ppFile, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "is empty"},
		{"a world-readable token", func(t *testing.T, g *gateCase) {
			t.Helper()
			chmod(t, g.cr.tokenFile, 0o644)
		}, "is mode 644"},
		{"a group-readable passphrase", func(t *testing.T, g *gateCase) {
			t.Helper()
			chmod(t, g.cr.ppFile, 0o640)
		}, "is mode 640"},
		{"a directory for a secret (B7)", func(t *testing.T, g *gateCase) {
			t.Helper()
			g.cr.tokenFile = t.TempDir()
			chmod(t, g.cr.tokenFile, 0o600)
		}, "is not a regular file"},
		{"an account outside the grammar", func(_ *testing.T, g *gateCase) { g.cr.account = "user; rm -rf ~" }, "is not a GitHub username"},
		{"a trailing hyphen", func(_ *testing.T, g *gateCase) { g.cr.account = "user-" }, "is not a GitHub username"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newGateCase(t)
			tc.spoil(t, g)
			err := g.check()
			if err == nil {
				t.Fatal("permitted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %q, want it to say %q", err, tc.want)
			}
			if strings.Contains(err.Error(), plantedToken) || strings.Contains(err.Error(), passphraseValue) {
				t.Fatalf("the refusal echoes a secret: %q", err)
			}
		})
	}
}
