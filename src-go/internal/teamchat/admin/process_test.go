package admin

import (
	"os"
	"slices"
	"testing"
)

func TestOSProcess(t *testing.T) {
	p := OSProcess()
	if p.Getuid() != os.Getuid() || p.Geteuid() != os.Geteuid() || !slices.Equal(p.Environ(), os.Environ()) {
		t.Fatal("OSProcess does not report this process")
	}
	wd, err := p.Getwd()
	if want, werr := os.Getwd(); err != nil || werr != nil || wd != want {
		t.Fatalf("Getwd = %q, %v", wd, err)
	}
	groups, err := p.Getgroups()
	if want, gerr := os.Getgroups(); err != nil || gerr != nil || !slices.Equal(groups, want) {
		t.Fatalf("Getgroups = %v, %v", groups, err)
	}
}

func TestOSProcessLookupUser(t *testing.T) {
	p := OSProcess()
	u, found, err := p.LookupUser("root")
	if err != nil || !found || u.UID != 0 || u.GID != 0 {
		t.Fatalf("root = %+v %v %v", u, found, err)
	}
	if u, found, err := p.LookupUser("teamchat-no-such-user-n2a"); err != nil || found {
		t.Fatalf("missing user = %+v %v %v", u, found, err)
	}
}
