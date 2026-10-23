package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ghaccount"
)

// TestMain is the askpass program when ssh-keygen starts this test binary as one, as it starts
// the command (E21).
func TestMain(m *testing.M) {
	if ghaccount.IsAskpass(os.Getenv) {
		os.Exit(int(ghaccount.Askpass(os.Getenv, os.Args[1:], os.Stdout)))
	}
	os.Exit(m.Run())
}

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--frobnicate"}); code != 64 {
		t.Fatalf("--frobnicate: exit %d", code)
	}
}

// keygen is ssh-keygen with this binary as its askpass program, holding the passphrase.
func keygen(t *testing.T, pass string, args ...string) execx.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := execx.NewSecret("GH_ACCOUNT_SETUP_PASSPHRASE", pass)
	if err != nil {
		t.Fatal(err)
	}
	return execx.Cmd{
		Name:    "ssh-keygen",
		Args:    args,
		Env:     []string{"SSH_ASKPASS=" + exe, "SSH_ASKPASS_REQUIRE=force", ghaccount.AskpassModeVar + "=keygen"},
		Secrets: []execx.Secret{secret},
	}
}

// E21 against the real ssh-keygen: a key made with the passphrase through askpass is encrypted
// with it, and read back through the same askpass.
func TestTheRealSSHKeygenGetsThePassphraseThroughAskpass(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Fatalf("ssh-keygen is needed for this proof: %v", err)
	}
	ctx := context.Background()
	key := filepath.Join(t.TempDir(), "github_work")
	const pass = "a passphrase with spaces"
	if _, err := (execx.OS{}).Run(ctx, keygen(t, pass, "-q", "-t", "ed25519", "-C", "jane@github", "-f", key)); err != nil {
		t.Fatalf("make the key: %v", err)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (execx.OS{}).Run(ctx, execx.Cmd{Name: "ssh-keygen", Args: []string{"-y", "-P", "", "-f", key}}); err == nil {
		t.Fatal("the key opens without a passphrase")
	}
	res, err := (execx.OS{}).Run(ctx, keygen(t, pass, "-y", "-f", key))
	if err != nil {
		t.Fatalf("read the key back: %v", err)
	}
	if got, want := strings.Fields(string(res.Stdout)), strings.Fields(string(pub)); len(got) < 2 || got[1] != want[1] {
		t.Fatalf("read back %q, want the key of %q", res.Stdout, pub)
	}
	if _, err := (execx.OS{}).Run(ctx, keygen(t, "the wrong one", "-y", "-f", key)); err == nil {
		t.Fatal("the key opened with another passphrase")
	}
}
