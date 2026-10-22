package admin

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	ctlUID = 990
	ctlGID = 985
	owner  = 1000
	digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// fakeProc is the process as the tests decide it: its uids, the teamchat-ctl account, and
// what the child's self-check reads.
type fakeProc struct {
	uid, euid int
	noCtl     bool
	lookupErr error
	groups    []int
	groupsErr error
	wd        string
	wdErr     error
	env       []string
	looked    []string
}

func (p *fakeProc) Getuid() int  { return p.uid }
func (p *fakeProc) Geteuid() int { return p.euid }

func (p *fakeProc) Getgroups() ([]int, error) { return p.groups, p.groupsErr }

func (p *fakeProc) Getwd() (string, error) { return p.wd, p.wdErr }

func (p *fakeProc) Environ() []string { return p.env }

func (p *fakeProc) LookupUser(name string) (User, bool, error) {
	p.looked = append(p.looked, name)
	if p.lookupErr != nil {
		return User{}, false, p.lookupErr
	}
	if p.noCtl || name != paths.CtlUser {
		return User{}, false, nil
	}
	return User{UID: ctlUID, GID: ctlGID}, true, nil
}

func rootProc() *fakeProc  { return &fakeProc{uid: 0, euid: 0} }
func ownerProc() *fakeProc { return &fakeProc{uid: owner, euid: owner} }

// childProc is a teamchat-ctl process started as root starts it (CC1).
func childProc() *fakeProc {
	return &fakeProc{uid: ctlUID, euid: ctlUID, wd: "/", env: []string{"PATH=" + childPath}}
}

type outcome struct {
	code           term.ExitCode
	stdout, stderr string
}

func run(t *testing.T, proc Process, stdin string, args ...string) outcome {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(Env{Args: args, Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr, Proc: proc, Digest: digest})
	return outcome{code, stdout.String(), stderr.String()}
}

// specVerbs is CA1's list, written from C 3.3's table.
func specVerbs() map[string]Class {
	out := map[string]Class{}
	add := func(class Class, group string, words ...string) {
		if len(words) == 0 {
			out[group] = class
		}
		for _, w := range words {
			out[group+" "+w] = class
		}
	}
	add(ClassRoot, "team", "apply", "approve", "remove")
	add(ClassRoot, "member", "add", "return", "park", "retire", "reissue", "set")
	add(ClassRoot, "role", "apply")
	add(ClassRoot, "human", "password", "devices", "logout", "lock", "unlock")
	for _, v := range []string{"redact", "backup", "backups", "restore", "health", "check", "footprint"} {
		add(ClassRoot, v)
	}
	add(ClassRoot, "lxc", "apply", "restart")
	add(ClassRoot, "migrate", "preflight", "stage", "fence", "fence-status", "capture", "carry", "commit",
		"accept", "archive", "archive-check", "unfence", "rollback")
	add(ClassCtl, "ctl-child")
	add(ClassOther, "declare", "check")
	add(ClassOther, "element", "check", "apply")
	add(ClassOther, "status")
	add(ClassAny, "version")
	return out
}

func TestVerbTableIsCA1(t *testing.T) {
	want := specVerbs()
	got := map[string]Class{}
	for _, v := range verbTable {
		if _, dup := got[v.name]; dup {
			t.Fatalf("verb %q twice", v.name)
		}
		got[v.name] = v.class
	}
	for name, class := range want {
		if got[name] != class {
			t.Errorf("verb %q: class %v, want %v", name, got[name], class)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("verb %q is not in C 3.3", name)
		}
	}
}

// procFor is a process of the class.
func procFor(c Class) *fakeProc {
	switch c {
	case ClassRoot:
		return rootProc()
	case ClassCtl:
		return childProc()
	case ClassOther, ClassAny:
	}
	return ownerProc()
}

// TestEveryVerbEveryClass is C 3.14's one table test: every verb asked by every class (CA3,
// CA5).
func TestEveryVerbEveryClass(t *testing.T) {
	names := map[Class]string{ClassRoot: "root", ClassCtl: "teamchat-ctl", ClassOther: "a user other than root and teamchat-ctl"}
	for name, class := range specVerbs() {
		for _, caller := range []Class{ClassRoot, ClassCtl, ClassOther} {
			args := strings.Fields(name)
			got := run(t, procFor(caller), "", args...)
			switch {
			case class != ClassAny && class != caller:
				want := "teamchat-admin: " + name + " must be run by " + names[class] + "\n"
				if got.code != ExitClass || got.stderr != want || got.stdout != "" {
					t.Errorf("%s by %v = %+v, want 77 %q", name, caller, got, want)
				}
			case name == "version" || name == "ctl-child":
			default:
				want := "teamchat-admin: " + name + " is not built yet\n"
				if got.code != term.ExitFailure || got.stderr != want || got.stdout != "" {
					t.Errorf("%s by %v = %+v, want 1 %q", name, caller, got, want)
				}
			}
		}
	}
}

