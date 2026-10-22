package seat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func uid() int { return os.Getuid() }

func mustList(t *testing.T, top string) Listing {
	t.Helper()
	listing, err := List(top, uid())
	if err != nil {
		t.Fatal(err)
	}
	return listing
}

func wantRefusal(t *testing.T, err error, want string) {
	t.Helper()
	refusal, ok := errors.AsType[*RefusalError](err)
	if !ok || refusal.Msg != want || refusal.Error() != want {
		t.Fatalf("got %v\nwant refusal %q", err, want)
	}
}

func wantCrash(t *testing.T, err error) {
	t.Helper()
	if err == nil || isRefusal(err) {
		t.Fatalf("got %v, want a crash-class error", err)
	}
}

var orderedRefs = [][2]string{{"b9", "1"}, {"alpha-b", "dev"}, {"alpha", "2"}, {"alpha", "10"}, {"alpha", "1"}, {"alpha", "dev"}}

func TestListIsByTeamThenSeatInNameOrder(t *testing.T) {
	top := newCheckout(t)
	for _, ref := range orderedRefs {
		fx{t}.addMember(top, ref[0], ref[1])
	}
	got := mustList(t, top)
	order := make([]string, 0, len(got.Seats))
	for _, s := range got.Seats {
		if s.Handle != testHandle || s.Held {
			t.Errorf("seat %+v", s)
		}
		order = append(order, s.Seat+"@"+s.Team)
	}
	want := []string{"1@alpha", "10@alpha", "2@alpha", "dev@alpha", "dev@alpha-b", "1@b9"}
	if !reflect.DeepEqual(order, want) || len(got.Failures) != 0 {
		t.Fatalf("got %v %q, want %v", order, got.Failures, want)
	}
}

func TestListHeldSeat(t *testing.T) {
	f := fx{t}
	top := newCheckout(t)
	dir := f.addMember(top, testTeam, "1")
	f.addMember(top, testTeam, "2")
	f.writeFile(filepath.Join(dir, lockFile), "seat 1")
	holdLock(t, filepath.Join(dir, lockFile), "-x")
	got := mustList(t, top)
	want := []Seat{{Team: testTeam, Seat: "1", Held: true, Handle: testHandle}, {Team: testTeam, Seat: "2", Handle: testHandle}}
	if !reflect.DeepEqual(got.Seats, want) {
		t.Fatalf("got %+v", got.Seats)
	}
}

// V34: a seat whose bundle or lock is refused is left out and named; the others stay.
func TestListNamesEachUnreadableSeat(t *testing.T) {
	f := fx{t}
	top := newCheckout(t)
	f.addMember(top, testTeam, "1")
	two := f.addSeat(top, testTeam, "2")
	three := f.addMember(top, testTeam, "3")
	f.symlink("elsewhere", filepath.Join(three, lockFile))
	four := f.addSeat(top, testTeam, "4") // both refused: the bundle is named
	f.symlink("elsewhere", filepath.Join(four, lockFile))
	got := mustList(t, top)
	if len(got.Seats) != 1 || got.Seats[0].Seat != "1" {
		t.Fatalf("seats %+v", got.Seats)
	}
	want := []string{
		"seat 2@alpha: team alpha: " + filepath.Join(two, memberFile) + ": no bundle (member.json is missing)",
		"seat 3@alpha: " + filepath.Join(three, lockFile) + " is a symlink",
		"seat 4@alpha: team alpha: " + filepath.Join(four, memberFile) + ": no bundle (member.json is missing)",
	}
	if !reflect.DeepEqual(got.Failures, want) {
		t.Fatalf("failures %q\nwant %q", got.Failures, want)
	}
}

func TestListNoSeatsDirectoryIsNoSeats(t *testing.T) {
	top := newCheckout(t)
	if got := mustList(t, top); len(got.Seats)+len(got.Failures) != 0 {
		t.Fatalf("got %+v", got)
	}
	fx{t}.mkdirs(filepath.Join(seatsRoot(top), testTeam)) // a team with no seats
	if got := mustList(t, top); len(got.Seats)+len(got.Failures) != 0 {
		t.Fatalf("got %+v", got)
	}
}

const noCCY = " has no .claude/ccy/: launch ccy in it once, then again with --teams"

