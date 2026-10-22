package apply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	prefix = "myrepo+desk.podman"
	third  = "myrepo.3+desk.podman"
)

// ticking is a clock that moves one millisecond on every read, so each operation's since_ms,
// and so each notice's transaction ID, is its own.
func ticking() func() time.Time {
	var mu sync.Mutex
	at := fixedNow()
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		at = at.Add(time.Millisecond)
		return at
	}
}

// team is the applied alpha: teamDoc approved and applied, its team as `team.json` holds it.
func (w *world) team(t *testing.T) declare.Team {
	t.Helper()
	in := input(t, teamDoc())
	w.approve(t, in)
	res := w.run(t, in)
	team, err := declare.DecodeTeamFile(res.TeamFile)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

func (w *world) memberDeps(admin Admin) Deps {
	d := w.deps()
	d.Now = w.clock
	if admin != nil {
		d.Admin = admin
	}
	return d
}

func (w *world) member(t *testing.T, team declare.Team, req MemberRequest) Result {
	t.Helper()
	res, err := RunMember(t.Context(), w.memberDeps(nil), team, req)
	if err != nil {
		t.Fatalf("Member %s: %v\nchanged: %v", req.Op, err, res.Changed)
	}
	return res
}

func add(seat int, cred string) MemberRequest {
	return MemberRequest{Op: OpMemberAdd, Prefix: prefix, Seat: seat, Kind: "podman", HumanText: true, Credential: cred}
}

func on(op, handle string) MemberRequest {
	return MemberRequest{Op: op, Handle: handle, Credential: credPassword, HumanText: true}
}

// notices are the admin's notices in main, oldest first, as `<event> <subject localpart>`
// with `:<role>` for the events that carry one.
func (w *world) notices(t *testing.T) []string {
	t.Helper()
	main := w.file(t).rooms.Main
	page, err := w.admin.Messages(t.Context(), main, matrix.MessagesRequest{Dir: matrix.Forward, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, raw := range page.Chunk {
		v, err := proto.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		content, found := v.Get("content")
		if !found {
			continue
		}
		if _, notice := content.Get(proto.KeyNotice); !notice {
			continue
		}
		c, reason := proto.DecodeContent(content, proto.AdminMainKeys)
		if reason != "" {
			t.Fatalf("notice %s: %s", content.Compact(), reason)
		}
		line := c.Notice.Event + " " + strings.TrimSuffix(strings.TrimPrefix(c.Notice.Subject, "@"), ":"+server)
		if c.Notice.Role != "" {
			line += ":" + c.Notice.Role
		}
		out = append(out, line)
	}
	return out
}

// signsIn is whether a password signs the handle in.
func (w *world) signsIn(t *testing.T, handle, password string) bool {
	t.Helper()
	_, err := w.admin.LoginDevice(t.Context(), handle, password, "TCTEST")
	return err == nil
}

func (w *world) membership(t *testing.T, room, handle string) string {
	t.Helper()
	m, err := w.admin.Membership(t.Context(), room, user(handle))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMemberAdd(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	res := w.member(t, team, add(3, credPassword))
	if res.Handle != third || res.Credential == nil || res.Credential.Kind != credPassword {
		t.Fatalf("result %+v", res)
	}
	if !w.signsIn(t, third, res.Credential.Secret) {
		t.Fatal("the answered password does not sign in")
	}
	want := []string{"member " + third + " claimed", "member " + third + " account created", "member " + third + " issued"}
	if !slices.Equal(res.Changed[:3], want) || !slices.Contains(res.Changed, "room main invited "+user(third)) ||
		res.Changed[len(res.Changed)-1] != "notice added "+third {
		t.Fatalf("changed %v", res.Changed)
	}
	if strings.Contains(strings.Join(res.Changed, "\n"), res.Credential.Secret) {
		t.Fatal("a CHANGED line carries the secret")
	}
	w.checkAdded(t)
}

// checkAdded is the world after `member add` of the third seat.
func (w *world) checkAdded(t *testing.T) {
	t.Helper()
	f := w.file(t).roster
	m := f.Members[third]
	if m.State != MemberActive || m.Cred != credPassword || m.CredSeq == 0 || m.Needs != "" || f.Counters[prefix] != 3 || f.Pending != nil {
		t.Fatalf("roster file %+v", f)
	}
	if got := w.notices(t); !slices.Equal(got, []string{"added " + third + ":worker"}) {
		t.Fatalf("notices %v", got)
	}
	if w.membership(t, w.file(t).rooms.Main, third) != "invite" {
		t.Fatal("not invited to main")
	}
}

func TestMemberAddToken(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	res := w.member(t, team, add(3, credToken))
	if res.Credential == nil || res.Credential.Kind != credToken {
		t.Fatalf("result %+v", res)
	}
	member, err := w.admin.WithToken(res.Credential.Secret)
	if err != nil {
		t.Fatal(err)
	}
	id, err := member.Whoami(t.Context())
	if err != nil || id.UserID != user(third) || id.DeviceID != seatDevice {
		t.Fatalf("whoami %+v %v", id, err)
	}
	if w.file(t).roster.Members[third].Cred != credToken {
		t.Fatal("cred is not token")
	}
}

func TestSeatNextSkipsEveryHeldNumber(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	w.addMember(t, "myrepo.1+desk.podman", active())
	retired := active()
	retired.State = MemberRetired
	w.addMember(t, "myrepo.2+desk.podman", retired)
	if res := w.member(t, team, add(0, credToken)); res.Handle != third {
		t.Fatalf("next chose %s", res.Handle)
	}
	if w.file(t).roster.Counters[prefix] != 3 {
		t.Fatal("counter not raised")
	}
	// A lower seat named explicitly never lowers the counter.
	w.member(t, team, MemberRequest{Op: OpMemberAdd, Prefix: "other+desk.podman", Seat: 5, Kind: "podman", HumanText: true, Credential: credToken})
	w.member(t, team, MemberRequest{Op: OpMemberAdd, Prefix: "other+desk.podman", Seat: 2, Kind: "podman", HumanText: true, Credential: credToken})
	if w.file(t).roster.Counters["other+desk.podman"] != 5 {
		t.Fatalf("counters %v", w.file(t).roster.Counters)
	}
}

func refusedAt(t *testing.T, name string, err error, step, text string) {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok || !e.Drift || e.Step != step || !strings.Contains(err.Error(), text) {
		t.Errorf("%s: err = %v, want a refusal at %s naming %q", name, err, step, text)
	}
}

func TestMemberRefusals(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	for handle, state := range map[string]string{"myrepo.1+desk.podman": MemberActive, "myrepo.2+desk.podman": MemberParked, "myrepo.4+desk.podman": MemberLost, "myrepo.5+desk.podman": MemberRetired} {
		m := active()
		m.State = state
		w.addMember(t, handle, m)
	}
	mark, seq := w.mark(), w.file(t).roster.Seq
	cases := []struct {
		name string
		req  MemberRequest
		text string
	}{
		{"add active", add(1, credPassword), "is taken"},
		{"add parked", add(2, credPassword), "member return"},
		{"add lost", add(4, credPassword), "member return"},
		{"add retired", add(5, credPassword), "never return"},
		{"kind", MemberRequest{Op: OpMemberAdd, Prefix: prefix, Seat: 3, Kind: "lxc", Credential: credPassword}, "kind"},
		{"credential", MemberRequest{Op: OpMemberAdd, Prefix: prefix, Seat: 3, Kind: "podman", Credential: "key"}, "credential"},
		{"prefix", MemberRequest{Op: OpMemberAdd, Prefix: "no prefix", Seat: 3, Kind: "podman", Credential: credPassword}, "prefix"},
		{"probe", on(OpMemberPark, proto.ProbeHandle("desk").String()), "probe"},
		{"park parked", on(OpMemberPark, "myrepo.2+desk.podman"), "parked"},
		{"return active", on(OpMemberReturn, "myrepo.1+desk.podman"), "active"},
		{"reissue parked", on(OpMemberReissue, "myrepo.2+desk.podman"), "parked"},
		{"retire retired", on(OpMemberRetire, "myrepo.5+desk.podman"), "retired"},
		{"retire the declared coordinator", on(OpMemberRetire, coord), "coordinator"},
		{"set unknown", on(OpMemberSet, third), "not a member"},
		{"unknown op", MemberRequest{Op: "member.fly", Handle: other}, "member.fly"},
	}
	for _, c := range cases {
		_, err := RunMember(t.Context(), w.memberDeps(nil), team, c.req)
		refusedAt(t, c.name, err, StepFiles, c.text)
	}
	if len(w.writes(mark)) != 0 || w.file(t).roster.Seq != seq {
		t.Fatalf("a refusal wrote: %v", w.writes(mark))
	}
}

func TestMemberNeedsAnAppliedTeam(t *testing.T) {
	w := newWorld(t)
	team := input(t, teamDoc()).Team
	_, err := RunMember(t.Context(), w.memberDeps(nil), team, add(3, credPassword))
	refusedAt(t, "no main room", err, StepFiles, "team apply")
}

func TestMemberLifecycle(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	w.member(t, team, add(3, credPassword))
	main := w.file(t).rooms.Main
	w.member(t, team, on(OpMemberSet, third))
	if res := w.member(t, team, MemberRequest{Op: OpMemberSet, Handle: third, HumanText: false}); !slices.Contains(res.Changed, "member "+third+" human_text false") {
		t.Fatalf("set %v", res.Changed)
	}
	if w.file(t).roster.Members[third].HumanText {
		t.Fatal("human_text not set")
	}
	w.reissueParkReturn(t, team)
	w.retireThird(t, team, main)
	w.addMember(t, other, active())
	mark := w.mark()
	if res := w.member(t, team, on(OpMemberSet, other)); len(res.Changed) != 0 || len(w.writes(mark)) != 0 {
		t.Fatalf("a set that changes nothing: %v %v", res.Changed, w.writes(mark))
	}
}

func (w *world) reissueParkReturn(t *testing.T, team declare.Team) {
	t.Helper()
	issued := w.member(t, team, on(OpMemberReissue, third))
	if issued.Credential == nil || !w.signsIn(t, third, issued.Credential.Secret) {
		t.Fatal("reissue gave no working password")
	}
	parked := w.member(t, team, on(OpMemberPark, third))
	if parked.Credential != nil || w.signsIn(t, third, issued.Credential.Secret) || w.file(t).roster.Members[third].State != MemberParked {
		t.Fatalf("park %+v", parked)
	}
	back := w.member(t, team, on(OpMemberReturn, third))
	if back.Credential == nil || !w.signsIn(t, third, back.Credential.Secret) || w.file(t).roster.Members[third].State != MemberActive {
		t.Fatalf("return %+v", back)
	}
}

func (w *world) retireThird(t *testing.T, team declare.Team, main string) {
	t.Helper()
	retired := w.member(t, team, on(OpMemberRetire, third))
	if !slices.Contains(retired.Changed, "room main kicked "+user(third)) || !slices.Contains(retired.Changed, "member "+third+" deactivated") {
		t.Fatalf("retire %v", retired.Changed)
	}
	if acct, _, err := w.admin.Account(t.Context(), user(third)); err != nil || !acct.Deactivated {
		t.Fatalf("account %+v %v", acct, err)
	}
	if m := w.membership(t, main, third); m == "join" || m == "invite" {
		t.Fatalf("still %s in main", m)
	}
	want := []string{"added " + third + ":worker", "parked " + third, "added " + third + ":worker", "removed " + third}
	if got := w.notices(t); !slices.Equal(got, want) {
		t.Fatalf("notices %v", got)
	}
	if w.file(t).roster.Pending != nil {
		t.Fatal("pending left")
	}
}

func TestAddPromotesTheWantedCoordinator(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	if w.file(t).roster.CoordinatorWanted != coord {
		t.Fatal("coordinator not wanted")
	}
	res := w.member(t, team, add(1, credPassword))
	if !slices.Contains(res.Changed, "member "+coord+" account adopted") || !slices.Contains(res.Changed, "role "+coord+" promoted") {
		t.Fatalf("changed %v", res.Changed)
	}
	f := w.file(t).roster
	if f.Coordinator != coord || f.CoordinatorWanted != "" {
		t.Fatalf("roster %+v", f)
	}
	if got := w.notices(t); !slices.Equal(got, []string{"added " + coord + ":worker", "role " + coord + ":coordinator"}) {
		t.Fatalf("notices %v", got)
	}
	// The coordinator cannot lose human text.
	_, err := RunMember(t.Context(), w.memberDeps(nil), team, MemberRequest{Op: OpMemberSet, Handle: coord, HumanText: false})
	refusedAt(t, "set", err, StepFiles, "coordinator")
}

func TestAWantedCoordinatorWithoutHumanTextIsANote(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	req := add(1, credPassword)
	req.HumanText = false
	res := w.member(t, team, req)
	if res.Credential == nil || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "human text") {
		t.Fatalf("result %+v", res)
	}
	if f := w.file(t).roster; f.Coordinator != "" || f.CoordinatorWanted != coord || f.Members[coord].State != MemberActive {
		t.Fatalf("roster %+v", f)
	}
}

func TestRoleApply(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	w.member(t, team, add(1, credPassword))
	w.member(t, team, add(2, credPassword))
	team.Coordinator = other
	res, err := RoleApply(t.Context(), w.memberDeps(nil), team)
	if err != nil {
		t.Fatal(err)
	}
	demoted, promoted := slices.Index(res.Changed, "role "+coord+" demoted"), slices.Index(res.Changed, "role "+other+" promoted")
	if demoted < 0 || promoted < demoted {
		t.Fatalf("changed %v", res.Changed)
	}
	got := w.notices(t)
	if !slices.Equal(got[len(got)-2:], []string{"role " + coord + ":worker", "role " + other + ":coordinator"}) {
		t.Fatalf("notices %v", got)
	}
	mark := w.mark()
	if again, err := RoleApply(t.Context(), w.memberDeps(nil), team); err != nil || len(again.Changed) != 0 || len(w.writes(mark)) != 0 {
		t.Fatalf("again %v %v", again.Changed, err)
	}
}

// crashing is the admin client with one call failing: the n-th call of method, its effect
// landed first when landed (the answer lost). It records each password it set.
type crashing struct {
	Admin

	method    string
	n, calls  int
	landed    bool
	passwords []string
}

var errCrash = errors.New("the process died here")

type found struct {
	a  matrix.Account
	ok bool
}

func (c *crashing) Account(ctx context.Context, id string) (matrix.Account, bool, error) {
	got, err := hit(c, "Account", func() (found, error) {
		a, ok, err := c.Admin.Account(ctx, id)
		return found{a, ok}, err
	})
	return got.a, got.ok, err
}

func (c *crashing) CreateAccount(ctx context.Context, id, pw, name string) (matrix.Account, error) {
	return hit(c, "CreateAccount", func() (matrix.Account, error) { return c.Admin.CreateAccount(ctx, id, pw, name) })
}

func (c *crashing) ResetPassword(ctx context.Context, id, pw string) error {
	c.passwords = append(c.passwords, pw)
	return hitErr(c, "ResetPassword", func() error { return c.Admin.ResetPassword(ctx, id, pw) })
}

func (c *crashing) LoginDevice(ctx context.Context, name, pw, device string) (matrix.Session, error) {
	return hit(c, "LoginDevice", func() (matrix.Session, error) { return c.Admin.LoginDevice(ctx, name, pw, device) })
}

func (c *crashing) SetDeactivated(ctx context.Context, id string, off bool) error {
	return hitErr(c, "SetDeactivated", func() error { return c.Admin.SetDeactivated(ctx, id, off) })
}

func (c *crashing) Invite(ctx context.Context, room, id string) error {
	return hitErr(c, "Invite", func() error { return c.Admin.Invite(ctx, room, id) })
}

func (c *crashing) Kick(ctx context.Context, room, id, reason string) error {
	return hitErr(c, "Kick", func() error { return c.Admin.Kick(ctx, room, id, reason) })
}

func (c *crashing) PutState(ctx context.Context, room, typ, key string, content json.RawMessage) (string, error) {
	return hit(c, "PutState", func() (string, error) { return c.Admin.PutState(ctx, room, typ, key, content) })
}

func (c *crashing) Send(ctx context.Context, room, typ, txn string, content json.RawMessage) (string, error) {
	return hit(c, "Send", func() (string, error) { return c.Admin.Send(ctx, room, typ, txn, content) })
}

// hit makes call, failing it with errCrash when it is the n-th call of the crashing method.
func hit[T any](c *crashing, method string, call func() (T, error)) (T, error) {
	if method != c.method {
		return call()
	}
	c.calls++
	if c.calls != c.n {
		return call()
	}
	var zero T
	if c.landed {
		if _, err := call(); err != nil {
			return zero, err
		}
	}
	return zero, errCrash
}

func hitErr(c *crashing, method string, call func() error) error {
	_, err := hit(c, method, func() (struct{}, error) { return struct{}{}, call() })
	return err
}

type crash struct {
	method string
	n      int
	landed bool
	step   string
}

// TestAddRollsForwardFromEveryStep crashes `member add` at each step, then shows the same verb
// asked again finishing it with a working credential and one of each notice.
func TestAddRollsForwardFromEveryStep(t *testing.T) {
	crashes := []crash{
		{"Account", 2, false, StepAccount}, // the first is the refusals' (MO3)
		{"CreateAccount", 1, true, StepAccount},
		{"ResetPassword", 1, true, StepCredential},
		{"Invite", 1, false, StepRooms},
		{"Send", 1, true, StepNotice},
		{"Send", 2, true, "role promoted " + coord},
	}
	for _, c := range crashes {
		t.Run(c.method+"/"+c.step, func(t *testing.T) { addRollsForward(t, c) })
	}
}

func addRollsForward(t *testing.T, c crash) {
	t.Helper()
	w := newWorld(t)
	team := w.team(t)
	req := add(1, credPassword)
	if c.method == "CreateAccount" {
		req = add(3, credPassword)
	}
	bad := &crashing{Admin: w.admin, method: c.method, n: c.n, landed: c.landed}
	_, err := RunMember(t.Context(), w.memberDeps(bad), team, req)
	if e, ok := errors.AsType[*Error](err); !ok || e.Step != c.step || !errors.Is(err, errCrash) {
		t.Fatalf("err = %v, want the crash at %s", err, c.step)
	}
	if p := w.file(t).roster.Pending; p == nil || p.Op != OpMemberAdd {
		t.Fatalf("pending %+v", p)
	}
	res := w.member(t, team, req)
	if res.Credential == nil || !w.signsIn(t, res.Handle, res.Credential.Secret) {
		t.Fatalf("the retry's credential %+v", res.Credential)
	}
	f := w.file(t).roster
	if f.Pending != nil || f.Members[res.Handle].State != MemberActive || f.Members[res.Handle].Needs != "" {
		t.Fatalf("roster %+v", f)
	}
	if n := countPrefixed(w.notices(t), "added "); n != 1 {
		t.Fatalf("notices %v", w.notices(t))
	}
	if res.Handle == coord && f.Coordinator != coord {
		t.Fatal("not promoted")
	}
}

func countPrefixed(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

// TestAnotherVerbRollsAPendingAddForward: a later verb on the team completes the add to active
// with its credential revoked, answers none, and marks the member for a reissue.
func TestAnotherVerbRollsAPendingAddForward(t *testing.T) {
	for _, c := range []crash{{"ResetPassword", 1, true, StepCredential}, {"Invite", 1, false, StepRooms}} {
		t.Run(c.step, func(t *testing.T) {
			w := newWorld(t)
			team := w.team(t)
			bad := &crashing{Admin: w.admin, method: c.method, n: c.n, landed: c.landed}
			if _, err := RunMember(t.Context(), w.memberDeps(bad), team, add(3, credPassword)); !errors.Is(err, errCrash) {
				t.Fatalf("err = %v", err)
			}
			issued := bad.passwords[len(bad.passwords)-1]
			w.addMember(t, other, active())
			res := w.member(t, team, MemberRequest{Op: OpMemberSet, Handle: other, HumanText: false})
			if res.Credential != nil || res.Handle != other {
				t.Fatalf("result %+v", res)
			}
			f := w.file(t).roster
			if m := f.Members[third]; m.State != MemberActive || m.Needs != needsReissue || f.Pending != nil || f.Members[other].HumanText {
				t.Fatalf("roster %+v", f)
			}
			if w.signsIn(t, third, issued) {
				t.Fatal("the credential issued before the crash still signs in")
			}
			if got := w.notices(t); !slices.Equal(got, []string{"added " + third + ":worker"}) {
				t.Fatalf("notices %v", got)
			}
		})
	}
}

func TestParkAndRetireRollForward(t *testing.T) {
	cases := []struct {
		crash

		op     string
		notice string
	}{
		{crash{"ResetPassword", 1, false, StepRevoke}, OpMemberPark, "parked"},
		{crash{"Send", 1, true, StepNotice}, OpMemberPark, "parked"},
		{crash{"Kick", 1, false, StepRooms}, OpMemberRetire, "removed"},
		{crash{"SetDeactivated", 1, true, StepDeactivate}, OpMemberRetire, "removed"},
		{crash{"Send", 1, false, StepNotice}, OpMemberRetire, "removed"},
	}
	for _, c := range cases {
		t.Run(c.op+"/"+c.step, func(t *testing.T) {
			w := newWorld(t)
			team := w.team(t)
			w.member(t, team, add(3, credPassword))
			w.addMember(t, other, active())
			bad := &crashing{Admin: w.admin, method: c.method, n: c.n, landed: c.landed}
			_, err := RunMember(t.Context(), w.memberDeps(bad), team, on(c.op, third))
			if e, ok := errors.AsType[*Error](err); !ok || e.Step != c.step {
				t.Fatalf("err = %v", err)
			}
			// Another verb finishes it as it was asked.
			w.member(t, team, MemberRequest{Op: OpMemberSet, Handle: other, HumanText: false})
			f := w.file(t).roster
			want := map[string]string{OpMemberPark: MemberParked, OpMemberRetire: MemberRetired}[c.op]
			if f.Pending != nil || f.Members[third].State != want {
				t.Fatalf("roster %+v", f)
			}
			got := w.notices(t)
			if got[len(got)-1] != c.notice+" "+third || slices.Index(got, c.notice+" "+third) != len(got)-1 {
				t.Fatalf("notices %v", got)
			}
		})
	}
}

// TestTeamApplyRollsAPendingMemberOperationForward replaces n2b-D22's refusal (MO5).
func TestTeamApplyRollsAPendingMemberOperationForward(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	bad := &crashing{Admin: w.admin, method: "Invite", n: 1}
	if _, err := RunMember(t.Context(), w.memberDeps(bad), team, add(3, credPassword)); !errors.Is(err, errCrash) {
		t.Fatalf("err = %v", err)
	}
	res, err := Run(t.Context(), w.memberDeps(nil), input(t, teamDoc()))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Changed, "member "+third+" revoked") {
		t.Fatalf("changed %v", res.Changed)
	}
	if f := w.file(t).roster; f.Pending != nil || f.Members[third].Needs != needsReissue {
		t.Fatalf("roster %+v", f)
	}
}

// TestARoleStepRollsForward: a crash between a demotion's rooms step and its notice still
// posts the notice once, then finishes the promotion (MO6, C17).
func TestARoleStepRollsForward(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	w.member(t, team, add(1, credPassword))
	w.member(t, team, add(2, credPassword))
	team.Coordinator = other
	before := len(w.notices(t))
	bad := &crashing{Admin: w.admin, method: "Send", n: 1, landed: true}
	_, err := RoleApply(t.Context(), w.memberDeps(bad), team)
	if e, ok := errors.AsType[*Error](err); !ok || e.Step != "role demoted "+coord {
		t.Fatalf("err = %v", err)
	}
	w.member(t, team, on(OpMemberSet, coord))
	got := w.notices(t)[before:]
	if !slices.Equal(got, []string{"role " + coord + ":worker", "role " + other + ":coordinator"}) {
		t.Fatalf("notices %v", got)
	}
	if f := w.file(t).roster; f.Coordinator != other || f.Pending != nil {
		t.Fatalf("roster %+v", f)
	}
}

// TestAPendingTeamApplyIsFinishedByAMemberVerb: a room create that died is the member verb's
// rooms step to finish.
func TestAPendingTeamApplyIsFinishedByAMemberVerb(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	s := w.file(t)
	s.roster.Pending = &Pending{Op: OpTeamApply, Step: StepRooms, SinceMS: 1}
	if err := s.saveRoster(); err != nil {
		t.Fatal(err)
	}
	w.member(t, team, add(3, credPassword))
	if w.file(t).roster.Pending != nil {
		t.Fatal("pending left")
	}
}

func TestCredentialHidesItsSecret(t *testing.T) {
	c := Credential{Kind: credPassword, Secret: "s3cret-value"}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if s := fmt.Sprintf(verb, c); strings.Contains(s, "s3cret") {
			t.Fatalf("%s shows the secret: %s", verb, s)
		}
	}
}
