package lxc

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"path"
	"slices"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/unit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	memberstate "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// The why of each offline and deferred line and record entry (C 3.9.3, n3b-D1, n3b-D2).
const (
	whyStopped       = "offline: container stopped"
	whyMissing       = "offline: container missing"
	whyStoppedDuring = "offline: container stopped during the apply"
	whyBundle        = "offline: bundle not placed"
	whyMembership    = "offline: membership pending"
	whyUnitDown      = "offline: unit down; ccy-teams restart starts it"
	whySeatHeld      = "seat held; the new identity is placed at the next apply"
	whyBundleFailed  = "bundle not placed; reissued at the next apply"
	whyFailed        = "the last apply failed for this member"
	whyV1            = "v1 not yet absent"
	whyCheck         = "offline: guest check "
	whyOldUser       = "the seat's files under its old user are not yet removed"
	whyDeactivated   = "'s account is deactivated; give the entry another seat"
	whyMoved         = "the seat moved off its old user; it returns at the next apply"
)

// The steps an error names: step 01, step 03's reads, and the membership change a join or a
// leave makes.
const (
	stepValidate   = "validate"
	stepReads      = "reads"
	stepMembership = "membership"
	stepRecord     = "record"
)

// The guest files the reads take (C 3.9.2 step 03).
const (
	guestPasswd  = "/etc/passwd"
	guestGroup   = "/etc/group"
	guestSystemd = "/usr/lib/systemd/systemd"
	gitConfig    = "/.git/config"
	envLocal     = "/.claude/ccy/ccy.env.local"
	maxUnit      = 64 << 10
)

// The record statuses a phase uses beyond lxcstate's: the entry dropped, left as it is, or
// rewritten for a user move's new user.
const (
	statusDrop = "drop"
	statusKeep = "keep"
	statusMove = "move"
)

// member is one acted seat and what this phase made of it.
type member struct {
	decl declare.LXCMember
	team declare.Team
	box  paths.ContainerName
	id   lxcstate.SeatID
	key  paths.SeatKey
	// prior is the seat's record entry, when recorded.
	prior    lxcstate.Entry
	recorded bool
	handle   string
	// want is the handle step 03 derived from the guest's facts.
	want   string
	acct   guestfacts.Account
	group  string
	prefix proto.Prefix
	target bool
	// moveTo is a user move's declared user; the leaver is the record's (the absent phase).
	moveTo paths.GuestUser
	// done is set once the member takes no further step this phase.
	done bool
	// failed is a member whose phase failed: its record entry is left unless status is set.
	failed bool
	// joined is a join made this phase; settled is a seat whose bundle is in place.
	joined, settled bool
	// complete is a seat that took every step of the present phase.
	complete bool
	// heldSeat is a place-bundle 75: the seat is left out of its desired membership.
	heldSeat    bool
	joinOp      string
	bundle      []byte // never logged, never in an error
	status, why string
}

// needsReissue is a recorded handle whose bundle was never placed (n3-D23): reissued, from
// host facts only.
func (m *member) needsReissue() bool {
	return m.prior.Handle != "" && (m.prior.Why == whyBundle || m.prior.Why == whySeatHeld || m.prior.Why == whyBundleFailed)
}

// name is the member as errors name it: `<user>@<container>/<repo>-<seat>`.
func (m *member) name() string {
	return m.decl.User.String() + "@" + seatText(m.box, m.decl.Checkout, m.decl.Seat)
}

// seatText is a seat as messages name it: `<container>/<repo>-<seat>`.
func seatText(c paths.ContainerName, checkout string, seat int) string {
	return c.String() + "/" + paths.LXCRepoLabel(checkout) + "-" + strconv.Itoa(seat)
}

// The seat's unit, its file and its wants link (SU1).
func (m *member) unitName() string { return paths.SessionUnit(m.id.Team, m.id.Checkout, m.id.Seat) }

func (m *member) unitFile() string { return paths.GuestUnitFile(m.id.Team, m.id.Checkout, m.id.Seat) }

func (m *member) wantsLink() string {
	return paths.GuestUnitWantsLink(m.id.Team, m.id.Checkout, m.id.Seat)
}

// guestBox is one container of the phase.
type guestBox struct {
	name      paths.ContainerName
	root      Root
	cfg       guestfacts.Config
	members   []*member
	checkouts map[string]*checkoutFiles
	gone      bool
	missing   bool
}

// checkoutFiles is one checkout's guest files, read once per container however many seats
// name it.
type checkoutFiles struct {
	git, env []byte
	envUID   int
	envFound bool
}

// live is the box's members still taking steps.
func (g *guestBox) live() []*member {
	var out []*member
	for _, m := range g.members {
		if !m.done {
			out = append(out, m)
		}
	}
	return out
}

// checkKey is what one `guest check` answers for: a user and a team in a container.
type checkKey struct {
	box  paths.ContainerName
	user paths.GuestUser
	team paths.TeamID
}

// run is one phase in progress.
type run struct {
	d       Deps
	req     Request
	teams   []declare.Team
	origins map[paths.TeamID]string
	records map[paths.TeamID]lxcstate.Record
	members []*member
	boxes   []*guestBox
	lines   []Line
	errs    []error
	changed map[paths.TeamID]bool
	reach   map[paths.TeamID][]byte
	checked map[checkKey]error
	kit     *hostKit
}