func TestListCheckoutRefusals(t *testing.T) {
	ccy := func(top string) string { return filepath.Join(top, ".claude", "ccy") }
	for _, c := range []struct {
		about string
		build func(f fx, top string) string // returns what List is given
		uid   int
		want  string // %s is the top given
	}{
		{"top a symlink", func(f fx, top string) string {
			f.symlink(top, top+".link")
			return top + ".link"
		}, uid(), "%s is a symlink; the checkout must be a real directory"},
		{"top a file", func(f fx, top string) string {
			f.writeFile(top+".file", "")
			return top + ".file"
		}, uid(), "%s is not a directory; the checkout must be one"},
		{
			"top someone else's", func(_ fx, top string) string { return top }, uid() + 4242,
			fmt.Sprintf("%%s is owned by uid %d, not %d; the checkout must be yours", uid(), uid()+4242),
		},
		{"top missing", func(_ fx, top string) string { return top + ".gone" }, uid(), "%s" + noCCY},
		{"no .claude", func(f fx, top string) string {
			f.mkdirs(top + ".bare")
			return top + ".bare"
		}, uid(), "%s" + noCCY},
		{"ccy a symlink", func(f fx, top string) string {
			f.rename(ccy(top), ccy(top)+".real")
			f.symlink("ccy.real", ccy(top))
			return top
		}, uid(), "%s/.claude/ccy is a symlink; ccy's directory must be a real directory"},
		{"ccy a file", func(f fx, top string) string {
			f.remove(ccy(top))
			f.writeFile(ccy(top), "")
			return top
		}, uid(), "%s/.claude/ccy is not a directory; ccy's directory must be one"},
	} {
		t.Run(c.about, func(t *testing.T) {
			given := c.build(fx{t}, newCheckout(t))
			_, err := List(given, c.uid)
			wantRefusal(t, err, strings.ReplaceAll(c.want, "%s", given))
		})
	}
}

func TestListCCYOfAnotherUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to give the directory away")
	}
	top := newCheckout(t)
	ccy := filepath.Join(top, ".claude", "ccy")
	fx{t}.check(os.Chown(ccy, 4242, -1))
	_, err := List(top, 0)
	wantRefusal(t, err, ccy+" is owned by uid 4242, not 0; ccy's directory must be yours")
}

// treeCase is a seats tree that refuses the whole list; want's %s is the seats root.
type treeCase struct {
	about string
	build func(f fx, root string)
	want  string
}

var treeCases = []treeCase{
	{"seats a symlink", func(f fx, root string) {
		f.mkdirs(root + ".real")
		f.symlink("seats.real", root)
	}, "%s is not a directory (a symlink is refused)"},
	{"seats a file", func(f fx, root string) {
		f.mkdirs(filepath.Dir(root))
		f.writeFile(root, "")
	}, "%s is not a directory (a symlink is refused)"},
	{"a team folder that is not a team name, sorted first", func(f fx, root string) {
		f.mkdirs(filepath.Join(root, "alpha", "1"))
		f.mkdirs(filepath.Join(root, "Bad"))
	}, "%s/Bad is not a team's seats (not a team name)"},
	{"a team name too long", func(f fx, root string) {
		f.mkdirs(filepath.Join(root, "a"+strings.Repeat("b", 24)))
	}, "%s/a" + strings.Repeat("b", 24) + " is not a team's seats (not a team name)"},
	{
		"a team folder that is undecodable", func(f fx, root string) { f.mkdirs(filepath.Join(root, "a\xff")) },
		`%s/a\udcff is not a team's seats (not a team name)`,
	},
	{"names sorted by code point", func(f fx, root string) {
		f.mkdirs(filepath.Join(root, "\xee\x80\x80"))
		f.mkdirs(filepath.Join(root, "\xff"))
	}, `%s/\udcff is not a team's seats (not a team name)`},
	{"a team symlink", func(f fx, root string) {
		f.mkdirs(filepath.Join(root, "real"))
		f.symlink("real", filepath.Join(root, "alpha"))
	}, "%s/alpha is not a directory (a symlink is refused)"},
	{"a team file", func(f fx, root string) {
		f.mkdirs(root)
		f.writeFile(filepath.Join(root, "alpha"), "")
	}, "%s/alpha is not a directory (a symlink is refused)"},
	{
		"a seat folder that is not a seat name", func(f fx, root string) { f.mkdirs(filepath.Join(root, "alpha", "0")) },
		"%s/alpha/0 is not a seat (not a seat name)",
	},
	{"a seat symlink", func(f fx, root string) {
		f.mkdirs(filepath.Join(root, "alpha", "dev"))
		f.symlink("dev", filepath.Join(root, "alpha", "devb"))
	}, "%s/alpha/devb is not a directory (a symlink is refused)"},
	{"a seat file", func(f fx, root string) {
		f.mkdirs(filepath.Join(root, "alpha"))
		f.writeFile(filepath.Join(root, "alpha", "1"), "")
	}, "%s/alpha/1 is not a directory (a symlink is refused)"},
	{"a refusal after good seats", func(f fx, root string) {
		f.mkdirs(filepath.Join(root, "alpha", "1"))
		f.mkdirs(filepath.Join(root, "zz", "-"))
	}, "%s/zz/- is not a seat (not a seat name)"},
}

