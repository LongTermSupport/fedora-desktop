package agentbus

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

type rig struct {
	fake *execx.Fake
	out  *bytes.Buffer
	errw *bytes.Buffer
	c    *Client
}

func newRig() *rig {
	f := &execx.Fake{}
	r := &rig{fake: f, out: &bytes.Buffer{}, errw: &bytes.Buffer{}}
	root := &sudo.Sudo{
		Runner: f,
		Term:   &term.Term{Err: &bytes.Buffer{}, IsTerminal: func(any) bool { return false }, Getenv: func(string) string { return "" }},
		Stdin:  strings.NewReader(""),
		Stdout: r.out,
	}
	r.c = &Client{Runner: f, Root: root, Err: r.errw}
	return r
}

func (r *rig) last(t *testing.T) execx.Cmd {
	t.Helper()
	calls := r.fake.Calls()
	if len(calls) == 0 {
		t.Fatal("nothing ran")
	}
	return calls[len(calls)-1]
}

// seatMember is a valid member.json for team alpha (placeholder values only).
const seatMember = `{"protocol": 1, "team": "alpha", "user_id": "@demo.1+box.podman:bus.example.com", ` +
	`"server_name": "bus.example.com", "base_url": "https://bus.example.com", "plain_http_hosts": [], ` +
	`"token_file": "token", "admin": "@admin:bus.example.com", "room": "!AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`