// Apply is one `lxc apply` phase (C 3.9.2). Its error is every failed member's, joined, and the
// report holds the lines it reached; an offline member is a line, never an error, unless the
// request's --member names it.
func Apply(ctx context.Context, d Deps, req Request) (Report, error) {
	r, err := validate(ctx, d, req)
	if err != nil {
		return Report{}, err
	}
	defer r.close()
	switch req.Phase {
	case PhasePresent:
		r.present(ctx)
	case PhaseAbsent:
		r.absent(ctx)
	}
	r.finish(ctx)
	return report(r.lines), errors.Join(r.errs...)
}

// validate is step 01: every acted entry checked, each acted team's input held against its
// approval, and the seat keys of every team checked, before any container is touched.
func validate(ctx context.Context, d Deps, req Request) (*run, error) {
	if req.Phase != PhasePresent && req.Phase != PhaseAbsent {
		return nil, fmt.Errorf("lxc apply: phase %q is not absent or present", req.Phase)
	}
	r := &run{
		d: d, req: req, origins: map[paths.TeamID]string{}, records: map[paths.TeamID]lxcstate.Record{},
		changed: map[paths.TeamID]bool{}, reach: map[paths.TeamID][]byte{}, checked: map[checkKey]error{},
	}
	inputs := req.Decl.Inputs()
	for _, id := range req.OnlyTeams {
		if !slices.ContainsFunc(inputs, func(in declare.Input) bool { return in.ID() == id }) {
			return nil, invalid(fmt.Errorf("--only-teams names %s, which is not a present team", id))
		}
	}
	targets := map[Target]bool{}
	for _, in := range inputs {
		if req.OnlyTeams != nil && !slices.Contains(req.OnlyTeams, in.ID()) {
			continue
		}
		approved, err := d.Approvals.Load(in.ID())
		if err != nil {
			return nil, &host.Error{Verb: verb, Step: stepValidate, Err: err}
		}
		if err := declare.Hold(approved, in); err != nil {
			return nil, err
		}
		if err := r.team(ctx, in, targets); err != nil {
			return nil, err
		}
	}
	for _, t := range req.Members {
		if !targets[t] {
			return nil, invalid(fmt.Errorf("--member %s/%s names no seat of the acted teams", t.Container, t.Key))
		}
	}
	if err := r.keys(); err != nil {
		return nil, err
	}
	return r, nil
}

func invalid(err error) error {
	return &host.Error{Verb: verb, Step: stepValidate, Drift: true, Err: err}
}

// record is team's LXC record, read once per phase.
func (r *run) record(team paths.TeamID) (lxcstate.Record, error) {
	if rec, ok := r.records[team]; ok {
		return rec, nil
	}
	rec, _, err := lxcstate.ReadRecord(r.d.Root, team)
	if err != nil {
		return lxcstate.Record{}, &host.Error{Verb: verb, Step: stepValidate, Err: err}
	}
	r.records[team] = rec
	return rec, nil
}

// team adds one acted team: its stamp's origin, its record and its seats, each (container,
// checkout, seat) once.
func (r *run) team(ctx context.Context, in declare.Input, targets map[Target]bool) error {
	id := in.ID()
	origin, err := r.d.Origin(ctx, id)
	if err != nil {
		return &host.Error{Verb: verb, Step: stepValidate, Err: fmt.Errorf("team %s has no install stamp: %w", id, err)}
	}
	rec, err := r.record(id)
	if err != nil {
		return err
	}
	r.teams = append(r.teams, in.Team)
	r.origins[id] = origin
	for _, m := range in.LXC {
		c, err := paths.ParseContainerName(m.Container)
		if err != nil {
			return invalid(err)
		}
		sid, err := seatOf(id, m)
		if err != nil {
			return invalid(fmt.Errorf("an entry of container %s in team %s: %w", c, id, err))
		}
		t := Target{Container: c, Key: sid.Key()}
		if targets[t] {
			return invalid(fmt.Errorf("%s is declared twice in team %s", seatText(c, m.Checkout, m.Seat), id))
		}
		targets[t] = true
		if r.req.Members != nil && !slices.Contains(r.req.Members, t) {
			continue
		}
		mem := &member{decl: m, team: in.Team, box: c, id: sid, key: t.Key, target: r.req.Members != nil}
		if prior, found := rec.Find(c, m.Checkout, m.Seat); found {
			mem.prior, mem.handle, mem.recorded = prior, prior.Handle, true
		}
		r.members = append(r.members, mem)
	}
	return nil
}

// seatOf is m's seat in team, checked by declare's rule (DR4) before its key is made, since a
// key of any other panics.
func seatOf(team paths.TeamID, m declare.LXCMember) (lxcstate.SeatID, error) {
	c := m.Checkout
	if !paths.ValidLXCSeat(m.Seat) {
		return lxcstate.SeatID{}, fmt.Errorf("seat %d is not 1-%d", m.Seat, paths.MaxLXCSeat)
	}
	if !paths.ValidGuestPath(c) {
		return lxcstate.SeatID{}, errors.New("the checkout is not an absolute, clean path below / with no control or format character")
	}
	return lxcstate.SeatID{Team: team, Checkout: c, Seat: m.Seat}, nil
}

// keyOwners is step 01's key rule: each seat key of a container held by one seat.
type keyOwners map[Target]lxcstate.SeatID

func (o keyOwners) claim(c paths.ContainerName, id lxcstate.SeatID) error {
	t := Target{Container: c, Key: id.Key()}
	if held, ok := o[t]; ok && held != id {
		return invalid(fmt.Errorf("%s of team %s and %s of team %s have one seat key %s; give one entry another seat",
			seatText(c, held.Checkout, held.Seat), held.Team, seatText(c, id.Checkout, id.Seat), id.Team, t.Key))
	}
	o[t] = id
	return nil
}

