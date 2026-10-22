package declare

import (
	"errors"
	"testing"
)

func TestErrorKindsAreTheExitClassesOfDeclarePy(t *testing.T) {
	for _, c := range []struct {
		err  *Error
		code int
		is   []error
		not  []error
	}{
		{&Error{Kind: KindUsage, Message: "--repo: 'x' is not a lower-case OWNER/REPO"}, 64, []error{ErrUsage}, []error{ErrRefused, ErrVaulted, ErrCrash}},
		{&Error{Kind: KindRefused, Message: "line 3: the port is not a number"}, 78, []error{ErrRefused}, []error{ErrUsage, ErrVaulted, ErrCrash}},
		{&Error{Kind: KindRefused, Vaulted: true, Message: "agent_bus_teams is tagged"}, 78, []error{ErrRefused, ErrVaulted}, []error{ErrUsage, ErrCrash}},
		{&Error{Kind: KindCrash, Message: "'int' object is not iterable"}, 1, []error{ErrCrash}, []error{ErrUsage, ErrRefused, ErrVaulted}},
	} {
		if c.err.Code() != c.code {
			t.Errorf("%v: code %d, want %d", c.err, c.err.Code(), c.code)
		}
		if c.err.Error() != c.err.Message {
			t.Errorf("Error() = %q, want the message without a prefix", c.err.Error())
		}
		var wrapped error = c.err
		for _, target := range c.is {
			if !errors.Is(wrapped, target) {
				t.Errorf("%v is not %v", c.err, target)
			}
		}
		for _, target := range c.not {
			if errors.Is(wrapped, target) {
				t.Errorf("%v is %v", c.err, target)
			}
		}
	}
}

func TestErrorConstructorsSetTheKind(t *testing.T) {
	for _, c := range []struct {
		err     error
		kind    Kind
		vaulted bool
		msg     string
	}{
		{usagef("--team: team %s", "a"), KindUsage, false, "--team: team a"},
		{refusedf("line %d", 3), KindRefused, false, "line 3"},
		{vaultedf("%s is tagged", "x"), KindRefused, true, "x is tagged"},
		{crashf("%s: boom", "x"), KindCrash, false, "x: boom"},
	} {
		e, ok := asError(c.err)
		if !ok || e.Kind != c.kind || e.Vaulted != c.vaulted || e.Message != c.msg {
			t.Errorf("got %+v, want kind %d vaulted %v %q", e, c.kind, c.vaulted, c.msg)
		}
	}
	if e, ok := asError(errors.New("plain")); ok || e != nil {
		t.Error("a plain error is not an *Error")
	}
}
