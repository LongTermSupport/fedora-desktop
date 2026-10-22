package apply

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The operations a `pending` record names (SPEC-n2d MO1).
const (
	OpMemberAdd     = "member.add"
	OpMemberReturn  = "member.return"
	OpMemberReissue = "member.reissue"
	OpMemberPark    = "member.park"
	OpMemberRetire  = "member.retire"
	OpMemberSet     = "member.set"
	OpRoleApply     = "role.apply"
)

// The member operations' steps (MO2); a role change is the step `role <change> <handle>`
// (MO6).
const (
	StepClaim      = "claim"
	StepAccount    = "account"
	StepCredential = "credential"
	StepRevoke     = "revoke"
	StepDeactivate = "deactivate"
	StepNotice     = "notice"
	StepRole       = "role"
)

// The credential kinds a member verb asks for (DC16).
const (
	CredPassword = credPassword
	CredToken    = credToken
)

// seatDevice is the seat's one fixed device ID (C 3.7.2), the seat store's paths.SeatDevice.
const seatDevice = "TCSEAT"

// opSteps are each member operation's steps in order (MO2); `add` claims before its first.
var opSteps = map[string][]string{
	OpMemberAdd:     {StepAccount, StepCredential, StepRooms, StepNotice, StepRole},
	OpMemberReturn:  {StepAccount, StepCredential, StepRooms, StepNotice, StepRole},
	OpMemberReissue: {StepCredential},
	OpMemberPark:    {StepRevoke, StepRooms, StepNotice},
	OpMemberRetire:  {StepRooms, StepDeactivate, StepNotice},
	OpMemberSet:     {StepRooms},
	OpRoleApply:     {StepRoles},
}

// fromStates are the states each verb takes a member in (MO2).
var fromStates = map[string][]string{
	OpMemberReturn:  {MemberParked, MemberLost},
	OpMemberReissue: {MemberActive},
	OpMemberPark:    {MemberActive},
	OpMemberRetire:  {MemberActive, MemberParked},
	OpMemberSet:     {MemberActive, MemberParked},
}

// MemberRequest is one member verb: the handle, or for `add` the prefix, the seat (0 for the
// lowest free one, MO4) and the kind; human_text for `add` and `set`; the credential kind for
// `add`, `return` and `reissue`.
type MemberRequest struct {
	Op, Handle, Prefix string
	Seat               int
	Kind               string
	HumanText          bool
	Credential         string
}

// Credential is an issued credential (C 3.6.3); no format of it shows the secret.
type Credential struct {
	Kind, Secret string
}

func (c Credential) String() string { return c.Kind + " credential" }

// Format writes String for every verb, so the secret never reaches a log by a format.
func (c Credential) Format(f fmt.State, _ rune) {
	if _, err := io.WriteString(f, c.String()); err != nil {
		return
	}
}

// RunMember runs one member verb over the applied team (`team.json`), first completing any
// operation `pending` names (MO5).
func RunMember(ctx context.Context, d Deps, team declare.Team, req MemberRequest) (Result, error) {
	r := newRun(d, team, req.Op)
	err := r.member(ctx, req)
	r.res.Rooms = r.files.rooms
	return r.res, withOp(err, req.Op)
}

// RoleApply converges the coordinator to the applied team's (C 3.6.4, MO6).
func RoleApply(ctx context.Context, d Deps, team declare.Team) (Result, error) {
	r := newRun(d, team, OpRoleApply)
	err := r.roleApply(ctx)
	r.res.Rooms = r.files.rooms
	return r.res, withOp(err, OpRoleApply)
}

func withOp(err error, op string) error {
	if e, ok := errors.AsType[*Error](err); ok {
		e.Op = op
	}
	return err
}

func refusedf(format string, args ...any) error {
	return refused(StepFiles, fmt.Errorf(format, args...))
}

// loadApplied reads the files of a team `team apply` has made.
func (r *run) loadApplied() error {
	if err := r.files.load(); err != nil {
		return &Error{Step: StepFiles, Err: err}
	}
	if r.files.rooms.Main == "" {
		return refusedf("the team has no main room: run team apply first")
	}
	return nil
}