// seatCheckout is a checkout top (its symlinks resolved) with ccy's directory, and a seat
// of alpha for each name, with seatMember unless the name is in bare.
func seatCheckout(t *testing.T, seats []string, bare ...string) string {
	t.Helper()
	top, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(top, ".claude", "ccy"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, seat := range seats {
		dir := filepath.Join(top, ".claude", "ccy", "pingbus", "seats", "alpha", seat)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(bare, seat) {
			if err := os.WriteFile(filepath.Join(dir, "member.json"), []byte(seatMember), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return top
}

// A named checkout is read in-process: no command runs (V30, B-S2).
func TestSeatListReadsANamedCheckoutWithoutAProcess(t *testing.T) {
	r := newRig()
	top := seatCheckout(t, []string{"2", "1"})
	seats, unreadable, err := r.c.SeatList(context.Background(), top)
	want := []Seat{{Team: "alpha", Seat: "1", Handle: "demo.1+box.podman"}, {Team: "alpha", Seat: "2", Handle: "demo.1+box.podman"}}
	if err != nil || unreadable || !slices.Equal(seats, want) {
		t.Fatalf("%+v %v %v", seats, unreadable, err)
	}
	if calls := r.fake.Calls(); len(calls) != 0 || r.errw.Len() != 0 {
		t.Fatalf("ran %v, said %q", calls, r.errw.String())
	}
}

func TestSeatListResolvesANamedCheckoutsSymlinks(t *testing.T) {
	r := newRig()
	top := seatCheckout(t, []string{"1"})
	link := filepath.Join(t.TempDir(), "co")
	if err := os.Symlink(top, link); err != nil {
		t.Fatal(err)
	}
	seats, unreadable, err := r.c.SeatList(context.Background(), link)
	if err != nil || unreadable || len(seats) != 1 {
		t.Fatalf("%+v %v %v", seats, unreadable, err)
	}
}

// V34: an unreadable seat is named on Err as the wrapper's stderr carried it; the others stay.
func TestSeatListNamesEachUnreadableSeat(t *testing.T) {
	r := newRig()
	top := seatCheckout(t, []string{"1", "2"}, "2")
	seats, unreadable, err := r.c.SeatList(context.Background(), top)
	if err != nil || !unreadable || len(seats) != 1 || seats[0].Seat != "1" {
		t.Fatalf("%+v %v %v", seats, unreadable, err)
	}
	want := "agent-bus: seat 2@alpha: team alpha: " + filepath.Join(top, ".claude", "ccy", "pingbus", "seats", "alpha", "2", "member.json") +
		": no bundle (member.json is missing)\n"
	if r.errw.String() != want {
		t.Fatalf("said %q\nwant %q", r.errw.String(), want)
	}
}

func TestSeatListWholeListRefused(t *testing.T) {
	r := newRig()
	top, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seats, unreadable, err := r.c.SeatList(context.Background(), top)
	if err != nil || !unreadable || len(seats) != 0 {
		t.Fatalf("%+v %v %v", seats, unreadable, err)
	}
	if want := "agent-bus: " + top + " has no .claude/ccy/: launch ccy in it once, then again with --teams\n"; r.errw.String() != want {
		t.Fatalf("said %q", r.errw.String())
	}
}

// The uid the checkout must belong to is the user's; a test gives another.
func TestSeatListChecksTheOwner(t *testing.T) {
	r := newRig()
	r.c.uid = func() int { return os.Getuid() + 4242 }
	top := seatCheckout(t, []string{"1"})
	if _, unreadable, err := r.c.SeatList(context.Background(), top); err != nil || !unreadable ||
		!strings.Contains(r.errw.String(), "the checkout must be yours") {
		t.Fatalf("%v %v %q", unreadable, err, r.errw.String())
	}
}

// ListSeats replaces the reader of a resolved top (a test's world syncs its disk there first).
func TestSeatListReadsThroughListSeatsWhenSet(t *testing.T) {
	r := newRig()
	top := seatCheckout(t, []string{"1"})
	var asked []string
	r.c.ListSeats = func(dir string, uid int) (seat.Listing, error) {
		asked = append(asked, dir)
		if uid != os.Getuid() {
			t.Errorf("uid %d", uid)
		}
		return seat.Listing{Seats: []seat.Seat{{Team: "beta", Seat: "9", Held: true, Handle: "h"}}}, nil
	}
	seats, unreadable, err := r.c.SeatList(context.Background(), top)
	if err != nil || unreadable || !slices.Equal(seats, []Seat{{Team: "beta", Seat: "9", Held: true, Handle: "h"}}) ||
		!slices.Equal(asked, []string{top}) {
		t.Fatalf("%+v %v %v asked %v", seats, unreadable, err, asked)
	}
}

// What v1 crashed on (SPEC-view B-S3) is an error, not an unreadable list.
func TestSeatListCrashIsAnError(t *testing.T) {
	r := newRig()
	top := seatCheckout(t, nil)
	if err := os.WriteFile(filepath.Join(top, ".claude", "ccy", "pingbus"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if seats, unreadable, err := r.c.SeatList(context.Background(), top); err == nil || unreadable || seats != nil {
		t.Fatalf("%+v %v %v", seats, unreadable, err)
	}
}

func TestSeatListWorkingDirectoryAsksGitOnce(t *testing.T) {
	r := newRig()
	top := seatCheckout(t, []string{"1"})
	r.fake.On([]string{"git", "rev-parse", "--show-toplevel"}, execx.Result{Stdout: []byte(top + "\n")})
	seats, unreadable, err := r.c.SeatList(context.Background(), "")
	if err != nil || unreadable || len(seats) != 1 {
		t.Fatalf("%+v %v %v", seats, unreadable, err)
	}
	if calls := r.fake.Calls(); len(calls) != 1 || calls[0].Dir != "" {
		t.Fatalf("ran %+v", calls)
	}
}

func TestSeatListOutsideAGitCheckout(t *testing.T) {
	r := newRig()
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	r.fake.On([]string{"git", "rev-parse", "--show-toplevel"}, execx.Result{ExitCode: 128, Stderr: []byte("fatal: not a git repository\n")})
	seats, unreadable, err := r.c.SeatList(context.Background(), "")
	if err != nil || !unreadable || len(seats) != 0 {
		t.Fatalf("%+v %v %v", seats, unreadable, err)
	}
	want := "agent-bus: " + cwd + " is not in a git checkout: run agent-bus seat in the ccy checkout (fatal: not a git repository)\n"
	if r.errw.String() != want {
		t.Fatalf("said %q", r.errw.String())
	}
}

func TestSeatListWithNoErrWriter(t *testing.T) {
	r := newRig()
	r.c.Err = nil
	top := seatCheckout(t, []string{"1", "2"}, "2")
	if seats, unreadable, err := r.c.SeatList(context.Background(), top); err != nil || !unreadable || len(seats) != 1 {
		t.Fatalf("%+v %v %v", seats, unreadable, err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stderr closed") }

func TestSeatListUnwritableErrIsAnError(t *testing.T) {
	r := newRig()
	r.c.Err = failingWriter{}
	top := seatCheckout(t, []string{"1", "2"}, "2")
	if _, _, err := r.c.SeatList(context.Background(), top); err == nil {
		t.Fatal("no error")
	}
}

func TestSeatRemove(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "remove", "1@alpha"}, execx.Result{})
	if err := r.c.SeatRemove(context.Background(), "/co", "1@alpha"); err != nil {
		t.Fatal(err)
	}
	if c := r.last(t); c.Line() != "agent-bus seat remove 1@alpha" || c.Dir != "/co" {
		t.Fatalf("ran %q in %q", c.Line(), c.Dir)
	}
	r.fake.On([]string{"agent-bus", "seat", "remove"}, execx.Result{ExitCode: 77})
	if err := r.c.SeatRemove(context.Background(), "/co", "1@alpha"); !errors.Is(err, ErrNotRoot) {
		t.Fatalf("got %v", err)
	}
}

func TestListUsesTheAbsoluteWrapperThroughAsRoot(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "list", "alpha"},
		execx.Result{Stdout: []byte("HUMAN\towner\tjoin\nMEMBER\th.podman\tworker\tjoin\tactive\n")})
	roster, err := r.c.List(context.Background(), "alpha")
	if err != nil || len(roster.Humans) != 1 || len(roster.Members) != 1 {
		t.Fatalf("%+v %v", roster, err)
	}
	if line := r.last(t).Line(); line != "sudo -n /usr/local/bin/agent-bus list alpha" {
		t.Fatalf("ran %q", line)
	}
}

func TestListPropagatesTheAsRootError(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"sudo"}, execx.Result{ExitCode: 70, Stderr: []byte("no\n")})
	if _, err := r.c.List(context.Background(), "alpha"); !errors.Is(err, ErrRefused) {
		t.Fatalf("got %v", err)
	}
}

func TestParkMemberShowsWhatTheWrapperSaid(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "park-member", "alpha", "h.podman"},
		execx.Result{Stdout: []byte("CHANGED\tparked h.podman\n")})
	if err := r.c.ParkMember(context.Background(), "alpha", "h.podman"); err != nil {
		t.Fatal(err)
	}
	if r.out.String() != "CHANGED\tparked h.podman\n" {
		t.Fatalf("out %q", r.out.String())
	}
}

func TestSeatChooseRunsAsTheUserAndParsesWhatItOffers(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "choose", "alpha"},
		execx.Result{Stdout: []byte("CHOICE\t1\tfree\th.1+b.podman\tw\nDEFAULT\t1\n")})
	got, err := r.c.SeatChoose(context.Background(), "alpha")
	if err != nil || got.Default != "1" || len(got.Rows) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if c := r.last(t); c.Line() != "agent-bus seat choose alpha" {
		t.Fatalf("ran %q", c.Line())
	}
}

