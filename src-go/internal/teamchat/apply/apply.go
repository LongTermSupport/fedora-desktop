// Package apply is `team apply`'s convergence of one team after its homeserver is ready
// (Plan 00209, DESIGN-v2-C-host.md 3.5.2 step 10, 3.6.1, 3.6.2 steps 2-5, 3.6.4, 3.6.5;
// SPEC-n2b.md AP1-AP12): the approval gate first, then one server admin, the humans' accounts,
// the rooms through the rooms engine from the roster derived from the roster file and the
// approved team, and the coordinator role, demotion before promotion. It keeps the roster file
// and the rooms record, and hands the approved team back as `team.json` for the caller to write.
package apply

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
)

// Admin is the part of the matrix admin client apply calls.
type Admin interface {
	rooms.Client
	ServerAdmins(ctx context.Context) ([]string, error)
	Account(ctx context.Context, userID string) (matrix.Account, bool, error)
	CreateAccount(ctx context.Context, userID, password, displayName string) (matrix.Account, error)
	SetLocked(ctx context.Context, userID string, locked bool) error
}

// Deps is what a run uses: the admin client, the approvals, the team's two files
// (paths.RosterFile and paths.RoomsFile in production) and the clock of `since_ms`.
type Deps struct {
	Admin                 Admin
	Approvals             declare.Approvals
	RosterPath, RoomsPath string
	Now                   func() time.Time
}

// The steps a failure names (AP11); StepFiles is reading the roster file and rooms record.
const (
	StepGate   = "gate"
	StepAdmins = "admins"
	StepFiles  = "files"
	StepHumans = "humans"
	StepRooms  = "rooms"
	StepRoles  = "roles"
)

// Error is a failed run: the step, and whether it is drift apply refuses (exit 70).
type Error struct {
	Step  string
	Drift bool
	Err   error
}

func (e *Error) Error() string { return "team apply, step " + e.Step + ": " + e.Err.Error() }

func (e *Error) Unwrap() error { return e.Err }

func refused(step string, err error) error { return &Error{Step: step, Drift: true, Err: err} }

// failed is a step that could not finish; the rooms engine's drift stays drift.
func failed(step string, err error) error {
	re, ok := errors.AsType[*rooms.Error](err)
	return &Error{Step: step, Drift: ok && re.Drift, Err: err}
}

// The role changes a run answers (n2b-D9): the verb that posts notices posts the admin's
// `role` notice for each.
const (
	RoleDemoted  = "demoted"
	RolePromoted = "promoted"
)

// RoleChange is one coordinator demotion or promotion.
type RoleChange struct {
	Handle, Change string
}

// Result is what a run changed: `team.json` of the approved team, the CHANGED lines, the role
// changes and the rooms. On a failure it is what changed before it.
type Result struct {
	TeamFile []byte
	Changed  []string
	Roles    []RoleChange
	Rooms    proto.RoomIDs
}

type run struct {
	d     Deps
	team  declare.Team
	files *store
	res   Result
}

// Run converges the team of the input, which must be approved (AP5).
func Run(ctx context.Context, d Deps, in declare.Input) (Result, error) {
	approved, err := d.Approvals.Load(in.ID())
	if err != nil {
		return Result{}, &Error{Step: StepGate, Err: err}
	}
	if err = declare.Hold(approved, in); err != nil {
		return Result{}, refused(StepGate, err)
	}
	r := &run{
		d: d, team: asApproved(in.Team, *approved),
		files: &store{team: in.ID(), rosterPath: d.RosterPath, roomsPath: d.RoomsPath},
	}
	r.res.TeamFile = r.team.Canonical()
	for _, step := range []func(context.Context) error{r.admins, r.load, r.checkRoles, r.humans, r.rooms, r.lockRemoved, r.roles, r.finish} {
		if err = step(ctx); err != nil {
			break
		}
	}
	r.res.Rooms = r.files.rooms
	return r.res, err
}

// asApproved is the team with its gated scalars from the approval; the gate has already held
// the lists equal as sets, and their declared order is kept.
func asApproved(t declare.Team, g declare.Gated) declare.Team {
	t.Coordinator, t.URL, t.Trust = g.Coordinator, g.URL, g.Trust
	return t
}

func (r *run) now() int64 { return r.d.Now().UnixMilli() }

func (r *run) note(line string) { r.res.Changed = append(r.res.Changed, line) }

func (r *run) save(step string) error {
	if err := r.files.saveRoster(); err != nil {
		return failed(step, err)
	}
	return nil
}