func (r *run) member(ctx context.Context, req MemberRequest) error {
	if err := r.loadApplied(); err != nil {
		return err
	}
	handle, err := r.target(req)
	if err != nil {
		return err
	}
	if p := r.files.roster.Pending; p != nil {
		if p.Op == req.Op && p.Handle == handle {
			r.res.Handle = handle
			return r.resume(ctx, p, req.Credential)
		}
		if err = r.rollForward(ctx); err != nil {
			return err
		}
		if handle, err = r.target(req); err != nil {
			return err
		}
	}
	r.res.Handle = handle
	if err = r.refuse(req, handle); err != nil {
		return err
	}
	return r.start(ctx, req, handle)
}

// target checks what a request says on its own and answers its handle (MO3, MO4).
func (r *run) target(req MemberRequest) (string, error) {
	if _, ok := opSteps[req.Op]; !ok || req.Op == OpRoleApply {
		return "", refusedf("operation %q is not a member operation", req.Op)
	}
	if issues(req.Op) && req.Credential != CredPassword && req.Credential != CredToken {
		return "", refusedf("the credential is password or token")
	}
	if req.Op == OpMemberAdd {
		return r.addTarget(req)
	}
	h, ok := proto.ParseHandle(req.Handle)
	switch {
	case !ok || h.String() != req.Handle:
		return "", refusedf("%q is not a member handle", req.Handle)
	case h.IsProbe():
		return "", refusedf("the probe handle is the migrate verbs' only")
	}
	return req.Handle, nil
}

// addTarget is the handle an `add` claims: the prefix numbered by the seat, or the next (MO4).
func (r *run) addTarget(req MemberRequest) (string, error) {
	p, ok := proto.ParsePrefix(req.Prefix)
	switch {
	case !ok || p.String() != req.Prefix:
		return "", refusedf("%q is not a handle prefix", req.Prefix)
	case req.Kind != p.Type:
		return "", refusedf("the kind %q is not the prefix's type %q", req.Kind, p.Type)
	case req.Seat < 0:
		return "", refusedf("the seat is a number from 1, or next")
	}
	seat := req.Seat
	if seat == 0 {
		seat = r.nextSeat(p)
	}
	h, ok := p.Numbered(seat)
	if !ok || h.IsProbe() {
		return "", refusedf("seat %d of %s is not a member handle", seat, p)
	}
	return h.String(), nil
}

// nextSeat is the lowest number from 1 no member of the prefix holds, in any state (n2d-D3).
func (r *run) nextSeat(p proto.Prefix) int {
	for n := 1; ; n++ {
		h, ok := p.Numbered(n)
		if !ok {
			return n
		}
		if _, held := r.files.roster.Members[h.String()]; !held {
			return n
		}
	}
}

func issues(op string) bool {
	return op == OpMemberAdd || op == OpMemberReturn || op == OpMemberReissue
}

// refuse is MO3's refusals of the handle's state, before any change.
func (r *run) refuse(req MemberRequest, h string) error {
	f := &r.files.roster
	m, known := f.Members[h]
	coordinator := h == r.team.Coordinator || h == f.Coordinator
	switch {
	case req.Op == OpMemberAdd && known:
		return refusedAdd(h, m.State)
	case req.Op == OpMemberAdd:
		return nil
	case !known:
		return refusedf("%s is not a member of the team", h)
	case !slices.Contains(fromStates[req.Op], m.State):
		return refusedf("member %s is %s; %s takes a member that is %s", h, m.State, req.Op, strings.Join(fromStates[req.Op], " or "))
	case req.Op == OpMemberRetire && coordinator:
		return refusedf("member %s is the coordinator: change the declaration and run team apply first", h)
	case req.Op == OpMemberSet && !req.HumanText && coordinator:
		return refusedf("member %s is the coordinator, which must read human text", h)
	}
	return nil
}