func TestSeatChooseAsAsksForTheMemberTypeOfTheLauncher(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "choose", "alpha", "--type=host"},
		execx.Result{Stdout: []byte("CHOICE\t1\tfree\th.1+b.host\tw\nDEFAULT\t1\n")})
	got, err := r.c.SeatChooseAs(context.Background(), "alpha", "host")
	if err != nil || got.Default != "1" {
		t.Fatalf("%+v %v", got, err)
	}
	if c := r.last(t); c.Line() != "agent-bus seat choose alpha --type=host" {
		t.Fatalf("ran %q", c.Line())
	}
}

func TestSeatChooseFailureIsTheWrappersExit(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "choose", "alpha"}, execx.Result{ExitCode: 78, Stderr: []byte("not running")})
	if _, err := r.c.SeatChoose(context.Background(), "alpha"); !errors.Is(err, ErrConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestSetRoleRunsSetRoleAsRootAndShowsWhatItSaid(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "set-role", "alpha", "h.podman", "--role=coordinator"},
		execx.Result{Stdout: []byte("CHANGED\th.podman is now coordinator\n")})
	if err := r.c.SetRole(context.Background(), "alpha", "h.podman", RoleCoordinator); err != nil {
		t.Fatal(err)
	}
	if r.out.String() != "CHANGED\th.podman is now coordinator\n" {
		t.Fatalf("out %q", r.out.String())
	}
}