// keys checks the key rule across every team's entries and record entries not left, whatever
// --only-teams says (C 3.9.2 step 01).
func (r *run) keys() error {
	owners := keyOwners{}
	for _, m := range r.req.Decl.LXC {
		c, err := paths.ParseContainerName(m.Container)
		if err != nil {
			continue // an acted team's entry was refused by team; another team's apply refuses its own
		}
		id, err := seatOf(m.Team, m)
		if err != nil {
			continue
		}
		if err := owners.claim(c, id); err != nil {
			return err
		}
	}
	for _, t := range r.req.Decl.Teams {
		rec, err := r.record(t.ID)
		if err != nil {
			return err
		}
		for _, e := range rec.Members {
			if e.Status == lxcstate.StatusLeft {
				continue
			}
			if err := owners.claim(e.Container, rec.SeatOf(e)); err != nil {
				return err
			}
		}
	}
	return nil
}

// close releases every pinned root.
func (r *run) close() {
	for _, g := range r.boxes {
		if g.root != nil {
			if err := g.root.Close(); err != nil {
				r.errs = append(r.errs, err)
			}
		}
	}
}

func (r *run) line(m *member, kind LineKind, why string) {
	r.lines = append(r.lines, Line{
		Kind: kind, Team: m.team.ID, User: m.decl.User, Container: m.box, Checkout: m.decl.Checkout,
		Seat: m.decl.Seat, Key: m.key, Why: why,
	})
}

// offline is n3b-D1 for one seat: no further step, the record offline with why, and a SKIPPED
// line when the record knows a handle, else DEFERRED. A pending reissue keeps its why, so the
// next apply that finds the container running still reissues.
func (r *run) offline(m *member, why string) {
	if m.done {
		return
	}
	if !m.joined && m.needsReissue() {
		why = whyBundle
	}
	m.done, m.status, m.why = true, lxcstate.StatusOffline, why
	kind := Deferred
	if m.handle != "" {
		kind = Skipped
	}
	r.line(m, kind, why)
	if m.target {
		r.errs = append(r.errs, &OfflineError{Container: m.box, Why: why})
	}
}

// boxGone is a container that stopped during the phase: each live seat offline, by how far it
// got.
func (r *run) boxGone(g *guestBox, why string) {
	g.gone = true
	for _, m := range g.live() {
		switch {
		case m.settled:
			r.offline(m, whyMembership)
		case m.joined:
			r.offline(m, whyBundle)
		default:
			r.offline(m, why)
		}
	}
}

// fail is one seat's failure: no further step, the error kept. A handle joined this phase is
// recorded deferred, so the next apply reissues it rather than adding a second member. A held
// seat is already done, and its failure is still kept.
func (r *run) fail(m *member, err error) {
	if m.done && !m.heldSeat {
		return
	}
	if !m.done {
		switch {
		case m.joined && !m.settled:
			m.status, m.why = lxcstate.StatusDeferred, whyBundleFailed
		case m.settled:
			m.status, m.why = lxcstate.StatusDeferred, whyFailed
		}
	}
	m.done, m.failed = true, true
	r.errs = append(r.errs, err)
}

// refuse is a running seat's wrong fact (exit 70), failing that seat alone.
func (r *run) refuse(m *member, step string, err error) {
	r.fail(m, &host.Error{Verb: verb, Step: step, Drift: true, Err: fmt.Errorf("%s: %w", m.name(), err)})
}

// gone is whether err, met in container c, means c has gone offline (n3b-D1): the pin moved
// or the root died; or an attach, an lxc-info call or an op that timed out, after which
// lxc-info says the container is no longer running. A guest op's own error object never is.
func (r *run) gone(ctx context.Context, c paths.ContainerName, err error) bool {
	if errors.Is(err, guestroot.ErrMoved) || errors.Is(err, guestroot.ErrGone) {
		return true
	}
	if ge, ok := errors.AsType[*lxccmd.GuestError](err); ok && ge != nil {
		return false
	}
	st, serr := r.d.LXC.State(ctx, c.String())
	return serr != nil || st != lxccmd.Running
}

// step is a failure of the steps after the reads, in a container: offline when the container
// went, else the seat's failure.
func (r *run) step(ctx context.Context, g *guestBox, m *member, name string, err error) {
	switch {
	case refused(err):
		r.refuse(m, name, err)
	case r.gone(ctx, g.name, err):
		r.boxGone(g, whyStoppedDuring)
	default:
		r.fail(m, &host.Error{Verb: verb, Step: name, Err: fmt.Errorf("%s: %w", m.name(), err)})
	}
}

// refused is a guest fact TB1 or a grammar refused.
func refused(err error) bool {
	return errors.Is(err, guestfacts.ErrRefused) || errors.Is(err, guestroot.ErrRefused) || errors.Is(err, guestroot.ErrTooLarge)
}