func refusedAdd(h, state string) error {
	switch state {
	case MemberActive:
		return refusedf("member %s is active: the seat number is taken, perhaps just now by another checkout of the same repository; choose again (member reissue only for a bundle this checkout lost)", h)
	case MemberParked, MemberLost:
		return refusedf("member %s is %s: bring it back with member return", h, state)
	case MemberRetired:
		return refusedf("member %s is retired and can never return", h)
	}
	return refusedf("member %s is %s by an add that did not finish", h, state)
}

// start makes the operation's first write, its pending record with it (MO1), and runs it.
func (r *run) start(ctx context.Context, req MemberRequest, h string) error {
	f := &r.files.roster
	m := f.Members[h]
	switch req.Op {
	case OpMemberAdd:
		if seat, numbered := mustHandle(h).SeatNumber(); numbered {
			f.Counters[req.Prefix] = max(f.Counters[req.Prefix], int64(seat))
		}
		m = Member{State: MemberClaimed, Kind: req.Kind, HumanText: req.HumanText, SinceMS: r.now()}
		r.note("member " + h + " claimed")
	case OpMemberRetire:
		m.State, m.SinceMS = MemberRetired, r.now()
		r.note("member " + h + " retired")
	case OpMemberSet:
		if m.HumanText == req.HumanText {
			return nil
		}
		m.HumanText = req.HumanText
		r.note("member " + h + " human_text " + strconv.FormatBool(req.HumanText))
	}
	f.Members[h] = m
	f.Pending = &Pending{Op: req.Op, Handle: h, Step: opSteps[req.Op][0], SinceMS: r.now()}
	if err := r.save(StepClaim); err != nil {
		return err
	}
	return r.resume(ctx, f.Pending, req.Credential)
}

func mustHandle(s string) proto.Handle {
	h, ok := proto.ParseHandle(s)
	if !ok {
		panic("apply: a handle the roster file holds does not parse: " + strconv.Quote(s))
	}
	return h
}

// rollForward completes the pending operation another verb started (MO5): a credential it
// issued is revoked and never answered.
func (r *run) rollForward(ctx context.Context) error {
	p := r.files.roster.Pending
	switch {
	case p == nil:
		return nil
	case p.Op == OpTeamApply && !isRoleStep(p.Step):
		if err := r.converge(ctx, StepRooms); err != nil {
			return err
		}
		return r.clear(StepRooms)
	case p.Op == OpTeamApply:
		if err := r.finishRole(ctx, p); err != nil {
			return err
		}
		if err := r.roles(ctx); err != nil {
			return err
		}
		return r.clear(StepRoles)
	}
	return r.resume(ctx, p, "")
}

func (r *run) clear(step string) error {
	r.files.roster.Pending = nil
	return r.save(step)
}

func isRoleStep(step string) bool { return strings.HasPrefix(step, StepRole+" ") }

// resume runs p's operation from its step. cred is the credential kind of the caller who asked
// for this handle, "" when another verb completes it, which revokes instead of issuing.
func (r *run) resume(ctx context.Context, p *Pending, cred string) error {
	steps := opSteps[p.Op]
	i := slices.Index(steps, p.Step)
	role := isRoleStep(p.Step)
	if role {
		i = slices.IndexFunc(steps, func(s string) bool { return s == StepRole || s == StepRoles })
	}
	if i < 0 {
		return refusedf("the pending operation %q has no step %q", p.Op, p.Step)
	}
	if c := slices.Index(steps, StepCredential); c >= 0 && i > c {
		if err := r.credential(ctx, p, cred); err != nil {
			return err
		}
		if err := r.save(p.Step); err != nil {
			return err
		}
	}
	if role {
		if err := r.finishRole(ctx, p); err != nil {
			return err
		}
	}
	for ; i < len(steps); i++ {
		p.Step = steps[i]
		if err := r.step(ctx, p, cred); err != nil {
			return err
		}
		if i+1 < len(steps) {
			p.Step = steps[i+1]
			if err := r.save(p.Step); err != nil {
				return err
			}
		}
	}
	return r.clear(p.Step)
}

