package lxc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"path"
	"slices"

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

// The why of each offline and deferred line and record entry (C 3.9.3, n3b-D1).
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
)

// stepValidate is step 01's name in an error.
const stepValidate = "validate"

// The guest files the reads take (C 3.9.2 step 03).
const (
	guestPasswd  = "/etc/passwd"
	guestGroup   = "/etc/group"
	guestSystemd = "/usr/lib/systemd/systemd"
	gitConfig    = "/.git/config"
	envLocal     = "/.claude/ccy/ccy.env.local"
	maxUnit      = 64 << 10
)

// member is one acted entry and what this phase made of it.
type member struct {
	decl   declare.LXCMember
	team   declare.Team
	box    paths.ContainerName
	prior  lxcstate.Entry
	handle string
	acct   guestfacts.Account
	group  string
	prefix proto.Prefix
	target bool
	// done is set once the member takes no further step this phase.
	done bool
	// failed is a member whose phase failed: its record entry is left unless status is set.
	failed bool
	// joined is a join made this phase; settled is a member whose bundle is in place.
	joined, settled bool
	// heldSeat is a place-bundle 75: the seat is left out of the desired seats.
	heldSeat    bool
	status, why string
}

// needsReissue is a recorded handle whose bundle was never placed (n3-D23): reissued, from
// host facts only.
func (m *member) needsReissue() bool {
	return m.prior.Handle != "" && (m.prior.Why == whyBundle || m.prior.Why == whySeatHeld || m.prior.Why == whyBundleFailed)
}

// guestBox is one container of the phase.
type guestBox struct {
	name    paths.ContainerName
	root    Root
	cfg     guestfacts.Config
	members []*member
	gone    bool
	missing bool
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

// validate is step 01: every acted entry checked, and each acted team's input held against its
// approval, before any container is touched.
func validate(ctx context.Context, d Deps, req Request) (*run, error) {
	if req.Phase != PhasePresent && req.Phase != PhaseAbsent {
		return nil, fmt.Errorf("lxc apply: phase %q is not absent or present", req.Phase)
	}
	r := &run{
		d: d, req: req, origins: map[paths.TeamID]string{},
		records: map[paths.TeamID]lxcstate.Record{}, changed: map[paths.TeamID]bool{}, reach: map[paths.TeamID][]byte{},
	}
	inputs := req.Decl.Inputs()
	for _, id := range req.OnlyTeams {
		if !slices.ContainsFunc(inputs, func(in declare.Input) bool { return in.ID() == id }) {
			return nil, invalid(fmt.Errorf("--only-teams names %s, which is not a present team", id))
		}
	}
	seen := map[string]bool{}
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
		if err := r.team(ctx, in, seen, targets); err != nil {
			return nil, err
		}
	}
	for _, t := range req.Members {
		if !targets[t] {
			return nil, invalid(fmt.Errorf("--member %s/%s names no entry of the acted teams", t.Container, t.User))
		}
	}
	return r, nil
}

func invalid(err error) error {
	return &host.Error{Verb: verb, Step: stepValidate, Drift: true, Err: err}
}