func TestListTreeRefusals(t *testing.T) {
	for _, c := range treeCases {
		t.Run(c.about, func(t *testing.T) {
			top := newCheckout(t)
			c.build(fx{t}, seatsRoot(top))
			_, err := List(top, uid())
			wantRefusal(t, err, strings.ReplaceAll(c.want, "%s", seatsRoot(top)))
		})
	}
}

// crashTrees are checkouts on which v1 raised an exception it did not catch.
var crashTrees = []struct {
	about string
	build func(f fx, top string)
}{
	{".claude a file", func(f fx, top string) {
		f.remove(filepath.Join(top, ".claude"))
		f.writeFile(filepath.Join(top, ".claude"), "")
	}},
	{"pingbus a file", func(f fx, top string) { f.writeFile(filepath.Join(top, ".claude", "ccy", "pingbus"), "") }},
	{"a seat lock that is a socket", func(f fx, top string) { f.mksock(filepath.Join(f.addMember(top, testTeam, "1"), lockFile)) }},
	{"member.json nested too deep", func(f fx, top string) {
		f.writeFile(filepath.Join(f.addSeat(top, testTeam, "1"), memberFile), crashDocs()[0])
	}},
	{"a base_url urlsplit refuses", func(f fx, top string) {
		f.addMember(top, testTeam, "1", with("base_url", js("https://[192.0.2.1]")))
	}},
}

func TestListCrashes(t *testing.T) {
	for _, c := range crashTrees {
		t.Run(c.about, func(t *testing.T) {
			top := newCheckout(t)
			c.build(fx{t}, top)
			_, err := List(top, uid())
			wantCrash(t, err)
		})
	}
}

// gitStub answers TopLevel; any call is recorded.
type gitStub struct {
	top   string
	err   error
	calls []string
}

func (g *gitStub) TopLevel(_ context.Context, dir string) (string, error) {
	g.calls = append(g.calls, dir)
	return g.top, g.err
}

// B-S2: a named checkout is read at that folder, its symlinks resolved, and git is not asked.
func TestCheckoutNamedResolvesSymlinksWithoutGit(t *testing.T) {
	top := realTemp(t)
	link := filepath.Join(realTemp(t), "co")
	fx{t}.symlink(top, link)
	git := &gitStub{err: errors.New("git must not run")}
	got, err := Checkout(context.Background(), git, link)
	if err != nil || got != top || len(git.calls) != 0 {
		t.Fatalf("got %q, %v, git %q", got, err, git.calls)
	}
}

func TestCheckoutNamedRelative(t *testing.T) {
	parent := realTemp(t)
	fx{t}.mkdirs(filepath.Join(parent, "co"))
	t.Chdir(parent)
	got, err := Checkout(context.Background(), &gitStub{}, "co")
	if err != nil || got != filepath.Join(parent, "co") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCheckoutNamedMissingIsACrash(t *testing.T) {
	_, err := Checkout(context.Background(), &gitStub{}, filepath.Join(realTemp(t), "gone"))
	wantCrash(t, err)
}

func TestCheckoutWorkingDirectoryAsksGitOnce(t *testing.T) {
	git := &gitStub{top: "/somewhere/top"}
	got, err := Checkout(context.Background(), git, "")
	if err != nil || got != "/somewhere/top" || !reflect.DeepEqual(git.calls, []string{""}) {
		t.Fatalf("got %q, %v, git %q", got, err, git.calls)
	}
}

func TestCheckoutOutsideAGitCheckout(t *testing.T) {
	cwd := realTemp(t)
	t.Chdir(cwd)
	git := &gitStub{err: &execx.ExitError{Name: "git", Code: 128, Stderr: "fatal: not a git repository (or any of the parent directories): .git\n"}}
	_, err := Checkout(context.Background(), git, "")
	wantRefusal(t, err, cwd+" is not in a git checkout: run agent-bus seat in the ccy checkout "+
		"(fatal: not a git repository (or any of the parent directories): .git)")
}

func TestCheckoutWithoutGit(t *testing.T) {
	_, err := Checkout(context.Background(), &gitStub{err: &execx.NotInstalledError{Name: "git"}}, "")
	wantRefusal(t, err, "git is not installed: a checkout cannot be found")
}

func TestCheckoutGitThatCannotStartIsACrash(t *testing.T) {
	_, err := Checkout(context.Background(), &gitStub{err: errors.New("fork: no memory")}, "")
	wantCrash(t, err)
}
