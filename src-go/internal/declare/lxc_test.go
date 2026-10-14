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

func TestLxcSuggestSeatReadsDefaultOrNeededWithASuggestion(t *testing.T) {
	cases := map[string]struct {
		out  string
		want SeatAdvice
	}{
		"default":                {out: "SEAT\tdefault\n", want: SeatAdvice{}},
		"needed with suggestion": {out: "SEAT\tneeded\tc2024box\n", want: SeatAdvice{Needed: true, Suggestion: "c2024box"}},
		"needed with none":       {out: "SEAT\tneeded\t\n", want: SeatAdvice{Needed: true}},
	}
	for name, tc := range cases {
		f := &execx.Fake{}
		script(f, "lxc-suggest-seat", tc.out)
		got, err := newClient(f).LxcSuggestSeat(context.Background(), "alpha", "2024box")
		if err != nil || got != tc.want {
			t.Fatalf("%s: %+v %v", name, got, err)
		}
		if line := lastLine(t, f); line != prefix+" lxc-suggest-seat --file /hv.yml --team alpha --container 2024box" {
			t.Fatalf("%s: ran %q", name, line)
		}
	}
	f := &execx.Fake{}
	script(f, "lxc-suggest-seat", "garbage\n")
	if _, err := newClient(f).LxcSuggestSeat(context.Background(), "alpha", "box"); err == nil {
		t.Fatal("an unreadable answer was accepted")
	}
}

func TestLxcCheckSeatPassesTheSeatToBeJudged(t *testing.T) {
	f := &execx.Fake{}
	script(f, "lxc-suggest-seat", "")
	if err := newClient(f).LxcCheckSeat(context.Background(), "alpha", "box", "boxc"); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != prefix+" lxc-suggest-seat --file /hv.yml --team alpha --container box --seat boxc" {
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

func TestRealSeatAdviceForANameWithNoSeatAndForACollision(t *testing.T) {
	c := realCopy(t)
	ctx := context.Background()
	advice, err := c.LxcSuggestSeat(ctx, "alpha", "2024box")
	if err != nil || !advice.Needed || advice.Suggestion != "c2024box" {
		t.Fatalf("%+v %v", advice, err)
	}
	if err = c.LxcCheckSeat(ctx, "alpha", "2024box", "c2024box"); err != nil {
		t.Fatal(err)
	}
	held := LxcEntry{
		Team: "alpha", Container: "longcontainername-one", User: "agent", Host: "worker", Checkout: "/srv/agent/one",
	}
	if _, err = c.LxcAdd(ctx, held); err != nil {
		t.Fatal(err)
	}
	advice, err = c.LxcSuggestSeat(ctx, "alpha", "longcontainername-two")
	if err != nil || !advice.Needed || advice.Suggestion != "longcontain2" {
		t.Fatalf("%+v %v", advice, err)
	}
	if err = c.LxcCheckSeat(ctx, "alpha", "longcontainername-two", "longcontaine"); !errors.Is(err, ErrUsage) {
		t.Fatalf("a taken seat was accepted: %v", err)
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
