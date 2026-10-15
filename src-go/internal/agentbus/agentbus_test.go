package agentbus

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

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

func TestSeatListRunsAsTheUserInTheCheckout(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "list"},
		execx.Result{Stdout: []byte("SEAT\talpha\t1\tfree\t-\th\n")})
	seats, unreadable, err := r.c.SeatList(context.Background(), "/co")
	if err != nil || unreadable || len(seats) != 1 {
		t.Fatalf("%+v %v %v", seats, unreadable, err)
	}
	if c := r.last(t); c.Line() != "agent-bus seat list" || c.Dir != "/co" {
		t.Fatalf("ran %q in %q", c.Line(), c.Dir)
	}
}

func TestSeatListExit78KeepsTheSeatsItPrinted(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "list"},
		execx.Result{Stdout: []byte("SEAT\talpha\t2\theld\tself\th2\n"), ExitCode: 78})
	seats, unreadable, err := r.c.SeatList(context.Background(), "/co")
	if err != nil || !unreadable || len(seats) != 1 || seats[0].Seat != "2" {
		t.Fatalf("%+v %v %v", seats, unreadable, err)
	}
}

func TestSeatListOtherFailuresAreErrors(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "list"}, execx.Result{ExitCode: 69, Stderr: []byte("down\n")})
	_, _, err := r.c.SeatList(context.Background(), "/co")
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("got %v", err)
	}
	failure, ok := errors.AsType[*CommandError](err)
	if !ok || failure.Code != 69 || failure.Stderr != "down\n" {
		t.Fatalf("failure %+v", failure)
	}
}

func TestSeatListWithMalformedOutputFails(t *testing.T) {
	r := newRig()
	r.fake.On([]string{"agent-bus", "seat", "list"}, execx.Result{Stdout: []byte("SEAT\tx\n")})
	if _, _, err := r.c.SeatList(context.Background(), "/co"); err == nil {
		t.Fatal("accepted a short SEAT line")
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
	_, _, err := r.c.SeatList(context.Background(), "/co")
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