func TestSetRoleRefusesARoleThatIsNotOne(t *testing.T) {
	r := newRig()
	if err := r.c.SetRole(context.Background(), "alpha", "h.podman", "boss"); err == nil {
		t.Fatal("accepted boss")
	}
	if len(r.fake.Calls()) != 0 {
		t.Fatal("ran sudo for a role agent-bus would refuse")
	}
}

func TestHumanDevices(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "human", "devices", "alpha", "owner"},
		execx.Result{Stdout: []byte("DEVICE\tD1\t5\nDEVICE\tD2\t-\n")})
	devices, err := r.c.HumanDevices(context.Background(), "alpha", "owner")
	if err != nil || len(devices) != 2 {
		t.Fatalf("%+v %v", devices, err)
	}
}

func TestHumanPasswordIsTheFirstLine(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "human", "password", "alpha", "owner"},
		execx.Result{Stdout: []byte("s3cret-pw\n")})
	pw, err := r.c.HumanPassword(context.Background(), "alpha", "owner")
	if err != nil || pw != "s3cret-pw" {
		t.Fatalf("%q %v", pw, err)
	}
	r.fake.On([]string{"sudo"}, execx.Result{Stdout: []byte("\n")})
	if _, err := r.c.HumanPassword(context.Background(), "alpha", "owner"); err == nil {
		t.Fatal("accepted an empty password")
	}
}

func TestInstallerCalls(t *testing.T) {
	const installer = "/usr/local/sbin/agent-bus-install"
	r := newRig()
	r.fake.On([]string{"sudo", "-n", installer, "backups", "--team", "alpha"},
		execx.Result{Stdout: []byte("BACKUP\t1\tt1\n")})
	backups, err := r.c.Backups(context.Background(), "alpha")
	if err != nil || len(backups) != 1 {
		t.Fatalf("%+v %v", backups, err)
	}
	r.fake.On([]string{"sudo"}, execx.Result{Stdout: []byte("done\n")})
	steps := []struct {
		run  func() error
		want []string
	}{
		{
			func() error { return r.c.BackupNow(context.Background(), "alpha") },
			[]string{"sudo", "-n", installer, "backup-now", "--team", "alpha"},
		},
		{
			func() error { return r.c.Restore(context.Background(), "alpha", "3") },
			[]string{"sudo", "-n", installer, "restore", "--team", "alpha", "--backup", "3"},
		},
		{
			func() error { return r.c.RemoveTeam(context.Background(), "alpha", false) },
			[]string{"sudo", "-n", installer, "remove", "--team", "alpha"},
		},
		{
			func() error { return r.c.RemoveTeam(context.Background(), "alpha", true) },
			[]string{"sudo", "-n", installer, "remove", "--team", "alpha", "--purge"},
		},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			t.Fatal(err)
		}
		c := r.last(t)
		if got := append([]string{c.Name}, c.Args...); !slices.Equal(got, s.want) {
			t.Fatalf("ran %v, want %v", got, s.want)
		}
	}
	if r.out.String() != "done\ndone\ndone\ndone\n" {
		t.Fatalf("out %q", r.out.String())
	}
}

func TestInstallerFailureIsTyped(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"sudo"}, execx.Result{ExitCode: 1, Stderr: []byte("boom\n")})
	err := r.c.BackupNow(context.Background(), "alpha")
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("got %v", err)
	}
	if exit, ok := errors.AsType[*execx.ExitError](err); !ok || exit.Code != 1 {
		t.Fatal("the underlying exit error is lost")
	}
}