// present is C 3.9.2 steps 02-09.
func (r *run) present(ctx context.Context) {
	r.only(declare.StatePresent)
	r.holdMoves()
	if !r.containers(ctx) {
		return
	}
	for _, g := range r.boxes {
		r.reads(ctx, g)
	}
	r.equalHandles()
	for _, g := range r.boxes {
		if len(g.live()) > 0 && !r.placeKit(ctx, g) {
			return
		}
	}
	for _, g := range r.boxes {
		for _, m := range g.live() {
			r.check(ctx, g, m)
		}
	}
	for _, t := range r.teams {
		if err := r.joins(ctx, t.ID); err != nil {
			r.errs = append(r.errs, err)
			return
		}
	}
	for _, g := range r.boxes {
		for _, m := range g.members {
			r.membership(ctx, g, m)
			r.unit(ctx, g, m)
		}
	}
}

// holdMoves holds back each seat whose record entry names another user, a user move: the
// absent phase moves it off the old user first, and until then its entry is left as it is
// (C 3.9.3).
func (r *run) holdMoves() {
	for _, m := range r.members {
		if m.recorded && m.prior.User != m.decl.User {
			m.done, m.status = true, statusKeep
			r.line(m, Deferred, whyOldUser)
		}
	}
}

// containers is step 02: each seat's container listed and asked its state. A missing or not
// running one is offline; the running ones become the phase's boxes. False is a host fault.
func (r *run) containers(ctx context.Context) bool {
	byName := map[paths.ContainerName]*guestBox{}
	for _, m := range r.members {
		g, ok := byName[m.box]
		if !ok {
			g = &guestBox{name: m.box, checkouts: map[string]*checkoutFiles{}}
			byName[m.box] = g
			r.boxes = append(r.boxes, g)
		}
		g.members = append(g.members, m)
	}
	if len(r.boxes) == 0 {
		return true
	}
	names, err := r.d.LXC.Containers(ctx)
	if err != nil {
		r.errs = append(r.errs, &host.Error{Verb: verb, Step: "state", Err: err})
		return false
	}
	slices.SortFunc(r.boxes, func(a, b *guestBox) int { return cmp.Compare(a.name.String(), b.name.String()) })
	for _, g := range r.boxes {
		why := ""
		if !slices.Contains(names, g.name.String()) {
			why = whyMissing
		} else if st, err := r.d.LXC.State(ctx, g.name.String()); err != nil || st != lxccmd.Running {
			why = whyStopped
		}
		if why != "" {
			g.gone = true
			for _, m := range g.members {
				r.offline(m, why)
			}
		}
	}
	return true
}

// reads is step 03 for one running container: the pin, its settings and addresses, and each
// seat's facts, with nothing changed.
func (r *run) reads(ctx context.Context, g *guestBox) {
	if g.gone || len(g.live()) == 0 {
		return
	}
	root, err := r.d.Pin(ctx, g.name.String())
	if err != nil {
		r.readsFailed(ctx, g, "pin", err)
		return
	}
	g.root = root
	kv, err := r.d.LXC.Config(ctx, g.name.String(), guestfacts.ConfigKeys...)
	if err != nil {
		r.readsFailed(ctx, g, "config", err)
		return
	}
	if g.cfg, err = guestfacts.ParseConfig(kv); err != nil {
		r.readsFailed(ctx, g, "config", err)
		return
	}
	addrs, err := r.d.LXC.Addresses(ctx, g.name.String())
	if err != nil {
		r.readsFailed(ctx, g, "addresses", err)
		return
	}
	passwd, err := readGuest(root, guestPasswd)
	if err != nil {
		r.readsFailed(ctx, g, stepReads, err)
		return
	}
	for _, m := range g.live() {
		if err := r.member(ctx, g, m, passwd, addrs); err != nil {
			r.readsFailed(ctx, g, stepReads, err)
			return
		}
	}
}

// readsFailed routes a read's error: the container gone is offline for all its seats, a refused
// fact fails each of them by name, anything else fails each of them.
func (r *run) readsFailed(ctx context.Context, g *guestBox, step string, err error) {
	if !refused(err) && r.gone(ctx, g.name, err) {
		r.boxGone(g, whyStoppedDuring)
		return
	}
	for _, m := range g.live() {
		if refused(err) {
			r.refuse(m, step, err)
		} else {
			r.fail(m, &host.Error{Verb: verb, Step: step, Err: fmt.Errorf("container %s: %w", g.name, err)})
		}
	}
}

// readGuest is a guest account file (passwd, group) that must be there.
func readGuest(root Root, p string) ([]byte, error) {
	data, _, found, err := root.ReadFile(p, guestfacts.MaxPasswd)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: the guest has no %s", guestfacts.ErrRefused, p)
	}
	return data, nil
}

// member is step 03's reads of one seat; a refusal fails that seat alone, and only an error
// that is not the seat's own (the root gone, a host fault) is returned.
func (r *run) member(ctx context.Context, g *guestBox, m *member, passwd []byte, addrs []netip.Addr) error {
	err := r.facts(g, m, passwd, addrs)
	switch {
	case err == nil:
	case refused(err):
		r.refuse(m, stepReads, err)
		return nil
	default:
		return err
	}
	if r.origins[m.team.ID] != status.OriginMigration {
		return nil
	}
	held, err := r.d.V1.Holds(ctx, g.root, m.decl.User, m.team.ID)
	if err != nil {
		return err
	}
	if held {
		m.done, m.status, m.why = true, lxcstate.StatusDeferred, whyV1
		r.line(m, Deferred, whyV1)
	}
	return nil
}

