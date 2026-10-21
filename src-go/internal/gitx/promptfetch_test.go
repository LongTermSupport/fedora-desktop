package gitx_test

import (
	"context"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

var sshConfigGet = []string{"git", "config", "--get", "core.sshCommand"}

func TestSSHCommandIsTheConfiguredValue(t *testing.T) {
	f := &execx.Fake{}
	f.On(sshConfigGet, execx.Result{Stdout: []byte("ssh -i k -F /dev/null\n")})
	got, set, err := gitx.New(f).SSHCommand(context.Background(), dir)
	if err != nil || !set || got != "ssh -i k -F /dev/null" {
		t.Fatalf("got %q, %v, %v", got, set, err)
	}
	if f.Calls()[0].Dir != dir {
		t.Fatalf("dir %q", f.Calls()[0].Dir)
	}
}

// Git's exit 1 for config --get is "not set"; any other refusal is an error, never "not set".
func TestSSHCommandNotSetAndRefusals(t *testing.T) {
	f := &execx.Fake{}
	f.On(sshConfigGet, execx.Result{ExitCode: 1})
	if got, set, err := gitx.New(f).SSHCommand(context.Background(), dir); err != nil || set || got != "" {
		t.Fatalf("not set: got %q, %v, %v", got, set, err)
	}
	f = &execx.Fake{}
	f.On(sshConfigGet, gitFail("fatal: bad config line 3"))
	if _, set, err := gitx.New(f).SSHCommand(context.Background(), dir); err == nil || set {
		t.Fatalf("bad config: set %v, err %v", set, err)
	}
	if _, set, err := gitx.New(errRunner{}).SSHCommand(context.Background(), dir); err == nil || set {
		t.Fatalf("git missing: set %v, err %v", set, err)
	}
}

func TestFetchUnattendedHasNoWayToAsk(t *testing.T) {
	f := &execx.Fake{}
	argv := []string{"git", "-c", "core.askPass=", "fetch", "--quiet"}
	f.On(argv, execx.Result{})
	if err := gitx.New(f).FetchUnattended(context.Background(), dir, "ssh -o BatchMode=yes"); err != nil {
		t.Fatal(err)
	}
	c := f.Calls()[0]
	if c.Dir != dir || c.Stdin != nil {
		t.Fatalf("dir %q stdin %v", c.Dir, c.Stdin)
	}
	for _, kv := range []string{"GIT_TERMINAL_PROMPT=0", "SSH_ASKPASS_REQUIRE=never", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"} {
		if !slices.Contains(c.Env, kv) {
			t.Errorf("env %q lacks %s", c.Env, kv)
		}
	}
	for _, name := range []string{"GIT_ASKPASS", "SSH_ASKPASS"} {
		if !slices.Contains(c.Unset, name) {
			t.Errorf("%s not unset: %v", name, c.Unset)
		}
	}
	f = &execx.Fake{}
	f.On(argv, gitFail("Permission denied (publickey)."))
	if err := gitx.New(f).FetchUnattended(context.Background(), dir, "ssh"); err == nil {
		t.Fatal("a failed fetch was not an error")
	}
}
