package restart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// result is where a session's restart stands.
type result int

const (
	resultPending result = iota
	resultDone
	resultFailed
	resultLeft
)

// action is what is done to a session.
type action int

const (
	actNone action = iota
	// actInPlace asks a running ccy session's supervisor to restart it (B4).
	actInPlace
	// actStart starts a stopped session again (B6).
	actStart
)

// stateRunning is podman's word for a running container.
const stateRunning = "running"

// seatRef is one seat a session names.
type seatRef struct{ dir, seat, team string }

func (s seatRef) String() string { return s.seat + "@" + s.team }

// leftOut is a member this restart does not touch, and why (B3).
type leftOut struct{ member, why string }

// job is one session: what it is, what is done to it, and how far it has gone.
type job struct {
	name, dir, seat, handle string
	seats                   []seatRef
	rec                     registry.Record
	action                  action

	// id is the conversation a start resumes, args the launcher's arguments for it.
	id   string
	args []string
	// memDir is a running ccy session's membership directory, generation its desired one's.
	memDir     string
	generation int
	// container is the running session's container before the request; wrote is whether this
	// restart wrote the request (a first join's already pending is not ours, joinPending).
	container   string
	wrote       bool
	joinPending bool

	started time.Time
	result  result
	note    string
}

func (j *job) done(r result, note string) { j.result, j.note = r, note }

// plan is what the preview says will be done to the session.
func (j *job) plan() string {
	switch {
	case j.result == resultFailed:
		return "cannot be restarted: " + j.note
	case j.result == resultLeft:
		return j.note
	case j.action == actInPlace:
		return "running, so it restarts in place at its next idle moment, resuming its conversation in a fresh container"
	case j.action == actStart:
		return "stopped, so it is started, resuming conversation " + j.id
	}
	return j.note
}

// plan is the team's sessions, one job each, and its members left out.
type plan struct {
	jobs    []*job
	leftOut []leftOut
}

// acting is how many sessions will be restarted or started.
func (p *plan) acting() int {
	n := 0
	for _, j := range p.jobs {
		if j.result == resultPending && j.action != actNone {
			n++
		}
	}
	return n
}

// planFor is a job for each session a record names for a seat of team, and the members left out
// (B2, B3); each job is then worked out from its record and whether it runs.
func (r *Restart) planFor(snap *board.Snapshot, team string) plan {
	var p plan
	for _, row := range snap.Rows {
		if row.Team != team {
			continue
		}
		switch {
		case row.Kind == board.KindTeam, row.Kind == board.KindHuman, row.State == wordParked:
		case row.Kind == board.KindCcy && row.Dir != "-":
			p.addSeat(snap, team, row)
		case row.Kind == kindLxc:
			p.leftOut = append(p.leftOut, leftOut{row.Handle, whyLxc})
		default:
			p.leftOut = append(p.leftOut, leftOut{row.Handle, whyElsewhere})
		}
	}
	if why := snap.MembersUnknown[team]; why != "" {
		p.leftOut = append(p.leftOut, leftOut{
			"the members of " + team,
			"the team's members could not be read (" + why + "), so members outside this desktop's checkouts are not listed",
		})
	}
	for _, j := range p.jobs {
		_, live := snap.Live[j.name]
		r.prepare(j, team, live)
	}
	return p
}

// addSeat makes a job of the session a seat row's record names, once per session; a member's
// or held seat no record names has nothing to restart, and is named.
func (p *plan) addSeat(snap *board.Snapshot, team string, row board.Row) {
	if row.Record == "" {
		// A free seat that is no member's is only a seat in the checkout: nothing to name.
		if !row.Held && row.Role == "" {
			return
		}
		member := row.Member + "@" + team + " (" + row.Handle + ")"
		p.leftOut = append(p.leftOut, leftOut{member, "nothing recorded to restart; Start a ccy session in " + team})
		return
	}
	if slices.ContainsFunc(p.jobs, func(j *job) bool { return j.name == row.Record }) {
		return
	}
	p.jobs = append(p.jobs, &job{name: row.Record, dir: row.Dir, seat: row.Member, handle: row.Handle, seats: seatsOf(snap, row.Record, team)})
}

// seatsOf is every seat the session names: the team's first, then the others by team.
func seatsOf(snap *board.Snapshot, name, team string) []seatRef {
	keys := snap.Seats.SeatsOf(name)
	refs := make([]seatRef, 0, len(keys))
	for _, k := range keys {
		refs = append(refs, seatRef{dir: k.Dir, seat: k.Seat, team: k.Team})
	}
	slices.SortFunc(refs, func(a, b seatRef) int {
		if (a.team == team) != (b.team == team) {
			if a.team == team {
				return -1
			}
			return 1
		}
		return strings.Compare(a.team+"@"+a.seat, b.team+"@"+b.seat)
	})
	return refs
}