// step runs one step of p's operation.
func (r *run) step(ctx context.Context, p *Pending, cred string) error {
	switch p.Step {
	case StepAccount:
		return r.account(ctx, p)
	case StepCredential:
		return r.credential(ctx, p, cred)
	case StepRevoke:
		return r.park(ctx, p)
	case StepRooms:
		return r.converge(ctx, StepRooms)
	case StepDeactivate:
		return r.deactivate(ctx, p)
	case StepNotice:
		return r.memberNotice(ctx, p)
	case StepRole:
		return r.promoteWanted(ctx, p)
	case StepRoles:
		if err := r.checkRoles(ctx); err != nil {
			return err
		}
		return r.roles(ctx)
	}
	return refusedf("the pending operation %q has no step %q", p.Op, p.Step)
}

func (r *run) userID(h string) string { return proto.UserID(h, r.team.ServerName) }

// account makes the member's account when there is none; a deactivated one is refused.
func (r *run) account(ctx context.Context, p *Pending) error {
	id := r.userID(p.Handle)
	acct, found, err := r.d.Admin.Account(ctx, id)
	switch {
	case err != nil:
		return failed(StepAccount, err)
	case found && acct.Deactivated:
		return refused(StepAccount, fmt.Errorf("member %s's account is deactivated", p.Handle))
	case found && r.files.roster.Members[p.Handle].State == MemberClaimed:
		r.note("member " + p.Handle + " account adopted")
	case !found:
		if _, err := r.d.Admin.CreateAccount(ctx, id, rand.Text(), ""); err != nil {
			return failed(StepAccount, err)
		}
		r.note("member " + p.Handle + " account created")
	}
	return nil
}

// credential issues the asked credential (a password reset with device logout, and for a token
// one login as the seat device), or with no caller for it revokes and marks a reissue (MO5).
func (r *run) credential(ctx context.Context, p *Pending, kind string) error {
	f := &r.files.roster
	m := f.Members[p.Handle]
	password := rand.Text()
	if err := r.d.Admin.ResetPassword(ctx, r.userID(p.Handle), password); err != nil {
		return failed(StepCredential, err)
	}
	m.State = MemberActive
	if kind == "" {
		m.Needs = needsReissue
		f.Members[p.Handle] = m
		r.res.Credential = nil
		r.note("member " + p.Handle + " revoked")
		return nil
	}
	c := &Credential{Kind: kind, Secret: password}
	if kind == CredToken {
		s, err := r.d.Admin.LoginDevice(ctx, p.Handle, password, seatDevice)
		if err != nil {
			return failed(StepCredential, err)
		}
		c.Secret = s.AccessToken.Reveal()
	}
	m.Cred, m.CredSeq, m.CredMS, m.Needs = kind, f.Seq+1, r.now(), ""
	f.Members[p.Handle] = m
	r.res.Credential = c
	r.note("member " + p.Handle + " issued")
	return nil
}

// park revokes the member's credential and marks it parked.
func (r *run) park(ctx context.Context, p *Pending) error {
	if err := r.d.Admin.ResetPassword(ctx, r.userID(p.Handle), rand.Text()); err != nil {
		return failed(StepRevoke, err)
	}
	m := r.files.roster.Members[p.Handle]
	m.State, m.SinceMS = MemberParked, r.now()
	r.files.roster.Members[p.Handle] = m
	r.note("member " + p.Handle + " parked")
	return nil
}

func (r *run) deactivate(ctx context.Context, p *Pending) error {
	id := r.userID(p.Handle)
	acct, found, err := r.d.Admin.Account(ctx, id)
	if err != nil {
		return failed(StepDeactivate, err)
	}
	if !found || acct.Deactivated {
		return nil
	}
	if err := r.d.Admin.SetDeactivated(ctx, id, true); err != nil {
		return failed(StepDeactivate, err)
	}
	r.note("member " + p.Handle + " deactivated")
	return nil
}

