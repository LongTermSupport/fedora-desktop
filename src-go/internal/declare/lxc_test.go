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

func TestLxcRemoveAndSuggestHost(t *testing.T) {
	f := &execx.Fake{}
	script(f, "lxc-remove", "UNCHANGED\tagent@container-a in team alpha\n")
	script(f, "lxc-suggest-host", "container-a\n")
	c := newClient(f)
	ctx := context.Background()
	change, err := c.LxcRemove(ctx, "alpha", "container-a", "agent")
	if err != nil || change.Changed {
		t.Fatalf("%+v %v", change, err)
	}
	if line := lastLine(t, f); line != prefix+" lxc-remove --file /hv.yml --team alpha --container container-a --user agent" {
		t.Fatalf("ran %q", line)
	}
	host, err := c.LxcSuggestHost(ctx, "container-a")
	if err != nil || host != "container-a" {
		t.Fatalf("%q %v", host, err)
	}
	if line := lastLine(t, f); line != prefix+" lxc-suggest-host --file /hv.yml --container container-a" {
		t.Fatalf("ran %q", line)
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

func TestRealLxcCoordinatorRemoveAndHost(t *testing.T) {
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
	if change, err := c.LxcRemove(ctx, "alpha", "container-b", "agent"); err != nil || !change.Changed {
		t.Fatalf("%+v %v", change, err)
	}
	if err := c.LxcCheck(ctx, second); err != nil {
		t.Fatalf("the coordinator is absent, so another may be: %v", err)
	}
	if host, err := c.LxcSuggestHost(ctx, "Container_B"); err != nil || host != "container-b" {
		t.Fatalf("%q %v", host, err)
	}
}
