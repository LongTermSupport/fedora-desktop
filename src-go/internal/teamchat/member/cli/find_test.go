package cli

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func executableAt(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakeShell is the guest's login shell for dev, answering claude and node in bin.
func (g *guestWorld) fakeShell(t *testing.T, bin string) {
	t.Helper()
	dir := t.TempDir()
	shell, passwd := filepath.Join(dir, "bash"), filepath.Join(dir, "passwd")
	executableAt(t, shell)
	if err := os.WriteFile(passwd, []byte("dev:x:1000:1000::"+g.home+":"+shell+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := &execx.Fake{}
	out := "teamchat-find-claude-begin\nclaude=" + filepath.Join(bin, "claude") + "\nnode=" + filepath.Join(bin, "node") + "\nteamchat-find-claude-end\n"
	run.On([]string{shell}, execx.Result{Stdout: []byte(out)})
	was := findShell
	t.Cleanup(func() { findShell = was })
	findShell = func() guest.Shell { return guest.Shell{Run: run, Passwd: passwd, SystemDirs: guest.SystemDirs} }
}

// R77: `guest find-claude --user U` prints the two FOUND lines and nothing else.
func TestGuestFindClaudePrintsWhatItFound(t *testing.T) {
	g := newGuestWorld(t)
	bin := filepath.Join(t.TempDir(), "nvm", "bin")
	executableAt(t, filepath.Join(bin, "claude"))
	executableAt(t, filepath.Join(bin, "node"))
	g.fakeShell(t, bin)
	if got := g.runIn("", "guest", "find-claude", "--user", "dev"); got != term.ExitOK {
		t.Fatalf("exit %d stderr %q", got, g.err.String())
	}
	want := "FOUND\tclaude\t" + filepath.Join(bin, "claude") + "\nFOUND\tnode\t" + filepath.Join(bin, "node") + "\n"
	if g.out.String() != want {
		t.Errorf("stdout %q", g.out.String())
	}
}

// A claude the shell cannot stand behind is 78 claude, with nothing on stdout.
func TestGuestFindClaudeRefusesWithNothingFound(t *testing.T) {
	g := newGuestWorld(t)
	g.fakeShell(t, filepath.Join(t.TempDir(), "absent"))
	if got := g.runIn("", "guest", "find-claude", "--user", "dev"); got != exits.ExitConfig || g.out.Len() != 0 {
		t.Fatalf("exit %d stdout %q", got, g.out.String())
	}
	if e := g.lastError(t); e.Code != exits.CodeClaude {
		t.Errorf("%+v", e)
	}
}

// The real seam: the test's own caller is not `agent`, so it is refused as the caller (GU1).
func TestGuestFindClaudeIsBuilt(t *testing.T) {
	old := unix.Umask(0o022)
	t.Cleanup(func() { unix.Umask(old) })
	var w world
	if w.run(digest, "guest", "find-claude", "--user", "agent") != exits.ExitConfig || w.out.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", w.out.String(), w.err.String())
	}
	if e := w.lastError(t); e.Code != exits.CodeIdentity {
		t.Errorf("%+v", e)
	}
}

func TestGuestFindClaudeRefusesAnInvocationWithoutItsUser(t *testing.T) {
	var w world
	if runFindClaude(Env{Stderr: &w.err}, Invocation{}) != exits.ExitUsage {
		t.Fatalf("stderr %q", w.err.String())
	}
}
