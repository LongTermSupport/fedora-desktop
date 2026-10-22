package admin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// teamFileArg is alpha's applied team.json as the child's team_file arg.
func (w *world) teamFileArg(t *testing.T) proto.Member {
	t.Helper()
	raw, err := os.ReadFile(w.root + paths.TeamDeclaration(alpha(t)))
	if err != nil {
		t.Fatal(err)
	}
	return proto.M(keyTeamFile, parsed(t, raw))
}

// withMember is a world with alpha applied and the third seat added.
func withMember(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.applied(t)
	w.ok(t, "", addArgs("3", "password")...)
	return w
}

func (w *world) serve(t *testing.T, op string, args ...proto.Member) Answer {
	t.Helper()
	return Serve(t.Context(), w.open, Request{Op: op, Team: alpha(t), Args: proto.Obj(args...)})
}

// TestServeTeamViewReadsTheTeam is CO1: the view of the team's members, humans and problems,
// in an answer root decodes.
func TestServeTeamViewReadsTheTeam(t *testing.T) {
	w := withMember(t)
	a := w.serve(t, OpTeamView, w.teamFileArg(t))
	if !a.OK || a.View == nil {
		t.Fatalf("answer %+v", a)
	}
	i := slices.IndexFunc(a.View.Members, func(m status.Member) bool { return m.Handle == sampleHandle })
	if i < 0 || a.View.Members[i].Rooms[proto.PurposeMain] == "" {
		t.Fatalf("members %+v", a.View.Members)
	}
	if len(a.View.Humans) != 2 {
		t.Fatalf("humans %+v", a.View.Humans)
	}
	raw, err := EncodeAnswer(a)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeAnswer(raw)
	if err != nil || answerFits(OpTeamView, back) != nil {
		t.Fatalf("decode: %v", err)
	}
}

