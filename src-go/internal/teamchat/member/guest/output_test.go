package guest

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var (
	opTest = Op{Verb: "guest test", Changes: []What{"token", "seat.json"}}
	opBusy = Op{Verb: "guest busy", Busy: true}
)

func TestChangedPrintsOnlyTheLine(t *testing.T) {
	var out bytes.Buffer
	o := NewOutput(&out, opTest)
	if err := o.Changed("token"); err != nil {
		t.Fatal(err)
	}
	if err := o.Changed("seat.json"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "CHANGED\ttoken\nCHANGED\tseat.json\n" {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestChangedOutsideTheSetPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a what outside the op's set was printed")
		}
	}()
	var out bytes.Buffer
	if err := NewOutput(&out, opTest).Changed("store.json"); err != nil {
		t.Error(err)
	}
}

type failWriter struct{}

var errWrite = errors.New("closed")

func (failWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestChangedReportsAFailedWrite(t *testing.T) {
	if err := NewOutput(failWriter{}, opTest).Changed("token"); !errors.Is(err, errWrite) {
		t.Errorf("err = %v", err)
	}
}

// Every registered op is a guest op once, and ChangesOf finds its set by verb (TB2.4's
// parser).
func TestOpsAreRegistered(t *testing.T) {
	seen := map[string]bool{}
	for _, op := range Ops() {
		if seen[op.Verb] || !strings.HasPrefix(op.Verb, "guest ") && op.Verb != "seat set-aside" {
			t.Errorf("op %q twice or not a guest op", op.Verb)
		}
		seen[op.Verb] = true
		if changes, ok := ChangesOf(op.Verb); !ok || !slices.Equal(changes, op.Changes) {
			t.Errorf("%s: changes %v, %v", op.Verb, changes, ok)
		}
	}
	if _, ok := ChangesOf("guest nothing"); ok {
		t.Error("an unknown verb has a set")
	}
}

// register keeps its own copy of the set, ChangesOf hands out a copy, and a verb is
// registered once.
func TestRegister(t *testing.T) {
	set := []What{"x"}
	registered := register(Op{Verb: "guest register-test", Changes: set})
	set[0] = "forged"
	changes, ok := ChangesOf(registered.Verb)
	if !ok || !slices.Equal(changes, []What{"x"}) {
		t.Fatalf("registered set = %v, %v", changes, ok)
	}
	changes[0] = "forged"
	if again, found := ChangesOf(registered.Verb); !found || again[0] != "x" {
		t.Errorf("ChangesOf shares its set: %v", again)
	}
	defer func() {
		if recover() == nil {
			t.Error("a verb registered twice")
		}
	}()
	register(Op{Verb: registered.Verb})
}

// finish is Finish's exit and the error object on stderr's last line.
func finish(t *testing.T, op Op, err error) (term.ExitCode, exits.Error) {
	t.Helper()
	var stderr bytes.Buffer
	exit := Finish(&stderr, op, err)
	if err == nil {
		if stderr.Len() != 0 {
			t.Errorf("success wrote %q", stderr.String())
		}
		return exit, exits.Error{}
	}
	lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	last, derr := exits.Decode([]byte(lines[len(lines)-1]))
	if derr != nil {
		t.Fatalf("last stderr line of %q: %v", stderr.String(), derr)
	}
	return exit, last
}

func TestFinish(t *testing.T) {
	cases := []struct {
		op   Op
		err  error
		exit term.ExitCode
	}{
		{opTest, nil, term.ExitOK},
		{opTest, exits.New(exits.CodeReach, nil, "unreachable"), exits.ExitConfig},
		{opTest, exits.New(exits.CodeUsage, nil, "usage"), exits.ExitUsage},
		{opBusy, exits.New(exits.CodeSeatHeld, nil, "held"), exits.ExitBusy},
	}
	for _, c := range cases {
		exit, last := finish(t, c.op, c.err)
		if exit != c.exit || exit != last.Exit && c.err != nil {
			t.Errorf("%v: exit %d, object %+v, want %d", c.err, exit, last, c.exit)
		}
	}
}

// GU1: no exit outside 0, 64, 75 and 78, and 75 only from an op that takes a seat lock; any
// other failure is a bug, exit 1 internal.
func TestFinishKeepsTheGuestExits(t *testing.T) {
	cases := map[string]struct {
		op  Op
		err error
	}{
		"75 from check": {opTest, exits.New(exits.CodeSeatHeld, nil, "held")},
		"20":            {opTest, exits.New(exits.CodeUnreachable, nil, "down")},
		"plain error":   {opTest, errors.New("broken")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			exit, last := finish(t, c.op, c.err)
			if exit != exits.ExitInternal || last.Code != exits.CodeInternal {
				t.Errorf("exit %d, object %+v", exit, last)
			}
		})
	}
}
