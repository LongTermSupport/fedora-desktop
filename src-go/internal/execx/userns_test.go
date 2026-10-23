package execx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
)

// TestUserNamespaceRunsTheChildAsRootOfItsOwn: the child is uid and gid 0 in a user namespace
// whose 0 is this process's ids, with a mount namespace of its own. A kernel that allows no
// unprivileged user namespace skips it.
func TestUserNamespaceRunsTheChildAsRootOfItsOwn(t *testing.T) {
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []bool{false, true} {
		maps := inUserNamespace(t, session, "/usr/bin/cat", "/proc/self/uid_map", "/proc/self/gid_map")
		want := []string{fmt.Sprintf("0 %d 1", os.Getuid()), fmt.Sprintf("0 %d 1", os.Getgid())}
		if len(maps) != 2 || strings.Join(strings.Fields(maps[0]), " ") != want[0] || strings.Join(strings.Fields(maps[1]), " ") != want[1] {
			t.Fatalf("session %v: maps %q, want %q", session, maps, want)
		}
		if mnt := inUserNamespace(t, session, "/usr/bin/readlink", "/proc/self/ns/mnt"); len(mnt) != 1 || mnt[0] == self {
			t.Errorf("session %v: the child's mount namespace %q, this one %q", session, mnt, self)
		}
	}
}

// inUserNamespace is the lines name prints with args, run in a user namespace.
func inUserNamespace(t *testing.T, session bool, name string, args ...string) []string {
	t.Helper()
	res, err := OS{}.Run(context.Background(), Cmd{Name: name, Args: args, UserNamespace: true, NewSession: session})
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOSPC) {
		t.Skipf("no unprivileged user namespace here: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(res.Stdout), "\n"), "\n")
}

// TestUserNamespaceBesideACredentialIsRefused: the child's ids are its namespace's root, so a
// credential beside it contradicts it; and a process replaced in place cannot enter one.
func TestUserNamespaceBesideACredentialIsRefused(t *testing.T) {
	c := Cmd{Name: "/usr/bin/true", UserNamespace: true, Credential: &syscall.Credential{Uid: 1, Gid: 1}}
	if _, err := (OS{}).Run(context.Background(), c); !errors.Is(err, ErrBadCmd) {
		t.Fatalf("run: got %v", err)
	}
	old := syscallExec
	syscallExec = func(string, []string, []string) error { t.Error("exec ran"); return nil }
	t.Cleanup(func() { syscallExec = old })
	if err := (OS{}).Exec(Cmd{Name: "/usr/bin/true", UserNamespace: true}); !errors.Is(err, ErrBadCmd) {
		t.Fatalf("exec: got %v", err)
	}
}