// prepare works out from the record what is done to the session, or why nothing can be.
func (r *Restart) prepare(j *job, team string, live bool) {
	store, err := r.Registry()
	if err == nil {
		j.rec, err = store.Get(j.name)
	}
	if err != nil {
		j.done(resultFailed, "its session record could not be read: "+err.Error())
		return
	}
	again := "/exit it, then run ccy-teams restart " + team + " again to start it"
	switch {
	case live && j.rec.Prefix == membership.KindCC:
		j.done(resultLeft, "left running: a cc session has no supervisor to restart it; "+again)
	case live:
		r.prepareInPlace(j, again)
	default:
		r.prepareStart(j)
	}
}

// prepareInPlace is D8: the session's supervisor restarts it, so it must have one and a
// membership directory to be asked through.
func (r *Restart) prepareInPlace(j *job, again string) {
	if !registry.Supervised(j.rec.Prefix, j.rec.Args) {
		j.done(resultFailed, "it runs without the ccy supervisor (--no-supervise), which is what restarts it in place; "+again)
		return
	}
	runtime := r.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtime) {
		j.done(resultFailed, "XDG_RUNTIME_DIR is not an absolute path, so its membership directory cannot be found")
		return
	}
	j.memDir = membership.DirOf(runtime, membership.KindCcy, j.name)
	d, err := membership.Read(j.memDir)
	if err != nil {
		j.done(resultFailed, "its supervisor cannot be asked: "+err.Error())
		return
	}
	j.generation, j.action = d.Generation, actInPlace
}

// prepareStart is B6: started as a restore starts a record, by its recorded conversation only.
func (r *Restart) prepareStart(j *job) {
	if info, err := os.Stat(j.rec.Dir); err != nil || !info.IsDir() {
		j.done(resultFailed, "its folder no longer exists: "+j.rec.Dir)
		return
	}
	id, err := conversation.For(j.rec, conversation.ClaudeHome(r.Getenv))
	if err != nil {
		j.done(resultFailed, strings.TrimPrefix(err.Error(), j.rec.Name+": "))
		return
	}
	args, err := registry.ResumeArgs(j.rec.Compose, j.rec.Prefix, j.rec.Args, id)
	if err != nil {
		j.done(resultFailed, "could not work out the arguments to start it with: "+err.Error())
		return
	}
	j.id, j.args, j.action = id, args, actStart
}

// actAll makes every request and start before anything is waited for (B7).
func (r *Restart) actAll(ctx context.Context, jobs []*job) {
	for _, j := range jobs {
		if j.result != resultPending {
			continue
		}
		switch j.action {
		case actInPlace:
			r.askToRestart(ctx, j)
		case actStart:
			r.start(ctx, j)
		case actNone:
		}
		j.started = r.Now()
	}
}

// askToRestart notes the session's container and writes the request; a first join's relaunch
// already pending is the same restart, so it is left as it is.
func (r *Restart) askToRestart(ctx context.Context, j *job) {
	running, err := r.containers(ctx)
	if err != nil {
		j.done(resultFailed, err.Error())
		return
	}
	if j.container = running[j.name]; j.container == "" {
		j.done(resultFailed, "its container is not running, so its supervisor cannot restart it; "+
			"end it in ccy-sessions, then run restart again to start it")
		return
	}
	pending, err := membership.RelaunchPending(j.memDir)
	if err == nil && !pending {
		err = membership.WriteRestart(j.memDir, j.generation, r.Now())
		j.wrote = err == nil
	}
	if err != nil {
		j.done(resultFailed, "the restart could not be asked for: "+err.Error())
		return
	}
	j.joinPending = pending
}

// start starts the stopped session on ccy's tmux, as ccy-sessions restore does.
func (r *Restart) start(ctx context.Context, j *job) {
	store, err := r.Registry()
	if err != nil {
		j.done(resultFailed, err.Error())
		return
	}
	command := append([]string{"env", "CCY_SESSION_RESTORE=1", j.rec.Launcher}, j.args...)
	spec := tmuxccy.StartSpec{Name: j.name, Dir: j.rec.Dir, Command: command, RecordPath: filepath.Join(store.Dir(), j.name), Prefix: j.rec.Prefix}
	if err := r.Tmux.StartDetached(ctx, spec); err != nil {
		j.done(resultFailed, "could not start it: "+err.Error())
	}
}

