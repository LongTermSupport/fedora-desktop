package shutdown

import (
	"context"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessclose"
)

// job is one session being shut down: what it is, the seats it holds, and how far it has gone.
// Closing it is sessclose's (SD10-SD13); its seats are a team matter, read here (SD14).
type job struct {
	name, dir, seat, handle string
	attached                int
	seats                   []seatRef

	result   sessclose.Outcome
	note     string
	released bool
}

func (j *job) done(result sessclose.Outcome, note string) { j.result, j.note = result, note }

// stopAll closes every session through sessclose: compacted then sent /exit, or /exit alone
// with --no-compact (SD10 to SD13).
func (s *Shutdown) stopAll(ctx context.Context, jobs []*job, noCompact bool) {
	targets := make([]sessclose.Target, len(jobs))
	for i, j := range jobs {
		targets[i] = sessclose.Target{Name: j.name, NoCompact: noCompact}
	}
	results := s.Closer.Close(ctx, targets)
	for i, j := range jobs {
		if i < len(results) && results[i].Name == j.name {
			j.done(results[i].Outcome, results[i].Note)
			continue
		}
		j.done(sessclose.Failed, "the close returned no result for it")
	}
}

// checkSeats reads again, until each is free or the wait is over, every seat that each ended or
// stopped session held (SD14). Nothing frees a seat by hand: the session's process ending does.
func (s *Shutdown) checkSeats(ctx context.Context, jobs []*job) {
	deadline := s.Now().Add(s.Waits.Release)
	for {
		var waiting []*job
		for _, j := range jobs {
			if (j.result == sessclose.Exited || j.result == sessclose.Stopped) && !j.released {
				waiting = append(waiting, j)
			}
		}
		if len(waiting) == 0 {
			return
		}
		held, unreadable := s.heldSeats(ctx, waiting)
		late := !s.Now().Before(deadline)
		for _, j := range waiting {
			s.judgeSeats(j, held, unreadable, late)
		}
		if late {
			return
		}
		s.Sleep(s.Waits.Poll)
	}
}

// judgeSeats marks a job's seats released when none is held; a seat list that could not be
// read fails it, and so does a seat still held once the wait is over.
func (s *Shutdown) judgeSeats(j *job, held map[string]bool, unreadable map[string]string, late bool) {
	still := ""
	for _, r := range j.seats {
		if why, bad := unreadable[r.dir]; bad {
			j.done(sessclose.Failed, why)
			return
		}
		if held[r.dir+"|"+r.String()] && still == "" {
			still = r.String()
		}
	}
	switch {
	case still == "":
		j.released = true
	case late:
		j.done(sessclose.Failed, "seat "+still+" is still held "+sessclose.Seconds(s.Waits.Release)+" after: something of the session still runs (podman ps)")
	}
}

// heldSeats is which seats of the waiting jobs' checkouts are held, by "<dir>|<seat>@<team>",
// and why each checkout whose seats could not be read could not.
func (s *Shutdown) heldSeats(ctx context.Context, jobs []*job) (map[string]bool, map[string]string) {
	held, unreadable, read := map[string]bool{}, map[string]string{}, map[string]bool{}
	for _, j := range jobs {
		for _, r := range j.seats {
			if read[r.dir] {
				continue
			}
			read[r.dir] = true
			seats, _, err := s.Bus.SeatList(ctx, r.dir)
			if err != nil {
				unreadable[r.dir] = "the seats in " + r.dir + " could not be read: " + err.Error()
				continue
			}
			for _, seat := range seats {
				held[r.dir+"|"+seat.Seat+"@"+seat.Team] = seat.Held
			}
		}
	}
	return held, unreadable
}
