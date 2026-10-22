package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func parsed(t *testing.T, raw []byte) proto.Value {
	t.Helper()
	v, err := proto.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func alphaInput(t *testing.T) declare.Input {
	t.Helper()
	in, err := declare.DecodeInput([]byte(encodeDoc(t, alphaDoc())))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// goodArgs are a well-formed args object for each op the child serves.
func goodArgs(t *testing.T) map[string][]proto.Member {
	t.Helper()
	in := alphaInput(t)
	tf := proto.M("team_file", parsed(t, in.Team.Canonical()))
	handle := proto.M("handle", proto.Str(sampleHandle))
	cred := proto.M("credential", proto.Str(apply.CredPassword))
	return map[string][]proto.Member{
		apply.OpMemberAdd: {
			tf, proto.M("prefix", proto.Str(prefix)), proto.M("seat", proto.Int(3)), proto.M("kind", proto.Str("podman")),
			proto.M("human_text", proto.Bool(true)), cred,
		},
		apply.OpMemberReturn:  {tf, handle, cred},
		apply.OpMemberReissue: {tf, handle, cred},
		apply.OpMemberPark:    {tf, handle},
		apply.OpMemberRetire:  {tf, handle},
		apply.OpMemberSet:     {tf, handle, proto.M("human_text", proto.Bool(false))},
		apply.OpRoleApply:     {tf},
		apply.OpTeamApply: {
			proto.M("input", parsed(t, in.Encode())), proto.M("approved", parsed(t, declare.EncodeApproved(in.ID(), in.Gated()))),
			proto.M("reg_key", proto.Str(strings.Repeat("ab", 64))),
		},
	}
}

// TestServeDecodesEachOpsArgsStrictly is CV1: a key missing, one too many, or a value of the
// wrong type is a failure before the child reaches the homeserver.
func TestServeDecodesEachOpsArgsStrictly(t *testing.T) {
	w := newWorld(t)
	for op, members := range goodArgs(t) {
		for i, m := range members {
			cases := map[string][]proto.Member{
				"without " + m.Key:          append(append([]proto.Member{}, members[:i]...), members[i+1:]...),
				"with " + m.Key + " a list": replaced(members, i, proto.Arr()),
			}
			if i == 0 {
				cases["with an extra key"] = append(append([]proto.Member{}, members...), proto.M("extra", proto.Int(1)))
			}
			for name, args := range cases {
				a := Serve(t.Context(), w.open, Request{Op: op, Team: alpha(t), Args: proto.Obj(args...)})
				if a.OK || a.Error.Class != ClassFailed {
					t.Errorf("%s %s: %+v", op, name, a)
				}
			}
		}
	}
	if len(w.regKeys) != 0 || len(w.srv.Requests()) != 0 {
		t.Fatalf("a bad request reached the team: %d opens, %d requests", len(w.regKeys), len(w.srv.Requests()))
	}
}

func replaced(members []proto.Member, i int, v proto.Value) []proto.Member {
	out := append([]proto.Member{}, members...)
	out[i] = proto.M(members[i].Key, v)
	return out
}

func TestServeRefusesWhatTheArgsCannotMean(t *testing.T) {
	w := newWorld(t)
	good := goodArgs(t)
	other := alphaDoc()
	docTeam(other)["team"] = "beta"
	beta, err := declare.DecodeInput([]byte(encodeDoc(t, other)))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, op string
		args     []proto.Member
		text     string
	}{
		{"an unserved op", "member.fly", good[apply.OpMemberPark], "member.fly is not served"},
		{"another team's file", apply.OpMemberPark, replaced(good[apply.OpMemberPark], 0, parsed(t, beta.Team.Canonical())), "team beta"},
		{"a team file that does not decode", apply.OpRoleApply, []proto.Member{proto.M("team_file", proto.Obj())}, "team.json"},
		{"a seat below zero", apply.OpMemberAdd, replaced(good[apply.OpMemberAdd], 2, proto.Int(-1)), "seat"},
		{"another team's input", apply.OpTeamApply, replaced(good[apply.OpTeamApply], 0, parsed(t, beta.Encode())), "team beta"},
		{"an input that does not decode", apply.OpTeamApply, replaced(good[apply.OpTeamApply], 0, proto.Obj()), "input"},
		{"an approval that does not decode", apply.OpTeamApply, replaced(good[apply.OpTeamApply], 1, proto.Obj()), "approved"},
	}
	for _, c := range cases {
		a := Serve(t.Context(), w.open, Request{Op: c.op, Team: alpha(t), Args: proto.Obj(c.args...)})
		if a.OK || a.Error.Class != ClassFailed || !strings.Contains(a.Error.Message, c.text) {
			t.Errorf("%s: %+v", c.name, a.Error)
		}
	}
}

// TestErrorClassIsTheExitCodesClass: the child answers the class root maps back to C 3.3's code.
func TestErrorClassIsTheExitCodesClass(t *testing.T) {
	cases := map[error]ErrorClass{
		&apply.Error{Step: "x", Drift: true, Err: errors.New("no")}: ClassRefused,
		&apply.Error{Step: "x", Err: errors.New("boom")}:            ClassFailed,
		fmt.Errorf("send: %w", matrix.ErrUnreachable):               ClassUnreachable,
		errors.New("anything"):                                      ClassFailed,
	}
	for err, want := range cases {
		if got := errorClass(err); got != want {
			t.Errorf("%v: %s, want %s", err, got, want)
		}
	}
}

func failingOpener(context.Context, paths.TeamID, string) (apply.Deps, error) {
	return apply.Deps{}, errors.New("no token")
}

// TestServeAnOpenerThatFails: the child's own files are its configuration (78).
func TestServeAnOpenerThatFails(t *testing.T) {
	a := Serve(t.Context(), failingOpener, Request{Op: apply.OpRoleApply, Team: alpha(t), Args: proto.Obj(goodArgs(t)[apply.OpRoleApply]...)})
	if a.OK || a.Error.Class != ClassConfig || !strings.Contains(a.Error.Message, "no token") {
		t.Fatalf("%+v", a.Error)
	}
}
