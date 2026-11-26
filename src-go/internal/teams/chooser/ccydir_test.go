package chooser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// A host launch's chooser readies the checkout's .claude/ccy/ once the consent is given, before
// it prints the list: what `agent-bus seat take` and the seat keeper need, made as ccy makes it,
// so cc --teams works in a repository never used with ccy (moved from cc-teams.bash).

func (r *rig) ccyDir() string { return filepath.Join(r.Checkout(), ".claude", "ccy") }

func (r *rig) accept(t *testing.T) {
	t.Helper()
	a := r.consentRig()
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.record, []byte("accepted "+acceptedAt+"\nclients\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAHostLaunchMakesTheCheckoutsCcyDirectory(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.accept(t)
	exit(t, r.run("--type=host", "1@alpha"), term.ExitOK, r)
	info, err := os.Lstat(r.ccyDir())
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf(".claude/ccy: %v, %v", info, err)
	}
	ignore, err := os.ReadFile(filepath.Join(r.ccyDir(), ".gitignore"))
	if err != nil || string(ignore) != "# Created by cc --teams: the team seats hold tokens and are never committed.\npingbus/\n" {
		t.Fatalf(".gitignore %q, %v", ignore, err)
	}
}

func TestAnExistingCcyDirectoryIsLeftAsItIs(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.accept(t)
	if err := os.MkdirAll(r.ccyDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	exit(t, r.run("--type=host", "1@alpha"), term.ExitOK, r)
	if _, err := os.Lstat(filepath.Join(r.ccyDir(), ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("an existing .claude/ccy/ was changed: %v", err)
	}
}

func TestACcyDirectoryThatIsASymlinkIsRefusedAndNothingIsPrinted(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.accept(t)
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(r.ccyDir()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, r.ccyDir()); err != nil {
		t.Fatal(err)
	}
	exit(t, r.run("--type=host", "1@alpha"), exitConfig, r)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "is not a real directory; remove it and start cc again.")
}

// Plan 00266 review finding 6's twin (project_checkout_path_seam): a .claude a container linked
// out of the checkout is refused, and no ccy directory or .gitignore is made where it points.
func TestAClaudeDirectoryLinkedOutIsRefusedAndNothingIsMadeThere(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.accept(t)
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(r.Checkout(), ".claude")); err != nil {
		t.Fatal(err)
	}
	exit(t, r.run("--type=host", "1@alpha"), exitConfig, r)
	r.lacksOutput(t)
	if _, err := os.Lstat(filepath.Join(elsewhere, "ccy")); !os.IsNotExist(err) {
		t.Fatalf("a ccy directory was made through the link: %v", err)
	}
}

func TestBusMakesTheCcyDirectoryWhereTheKeeperReports(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.accept(t)
	exit(t, r.run("--type=host", "--bus"), term.ExitOK, r)
	if _, err := os.Lstat(r.ccyDir()); err != nil {
		t.Fatalf("--bus made no .claude/ccy/: %v", err)
	}
}

func TestADeclinedConsentOrAPodmanLaunchMakesNoCcyDirectory(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.consentRig()
	r.hostSeat()
	r.Script.Gum = []string{"NO"}
	exit(t, r.run("--type=host", "1@alpha"), term.ExitFailure, r)
	exit(t, r.run("--type=podman", "1@alpha"), term.ExitOK, r)
	if _, err := os.Lstat(r.ccyDir()); !os.IsNotExist(err) {
		t.Fatalf(".claude/ccy/ made (S-E4, and ccy makes its own): %v", err)
	}
}