// facts checks one seat's guest facts (C 3.9.2 step 03, LX4) and derives its handle: the
// entry's seat number under the checkout's prefix, which a recorded handle must equal.
func (r *run) facts(g *guestBox, m *member, passwd []byte, addrs []netip.Addr) error {
	acct, err := guestfacts.Passwd(passwd, m.decl.User, r.d.HostUIDs)
	if err != nil {
		return err
	}
	m.acct = acct
	if m.decl.Service {
		if err = r.service(g, m); err != nil {
			return err
		}
	}
	co, err := g.checkout(m.decl.Checkout)
	if err != nil {
		return err
	}
	if co.envFound && !r.owners(g, m.decl.Checkout, passwd)[co.envUID] {
		return fmt.Errorf("%w: %s%s is not owned by an agent user whose entries name the checkout", guestfacts.ErrRefused, m.decl.Checkout, envLocal)
	}
	if m.prefix, err = guestfacts.Prefix(m.decl, co.git, co.env); err != nil {
		return err
	}
	derived, ok := m.prefix.Numbered(m.decl.Seat)
	if !ok {
		return fmt.Errorf("%w: seat %d makes no handle of %s", guestfacts.ErrRefused, m.decl.Seat, m.prefix)
	}
	m.want = derived.String()
	if m.handle != "" {
		h, ok := proto.ParseHandle(m.handle)
		if !ok {
			return fmt.Errorf("%w: the record's handle is not a handle", guestfacts.ErrRefused)
		}
		if err := guestfacts.SameMember(derived, h); err != nil {
			return err
		}
	}
	return guestfacts.Source(addrs, m.team.AllowFrom, g.cfg.Link, r.d.Bridge)
}

// checkout is the checkout's .git/config and ccy.env.local, read once per container.
func (g *guestBox) checkout(dir string) (*checkoutFiles, error) {
	if co, ok := g.checkouts[dir]; ok {
		return co, nil
	}
	co := &checkoutFiles{}
	git, _, found, err := g.root.ReadFile(dir+gitConfig, guestfacts.MaxCheckout)
	if err != nil {
		return nil, err
	}
	if found {
		co.git = git
	}
	env, info, found, err := g.root.ReadFile(dir+envLocal, guestfacts.MaxCheckout)
	if err != nil {
		return nil, err
	}
	if found {
		co.env, co.envUID, co.envFound = env, int(info.UID), true
	}
	g.checkouts[dir] = co
	return co, nil
}

// owners is the uids of the agent users whose entries in the container name the checkout, any
// of whom may own its ccy.env.local (LX4), so seats of several users share one checkout.
func (r *run) owners(g *guestBox, dir string, passwd []byte) map[int]bool {
	out := map[int]bool{}
	for _, e := range r.req.Decl.LXC {
		if e.Container != g.name.String() || e.Checkout != dir {
			continue
		}
		if acct, err := guestfacts.Passwd(passwd, e.User, r.d.HostUIDs); err == nil {
			out[acct.UID] = true
		}
	}
	return out
}

// equalHandles is step 03's handle rule for each acted team: two seats whose handles are equal,
// derived here or held by a record entry not left, are refused by name; when the record holds
// the handle for one of them, the other is the wrong one.
func (r *run) equalHandles() {
	for _, t := range r.teams {
		holder := liveHandles(r.records[t.ID])
		first := map[string]*member{}
		for _, m := range r.members {
			if m.team.ID != t.ID || m.done || m.want == "" {
				continue
			}
			if e, ok := holder[m.want]; ok && (e.Container != m.box || e.Checkout != m.decl.Checkout || e.Seat != m.decl.Seat) {
				r.refuse(m, stepReads, fmt.Errorf("%w: its handle %s is held by %s@%s; give the entry another seat",
					guestfacts.ErrRefused, m.want, e.User, seatText(e.Container, e.Checkout, e.Seat)))
				continue
			}
			if o, ok := first[m.want]; ok {
				err := fmt.Errorf("%w: %s and %s derive one handle %s; give one entry another seat", guestfacts.ErrRefused, o.name(), m.name(), m.want)
				r.refuse(o, stepReads, err)
				r.refuse(m, stepReads, err)
				continue
			}
			first[m.want] = m
		}
	}
}

// liveHandles is each handle a record entry not left holds, with its entry.
func liveHandles(rec lxcstate.Record) map[string]lxcstate.Entry {
	holder := map[string]lxcstate.Entry{}
	for _, e := range rec.Members {
		if e.Handle != "" && e.Status != lxcstate.StatusLeft {
			holder[e.Handle] = e
		}
	}
	return holder
}

// service is the facts a session unit needs (SU1): start.auto, systemd as init (n3-D17), tmux
// where the unit runs it, and the unit's group.
func (r *run) service(g *guestBox, m *member) error {
	if !g.cfg.StartAuto {
		return fmt.Errorf("%w: %s is not 1; the container's own play owns its config", guestfacts.ErrRefused, guestfacts.KeyStartAuto)
	}
	exe, err := g.root.InitExe()
	if err != nil {
		return err
	}
	sd, err := g.root.Stat(guestSystemd)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err != nil || sd.Dev != exe.Dev || sd.Ino != exe.Ino {
		return fmt.Errorf("%w: the container's init is not %s; the container's own play owns its config", guestfacts.ErrRefused, guestSystemd)
	}
	if err = tmux(g.root); err != nil {
		return err
	}
	group, err := readGuest(g.root, guestGroup)
	if err != nil {
		return err
	}
	m.group, err = guestfacts.GroupName(group, m.acct.GID)
	return err
}