// team adds one acted team: its stamp's origin, its record and its entries.
func (r *run) team(ctx context.Context, in declare.Input, seen map[string]bool, targets map[Target]bool) error {
	id := in.ID()
	origin, err := r.d.Origin(ctx, id)
	if err != nil {
		return &host.Error{Verb: verb, Step: stepValidate, Err: fmt.Errorf("team %s has no install stamp: %w", id, err)}
	}
	rec, _, err := lxcstate.ReadRecord(r.d.Root, id)
	if err != nil {
		return &host.Error{Verb: verb, Step: stepValidate, Err: err}
	}
	r.teams = append(r.teams, in.Team)
	r.origins[id], r.records[id] = origin, rec
	for _, m := range in.LXC {
		c, err := paths.ParseContainerName(m.Container)
		if err != nil {
			return invalid(err)
		}
		key := id.String() + "\x00" + c.String() + "\x00" + m.User.String()
		if seen[key] {
			return invalid(fmt.Errorf("%s@%s is declared twice in team %s", m.User, c, id))
		}
		seen[key] = true
		t := Target{Container: c, User: m.User}
		targets[t] = true
		if r.req.Members != nil && !slices.Contains(r.req.Members, t) {
			continue
		}
		mem := &member{decl: m, team: in.Team, box: c, target: r.req.Members != nil}
		if prior, found := rec.Find(c, m.User); found {
			mem.prior, mem.handle = prior, prior.Handle
		}
		r.members = append(r.members, mem)
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
	r.lines = append(r.lines, Line{Kind: kind, Team: m.team.ID, User: m.decl.User, Container: m.box, Why: why})
}

// offline is n3b-D1 for one member: no further step, the record offline with why, and a
// SKIPPED line when the record knows a handle, else DEFERRED. A pending reissue keeps its why,
// so the next apply that finds the container running still reissues.
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

// boxGone is a container that stopped during the phase: each live member offline, by how far
// it got.
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

// fail is one member's failure: no further step, the error kept. A handle joined this phase is
// recorded deferred, so the next apply reissues it rather than adding a second member.
func (r *run) fail(m *member, err error) {
	if m.done {
		return
	}
	m.done, m.failed = true, true
	switch {
	case m.joined && !m.settled:
		m.status, m.why = lxcstate.StatusDeferred, whyBundleFailed
	case m.settled:
		m.status, m.why = lxcstate.StatusDeferred, whyFailed
	}
	r.errs = append(r.errs, err)
}

// refuse is a running member's wrong fact (exit 70), failing that member alone.
func (r *run) refuse(m *member, step string, err error) {
	r.fail(m, &host.Error{Verb: verb, Step: step, Drift: true, Err: fmt.Errorf("%s@%s: %w", m.decl.User, m.box, err)})
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
// went, else the member's failure.
func (r *run) step(ctx context.Context, g *guestBox, m *member, name string, err error) {
	switch {
	case refused(err):
		r.refuse(m, name, err)
	case r.gone(ctx, g.name, err):
		r.boxGone(g, whyStoppedDuring)
	default:
		r.fail(m, &host.Error{Verb: verb, Step: name, Err: fmt.Errorf("%s@%s: %w", m.decl.User, m.box, err)})
	}
}

// refused is a guest fact TB1 or a grammar refused.
func refused(err error) bool {
	return errors.Is(err, guestfacts.ErrRefused) || errors.Is(err, guestroot.ErrRefused) || errors.Is(err, guestroot.ErrTooLarge)
}

// present is C 3.9.2 steps 02-09.
func (r *run) present(ctx context.Context) {
	r.only(declare.StatePresent)
	if !r.containers(ctx) {
		return
	}
	for _, g := range r.boxes {
		r.reads(ctx, g)
	}
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
		for _, u := range users(g) {
			r.membership(ctx, g, u)
			r.unit(ctx, g, u)
		}
	}
}

// containers is step 02: each entry's container listed and asked its state. A missing or not
// running one is offline; the running ones become the phase's boxes. False is a host fault.
func (r *run) containers(ctx context.Context) bool {
	byName := map[paths.ContainerName]*guestBox{}
	for _, m := range r.members {
		g, ok := byName[m.box]
		if !ok {
			g = &guestBox{name: m.box}
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
	slices.SortFunc(r.boxes, func(a, b *guestBox) int { return compare(a.name.String(), b.name.String()) })
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

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// users is the box's guest users, in order.
func users(g *guestBox) []paths.GuestUser {
	var out []paths.GuestUser
	for _, m := range g.members {
		if !slices.Contains(out, m.decl.User) {
			out = append(out, m.decl.User)
		}
	}
	slices.SortFunc(out, func(a, b paths.GuestUser) int { return compare(a.String(), b.String()) })
	return out
}

// reads is step 03 for one running container: the pin, its settings and addresses, and each
// member's facts, with nothing changed.
func (r *run) reads(ctx context.Context, g *guestBox) {
	if g.gone {
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
	passwd, err := readGuest(root, guestPasswd, guestfacts.MaxPasswd)
	if err != nil {
		r.readsFailed(ctx, g, "reads", err)
		return
	}
	for _, m := range g.live() {
		if err := r.member(ctx, g, m, passwd, addrs); err != nil {
			r.readsFailed(ctx, g, "reads", err)
			return
		}
	}
}

// readsFailed routes a read's error: the container gone is offline for all its members, a
// refused fact fails each of them by name, anything else fails each of them.
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

// readGuest is a guest file that must be there.
func readGuest(root Root, p string, limit int64) ([]byte, error) {
	data, _, found, err := root.ReadFile(p, limit)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: the guest has no %s", guestfacts.ErrRefused, p)
	}
	return data, nil
}

// member is step 03's reads of one entry; a refusal fails that member alone, and only an error
// that is not the member's own (the root gone, a host fault) is returned.
func (r *run) member(ctx context.Context, g *guestBox, m *member, passwd []byte, addrs []netip.Addr) error {
	err := r.facts(g, m, passwd, addrs)
	switch {
	case err == nil:
	case refused(err):
		r.refuse(m, "reads", err)
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

// facts checks one entry's guest facts (C 3.9.2 step 03, LX4).
func (r *run) facts(g *guestBox, m *member, passwd []byte, addrs []netip.Addr) error {
	acct, err := guestfacts.Passwd(passwd, m.decl.User, r.d.HostUIDs)
	if err != nil {
		return err
	}
	m.acct = acct
	if m.decl.Service {
		if err := r.service(g, m); err != nil {
			return err
		}
	}
	git, _, found, err := g.root.ReadFile(m.decl.Checkout+gitConfig, guestfacts.MaxCheckout)
	if err != nil {
		return err
	}
	if !found {
		git = nil
	}
	env, info, found, err := g.root.ReadFile(m.decl.Checkout+envLocal, guestfacts.MaxCheckout)
	if err != nil {
		return err
	}
	if !found {
		env = nil
	} else if int(info.UID) != acct.UID {
		return fmt.Errorf("%w: %s%s is not owned by %s", guestfacts.ErrRefused, m.decl.Checkout, envLocal, m.decl.User)
	}
	if m.prefix, err = guestfacts.Prefix(m.decl, git, env); err != nil {
		return err
	}
	if m.handle != "" {
		h, ok := proto.ParseHandle(m.handle)
		if !ok {
			return fmt.Errorf("%w: the record's handle is not a handle", guestfacts.ErrRefused)
		}
		if err := guestfacts.SameMember(m.prefix, h); err != nil {
			return fmt.Errorf("%w; the checkout's origin changed: retire and add the member to change its handle", err)
		}
	}
	return guestfacts.Source(addrs, m.team.AllowFrom, g.cfg.Link, r.d.Bridge)
}

// service is the facts a session unit needs (SU1): start.auto, systemd as init (n3-D17), tmux
// on the guest PATH, and the unit's group.
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
	if err := tmux(g.root); err != nil {
		return err
	}
	group, err := readGuest(g.root, guestGroup, guestfacts.MaxPasswd)
	if err != nil {
		return err
	}
	m.group, err = guestfacts.GroupName(group, m.acct.GID)
	return err
}

// tmux finds tmux in a guest PATH directory by TB1: a link or a non-regular file at one entry is
// not there, and the next is tried.
func tmux(root Root) error {
	var tried []string
	for _, dir := range guest.SystemDirs {
		p := dir + "/tmux"
		info, err := root.Stat(p)
		switch {
		case err == nil && info.Mode.IsRegular():
			return nil
		case err == nil, errors.Is(err, guestroot.ErrRefused), errors.Is(err, fs.ErrNotExist):
			tried = append(tried, p)
		default:
			return err
		}
	}
	return fmt.Errorf("%w: tmux is not a regular file at %v; the container's own play installs it", guestfacts.ErrRefused, tried)
}

// attach is the member's lxc-attach identity, the digest kit's binary (never current).
func (r *run) attach(m *member) lxccmd.Attach {
	return lxccmd.Attach{
		Container: m.box.String(), User: m.decl.User, UID: m.acct.UID, GID: m.acct.GID,
		Home: m.acct.Home, Kit: paths.HostKitDir(r.d.Kit),
	}
}

// check is step 05, `guest check`: gh or reach failing is offline for that member alone, any
// other error object fails it.
func (r *run) check(ctx context.Context, g *guestBox, m *member) {
	reach, err := r.teamReach(ctx, m.team.ID)
	if err != nil {
		r.fail(m, &host.Error{Verb: verb, Step: "guest check", Err: err})
		return
	}
	_, err = r.d.LXC.Guest(ctx, r.attach(m), guest.OpCheck, []string{"--team", m.team.ID.String()}, reach)
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

// boxOf is the member's box.
func (r *run) boxOf(m *member) *guestBox {
	for _, g := range r.boxes {
		if g.name == m.box {
			return g
		}
	}
	return nil
}

// joins is steps 06 and 07 for one team under its lock: each live member's join, decided from
// host facts (n3-D23), then its bundle placed. A busy lock fails the phase (75).
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
		if err := lock.Release(); err != nil {
			r.errs = append(r.errs, err)
		}
	}()
	roster, err := r.d.Roster.Members(ctx, team)
	if err != nil {
		r.failAll(live, "roster", err)
		return nil
	}
	for _, m := range live {
		r.join(ctx, m, roster)
	}
	return nil
}

// join is one member's join and placement.
func (r *run) join(ctx context.Context, m *member, roster map[string]string) {
	j, need, err := joinOf(m, roster)
	if err != nil {
		r.refuse(m, "roster", err)
		return
	}
	if !need {
		m.settled = true
		return
	}
	got, err := r.d.Roster.Join(ctx, m.team.ID, j)
	if err == nil {
		err = joinedAs(j, m, got)
	}
	if err != nil {
		r.fail(m, &host.Error{Verb: verb, Step: "roster", Err: err})
		return
	}
	m.handle, m.joined = got.Handle, true
	r.changed[m.team.ID] = true
	_, err = r.d.LXC.Guest(ctx, r.attach(m), guest.OpPlaceBundle, nil, got.Bundle)
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
	r.line(m, Changed, joinVerb[j.Op]+" "+m.handle)
}

var joinVerb = map[string]string{
	apply.OpMemberAdd: "joined as", apply.OpMemberReturn: "returned as", apply.OpMemberReissue: "reissued as",
}

// joinOf is the join a member needs from host facts only (n3-D23): none recorded is an add; a
// parked handle returns; a lost one, or one whose bundle was never placed, is reissued; an
// active, placed one is left alone. A retired handle is refused by name.
func joinOf(m *member, roster map[string]string) (Join, bool, error) {
	if m.handle == "" {
		return Join{Op: apply.OpMemberAdd, Prefix: m.prefix, Seat: m.decl.Seat, HumanText: m.decl.HumanText}, true, nil
	}
	state, ok := roster[m.handle]
	switch {
	case !ok:
		return Join{}, false, fmt.Errorf("%w: handle %s is not on the roster; retire and add the member", guestfacts.ErrRefused, m.handle)
	case state == apply.MemberParked:
		return Join{Op: apply.OpMemberReturn, Handle: m.handle}, true, nil
	case state == apply.MemberLost, state == apply.MemberActive && m.needsReissue():
		return Join{Op: apply.OpMemberReissue, Handle: m.handle}, true, nil
	case state == apply.MemberActive:
		return Join{}, false, nil
	case state == apply.MemberRetired:
		return Join{}, false, fmt.Errorf("%w: handle %s is retired; retire and add the member to join it again", guestfacts.ErrRefused, m.handle)
	}
	return Join{}, false, fmt.Errorf("%w: handle %s is in a roster state the apply does not join", guestfacts.ErrRefused, m.handle)
}

// joinedAs checks the child's answer: a new handle of the member's own prefix, or the same one.
func joinedAs(j Join, m *member, got Joined) error {
	if j.Op != apply.OpMemberAdd {
		if got.Handle != m.handle {
			return fmt.Errorf("the child answered %s for handle %s with another handle", j.Op, m.handle)
		}
		return nil
	}
	h, ok := proto.ParseHandle(got.Handle)
	if !ok || h.Prefix() != m.prefix {
		return fmt.Errorf("the child's %s answered a handle outside %s", j.Op, m.prefix)
	}
	return nil
}

// group is a box's members of one guest user.
func group(g *guestBox, u paths.GuestUser) []*member {
	var out []*member
	for _, m := range g.members {
		if m.decl.User == u {
			out = append(out, m)
		}
	}
	return out
}

// membership is step 08 for one guest user: the seats of every team it is joined to (a team
// outside the scope taken from the host copy, n3-D13), less a held seat; the host copy
// written first, then `guest membership`. A converged copy writes nothing, but is written to
// a guest whose last membership did not land.
func (r *run) membership(ctx context.Context, g *guestBox, u paths.GuestUser) {
	ms := group(g, u)
	var active []*member
	rerun := false
	for _, m := range ms {
		if !m.done && m.settled || m.heldSeat {
			active = append(active, m)
			rerun = rerun || m.prior.Status != lxcstate.StatusJoined
		}
	}
	if g.gone || len(active) == 0 {
		return
	}
	cp, _, err := lxcstate.ReadCopy(r.d.Root, g.name, u)
	if err != nil {
		r.failAll(active, "membership", err)
		return
	}
	next, changed := cp.Next(seats(cp, ms), nil)
	if changed {
		if err := lxcstate.WriteCopy(r.d.Root, g.name, u, next); err != nil {
			r.failAll(active, "membership", err)
			return
		}
	}
	if !changed && !rerun {
		return
	}
	if err := r.desired(ctx, active[0], next); err != nil {
		for _, m := range active {
			r.step(ctx, g, m, "guest membership", err)
		}
	}
}

// desired is `guest membership` with the copy's desired.json on stdin.
func (r *run) desired(ctx context.Context, m *member, cp lxcstate.Copy) error {
	data, err := cp.Desired(m.decl.User, r.d.Clock.Now().UnixMilli()).Encode()
	if err != nil {
		return err
	}
	_, err = r.d.LXC.Guest(ctx, r.attach(m), guest.OpMembership, nil, data)
	return err
}

// seats is the user's desired seats: each acted team's settled handle, a held seat left out, a
// member that took no step keeping the copy's seat, and every other team's seat as copied.
func seats(cp lxcstate.Copy, ms []*member) []memberstate.DesiredSeat {
	byTeam := map[paths.TeamID]*member{}
	for _, m := range ms {
		byTeam[m.team.ID] = m
	}
	var out []memberstate.DesiredSeat
	for _, s := range cp.Seats {
		if m, ok := byTeam[s.Team]; !ok || !m.settled && !m.heldSeat {
			out = append(out, s)
		}
	}
	for _, m := range ms {
		if !m.settled || m.heldSeat {
			continue
		}
		if h, ok := proto.ParseHandle(m.handle); ok {
			out = append(out, memberstate.DesiredSeat{Team: m.team.ID, Seat: h.Seat})
		}
	}
	return out
}

func (r *run) failAll(ms []*member, step string, err error) {
	for _, m := range ms {
		r.fail(m, &host.Error{Verb: verb, Step: step, Err: err})
	}
}

// unit is step 09 for one guest user (SU1, SU2): the unit written with its wants link, then
// Reload and Start (new or last found down) or Restart (changed); unchanged is left alone;
// `service: false` stops and removes it. systemd refusing, or gone, is the unit down: offline, never a failure.
func (r *run) unit(ctx context.Context, g *guestBox, u paths.GuestUser) {
	var active []*member
	for _, m := range group(g, u) {
		if !m.done && m.settled {
			active = append(active, m)
		}
	}
	if g.gone || len(active) == 0 {
		return
	}
	m := active[0]
	var err error
	if m.decl.Service {
		err = r.placeUnit(ctx, g, m)
	} else {
		err = r.removeUnit(ctx, g, u)
	}
	if err == nil {
		return
	}
	if !refused(err) && unitDown(err) && !r.gone(ctx, g.name, err) {
		for _, a := range active {
			r.offline(a, whyUnitDown)
		}
		return
	}
	for _, a := range active {
		r.step(ctx, g, a, "unit", err)
	}
}

// errUnreachable is the guest's systemd that could not be dialled.
var errUnreachable = errors.New("the guest's systemd is unreachable")

// unitDown is the guest's systemd refusing a call, gone, or unreachable.
func unitDown(err error) bool {
	return errors.Is(err, guestbus.ErrSystemd) || errors.Is(err, guestbus.ErrGone) || errors.Is(err, errUnreachable)
}

func (r *run) placeUnit(ctx context.Context, g *guestBox, m *member) error {
	u := m.decl.User
	text, err := unit.Render(unit.Session{User: u, Group: m.group, Home: m.acct.Home, Checkout: m.decl.Checkout})
	if err != nil {
		return fmt.Errorf("%w: %w", guestfacts.ErrRefused, err)
	}
	file, link := paths.GuestUnitFile(u), paths.GuestUnitWantsLink(u)
	_, _, existed, err := g.root.ReadFile(file, maxUnit)
	if err != nil {
		return err
	}
	w := g.root.Writer()
	if _, err := w.MkdirAll(path.Dir(file)); err != nil {
		return err
	}
	wrote, err := w.WriteFile(path.Dir(file), path.Base(file), text, 0o644)
	if err != nil {
		return err
	}
	if _, err := w.MkdirAll(path.Dir(link)); err != nil {
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
		if err := s.Reload(ctx); err != nil {
			return err
		}
		return start(s, ctx, u)
	})
	if err == nil {
		r.line(m, Changed, how)
	}
	return err
}

func (r *run) removeUnit(ctx context.Context, g *guestBox, u paths.GuestUser) error {
	file, link := paths.GuestUnitFile(u), paths.GuestUnitWantsLink(u)
	_, _, found, err := g.root.ReadFile(file, maxUnit)
	if err != nil || !found {
		return err
	}
	return r.systemd(ctx, g, func(s guestbus.Systemd) error {
		if err := s.Stop(ctx, u); err != nil {
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

// statusDrop marks a member whose record entry the phase removes.
const statusDrop = "drop"

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
			r.errs = append(r.errs, &host.Error{Verb: verb, Step: "record", Err: err})
		}
		if r.changed[t.ID] {
			if err := r.d.Refresh(ctx, t.ID); err != nil {
				r.errs = append(r.errs, &host.Error{Verb: verb, Step: "status", Err: err})
			}
		}
	}
}

// settle is one member's record entry after the phase.
func settle(rec lxcstate.Record, m *member, now int64) lxcstate.Record {
	if m.status == statusDrop {
		return rec.Drop(m.box, m.decl.User)
	}
	e := lxcstate.Entry{Container: m.box, User: m.decl.User, Handle: m.handle, Status: m.status, Why: m.why}
	if e.Status == "" {
		if e.Handle == "" {
			return rec
		}
		e.Status, e.Why = lxcstate.StatusJoined, ""
	}
	return rec.Set(e, now)
}