// waitAll reads the containers and the seats again until every session is done or its wait
// is over (B4, B6, B7).
func (r *Restart) waitAll(ctx context.Context, jobs []*job) {
	for {
		var waiting []*job
		for _, j := range jobs {
			if j.result == resultPending && j.action != actNone {
				waiting = append(waiting, j)
			}
		}
		if len(waiting) == 0 {
			return
		}
		running, podmanErr := r.containers(ctx)
		held, unreadable := r.heldSeats(ctx, waiting)
		for _, j := range waiting {
			r.judge(j, running, podmanErr, held, unreadable)
		}
		r.Sleep(r.Waits.Poll)
	}
}

// judge is one look at a waiting session.
func (r *Restart) judge(j *job, running map[string]string, podmanErr error, held map[string]bool, unreadable map[string]string) {
	unheld := ""
	for _, s := range j.seats {
		if why, bad := unreadable[s.dir]; bad {
			r.fail(j, why)
			return
		}
		if !held[s.dir+"|"+s.String()] && unheld == "" {
			unheld = s.String()
		}
	}
	late := r.Now().Sub(j.started)
	switch j.action {
	case actInPlace:
		if podmanErr != nil {
			r.fail(j, podmanErr.Error())
			return
		}
		if id := running[j.name]; id != "" && id != j.container && unheld == "" {
			j.done(resultDone, "restarted in place, resuming its conversation; "+seatWords(j.seats)+" held")
			return
		}
		if late >= r.Waits.InPlace {
			r.fail(j, "not restarted within "+seconds(r.Waits.InPlace)+": it may be busy, or its supervisor does not take plugins")
		}
	case actStart:
		if unheld == "" {
			j.done(resultDone, "started, resuming conversation "+j.id+"; "+seatWords(j.seats)+" held")
			return
		}
		if late >= r.Waits.Start {
			j.done(resultFailed, "started, but seat "+unheld+" is not held "+seconds(r.Waits.Start)+
				" after: it may be waiting at a question; attach it (ccy-sessions)")
		}
	case actNone:
	}
}

// fail fails a session; a restart in place withdraws the request it wrote, so the session does
// not restart later, unasked (B7).
func (r *Restart) fail(j *job, why string) {
	if j.action != actInPlace {
		j.done(resultFailed, why)
		return
	}
	switch removed, err := membership.WithdrawRestart(j.memDir); {
	case err != nil:
		why += "; the request could not be withdrawn (" + err.Error() + "): remove " +
			filepath.Join(j.memDir, membership.RelaunchFile) + " or it restarts later"
	case removed:
		why += "; the request is withdrawn"
	case j.joinPending:
		why += "; its relaunch for a team join is still pending and stays"
	}
	j.done(resultFailed, why)
}

// containers is each running ccy session's container, by session name.
func (r *Restart) containers(ctx context.Context) (map[string]string, error) {
	out, err := sessprobe.LabelledSessions(ctx, r.Runner)
	if err != nil {
		return nil, errors.New("podman could not list the sessions' containers: " + strings.TrimSpace(err.Error()))
	}
	return runningContainers(out), nil
}

// runningContainers is sessprobe.LabelledSessions' running lines: session name -> container.
func runningContainers(out string) map[string]string {
	running := map[string]string{}
	for line := range strings.Lines(out) {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) == 3 && fields[1] == stateRunning && membership.ValidSession(fields[2]) {
			running[fields[2]] = fields[0]
		}
	}
	return running
}

// heldSeats is which seats of the waiting jobs' checkouts are held, by "<dir>|<seat>@<team>",
// and why each checkout whose seats could not be read could not.
func (r *Restart) heldSeats(ctx context.Context, jobs []*job) (map[string]bool, map[string]string) {
	held, unreadable, read := map[string]bool{}, map[string]string{}, map[string]bool{}
	for _, j := range jobs {
		for _, s := range j.seats {
			if read[s.dir] {
				continue
			}
			read[s.dir] = true
			seats, _, err := r.Bus.SeatList(ctx, s.dir)
			if err != nil {
				unreadable[s.dir] = "the seats in " + s.dir + " could not be read: " + err.Error()
				continue
			}
			for _, seat := range seats {
				held[s.dir+"|"+seat.Seat+"@"+seat.Team] = seat.Held
			}
		}
	}
	return held, unreadable
}

func seatWords(refs []seatRef) string {
	names := make([]string, len(refs))
	for i, s := range refs {
		names[i] = s.String()
	}
	return "seat" + plural(len(refs)) + " " + strings.Join(names, ", ")
}