func TestClassFromUIDs(t *testing.T) {
	cases := map[string]struct {
		proc *fakeProc
		want Class
	}{
		"root":                  {rootProc(), ClassRoot},
		"owner":                 {ownerProc(), ClassOther},
		"ctl":                   {childProc(), ClassCtl},
		"real root, effective":  {&fakeProc{uid: 0, euid: owner}, ClassOther},
		"effective root only":   {&fakeProc{uid: owner, euid: 0}, ClassOther},
		"ctl real only":         {&fakeProc{uid: ctlUID, euid: owner}, ClassOther},
		"ctl effective only":    {&fakeProc{uid: owner, euid: ctlUID}, ClassOther},
		"no ctl user, its uid":  {&fakeProc{uid: ctlUID, euid: ctlUID, noCtl: true}, ClassOther},
		"no ctl user, the root": {&fakeProc{noCtl: true}, ClassRoot},
	}
	for name, c := range cases {
		got, err := classOf(c.proc)
		if err != nil || got != c.want {
			t.Errorf("%s: class %v, %v; want %v", name, got, err, c.want)
		}
	}
	p := ownerProc()
	if _, err := classOf(p); err != nil || !slices.Equal(p.looked, []string{paths.CtlUser}) {
		t.Errorf("looked up %v (%v), want only %s", p.looked, err, paths.CtlUser)
	}
}

func TestLookupFailureIsConfiguration(t *testing.T) {
	p := ownerProc()
	p.lookupErr = errors.New("nss down")
	got := run(t, p, "", "status")
	if got.code != ExitConfig || got.stderr != "teamchat-admin: cannot look up the "+paths.CtlUser+" account: nss down\n" {
		t.Fatalf("lookup failure = %+v", got)
	}
}

func TestUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"none":        nil,
		"unknown":     {"frobnicate"},
		"unknown sub": {"team", "frobnicate"},
		"group alone": {"team"},
		"flag first":  {"--team", "x"},
	} {
		got := run(t, rootProc(), "", args...)
		if got.code != term.ExitUsage || got.stdout != "" || !strings.HasPrefix(got.stderr, "teamchat-admin: ") ||
			!strings.Contains(got.stderr, "usage: teamchat-admin <verb>") {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestUsageNamesEveryVerb(t *testing.T) {
	got := run(t, ownerProc(), "")
	for name := range specVerbs() {
		if !strings.Contains(got.stderr, "  "+name+"\n") {
			t.Errorf("usage lacks %q", name)
		}
	}
}

func TestFlagsBelongToTheVerb(t *testing.T) {
	got := run(t, rootProc(), "", "team", "apply", "--team", "x")
	if got.code != term.ExitFailure || got.stderr != "teamchat-admin: team apply is not built yet\n" {
		t.Fatalf("team apply --team x = %+v", got)
	}
	got = run(t, rootProc(), "", "restore", "--resume")
	if got.code != term.ExitFailure || got.stderr != "teamchat-admin: restore is not built yet\n" {
		t.Fatalf("restore --resume = %+v", got)
	}
}

func TestVersion(t *testing.T) {
	for _, c := range []Class{ClassRoot, ClassCtl, ClassOther} {
		got := run(t, procFor(c), "", "version")
		if got.code != term.ExitOK || got.stdout != digest+"\n" || got.stderr != "" {
			t.Errorf("version by %v = %+v", c, got)
		}
	}
	if got := run(t, ownerProc(), "", "version", "now"); got.code != term.ExitUsage || got.stdout != "" ||
		got.stderr != "teamchat-admin: version takes no arguments\n" {
		t.Errorf("version now = %+v", got)
	}
	for name, d := range map[string]string{"missing": "", "malformed": "not-a-digest"} {
		var stdout, stderr bytes.Buffer
		code := Run(Env{Args: []string{"version"}, Stdout: &stdout, Stderr: &stderr, Proc: ownerProc(), Digest: d})
		if code != term.ExitFailure || stdout.Len() != 0 ||
			stderr.String() != "teamchat-admin: this build carries no valid source digest\n" {
			t.Errorf("%s digest: %d %q %q", name, code, stdout.String(), stderr.String())
		}
	}
}

func TestExitTable(t *testing.T) {
	want := map[term.ExitCode]int{
		term.ExitOK: 0, term.ExitFailure: 1, term.ExitUsage: 64, ExitUnreachable: 69, ExitRefused: 70,
		ExitBusy: 75, ExitClass: 77, ExitConfig: 78,
	}
	for code, n := range want {
		if int(code) != n {
			t.Errorf("exit %d, want %d", code, n)
		}
	}
}

// failWriter refuses every write.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestUnwritableStdoutFails(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(Env{Args: []string{"version"}, Stdout: failWriter{}, Stderr: &stderr, Proc: ownerProc(), Digest: digest})
	if code != term.ExitFailure || stderr.String() != "teamchat-admin: cannot write the answer: closed\n" {
		t.Fatalf("unwritable stdout: %d %q", code, stderr.String())
	}
}