func TestEveryWrapperExitCodeHasAKind(t *testing.T) {
	kinds := map[int]error{
		64: ErrUsage, 69: ErrUnreachable, 70: ErrRefused, 75: ErrBusy, 77: ErrNotRoot, 78: ErrConfig, 1: ErrFailed,
	}
	for code, want := range kinds {
		err := (&CommandError{Tool: "agent-bus", Code: code}).Is
		if !err(want) {
			t.Fatalf("code %d is not %v", code, want)
		}
	}
	if (&CommandError{Code: 64}).Is(ErrRefused) {
		t.Fatal("64 matched ErrRefused")
	}
}

func TestStartFailureIsNotACommandError(t *testing.T) {
	r := newRig()
	err := r.c.SeatRemove(context.Background(), "/co", "1@alpha")
	if !errors.Is(err, execx.ErrUnexpected) {
		t.Fatalf("got %v", err)
	}
	if cmdErr, ok := errors.AsType[*CommandError](err); ok {
		t.Fatalf("a command that did not run is not a CommandError: %v", cmdErr)
	}
}

func TestRootShowStreamsInsteadOfCapturing(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"sudo"}, execx.Result{Stdout: []byte("progress\n")})
	if err := r.c.BackupNow(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if c := r.last(t); c.Stdout != r.c.Root.Stdout {
		t.Fatalf("the root command's stdout was captured, not streamed: %v", c.Stdout)
	}
}

func TestRootPathsAreSudosOwn(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"sudo"}, execx.Result{})
	if err := r.c.RemoveTeam(context.Background(), "alpha", false); err != nil {
		t.Fatal(err)
	}
	if c := r.last(t); c.Args[1] != sudo.InstallTool {
		t.Fatalf("ran %q", c.Line())
	}
}

func TestErrorTextCarriesStderr(t *testing.T) {
	f := &CommandError{Tool: "agent-bus", Code: 70, Stderr: " nope \n"}
	if f.Error() != "agent-bus exited 70: nope" {
		t.Fatalf("%q", f.Error())
	}
	if (&CommandError{Tool: "x", Code: 1}).Error() != "x exited 1" {
		t.Fatal("bare error text")
	}
}

func TestIsCommandErrorIsAToolThatRanAndRefused(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "list"}, execx.Result{ExitCode: 78, Stderr: []byte("boom")})
	_, err := r.c.List(context.Background(), "alpha")
	if !IsCommandError(err) {
		t.Fatalf("a tool that exited non-zero: %v", err)
	}
	if IsCommandError(errors.New("could not start")) || IsCommandError(nil) {
		t.Fatal("only a CommandError is one")
	}
}

func TestSeatChooseInRunsInTheCheckout(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "choose", "alpha", "--type=podman"},
		execx.Result{Stdout: []byte("CHOICE\t2\tnew\th.2+b.podman\tw\nDEFAULT\t2\n")})
	got, err := r.c.SeatChooseIn(context.Background(), "/co", "alpha", "podman")
	if err != nil || got.Default != "2" {
		t.Fatalf("%+v %v", got, err)
	}
	if c := r.last(t); c.Dir != "/co" {
		t.Fatalf("ran in %q", c.Dir)
	}
}

func TestSeatTakeRunsAsTheUserInTheCheckout(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "take"}, execx.Result{})
	if err := r.c.SeatTake(context.Background(), "/co", "2@alpha", "podman", true); err != nil {
		t.Fatal(err)
	}
	if c := r.last(t); c.Line() != "agent-bus seat take 2@alpha --type=podman --no-prompt" || c.Dir != "/co" {
		t.Fatalf("ran %q in %q", c.Line(), c.Dir)
	}
	r.fake.On([]string{"agent-bus", "seat", "take"}, execx.Result{ExitCode: 75})
	if err := r.c.SeatTake(context.Background(), "/co", "2@alpha", "podman", false); !errors.Is(err, ErrBusy) {
		t.Fatalf("a held seat: %v", err)
	}
}

func TestSeatTakeAllocatedSaysTheCallerHoldsTheAllocationLock(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "take"}, execx.Result{})
	if err := r.c.SeatTakeAllocated(context.Background(), "/co", "2@alpha", "podman", true); err != nil {
		t.Fatal(err)
	}
	if c := r.last(t); c.Line() != "agent-bus seat take 2@alpha --type=podman --no-prompt --alloc-held" {
		t.Fatalf("ran %q", c.Line())
	}
}