// tmux checks the tmux the unit runs (unit.Tmux) by TB1: a link or a non-regular file
// there is refused, and a tmux elsewhere on the guest PATH does not count.
func tmux(root Root) error {
	info, err := root.Stat(unit.Tmux)
	switch {
	case err == nil && info.Mode.IsRegular():
		return nil
	case err == nil, errors.Is(err, guestroot.ErrRefused), errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s is not a regular file; the container's own play installs it", guestfacts.ErrRefused, unit.Tmux)
	}
	return err
}

// attach is the seat's lxc-attach identity, the digest kit's binary (never current).
func (r *run) attach(m *member) lxccmd.Attach {
	return lxccmd.Attach{
		Container: m.box.String(), User: m.decl.User, Key: m.key, UID: m.acct.UID, GID: m.acct.GID,
		Home: m.acct.Home, Kit: paths.HostKitDir(r.d.Kit),
	}
}

// check is step 05, `guest check`, once per (user, team) in a container however many seats
// share it: gh or reach failing is offline for every such seat, any other error object fails it.
func (r *run) check(ctx context.Context, g *guestBox, m *member) {
	k := checkKey{box: m.box, user: m.decl.User, team: m.team.ID}
	err, ran := r.checked[k]
	if !ran {
		reach, reachErr := r.teamReach(ctx, m.team.ID)
		if reachErr != nil {
			r.fail(m, &host.Error{Verb: verb, Step: "guest check", Err: reachErr})
			return
		}
		_, err = r.d.LXC.Guest(ctx, r.attach(m), guest.OpCheck, []string{"--team", m.team.ID.String()}, reach)
		r.checked[k] = err
	}
	if err == nil {
		return
	}
	if ge, ok := errors.AsType[*lxccmd.GuestError](err); ok && (ge.Code == "gh" || ge.Code == "reach") {
		r.offline(m, whyCheck+ge.Code)
		return
	}
	r.step(ctx, g, m, "guest check", err)
}

func (r *run) teamReach(ctx context.Context, team paths.TeamID) ([]byte, error) {
	if b, ok := r.reach[team]; ok {
		return b, nil
	}
	b, err := r.d.Roster.Reach(ctx, team)
	if err == nil {
		r.reach[team] = b
	}
	return b, err
}

// boxOf is the seat's box.
func (r *run) boxOf(m *member) *guestBox {
	for _, g := range r.boxes {
		if g.name == m.box {
			return g
		}
	}
	return nil
}

// joins is steps 06 and 07 for one team under its lock: each live seat's join, decided from
// host facts and the roster (n3-D23), the record written with every joined handle, then each
// bundle placed. A busy lock fails the phase (75).
func (r *run) joins(ctx context.Context, team paths.TeamID) error {
	var live []*member
	for _, m := range r.members {
		if m.team.ID == team && !m.done {
			live = append(live, m)
		}
	}
	if len(live) == 0 {
		return nil
	}
	lock, err := teamlock.Take(ctx, r.d.LockDir, team, lockVerb, host.PlayWait, r.d.Clock)
	if err != nil {
		return &host.Error{Verb: verb, Step: host.StepLock, Err: err}
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			r.errs = append(r.errs, releaseErr)
		}
	}()
	roster, err := r.d.Roster.Members(ctx, team)
	if err != nil {
		r.failAll(live, "roster", err)
		return nil
	}
	var joined []*member
	for _, m := range live {
		if r.join(ctx, m, roster) {
			joined = append(joined, m)
		}
	}
	if err := r.recordJoins(team, joined); err != nil {
		r.failAll(joined, stepRecord, err)
		return nil
	}
	for _, m := range joined {
		r.place(ctx, m)
	}
	return nil
}

// join is one seat's join; true is a join made.
func (r *run) join(ctx context.Context, m *member, roster map[string]string) bool {
	j, need, err := joinOf(m, roster)
	if err != nil {
		r.refuse(m, "roster", err)
		return false
	}
	if !need {
		m.settled = true
		return false
	}
	got, err := r.d.Roster.Join(ctx, m.team.ID, j)
	if errors.Is(err, apply.ErrRetired) {
		why := "seat " + strconv.Itoa(m.decl.Seat) + whyDeactivated
		m.done, m.status, m.why = true, lxcstate.StatusDeferred, why
		r.line(m, Deferred, why)
		r.changed[m.team.ID] = true
		return false
	}
	if err == nil {
		err = joinedAs(j, m, got)
	}
	if err != nil {
		r.fail(m, &host.Error{Verb: verb, Step: "roster", Err: fmt.Errorf("%s: %w", m.name(), err)})
		return false
	}
	m.handle, m.joined, m.joinOp, m.bundle = got.Handle, true, j.Op, got.Bundle
	r.changed[m.team.ID] = true
	return true
}

// recordJoins writes the team's record as soon as its joins return, before any bundle is
// placed, each joined handle deferred: a join's handle is never unrecorded (n3-D12).
func (r *run) recordJoins(team paths.TeamID, joined []*member) error {
	if len(joined) == 0 {
		return nil
	}
	rec, now := r.records[team], r.d.Clock.Now().UnixMilli()
	for _, m := range joined {
		rec = rec.Set(entryOf(m, lxcstate.StatusDeferred, whyBundleFailed), now)
	}
	if _, err := lxcstate.WriteRecord(r.d.Root, rec, r.d.OwnerGID); err != nil {
		return err
	}
	r.records[team] = rec
	return nil
}

