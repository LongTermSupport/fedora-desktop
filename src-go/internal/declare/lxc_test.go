package declare

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestLxcMembersParsesTheMemberLines(t *testing.T) {
	f := &execx.Fake{}
	script(f, "lxc-members", "LXCMEMBER\talpha\tcontainer-a\tagent\tworker\tcoordinator\tpresent\n"+
		"LXCMEMBER\talpha\tcontainer-b\tagent\tother\tworker\tabsent\n")
	got, err := newClient(f).LxcMembers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []LxcMember{
		{Team: "alpha", Container: "container-a", User: "agent", Host: "worker", Role: "coordinator", State: "present"},
		{Team: "alpha", Container: "container-b", User: "agent", Host: "other", Role: "worker", State: "absent"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if line := lastLine(t, f); line != prefix+" lxc-members --file /hv.yml" {
		t.Fatalf("ran %q", line)
	}
}

func TestLxcMembersRejectsAShortLineAndNoLinesIsNoMembers(t *testing.T) {
	f := &execx.Fake{}
	script(f, "lxc-members", "LXCMEMBER\talpha\tcontainer-a\n")
	if _, err := newClient(f).LxcMembers(context.Background()); err == nil {
		t.Fatal("a short line was accepted")
	}
	g := &execx.Fake{}
	script(g, "lxc-members", "")
	got, err := newClient(g).LxcMembers(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestLxcFlagsAreThoseGivenAndNoOther(t *testing.T) {
	m := LxcEntry{
		Team: "alpha", Container: "container-c", User: "agent", Host: "worker", Checkout: "/srv/agent/three",
		Role: "coordinator", Seat: "boxc",
	}
	f := &execx.Fake{}
	script(f, "lxc-add", "CHANGED\tLXC member agent@container-c in team alpha, in /hv.yml\n")
	script(f, "lxc-check", "")
	c := newClient(f)
	ctx := context.Background()
	change, err := c.LxcAdd(ctx, m)
	if err != nil || !change.Changed || !strings.HasPrefix(change.Text, "LXC member agent@container-c") {
		t.Fatalf("%+v %v", change, err)
	}
	want := prefix + " lxc-add --file /hv.yml --team alpha --container container-c --user agent --host worker" +
		" --checkout /srv/agent/three --role coordinator --seat boxc"
	if line := lastLine(t, f); line != want {
		t.Fatalf("ran %q", line)
	}
	if err = c.LxcCheck(ctx, LxcEntry{Role: "worker"}); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != prefix+" lxc-check --file /hv.yml --role worker" {
		t.Fatalf("ran %q", line)
	}
}

func TestLxcAddDryRunReturnsTheEntryLines(t *testing.T) {
	f := &execx.Fake{}
	script(f, "lxc-add", "  - team: alpha\n    container: \"container-c\"\n")
	got, err := newClient(f).LxcAddDryRun(context.Background(), LxcEntry{Team: "alpha"})
	if err != nil || got != "  - team: alpha\n    container: \"container-c\"\n" {
		t.Fatalf("%q %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" lxc-add --file /hv.yml --team alpha --dry-run" {
		t.Fatalf("ran %q", line)
	}
}

func TestLxcMembersReadsADashAsNoHost(t *testing.T) {
	f := &execx.Fake{}
	script(f, "lxc-members", "LXCMEMBER\talpha\tcontainer-a\tagent\t-\tworker\tpresent\n")
	got, err := newClient(f).LxcMembers(context.Background())
	if err != nil || len(got) != 1 || got[0].Host != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestLxcScriptIsAskedForByName(t *testing.T) {
	f := &execx.Fake{}
	script(f, "lxc-script", "import os\nprint(1)\n")
	got, err := newClient(f).LxcScript(context.Background(), "root-read")
	if err != nil || got != "import os\nprint(1)\n" {
		t.Fatalf("%q %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" lxc-script --name root-read" {
		t.Fatalf("ran %q", line)
	}
}

func TestLxcDeriveReadsTheDerivedHandleAndPassesWhatWasRead(t *testing.T) {
	f := &execx.Fake{}
	script(f, "lxc-derive", "DERIVED\tproj\tworker\t2\tproj.2+worker.lxc\n")
	e := LxcEntry{Team: "alpha", Container: "container-a", User: "agent"}
	got, err := newClient(f).LxcDerive(context.Background(), e, `{"path": "/srv/p"}`, "proj.1+worker.lxc=active")
	want := Derived{Repo: "proj", Host: "worker", Seat: "2", Handle: "proj.2+worker.lxc"}
	if err != nil || got != want {
		t.Fatalf("%+v %v", got, err)
	}
	line := prefix + " lxc-derive --file /hv.yml --team alpha --container container-a --user agent" +
		` --probe {"path": "/srv/p"} --roster proj.1+worker.lxc=active`
	if ran := lastLine(t, f); ran != line {
		t.Fatalf("ran %q", ran)
	}
}

func TestLxcDeriveSendsAnEmptyRosterAsAnArgumentAndRefusesAnUnreadableAnswer(t *testing.T) {
	f := &execx.Fake{}
	script(f, "lxc-derive", "DERIVED\tproj\tlocal\t1\tproj.1+local.lxc\n")
	if _, err := newClient(f).LxcDerive(context.Background(), LxcEntry{Team: "alpha"}, "{}", ""); err != nil {
		t.Fatal(err)
	}
	if ran := lastLine(t, f); !strings.HasSuffix(ran, " --probe {} --roster ") {
		t.Fatalf("ran %q", ran)
	}
	g := &execx.Fake{}
	script(g, "lxc-derive", "garbage\n")
	if _, err := newClient(g).LxcDerive(context.Background(), LxcEntry{Team: "alpha"}, "{}", ""); err == nil {
		t.Fatal("an unreadable answer was accepted")
	}
}

func TestLxcCheckoutsReadsThePathAndRepository(t *testing.T) {
	f := &execx.Fake{}
	script(f, "lxc-checkouts", "CHECKOUT\t/srv/agent/one\texample-org/one\nCHECKOUT\t/srv/agent/two\texample-org/two\n")
	got, err := newClient(f).LxcCheckouts(context.Background(), "[]")
	want := []Checkout{{Path: "/srv/agent/one", Repo: "example-org/one"}, {Path: "/srv/agent/two", Repo: "example-org/two"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v %v", got, err)
	}
	if ran := lastLine(t, f); ran != prefix+" lxc-checkouts --file /hv.yml --scan []" {
		t.Fatalf("ran %q", ran)
	}
	g := &execx.Fake{}
	script(g, "lxc-checkouts", "CHECKOUT\t/srv/agent/one\n")
	if _, err = newClient(g).LxcCheckouts(context.Background(), "[]"); err == nil {
		t.Fatal("a short line was accepted")
	}
}

// realCopy is a client over a scratch copy of the test host_vars, which it may edit.
func realCopy(t *testing.T) *Client {
	t.Helper()
	c := realClient(t)
	text, err := os.ReadFile(c.File)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if err = root.WriteFile("localhost.yml", text, 0o600); err != nil {
		t.Fatal(err)
	}
	c.File = filepath.Join(dir, "localhost.yml")
	c.Err = &strings.Builder{}
	return c
}

func TestRealLxcAddListAndRefusals(t *testing.T) {
	c := realCopy(t)
	ctx := context.Background()
	m := LxcEntry{Team: "alpha", Container: "container-a", User: "agent", Host: "worker", Checkout: "/srv/agent/one"}
	if err := c.LxcCheck(ctx, m); err != nil {
		t.Fatalf("a valid member was refused: %v", err)
	}
	if _, err := c.LxcAdd(ctx, m); err != nil {
		t.Fatal(err)
	}
	listed, err := c.LxcMembers(ctx)
	if err != nil || len(listed) != 1 || listed[0].Role != "worker" || listed[0].State != "present" {
		t.Fatalf("%+v %v", listed, err)
	}
	if _, err = c.LxcAdd(ctx, m); !errors.Is(err, ErrUsage) {
		t.Fatalf("a duplicate was not refused as a usage mistake: %v", err)
	}
}

// declareBeta adds a second present team, a copy of alpha, to the scratch host_vars.
func declareBeta(t *testing.T, c *Client) {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(c.File))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	text, err := root.ReadFile(filepath.Base(c.File))
	if err != nil {
		t.Fatal(err)
	}
	all := string(text)
	beta := strings.Replace(all[strings.Index(all, "  - team: alpha"):strings.Index(all, "  - team: old")], "alpha", "beta", 1)
	beta = strings.Replace(beta, "6167", "6168", 1)
	if err = root.WriteFile(filepath.Base(c.File), []byte(all+beta), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRealUserOfOneTeamCanBeCheckedAndAddedToASecond(t *testing.T) {
	c := realCopy(t)
	ctx := context.Background()
	declareBeta(t, c)
	first := LxcEntry{Team: "alpha", Container: "container-a", User: "agent", Host: "worker", Checkout: "/srv/agent/one"}
	if _, err := c.LxcAdd(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := LxcEntry{Team: "beta", Container: "container-a", User: "agent"}
	if err := c.LxcCheck(ctx, second); err != nil {
		t.Fatalf("the user question was refused for a user the container already has in another team: %v", err)
	}
	second.Host = "other"
	if err := c.LxcCheck(ctx, second); !errors.Is(err, ErrUsage) {
		t.Fatalf("a second host for the same user was not refused: %v", err)
	}
	second.Host, second.Checkout = "worker", "/srv/agent/two"
	if _, err := c.LxcAdd(ctx, second); err != nil {
		t.Fatal(err)
	}
	if listed, err := c.LxcMembers(ctx); err != nil || len(listed) != 2 {
		t.Fatalf("%+v %v", listed, err)
	}
}

func TestRealDerivedHandleFromWhatTheReaderPrinted(t *testing.T) {
	c := realCopy(t)
	ctx := context.Background()
	probe := `{"path": "/srv/agent/one", "uid": 1000, "env_uid": 1000, ` +
		`"git_config": "[remote \"origin\"]\n\turl = https://github.com/example-org/proj.git\n", ` +
		`"env_local": "export HOOKS_DAEMON_HOSTNAME=worker\n"}`
	e := LxcEntry{Team: "alpha", Container: "container-a", User: "agent"}
	got, err := c.LxcDerive(ctx, e, probe, "")
	if err != nil || got.Handle != "proj.1+worker.lxc" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err = c.LxcDerive(ctx, e, probe, "proj.1+worker.lxc=active"); err != nil || got.Seat != "2" {
		t.Fatalf("an active seat was given again: %+v %v", got, err)
	}
	if _, err = c.LxcDerive(ctx, e, "{", ""); !errors.Is(err, ErrUsage) {
		t.Fatalf("an unreadable probe was not refused as a usage mistake: %v", err)
	}
	script, err := c.LxcScript(ctx, "root-read")
	if err != nil || !strings.Contains(script, "O_NOFOLLOW") {
		t.Fatalf("the root-side reader: %v", err)
	}
}

func TestRealCheckoutsOfferedAreThoseWithAGitHubRemote(t *testing.T) {
	c := realCopy(t)
	scan := `[{"path": "/srv/a", "git_config": "[remote \"origin\"]\n\turl = git@github.com:Example-Org/A.git\n"},` +
		`{"path": "/srv/b", "git_config": null}]`
	got, err := c.LxcCheckouts(context.Background(), scan)
	if err != nil || !reflect.DeepEqual(got, []Checkout{{Path: "/srv/a", Repo: "example-org/a"}}) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRealLxcCoordinatorRefusedTwice(t *testing.T) {
	c := realCopy(t)
	ctx := context.Background()
	first := LxcEntry{
		Team: "alpha", Container: "container-b", User: "agent", Host: "worker", Role: "coordinator",
		Checkout: "/srv/agent/two",
	}
	if _, err := c.LxcAdd(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := LxcEntry{Team: "alpha", Container: "container-c", User: "agent", Host: "worker", Role: "coordinator"}
	if err := c.LxcCheck(ctx, second); !errors.Is(err, ErrUsage) {
		t.Fatalf("a second coordinator: %v", err)
	}
}
