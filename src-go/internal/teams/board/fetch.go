package board

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// parallel is how many agent-bus calls a pass runs at once unless Parallel says otherwise. Each
// call is mostly Python starting (about half a second, more on a loaded host), so calls run
// together cost about one call's time; the bound keeps a busy host from being handed every
// interpreter at once.
const parallel = 8

// reads is everything a pass asks agent-bus, asked before the rows are built.
type reads struct {
	// seats is by checkout folder, as the snapshot names it: two names of one folder share a read.
	seats map[string]*seatRead
	// rosters is by team, for each team that is up.
	rosters map[string]*rosterRead
}

// seatRead is agent-bus seat list in one checkout.
type seatRead struct {
	seats      []agentbus.Seat
	unreadable bool
	err        error
	said       tape
}

// rosterRead is agent-bus list of one team, and the Element sessions of each person on it whose
// count is not kept.
type rosterRead struct {
	roster  agentbus.Roster
	err     error
	said    tape
	devices map[string]*devicesRead
}

// devicesRead is agent-bus human devices of one person.
type devicesRead struct {
	devices []agentbus.Device
	err     error
	said    tape
}

// read asks agent-bus what the pass needs, the calls run together up to the bound: the roster of
// each team that is up, then the sessions of each of its people whose count is not kept, and the
// seats of each checkout with a seat directory, once however many names it has here. The rosters
// go first because each has a second call waiting on it. What a call says on stderr is kept with
// its result, and written out where the result is used.
func (c *Collector) read(ctx context.Context, s *Snapshot) reads {
	r := reads{seats: map[string]*seatRead{}, rosters: map[string]*rosterRead{}}
	p := newPool(c.Parallel)
	for _, team := range s.Teams {
		if !s.up(team) {
			continue
		}
		at := &rosterRead{}
		r.rosters[team] = at
		p.run(func() { c.readRoster(ctx, p, team, at) })
	}
	c.readSeats(ctx, p, s, r.seats)
	p.wait()
	return r
}

// readSeats starts a seat list in each checkout of s with a seat directory, its read kept in
// into under the folder's name; a folder already asked under another name (a symlink, a bind mount) shares
// that read.
func (c *Collector) readSeats(ctx context.Context, p *pool, s *Snapshot, into map[string]*seatRead) {
	type folder struct {
		info os.FileInfo
		read *seatRead
	}
	var asked []folder
	for _, dir := range s.Checkouts {
		info, err := os.Stat(filepath.Join(dir, ".claude", "ccy", "pingbus", "seats"))
		if err != nil || !info.IsDir() {
			continue
		}
		var shared *seatRead
		for _, f := range asked {
			if os.SameFile(f.info, info) {
				shared = f.read
			}
		}
		if shared != nil {
			into[dir] = shared
			continue
		}
		at := &seatRead{}
		into[dir] = at
		asked = append(asked, folder{info: info, read: at})
		p.run(func() { at.seats, at.unreadable, at.err = c.Bus.WithStderr(at.said.on).SeatList(ctx, dir) })
	}
}

// readRoster reads team's roster into at, then starts a read of each person's sessions not kept.
// The kept counts are only read here: they change after every read has finished.
func (c *Collector) readRoster(ctx context.Context, p *pool, team string, at *rosterRead) {
	at.roster, at.err = c.Bus.WithStderr(at.said.on).List(ctx, team)
	if at.err != nil {
		return
	}
	at.devices = map[string]*devicesRead{}
	for _, h := range at.roster.Humans {
		if _, kept := c.devices[team+"|"+h.Name]; kept || at.devices[h.Name] != nil {
			continue
		}
		d := &devicesRead{}
		at.devices[h.Name] = d
		p.run(func() { d.devices, d.err = c.Bus.WithStderr(d.said.on).HumanDevices(ctx, team, h.Name) })
	}
}

// pool runs jobs on their own goroutines, at most its size of them at once. A job may start
// others; wait returns when every one has finished.
type pool struct {
	slots chan struct{}
	wg    sync.WaitGroup
}

func newPool(size int) *pool {
	if size <= 0 {
		size = parallel
	}
	return &pool{slots: make(chan struct{}, size)}
}

func (p *pool) run(job func()) {
	p.wg.Go(func() {
		p.slots <- struct{}{}
		defer func() { <-p.slots }()
		job()
	})
}

func (p *pool) wait() { p.wg.Wait() }

// tape is what one call run beside others wrote to its streams, kept to be written out when its
// result is used: in the order the calls would have been made one after another.
type tape struct {
	mu    sync.Mutex
	parts []taped
}

type taped struct {
	to   io.Writer
	text []byte
}

// on is a writer that keeps what is written for w on the tape.
func (t *tape) on(w io.Writer) io.Writer { return tapeWriter{tape: t, to: w} }

// play writes what was kept to the streams it was meant for, each time it is called; a failed
// write is kept in out, as any other.
func (t *tape) play(out *term.Printer) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, part := range t.parts {
		out.Print(part.to, string(part.text))
	}
}

type tapeWriter struct {
	tape *tape
	to   io.Writer
}

func (w tapeWriter) Write(b []byte) (int, error) {
	w.tape.mu.Lock()
	defer w.tape.mu.Unlock()
	w.tape.parts = append(w.tape.parts, taped{to: w.to, text: bytes.Clone(b)})
	return len(b), nil
}
