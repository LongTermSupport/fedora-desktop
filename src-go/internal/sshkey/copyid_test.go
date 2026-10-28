package sshkey

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// copyIDWorld is a home with the login key id_a and the key to install id_new (both halves).
func copyIDWorld(t *testing.T) (*world, string, string) {
	t.Helper()
	w := newWorld(t)
	login := w.key("id_a")
	priv := w.key("id_new")
	install := w.file("id_new.pub", "ssh-ed25519 AAAA new\n")
	if priv+".pub" != install {
		t.Fatalf("paths %s %s", priv, install)
	}
	w.probeSays()
	return w, login, install
}

// checkSays is the answer of the login E20 tries with the key to install alone.
func (w *world) checkSays(code int, stderr string) {
	w.fake.On([]string{"ssh", "-v"}, execx.Result{ExitCode: code, Stderr: []byte(stderr)})
}

const permissionDenied = "debug1: Authentications that can continue: publickey\n" +
	"admin@host.example.com: Permission denied (publickey).\n"

func TestE20AKeyNotInstalledIsInstalledWithoutSSHCopyIDsOwnCheck(t *testing.T) {
	w, login, install := copyIDWorld(t)
	w.checkSays(255, permissionDenied)
	wantCode(t, w.run(CopyID, "--auth-key", login, "-i", install, "-p", "2222", "admin@host.example.com"), term.ExitOK)
	priv := strings.TrimSuffix(install, ".pub")
	calls := w.fake.Calls()
	if len(calls) != 3 {
		t.Fatalf("want the probe, the check's probe and the check: %v", w.ran())
	}
	check := []string{
		"ssh", "-v", "-T", "-o", "BatchMode=yes", "-o", "ControlPath=none", "-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none", "-o", "PreferredAuthentications=publickey", "-i", priv,
		"-p", "2222", "admin@host.example.com", "exit",
	}
	if got := append([]string{calls[2].Name}, calls[2].Args...); !slices.Equal(got, check) {
		t.Errorf("check\n %q\nwant\n %q", calls[2].Line(), strings.Join(check, " "))
	}
	if calls[1].Args[0] != "-G" || !slices.Equal(calls[1].Args[1:], check[2:len(check)-1]) {
		t.Errorf("the check's probe %q", calls[1].Line())
	}
	got := w.execd()
	want := []string{
		"ssh-copy-id", "-f", "-o", "IdentityFile=" + login, "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none",
		"-o", "PreferredAuthentications=publickey", "-i", install, "-p", "2222", "admin@host.example.com",
	}
	if !slices.Equal(append([]string{got.Name}, got.Args...), want) {
		t.Errorf("exec\n %q\nwant\n %q", got.Line(), strings.Join(want, " "))
	}
}

func TestE20AKeyAlreadyInstalledIsSaidAndNothingRuns(t *testing.T) {
	accepted := "debug1: Server accepts key: ~/.ssh/id_new ED25519 SHA256:x explicit\n" + permissionDenied
	for name, answer := range map[string]struct {
		code   int
		stderr string
	}{"it logs in": {0, ""}, "accepted, no passphrase in batch mode": {255, accepted}} {
		w, login, install := copyIDWorld(t)
		w.checkSays(answer.code, answer.stderr)
		wantCode(t, w.run(CopyID, "--auth-key", login, "-i", install, "admin@host.example.com"), term.ExitOK)
		w.stderrHas("ssh-copy-id-with-key: ~/.ssh/id_new.pub is already installed on admin@host.example.com " +
			"(the host accepts the key); nothing to do.")
		if len(w.fake.Execs()) != 0 {
			t.Errorf("%s: ran %v", name, w.ran())
		}
	}
}

func TestE20ACheckThatCannotLogInStops(t *testing.T) {
	w, login, install := copyIDWorld(t)
	w.checkSays(255, "debug1: Reading configuration data /etc/ssh/ssh_config\nHost key verification failed.\n")
	wantCode(t, w.run(CopyID, "--auth-key", login, "-i", install, "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-copy-id-with-key: could not log in to host.example.com with ~/.ssh/id_new alone to see whether " +
		"it is already installed: Host key verification failed. Pass -f to install it without the check. Nothing was run.")
	if strings.Contains(w.stderr.String(), "debug1") {
		t.Errorf("ssh's debug lines are shown:\n%s", w.stderr.String())
	}
	w.noExec()
}

func TestE20TheConfigurationsKeyMakesTheCheckImpossible(t *testing.T) {
	w, login, install := copyIDWorld(t)
	w.fake.OnFunc([]string{"ssh", "-G"}, func(c execx.Cmd) (execx.Result, error) {
		out := "host host.example.com\nuser someone\nhostname host.example.com\nidentityfile " + login + "\n"
		if i := slices.Index(c.Args, "-i"); i >= 0 {
			out += "identityfile " + c.Args[i+1] + "\n"
		}
		return execx.Result{Stdout: []byte(out)}, nil
	})
	wantCode(t, w.run(CopyID, "--auth-key", login, "-i", install, "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-copy-id-with-key: ssh's configuration also gives host.example.com the key(s) ~/.ssh/id_a, so a " +
		"login with ~/.ssh/id_new alone cannot be tried to see whether it is already installed. Remove those IdentityFile " +
		"lines for this host, pass -F with a configuration that has none, or pass -f to install it without the check. " +
		"Nothing was run.")
	if w.fake.Called("ssh -v") {
		t.Error("the check logged in")
	}
	w.noExec()
}

func TestE20TheKeyToInstallNeedsItsPrivateHalfForTheCheck(t *testing.T) {
	w := newWorld(t)
	login := w.key("id_a")
	install := w.file("only.pub", "ssh-ed25519 AAAA only\n")
	w.probeSays()
	wantCode(t, w.run(CopyID, "--auth-key", login, "-i", install, "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-copy-id-with-key: ~/.ssh/only is not a readable private key (no such file or directory), and " +
		"the check whether ~/.ssh/only.pub is already installed logs in with it. Pass -f to install it without the check. " +
		"Nothing was run.")
	w.noExec()
}

func TestE20ForceSkipsTheCheck(t *testing.T) {
	w, login, install := copyIDWorld(t)
	wantCode(t, w.run(CopyID, "--auth-key", login, "-f", "-i", install, "host.example.com"), term.ExitOK)
	if n := len(w.fake.Calls()); n != 1 {
		t.Errorf("want the probe alone: %v", w.ran())
	}
	if got := w.execd(); strings.Count(got.Line(), " -f ") != 1 {
		t.Errorf("-f twice: %s", got.Line())
	}
}

func TestE20ADryRunIsCheckedThenForced(t *testing.T) {
	w, login, install := copyIDWorld(t)
	w.checkSays(255, permissionDenied)
	wantCode(t, w.run(CopyID, "--auth-key", login, "-n", "-i", install, "host.example.com"), term.ExitOK)
	got := w.execd()
	if got.Args[0] != "-f" || !slices.Contains(got.Args, "-n") {
		t.Errorf("exec %s", got.Line())
	}
}

func TestE20AMissingSSHForTheCheck(t *testing.T) {
	w, login, install := copyIDWorld(t)
	w.fake.OnFunc([]string{"ssh", "-v"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, &execx.NotInstalledError{Name: "ssh"}
	})
	wantCode(t, w.run(CopyID, "--auth-key", login, "-i", install, "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-copy-id-with-key: ssh is not installed.")
	w.noExec()
}