// memberNotice posts the notice of the change p made (MO7).
func (r *run) memberNotice(ctx context.Context, p *Pending) error {
	switch p.Op {
	case OpMemberAdd, OpMemberReturn:
		role := proto.RoleWorker
		if p.Handle == r.files.roster.Coordinator {
			role = proto.RoleCoordinator
		}
		return r.notice(ctx, p, proto.EventAdded, p.Handle, role)
	case OpMemberPark:
		return r.notice(ctx, p, proto.EventParked, p.Handle, "")
	}
	return r.notice(ctx, p, proto.EventRemoved, p.Handle, "")
}

// promoteWanted promotes an added or returned member the declaration names as coordinator; one
// that does not read human text stays wanted, and the answer says so (MO6).
func (r *run) promoteWanted(ctx context.Context, p *Pending) error {
	if p.Handle != r.team.Coordinator {
		return nil
	}
	if !r.files.roster.Members[p.Handle].HumanText {
		r.res.Notes = append(r.res.Notes, "coordinator "+p.Handle+" is declared but does not read human text, which a coordinator must: change the declaration or the member's human_text")
		return nil
	}
	return r.roles(ctx)
}

func (r *run) roleApply(ctx context.Context) error {
	if err := r.loadApplied(); err != nil {
		return err
	}
	if p := r.files.roster.Pending; p != nil && p.Op == OpRoleApply {
		return r.resume(ctx, p, "")
	}
	if err := r.rollForward(ctx); err != nil {
		return err
	}
	if err := r.checkRoles(ctx); err != nil {
		return err
	}
	if err := r.roles(ctx); err != nil {
		return err
	}
	if r.files.roster.Pending == nil {
		return nil
	}
	return r.clear(StepRoles)
}

// notice composes the admin's notice as A 7.4's composer judges it, over the roster just
// written, and posts it to main under a transaction ID derived from p, so a re-post is stored
// once (MO7).
func (r *run) notice(ctx context.Context, p *Pending, event, handle, role string) error {
	roster, err := DeriveRoster(r.files.roster, r.team)
	if err != nil {
		return refused(p.Step, err)
	}
	subject := r.userID(handle)
	to := slices.Clone(roster.Humans)
	if c, ok := roster.Coordinator(); ok && c != subject {
		to = append(to, c)
	}
	c := proto.Composer{Server: r.team.ServerName, Self: proto.AdminID(r.team.ServerName), Roster: roster}
	out, err := c.Notice(proto.Notice{Event: event, Subject: subject, Role: role, To: to}, noticeBody(event, handle, role))
	if err != nil {
		return refused(p.Step, err)
	}
	txn := noticeTxn(p, event, handle)
	if _, err := r.d.Admin.Send(ctx, r.files.rooms.Main, "m.room.message", txn, json.RawMessage(out.Content.Compact())); err != nil {
		return failed(p.Step, err)
	}
	r.note("notice " + event + " " + handle)
	return nil
}

// noticeBody is n2d-D5's one sentence per event.
func noticeBody(event, handle, role string) string {
	switch event {
	case proto.EventAdded:
		return handle + " was added to the team as " + role
	case proto.EventParked:
		return handle + " was parked by the owner"
	case proto.EventRemoved:
		return handle + " was removed from the team by the owner"
	}
	return handle + " is now the " + role
}

// noticeTxn is `tc` and 32 hex digits of SHA-256 over the operation, its start, its step and
// the notice.
func noticeTxn(p *Pending, event, handle string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{p.Op, p.Handle, strconv.FormatInt(p.SinceMS, 10), p.Step, event, handle}, "\x00")))
	return "tc" + hex.EncodeToString(sum[:16])
}

// finishRole completes a role step p records: the rooms converged and the `role` notice.
func (r *run) finishRole(ctx context.Context, p *Pending) error {
	parts := strings.Fields(p.Step)
	if len(parts) != 3 || (parts[1] != RoleDemoted && parts[1] != RolePromoted) || !validHandle(parts[2]) {
		return refusedf("the pending role step %q is not one this build wrote", p.Step)
	}
	if err := r.converge(ctx, p.Step); err != nil {
		return err
	}
	role := proto.RoleWorker
	if parts[1] == RolePromoted {
		role = proto.RoleCoordinator
	}
	return r.notice(ctx, p, proto.EventRole, parts[2], role)
}
