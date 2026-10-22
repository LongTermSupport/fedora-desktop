package guest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	agentUID = 1000
	agentGID = 1000
)

var errNoAccount = errors.New("no such account")

// fakeSystem is a caller and a passwd database; the umask it records is never the process's.
type fakeSystem struct {
	uid, euid, gid, egid int
	groups               []int
	groupsErr            error
	accounts             []Account
	umask                int
	calls                []string
}

func (f *fakeSystem) Umask(mask int) int {
	f.calls = append(f.calls, "umask")
	old := f.umask
	f.umask = mask
	return old
}

func (f *fakeSystem) Getuid() int  { f.calls = append(f.calls, "getuid"); return f.uid }
func (f *fakeSystem) Geteuid() int { return f.euid }
func (f *fakeSystem) Getgid() int  { return f.gid }
func (f *fakeSystem) Getegid() int { return f.egid }

func (f *fakeSystem) Getgroups() ([]int, error) { return f.groups, f.groupsErr }

func (f *fakeSystem) LookupName(name string) (Account, error) {
	for _, a := range f.accounts {
		if a.Name == name {
			return a, nil
		}
	}
	return Account{}, errNoAccount
}

func (f *fakeSystem) LookupID(uid int) (Account, error) {
	for _, a := range f.accounts {
		if a.UID == uid {
			return a, nil
		}
	}
	return Account{}, errNoAccount
}

// agentSystem is the caller `agent`, uid and gid 1000, no supplementary group, whose home is
// home.
func agentSystem(home string) *fakeSystem {
	return &fakeSystem{
		uid: agentUID, euid: agentUID, gid: agentGID, egid: agentGID, umask: 0o022,
		accounts: []Account{{Name: "agent", UID: agentUID, GID: agentGID, Home: home}},
	}
}

func agentUser(t *testing.T) paths.GuestUser {
	t.Helper()
	u, err := paths.ParseGuestUser("agent")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// home is a fresh directory owned by the test's own uid, which is what homefs checks.
func home(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func wantCode(t *testing.T, err error, exit int, code string) {
	t.Helper()
	e, ok := errors.AsType[*exits.Error](err)
	if !ok {
		t.Fatalf("err = %v, want a %d %s error object", err, exit, code)
	}
	if int(e.Exit) != exit || e.Code != code {
		t.Fatalf("err = %d %s (%s), want %d %s", e.Exit, e.Code, e.Message, exit, code)
	}
}

func TestEstablishOpensTheHome(t *testing.T) {
	dir := home(t)
	sys := agentSystem(dir)
	sys.groups = []int{agentGID}
	id, err := Establish(sys, agentUser(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := id.Close(); err != nil {
			t.Error(err)
		}
	}()
	if id.User != agentUser(t) || id.UID != agentUID || id.GID != agentGID || id.Root.Path() != dir {
		t.Errorf("identity = %+v", id)
	}
	if sys.umask != 0o077 || len(sys.calls) == 0 || sys.calls[0] != "umask" {
		t.Errorf("umask %o, calls %v: umask 077 must be set first", sys.umask, sys.calls)
	}
}

// GU1: each refusal is 78 identity, and none quotes a passwd value.
func TestEstablishRefusesAnotherCaller(t *testing.T) {
	dir := home(t)
	cases := map[string]func(*fakeSystem){
		"root":        func(f *fakeSystem) { f.uid, f.euid = 0, 0; f.accounts[0].UID = 0 },
		"setuid":      func(f *fakeSystem) { f.euid = agentUID + 1 },
		"no account":  func(f *fakeSystem) { f.accounts = nil },
		"another uid": func(f *fakeSystem) { f.uid, f.euid = agentUID+1, agentUID+1 },
		"name of uid differs": func(f *fakeSystem) {
			f.accounts = []Account{{Name: "other", UID: agentUID, GID: agentGID, Home: dir}, {Name: "agent", UID: agentUID, GID: agentGID, Home: dir}}
		},
		"gid":           func(f *fakeSystem) { f.gid = agentGID + 1 },
		"egid":          func(f *fakeSystem) { f.egid = agentGID + 1 },
		"supplementary": func(f *fakeSystem) { f.groups = []int{agentGID, 10} },
		"groups fail":   func(f *fakeSystem) { f.groupsErr = errors.New("getgroups failed") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			sys := agentSystem(dir)
			change(sys)
			id, err := Establish(sys, agentUser(t))
			if id != nil {
				t.Fatalf("identity %+v for a refused caller", id)
			}
			wantCode(t, err, 78, exits.CodeIdentity)
			if strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "other") {
				t.Errorf("message quotes a passwd value: %s", err)
			}
		})
	}
}

func TestEstablishRefusesTheHome(t *testing.T) {
	shared := home(t)
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"missing":  filepath.Join(t.TempDir(), "absent"),
		"relative": "home/agent",
		"shared":   shared,
		"link":     link(t, home(t)),
	}
	for name, dir := range cases {
		t.Run(name, func(t *testing.T) {
			id, err := Establish(agentSystem(dir), agentUser(t))
			if id != nil {
				t.Fatalf("identity %+v with a refused home", id)
			}
			wantCode(t, err, 78, exits.CodeHome)
		})
	}
}

func link(t *testing.T, target string) string {
	t.Helper()
	l := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, l); err != nil {
		t.Fatal(err)
	}
	return l
}

// OS is the real process: the test's own uid and its passwd entry.
func TestOSIsTheProcess(t *testing.T) {
	var sys OS
	if sys.Getuid() != os.Getuid() || sys.Geteuid() != os.Geteuid() || sys.Getgid() != os.Getgid() || sys.Getegid() != os.Getegid() {
		t.Error("ids differ from the process's")
	}
	groups, err := sys.Getgroups()
	if err != nil || groups == nil {
		t.Errorf("groups = %v, %v", groups, err)
	}
	old := sys.Umask(0o027)
	if back := sys.Umask(old); back != 0o027 {
		t.Errorf("umask read back %o", back)
	}
	me, err := sys.LookupID(os.Getuid())
	if err != nil || me.UID != os.Getuid() || !filepath.IsAbs(me.Home) {
		t.Fatalf("lookup own uid = %+v, %v", me, err)
	}
	byName, err := sys.LookupName(me.Name)
	if err != nil || byName != me {
		t.Errorf("lookup %s = %+v, %v", me.Name, byName, err)
	}
	if _, err := sys.LookupName("no-such-user-here"); err == nil {
		t.Error("an absent user was found")
	}
	if _, err := sys.LookupID(-2); err == nil {
		t.Error("an absent uid was found")
	}
}