// admins holds the server to one admin account, `@admin` (AP6).
func (r *run) admins(ctx context.Context) error {
	got, err := r.d.Admin.ServerAdmins(ctx)
	if err != nil {
		return failed(StepAdmins, err)
	}
	want := proto.AdminID(r.team.ServerName)
	if !slices.Equal(got, []string{want}) {
		others := slices.DeleteFunc(slices.Clone(got), func(id string) bool { return id == want })
		return refused(StepAdmins, fmt.Errorf("the server's admins must be %s alone; the others: %s", want, strings.Join(others, ", ")))
	}
	return nil
}

func (r *run) load(context.Context) error {
	if err := r.files.load(); err != nil {
		return &Error{Step: StepFiles, Err: err}
	}
	return nil
}

// checkRoles refuses a coordinator that can never hold the role before any change (n2b-D6).
func (r *run) checkRoles(context.Context) error {
	h := r.team.Coordinator
	m, ok := r.files.roster.Members[h]
	switch {
	case !ok:
		return nil
	case m.State == MemberRetired:
		return refused(StepRoles, fmt.Errorf("the declared coordinator %s is retired and can never be a member again", h))
	case !m.HumanText:
		return refused(StepRoles, fmt.Errorf("the declared coordinator %s does not read human text, which a coordinator must", h))
	}
	return nil
}

// humans gives each declared human an active account (AP7), every account read and judged
// before the first change, and marks each human no longer declared removed.
func (r *run) humans(ctx context.Context) error {
	accounts := map[string]*matrix.Account{}
	for _, name := range r.team.Humans {
		acct, found, err := r.d.Admin.Account(ctx, proto.UserID(name, r.team.ServerName))
		switch {
		case err != nil:
			return failed(StepHumans, err)
		case found && (acct.Admin || acct.Deactivated):
			return refused(StepHumans, fmt.Errorf("human %s's account is an admin or deactivated", name))
		case found:
			accounts[name] = &acct
		}
	}
	for _, name := range r.team.Humans {
		if err := r.human(ctx, name, accounts[name]); err != nil {
			return err
		}
	}
	f := &r.files.roster
	for _, name := range slices.Sorted(maps.Keys(f.Humans)) {
		if h := f.Humans[name]; h.State == HumanActive && !slices.Contains(r.team.Humans, name) {
			h.State, h.SinceMS = HumanRemoved, r.now()
			f.Humans[name] = h
			if err := r.save(StepHumans); err != nil {
				return err
			}
		}
	}
	return nil
}

// human creates the account (acct nil), or unlocks and re-activates one locked or removed.
func (r *run) human(ctx context.Context, name string, acct *matrix.Account) error {
	f := &r.files.roster
	id := proto.UserID(name, r.team.ServerName)
	h, known := f.Humans[name]
	switch {
	case acct == nil:
		if _, err := r.d.Admin.CreateAccount(ctx, id, rand.Text(), ""); err != nil {
			return failed(StepHumans, err)
		}
		f.Humans[name] = Human{State: HumanActive, SinceMS: r.now(), RevokedSeq: f.Seq + 1}
		r.note("human " + name + " created")
	case acct.Locked || (known && h.State == HumanRemoved):
		if acct.Locked {
			if err := r.d.Admin.SetLocked(ctx, id, false); err != nil {
				return failed(StepHumans, err)
			}
		}
		h.State, h.SinceMS = HumanActive, r.now()
		f.Humans[name] = h
		r.note("human " + name + " unlocked")
	case !known:
		f.Humans[name] = Human{State: HumanActive, SinceMS: r.now()}
	default:
		return nil
	}
	return r.save(StepHumans)
}

// rooms is bootstrap step 4 (AP8).
func (r *run) rooms(ctx context.Context) error { return r.converge(ctx, StepRooms) }

// converge runs the rooms engine over the roster derived now, and stores the roster rev the
// engine wrote, a failure's included.
func (r *run) converge(ctx context.Context, step string) error {
	roster, err := DeriveRoster(r.files.roster, r.team)
	if err != nil {
		return refused(step, err)
	}
	t := rooms.Team{Server: r.team.ServerName, Team: r.team.ID.String(), Description: r.team.Description, Roster: roster}
	res, err := rooms.Converge(ctx, r.d.Admin, recorder{r}, t, r.files.rooms)
	r.res.Changed = append(r.res.Changed, res.Changed...)
	if res.RosterRev != 0 {
		r.files.roster.Rev = res.RosterRev
		err = errors.Join(err, r.files.saveRoster())
	}
	if err != nil {
		return failed(step, err)
	}
	return nil
}