// place is step 07 for one joined seat: its bundle placed; a held seat (75) is deferred.
func (r *run) place(ctx context.Context, m *member) {
	_, err := r.d.LXC.Guest(ctx, r.attach(m), guest.OpPlaceBundle, nil, m.bundle)
	if ge, ok := errors.AsType[*lxccmd.GuestError](err); ok && ge.Exit == 75 {
		m.done, m.heldSeat, m.status, m.why = true, true, lxcstate.StatusDeferred, whySeatHeld
		r.line(m, Deferred, whySeatHeld)
		return
	}
	if err != nil {
		r.step(ctx, r.boxOf(m), m, "guest place-bundle", err)
		return
	}
	m.settled = true
	r.line(m, Changed, joinVerb[m.joinOp]+" "+m.handle)
}

var joinVerb = map[string]string{
	apply.OpMemberAdd: "joined as", apply.OpMemberReturn: "returned as", apply.OpMemberReissue: "reissued as",
}

// joinOf is the join a seat needs, chosen by the roster's state of its handle, the recorded one
// or else the derived one (C 3.9.2 step 03): none, or a pending add, is an add of the entry's
// seat; parked or lost returns; active is reissued unless the record holds it placed; a retired
// handle is refused by name.
func joinOf(m *member, roster map[string]string) (Join, bool, error) {
	handle := cmp.Or(m.handle, m.want)
	state, ok := roster[handle]
	switch {
	case !ok, state == apply.MemberClaimed:
		return Join{Op: apply.OpMemberAdd, Prefix: m.prefix, Seat: m.decl.Seat, HumanText: m.decl.HumanText}, true, nil
	case state == apply.MemberParked, state == apply.MemberLost:
		return Join{Op: apply.OpMemberReturn, Handle: handle}, true, nil
	case state == apply.MemberActive && (m.handle == "" || m.needsReissue()):
		return Join{Op: apply.OpMemberReissue, Handle: handle}, true, nil
	case state == apply.MemberActive:
		return Join{}, false, nil
	case state == apply.MemberRetired:
		return Join{}, false, fmt.Errorf("%w: seat %d of %s is retired; give the entry another seat", guestfacts.ErrRefused, m.decl.Seat, m.prefix)
	}
	return Join{}, false, fmt.Errorf("%w: handle %s is in a roster state the apply does not join", guestfacts.ErrRefused, handle)
}

// joinedAs checks the child's answer: the seat's derived handle for an add, else the same one.
func joinedAs(j Join, m *member, got Joined) error {
	want := j.Handle
	if j.Op == apply.OpMemberAdd {
		want = m.want
	}
	if got.Handle != want {
		return fmt.Errorf("the child answered %s for handle %s with another handle", j.Op, want)
	}
	return nil
}

// membership is step 08 for one seat: its host copy, holding the seat's own team once its bundle
// is placed (none for a held seat), written first, then `guest membership`. A converged copy
// writes nothing, but is written to a guest whose last membership did not land.
func (r *run) membership(ctx context.Context, g *guestBox, m *member) {
	if g.gone || !m.heldSeat && (m.done || !m.settled) {
		return
	}
	next, rerun, err := r.copyOf(m)
	if err != nil {
		r.fail(m, &host.Error{Verb: verb, Step: stepMembership, Err: fmt.Errorf("%s: %w", m.name(), err)})
		return
	}
	if !rerun {
		return
	}
	if err := r.desired(ctx, m, next); err != nil {
		r.step(ctx, g, m, "guest membership", err)
	}
}

// copyOf is the seat's next host copy, written when it changed, and whether `guest membership`
// runs: a change, or a seat whose last membership did not land.
func (r *run) copyOf(m *member) (lxcstate.Copy, bool, error) {
	cp, _, err := lxcstate.ReadCopy(r.d.Root, m.box, m.id)
	if err != nil {
		return lxcstate.Copy{}, false, err
	}
	var seats []memberstate.DesiredSeat
	if !m.heldSeat {
		h, ok := proto.ParseHandle(m.handle)
		if !ok {
			return lxcstate.Copy{}, false, errors.New("the seat's handle is not a handle")
		}
		seats = []memberstate.DesiredSeat{{Team: m.team.ID, Seat: h.Seat}}
	}
	next, changed := cp.Next(seats, nil)
	if changed {
		if err := lxcstate.WriteCopy(r.d.Root, m.box, next); err != nil {
			return lxcstate.Copy{}, false, err
		}
	}
	return next, changed || m.prior.Status != lxcstate.StatusJoined, nil
}

// desired is `guest membership` with the copy's desired.json on stdin.
func (r *run) desired(ctx context.Context, m *member, cp lxcstate.Copy) error {
	data, err := cp.Desired(r.d.Clock.Now().UnixMilli()).Encode()
	if err != nil {
		return err
	}
	_, err = r.d.LXC.Guest(ctx, r.attach(m), guest.OpMembership, nil, data)
	return err
}

func (r *run) failAll(ms []*member, step string, err error) {
	for _, m := range ms {
		r.fail(m, &host.Error{Verb: verb, Step: step, Err: err})
	}
}

// unit is step 09 for one seat (SU1, SU2): its unit written with its wants link, then Reload and
// Start (new or last found down) or Restart (changed); unchanged is left alone; `service: false`
// stops and removes it. systemd refusing, or gone, is the unit down: offline, never a failure.
func (r *run) unit(ctx context.Context, g *guestBox, m *member) {
	if g.gone || m.done || !m.settled {
		return
	}
	var err error
	if m.decl.Service {
		err = r.placeUnit(ctx, g, m)
	} else {
		err = r.removeUnit(ctx, g, m)
	}
	switch {
	case err == nil:
		m.complete = true
	case !refused(err) && unitDown(err) && !r.gone(ctx, g.name, err):
		r.offline(m, whyUnitDown)
	default:
		r.step(ctx, g, m, "unit", err)
	}
}

