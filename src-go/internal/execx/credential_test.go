package execx

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// TestCleanEnvIsTheWholeEnvironment is EX2: with CleanEnv the child sees Env and nothing of the
// parent's, and an empty Env is an empty environment.
func TestCleanEnvIsTheWholeEnvironment(t *testing.T) {
	t.Setenv("EXECX_PARENT_ONLY", "leaks")
	for _, session := range []bool{false, true} {
		res, err := OS{}.Run(context.Background(), Cmd{Name: "/usr/bin/env", Env: []string{"PATH=/usr/bin:/bin", "A=1"}, CleanEnv: true, NewSession: session})
		if err != nil {
			t.Fatal(err)
		}
		if got := string(res.Stdout); got != "PATH=/usr/bin:/bin\nA=1\n" {
			t.Fatalf("session %v: env %q", session, got)
		}
		res, err = OS{}.Run(context.Background(), Cmd{Name: "/usr/bin/env", CleanEnv: true, NewSession: session})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Stdout) != 0 {
			t.Fatalf("session %v: an empty Env gave %q", session, res.Stdout)
		}
	}
}

func TestWithoutCleanEnvTheParentsEnvironmentIsInherited(t *testing.T) {
	t.Setenv("EXECX_PARENT_ONLY", "inherited")
	res, err := OS{}.Run(context.Background(), Cmd{Name: "/usr/bin/env"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Stdout), "EXECX_PARENT_ONLY=inherited\n") {
		t.Fatal("the parent's variable is not inherited")
	}
}

// TestCleanEnvBesideUnsetIsRefused is EX2's contradiction, refused by every runner before any
// child starts.
func TestCleanEnvBesideUnsetIsRefused(t *testing.T) {
	c := Cmd{Name: "/usr/bin/env", CleanEnv: true, Unset: []string{"HOME"}}
	f := &Fake{}
	f.On([]string{"/usr/bin/env"}, Result{})
	for name, run := range map[string]func() error{
		"os":      func() error { _, err := OS{}.Run(context.Background(), c); return err },
		"session": func() error { s := c; s.NewSession = true; _, err := OS{}.Run(context.Background(), s); return err },
		"fake":    func() error { _, err := f.Run(context.Background(), c); return err },
		"exec":    func() error { return OS{}.Exec(c) },
	} {
		if err := run(); !errors.Is(err, ErrBadCmd) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if len(f.Calls()) != 0 {
		t.Fatal("the fake recorded a refused command")
	}
}

func TestNoSetGroupsIsRefused(t *testing.T) {
	c := Cmd{Name: "/usr/bin/true", Credential: &syscall.Credential{Uid: 1, Gid: 1, NoSetGroups: true}}
	if _, err := (OS{}).Run(context.Background(), c); !errors.Is(err, ErrBadCmd) {
		t.Fatalf("got %v", err)
	}
}

// TestExecRefusesACredential: a process replaced in place keeps its own ids, so a credential it
// cannot honour is refused rather than ignored.
func TestExecRefusesACredential(t *testing.T) {
	old := syscallExec
	syscallExec = func(string, []string, []string) error { t.Error("exec ran"); return nil }
	t.Cleanup(func() { syscallExec = old })
	err := OS{}.Exec(Cmd{Name: "/usr/bin/true", Credential: &syscall.Credential{Uid: 1, Gid: 1}})
	if !errors.Is(err, ErrBadCmd) {
		t.Fatalf("got %v", err)
	}
}

func TestExecWithCleanEnvPassesOnlyEnv(t *testing.T) {
	t.Setenv("EXECX_PARENT_ONLY", "leaks")
	var got []string
	old := syscallExec
	syscallExec = func(_ string, _, env []string) error { got = env; return nil }
	t.Cleanup(func() { syscallExec = old })
	if err := (OS{}).Exec(Cmd{Name: "/usr/bin/true", Env: []string{"A=1"}, CleanEnv: true}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"A=1"}) {
		t.Fatalf("env %q", got)
	}
}

// TestFakeRecordsCredentialAndCleanEnv is EX3.
func TestFakeRecordsCredentialAndCleanEnv(t *testing.T) {
	f := &Fake{}
	f.On([]string{"child"}, Result{})
	cred := &syscall.Credential{Uid: 990, Gid: 991, Groups: []uint32{}}
	if _, err := f.Run(context.Background(), Cmd{Name: "child", Credential: cred, CleanEnv: true, Env: []string{"PATH=/usr/bin:/bin"}}); err != nil {
		t.Fatal(err)
	}
	got := f.Calls()[0]
	if got.Credential == nil || got.Credential.Uid != 990 || got.Credential.Gid != 991 || got.Credential.Groups == nil || len(got.Credential.Groups) != 0 {
		t.Fatalf("credential %+v", got.Credential)
	}
	if !got.CleanEnv || !slices.Equal(got.Env, []string{"PATH=/usr/bin:/bin"}) {
		t.Fatalf("env %v %q", got.CleanEnv, got.Env)
	}
}

// TestCredentialRunsTheChildAsItsIDs is EX1 on a real child: the uid, gid and an empty group
// list read from the child's own /proc status. Changing ids needs root.
func TestCredentialRunsTheChildAsItsIDs(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing a child's ids needs root; the Fake's record covers the field")
	}
	for _, session := range []bool{false, true} {
		cred := &syscall.Credential{Uid: 4242, Gid: 4343, Groups: []uint32{}}
		res, err := OS{}.Run(context.Background(), Cmd{Name: "/usr/bin/cat", Args: []string{"/proc/self/status"}, Credential: cred, NewSession: session})
		if err != nil {
			t.Fatal(err)
		}
		status := string(res.Stdout)
		for _, want := range []string{"\nUid:\t4242\t4242\t4242\t4242\n", "\nGid:\t4343\t4343\t4343\t4343\n"} {
			if !strings.Contains(status, want) {
				t.Errorf("session %v: no %q in the child's status", session, want)
			}
		}
		if groups := statusGroups(t, status); len(groups) != 0 {
			t.Errorf("session %v: groups %q", session, groups)
		}
	}
	cred := &syscall.Credential{Uid: 4242, Gid: 4343, Groups: []uint32{4444, 4545}}
	res, err := OS{}.Run(context.Background(), Cmd{Name: "/usr/bin/cat", Args: []string{"/proc/self/status"}, Credential: cred})
	if err != nil {
		t.Fatal(err)
	}
	if groups := statusGroups(t, string(res.Stdout)); !slices.Equal(groups, []string{"4444", "4545"}) {
		t.Fatalf("groups %q", groups)
	}
}

// statusGroups is the Groups line of a /proc status, its ids.
func statusGroups(t *testing.T, status string) []string {
	t.Helper()
	for line := range strings.Lines(status) {
		if rest, ok := strings.CutPrefix(line, "Groups:"); ok {
			return strings.Fields(rest)
		}
	}
	t.Fatal("no Groups line")
	return nil
}