// lockRemoved locks every removed human's account after the rooms step kicked it (AP7).
func (r *run) lockRemoved(ctx context.Context) error {
	f := &r.files.roster
	for _, name := range slices.Sorted(maps.Keys(f.Humans)) {
		if f.Humans[name].State != HumanRemoved {
			continue
		}
		id := proto.UserID(name, r.team.ServerName)
		acct, found, err := r.d.Admin.Account(ctx, id)
		if err != nil {
			return failed(StepHumans, err)
		}
		if !found || acct.Locked {
			continue
		}
		if err := r.d.Admin.SetLocked(ctx, id, true); err != nil {
			return failed(StepHumans, err)
		}
		r.note("human " + name + " locked")
	}
	return nil
}

// roles converges the coordinator to the declared one (AP9): the old one demoted and the rooms
// converged before the new one is promoted; a declared one not yet a member is wanted.
func (r *run) roles(ctx context.Context) error {
	f := &r.files.roster
	want := r.team.Coordinator
	eligible := func(h string) bool {
		m, ok := f.Members[h]
		return ok && (m.State == MemberActive || m.State == MemberParked)
	}
	if cur := f.Coordinator; cur != "" && (cur != want || !eligible(cur)) {
		f.Coordinator = ""
		if err := r.role(ctx, cur, RoleDemoted); err != nil {
			return err
		}
	}
	wanted := ""
	switch {
	case want == "" || f.Coordinator == want:
	case eligible(want):
		f.Coordinator, f.CoordinatorWanted = want, ""
		if err := r.role(ctx, want, RolePromoted); err != nil {
			return err
		}
	default:
		wanted = want
	}
	if f.CoordinatorWanted == wanted {
		return nil
	}
	f.CoordinatorWanted = wanted
	return r.save(StepRoles)
}

func (r *run) role(ctx context.Context, handle, change string) error {
	r.note("role " + handle + " " + change)
	r.res.Roles = append(r.res.Roles, RoleChange{Handle: handle, Change: change})
	if err := r.save(StepRoles); err != nil {
		return err
	}
	return r.converge(ctx, StepRoles)
}

// finish clears the pending step a room create left (n2b-D8).
func (r *run) finish(context.Context) error {
	if r.files.roster.Pending == nil {
		return nil
	}
	r.files.roster.Pending = nil
	return r.save(StepRooms)
}

// recorder is the rooms engine's Recorder over the two files (AP3).
type recorder struct{ r *run }

func (rec recorder) Intent(purpose string) error {
	rec.r.files.roster.Pending = &Pending{Op: OpTeamApply, Step: "create " + purpose, SinceMS: rec.r.now()}
	return rec.r.files.saveRoster()
}

func (rec recorder) Record(purpose, roomID string) error {
	ids := &rec.r.files.rooms
	switch purpose {
	case proto.PurposeMain:
		ids.Main = roomID
	case proto.PurposeReports:
		ids.Reports = roomID
	case proto.PurposeQuestions:
		ids.Questions = roomID
	default:
		return fmt.Errorf("the rooms record has no %q room", purpose)
	}
	if err := rec.r.files.saveRooms(); err != nil {
		return err
	}
	if p := rec.r.files.roster.Pending; p != nil {
		p.Step = StepRooms
		return rec.r.files.saveRoster()
	}
	return nil
}

// placeholderRoom is a room ID of A's grammar, for validating a roster before its rooms exist.
func placeholderRoom(c string) string { return "!" + strings.Repeat(c, 43) }

// DeriveRoster is A's roster from the roster file and the applied team (AP4): the active
// declared humans, every active or parked member, A's default limits. It is validated by A's
// rules, with placeholder rooms, before the rooms step uses it.
func DeriveRoster(f RosterFile, team declare.Team) (proto.Roster, error) {
	server := team.ServerName
	r := proto.Roster{
		Team: team.ID.String(), Name: team.Name, Rev: f.Rev, RestoreEpoch: f.RestoreEpoch,
		Members: map[string]proto.RosterMember{}, Forge: team.Trust, Limits: proto.DefaultLimits(),
	}
	for _, name := range team.Humans {
		if h, ok := f.Humans[name]; ok && h.State == HumanActive {
			r.Humans = append(r.Humans, proto.UserID(name, server))
		}
	}
	for handle, m := range f.Members {
		if m.State != MemberActive && m.State != MemberParked {
			continue
		}
		role := proto.RoleWorker
		if handle == f.Coordinator {
			role = proto.RoleCoordinator
		}
		r.Members[proto.UserID(handle, server)] = proto.RosterMember{Role: role, HumanText: m.HumanText, Parked: m.State == MemberParked}
	}
	check := r
	check.Rev = max(r.Rev, 1)
	check.Rooms = proto.SideRooms{Reports: placeholderRoom("r"), Questions: placeholderRoom("q")}
	if err := check.Validate(server, r.Team, placeholderRoom("m")); err != nil {
		return proto.Roster{}, err
	}
	return r, nil
}