// errUnreachable is the guest's systemd that could not be dialled.
var errUnreachable = errors.New("the guest's systemd is unreachable")

// unitDown is the guest's systemd refusing a call, gone, or unreachable.
func unitDown(err error) bool {
	return errors.Is(err, guestbus.ErrSystemd) || errors.Is(err, guestbus.ErrGone) || errors.Is(err, errUnreachable)
}

func (r *run) placeUnit(ctx context.Context, g *guestBox, m *member) error {
	text, err := unit.Render(unit.Session{
		User: m.decl.User, Group: m.group, Home: m.acct.Home, Checkout: m.decl.Checkout, Team: m.team.ID, Seat: m.decl.Seat,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", guestfacts.ErrRefused, err)
	}
	file, link := m.unitFile(), m.wantsLink()
	_, _, existed, err := g.root.ReadFile(file, maxUnit)
	if err != nil {
		return err
	}
	w := g.root.Writer()
	if _, err = w.MkdirAll(path.Dir(file)); err != nil {
		return err
	}
	wrote, err := w.WriteFile(path.Dir(file), path.Base(file), text, 0o644)
	if err != nil {
		return err
	}
	if _, err = w.MkdirAll(path.Dir(link)); err != nil {
		return err
	}
	linked, err := w.Symlink(path.Dir(link), path.Base(link), file)
	retry := m.prior.Why == whyUnitDown
	if err != nil || !wrote && !linked && !retry {
		return err
	}
	how, start := "unit started", guestbus.Systemd.Start
	if existed && wrote {
		how, start = "unit restarted", guestbus.Systemd.Restart
	}
	err = r.systemd(ctx, g, func(s guestbus.Systemd) error {
		if reloadErr := s.Reload(ctx); reloadErr != nil {
			return reloadErr
		}
		return start(s, ctx, m.unitName())
	})
	if err == nil {
		r.line(m, Changed, how)
	}
	return err
}

// removeUnit stops and removes the seat's unit and its wants link, when it has one; no other
// seat's unit is touched.
func (r *run) removeUnit(ctx context.Context, g *guestBox, m *member) error {
	file, link := m.unitFile(), m.wantsLink()
	_, _, found, err := g.root.ReadFile(file, maxUnit)
	if err != nil || !found {
		return err
	}
	return r.systemd(ctx, g, func(s guestbus.Systemd) error {
		if err := s.Stop(ctx, m.unitName()); err != nil {
			return err
		}
		w := g.root.Writer()
		if _, err := w.Remove(path.Dir(file), path.Base(file)); err != nil {
			return err
		}
		if _, err := w.Remove(path.Dir(link), path.Base(link)); err != nil {
			return err
		}
		return s.Reload(ctx)
	})
}

// systemd runs calls on the guest's systemd, closing it after.
func (r *run) systemd(ctx context.Context, g *guestBox, calls func(guestbus.Systemd) error) error {
	s, err := r.d.Bus(ctx, g.root)
	if err != nil && !refused(err) {
		return fmt.Errorf("%w: %w", errUnreachable, err)
	}
	if err != nil {
		return err
	}
	return errors.Join(calls(s), s.Close())
}

// finish is step 10: each acted team's record, written only when it changed (LX9), and the
// status record refreshed for a team whose roster changed.
func (r *run) finish(ctx context.Context) {
	now := r.d.Clock.Now().UnixMilli()
	for _, t := range r.teams {
		rec := r.records[t.ID]
		for _, m := range r.members {
			if m.team.ID != t.ID || m.failed && m.status == "" {
				continue
			}
			rec = settle(rec, m, now)
		}
		if _, err := lxcstate.WriteRecord(r.d.Root, rec, r.d.OwnerGID); err != nil {
			r.errs = append(r.errs, &host.Error{Verb: verb, Step: stepRecord, Err: err})
		}
		if r.changed[t.ID] {
			if err := r.d.Refresh(ctx, t.ID); err != nil {
				r.errs = append(r.errs, &host.Error{Verb: verb, Step: "status", Err: err})
			}
		}
	}
}

// entryOf is the seat's record entry with status and why.
func entryOf(m *member, status, why string) lxcstate.Entry {
	return lxcstate.Entry{
		Container: m.box, Checkout: m.decl.Checkout, Seat: m.decl.Seat, User: m.decl.User, Handle: m.handle,
		Status: status, Why: why,
	}
}

// settle is one seat's record entry after the phase. A seat that set no status is joined only
// when it took every step; one whose steps the phase did not reach keeps its entry.
func settle(rec lxcstate.Record, m *member, now int64) lxcstate.Record {
	switch m.status {
	case statusDrop:
		return rec.Drop(m.box, m.decl.Checkout, m.decl.Seat)
	case statusKeep:
		return rec
	case statusMove:
		e := entryOf(m, lxcstate.StatusDeferred, whyMoved)
		e.User = m.moveTo
		return rec.Set(e, now)
	case "":
		if !m.complete || m.handle == "" {
			return rec
		}
		return rec.Set(entryOf(m, lxcstate.StatusJoined, ""), now)
	}
	return rec.Set(entryOf(m, m.status, m.why), now)
}