// TestServeTeamViewClassesItsFailures is CO1's classes: the files and an admin client that cannot
// read are config, a homeserver that does not answer unreachable, a refusal failed.
func TestServeTeamViewClassesItsFailures(t *testing.T) {
	t.Run("files", func(t *testing.T) {
		w := withMember(t)
		if err := os.WriteFile(w.root+paths.RosterFile(alpha(t)), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if a := w.serve(t, OpTeamView, w.teamFileArg(t)); a.OK || a.Error.Class != ClassConfig {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("admin client", func(t *testing.T) {
		w := withMember(t)
		w.crash = &crashing{method: "none"}
		if a := w.serve(t, OpTeamView, w.teamFileArg(t)); a.OK || a.Error.Class != ClassConfig {
			t.Fatalf("answer %+v", a)
		}
	})
	for name, c := range map[string]struct {
		fault fakehs.Fault
		class ErrorClass
	}{
		"unreachable": {fakehs.Fault{Method: "GET", Path: "/state", Drop: true, Times: 1000}, ClassUnreachable},
		"refused":     {fakehs.Fault{Method: "GET", Path: "/state", Status: 400, Body: []byte(`{"errcode":"M_UNKNOWN","error":"no"}`), Times: 1000}, ClassFailed},
	} {
		t.Run(name, func(t *testing.T) {
			w := withMember(t)
			w.srv.Inject(c.fault)
			if a := w.serve(t, OpTeamView, w.teamFileArg(t)); a.OK || a.Error.Class != c.class {
				t.Fatalf("answer %+v", a.Error)
			}
		})
	}
	t.Run("args", func(t *testing.T) {
		w := withMember(t)
		if a := w.serve(t, OpTeamView); a.OK || a.Error.Class != ClassFailed {
			t.Fatalf("answer %+v", a)
		}
		if a := w.serve(t, OpTeamView, w.teamFileArg(t), proto.M(keyHandle, proto.Str(sampleHandle))); a.OK || a.Error.Class != ClassFailed {
			t.Fatalf("answer %+v", a)
		}
	})
}

func runArgs(t *testing.T, w *world, handle string) []proto.Member {
	t.Helper()
	return []proto.Member{
		w.teamFileArg(t), proto.M(keyHandle, proto.Str(handle)), proto.M("run", proto.Str("0123456789abcdef")), proto.M("since_ms", proto.Int(t0ms)),
	}
}

// joinedWithCopy is the third seat joined to main as itself, with the guest token copy of its
// token; it answers the token.
func joinedWithCopy(t *testing.T, w *world) string {
	t.Helper()
	token, err := w.srv.MintToken(proto.UserID(sampleHandle, server))
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.member(token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Join(t.Context(), w.rooms(t).Main); err != nil {
		t.Fatal(err)
	}
	dir := w.root + paths.GuestTokenDir(alpha(t))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sampleHandle+".token"), []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	return token
}

// TestServeMemberDisconnectedPostsTheNotice is CO2: posted as the member with its token copy,
// once, and posted false with why when there is no copy.
func TestServeMemberDisconnectedPostsTheNotice(t *testing.T) {
	w := withMember(t)
	if a := w.serve(t, OpMemberDisconnected, runArgs(t, w, sampleHandle)...); !a.OK || a.Notice == nil || a.Notice.Posted || a.Notice.Why != "the member has no token copy" {
		t.Fatalf("no copy: %+v", a)
	}
	token := joinedWithCopy(t, w)
	for range 2 {
		a := w.serve(t, OpMemberDisconnected, runArgs(t, w, sampleHandle)...)
		if !a.OK || a.Notice == nil || !a.Notice.Posted {
			t.Fatalf("answer %+v %+v", a, a.Error)
		}
		raw, err := EncodeAnswer(a)
		if err != nil || strings.Contains(string(raw), token) {
			t.Fatalf("encode %s, %v", raw, err)
		}
	}
	if got := w.notices(t); !slices.Contains(got, "disconnected "+sampleHandle) {
		t.Fatalf("notices %q", got)
	}
}

func TestServeMemberDisconnectedClassesItsFailures(t *testing.T) {
	t.Run("transient", func(t *testing.T) {
		w := withMember(t)
		joinedWithCopy(t, w)
		w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/state", Drop: true, Times: 1000})
		if a := w.serve(t, OpMemberDisconnected, runArgs(t, w, sampleHandle)...); a.OK || a.Error.Class != ClassUnreachable {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("a copy that is not a token", func(t *testing.T) {
		w := withMember(t)
		joinedWithCopy(t, w)
		file := filepath.Join(w.root+paths.GuestTokenDir(alpha(t)), sampleHandle+".token")
		if err := os.WriteFile(file, []byte("two words"), 0o600); err != nil {
			t.Fatal(err)
		}
		if a := w.serve(t, OpMemberDisconnected, runArgs(t, w, sampleHandle)...); a.OK || a.Error.Class != ClassConfig || strings.Contains(a.Error.Message, "two words") {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("no rooms record", func(t *testing.T) {
		w := withMember(t)
		joinedWithCopy(t, w)
		if err := os.Remove(w.root + paths.RoomsFile(alpha(t))); err != nil {
			t.Fatal(err)
		}
		if a := w.serve(t, OpMemberDisconnected, runArgs(t, w, sampleHandle)...); a.OK || a.Error.Class != ClassConfig {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("no member client", func(t *testing.T) {
		w := withMember(t)
		joinedWithCopy(t, w)
		open := func(ctx context.Context, team paths.TeamID, port int, regKey string) (Opened, error) {
			o, err := w.open(ctx, team, port, regKey)
			o.Member = nil
			return o, err
		}
		a := Serve(t.Context(), open, Request{Op: OpMemberDisconnected, Team: alpha(t), Args: proto.Obj(runArgs(t, w, sampleHandle)...)})
		if a.OK || a.Error.Class != ClassConfig {
			t.Fatalf("answer %+v", a)
		}
	})
	w := withMember(t)
	bad := map[string][]proto.Member{
		"handle":   runArgs(t, w, "Not A Handle"),
		"run":      {w.teamFileArg(t), proto.M(keyHandle, proto.Str(sampleHandle)), proto.M("run", proto.Str("x")), proto.M("since_ms", proto.Int(1))},
		"since_ms": {w.teamFileArg(t), proto.M(keyHandle, proto.Str(sampleHandle)), proto.M("run", proto.Str("0123456789abcdef")), proto.M("since_ms", proto.Int(-1))},
		"since":    {w.teamFileArg(t), proto.M(keyHandle, proto.Str(sampleHandle)), proto.M("run", proto.Str("0123456789abcdef")), proto.M("since_ms", proto.Str("1"))},
		"missing":  {w.teamFileArg(t), proto.M(keyHandle, proto.Str(sampleHandle))},
	}
	for name, args := range bad {
		if a := w.serve(t, OpMemberDisconnected, args...); a.OK || a.Error.Class != ClassFailed {
			t.Errorf("%s: answer %+v", name, a)
		}
	}
}

// TestServeClassesTheOpenersFailure: the Opener's own refusal is 70, an unreachable homeserver
// 69, and anything else, the child's files, 78 (n2f-C1).
func TestServeClassesTheOpenersFailure(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	cases := map[string]struct {
		err   error
		class ErrorClass
	}{
		"drift":       {&apply.Error{Op: apply.OpTeamApply, Step: "admin", Drift: true, Err: errors.New("the token is another user's")}, ClassRefused},
		"unreachable": {matrix.ErrUnreachable, ClassUnreachable},
		"files":       {errors.New("ctl/alpha is not 0700"), ClassConfig},
	}
	for name, c := range cases {
		open := func(context.Context, paths.TeamID, int, string) (Opened, error) { return Opened{}, c.err }
		for _, op := range []string{apply.OpRoleApply, OpTeamView, OpMemberDisconnected} {
			args := []proto.Member{w.teamFileArg(t)}
			if op == OpMemberDisconnected {
				args = runArgs(t, w, sampleHandle)
			}
			a := Serve(t.Context(), open, Request{Op: op, Team: alpha(t), Args: proto.Obj(args...)})
			if a.OK || a.Error.Class != c.class {
				t.Errorf("%s %s: answer %+v", name, op, a.Error)
			}
		}
		in := alphaInput(t)
		a := Serve(t.Context(), open, Request{Op: apply.OpTeamApply, Team: alpha(t), Args: proto.Obj(goodArgs(t)[apply.OpTeamApply]...)})
		if a.OK || a.Error.Class != c.class || in.ID() != alpha(t) {
			t.Errorf("%s team.apply: answer %+v", name, a.Error)
		}
	}
}