// A root read that fails is that failure, and one whose lines are malformed names the command.
func TestRootReadsFailOnAFailedRunAndOnMalformedLines(t *testing.T) {
	reads := []struct {
		name, line string
		read       func(*Client) error
	}{
		{"list", "MEMBER\tx\n", func(c *Client) error { _, err := c.List(context.Background(), "alpha"); return err }},
		{"human devices", "DEVICE\tx\n", func(c *Client) error {
			_, err := c.HumanDevices(context.Background(), "alpha", "owner")
			return err
		}},
		{"backups", "BACKUP\tx\n", func(c *Client) error { _, err := c.Backups(context.Background(), "alpha"); return err }},
	}
	for _, read := range reads {
		r := newRig()
		r.fake.On([]string{"sudo"}, execx.Result{ExitCode: 70, Stderr: []byte("refused\n")})
		if err := read.read(r.c); !errors.Is(err, ErrRefused) {
			t.Fatalf("%s refused: %v", read.name, err)
		}
		r = newRig()
		r.fake.On([]string{"sudo"}, execx.Result{Stdout: []byte(read.line)})
		if err := read.read(r.c); err == nil || !strings.Contains(err.Error(), read.name) {
			t.Fatalf("%s malformed: %v", read.name, err)
		}
	}
}

func TestWithStderrSendsEachStderrThroughWrapAndLeavesTheClientAlone(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "choose"}, execx.Result{ExitCode: 69, Stderr: []byte("down\n")})
	r.fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "list", "alpha"}, execx.Result{ExitCode: 70, Stderr: []byte("refused\n")})
	rootErr, ok := r.c.Root.Term.Err.(*bytes.Buffer)
	if !ok {
		t.Fatal("the rig's root terminal is a buffer")
	}
	var user, root bytes.Buffer
	instead := map[io.Writer]io.Writer{r.errw: &user, rootErr: &root}
	c := r.c.WithStderr(func(w io.Writer) io.Writer { return instead[w] })
	if _, err := c.SeatChooseIn(context.Background(), "/co", "alpha", ""); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("seat choose: %v", err)
	}
	// The seats are read in-process, and what the reader says of an unreadable seat is the
	// user's stderr too.
	top := seatCheckout(t, []string{"1"}, "1")
	if _, unreadable, err := c.SeatList(context.Background(), top); err != nil || !unreadable {
		t.Fatalf("seat list: %v %v", unreadable, err)
	}
	if _, err := c.List(context.Background(), "alpha"); !errors.Is(err, ErrRefused) {
		t.Fatalf("list: %v", err)
	}
	if want := "down\nagent-bus: seat 1@alpha: team alpha: " +
		filepath.Join(top, ".claude", "ccy", "pingbus", "seats", "alpha", "1", "member.json") +
		": no bundle (member.json is missing)\n"; user.String() != want || root.String() != "refused\n" {
		t.Fatalf("user stderr %q, root stderr %q", user.String(), root.String())
	}
	if r.errw.Len() != 0 || rootErr.Len() != 0 {
		t.Fatalf("the original streams were written: %q %q", r.errw.String(), rootErr.String())
	}
	if r.c.Err != r.errw || r.c.Root.Term.Err != rootErr {
		t.Fatal("the original client was changed")
	}
}

func TestWithStderrKeepsACapturedStderrCaptured(t *testing.T) {
	r := newRig()
	r.c.Err = nil
	r.fake.On([]string{"agent-bus", "seat", "choose"}, execx.Result{ExitCode: 69, Stderr: []byte("down\n")})
	c := r.c.WithStderr(func(w io.Writer) io.Writer {
		if w == nil {
			t.Error("wrap was given the nil stream")
		}
		return w
	})
	_, err := c.SeatChooseIn(context.Background(), "/co", "alpha", "")
	if cmdErr, ok := errors.AsType[*CommandError](err); !ok || cmdErr.Stderr != "down\n" {
		t.Fatalf("a nil Err still captures the stderr into the error: %v", err)
	}
}
