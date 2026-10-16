package shutdown

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// exitCommand is what a person types to end claude, and what the supervisor types for a restart.
const exitCommand = "/exit"

// composeStop is the launcher's "Stop compose services?" question (screen.KnownPrompts): it is
// asked after claude has ended.
const composeStop = "compose-stop"

// What the result says of a session.
const (
	whyUnsent        = "its input box holds unsent text; attach it (ccy-sessions), send or clear it, and run shutdown again"
	noteByItself     = "ended by itself before /exit was typed"
	noteCompose      = "its launcher asks whether to stop compose services: attach it (ccy-sessions) to answer"
	noteEndedAfter   = "claude ended and its launcher reported an error, so the window was closed"
	noteEndedBefore  = "it had already ended (its launcher reported an error), so the window was closed"
	noteKilledSuffix = ", so its tmux session was killed"
)

// outcome is where a session's shutdown stands.
type outcome int

const (
	pending outcome = iota
	exited
	stopped
	failed
)

// job is one session being shut down: what it is, and how far it has gone.
type job struct {
	name, dir, seat, handle string
	attached                int
	seats                   []seatRef

	typed, escaped, reentered bool
	deadline                  time.Time
	result                    outcome
	note                      string
	released                  bool
}

func (j *job) done(result outcome, note string) { j.result, j.note = result, note }

// stopAll reads every pane, then again every poll, until each session has ended, been stopped
// or failed (SD10 to SD13).
func (s *Shutdown) stopAll(ctx context.Context, jobs []*job) {
	start := s.Now()
	for _, j := range jobs {
		j.deadline = start.Add(s.Waits.Prompt)
	}
	for {
		live, err := s.liveSessions(ctx)
		if err != nil {
			for _, j := range jobs {
				if j.result == pending {
					j.done(failed, err.Error()+"; the sessions could not be watched any further")
				}
			}
			return
		}
		left := false
		for _, j := range jobs {
			if j.result == pending {
				s.step(ctx, j, live)
				left = left || j.result == pending
			}
		}
		if !left {
			return
		}
		s.Sleep(s.Waits.Poll)
	}
}

func (s *Shutdown) liveSessions(ctx context.Context) (map[string]bool, error) {
	list, err := s.Tmux.List(ctx)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, sess := range list {
		live[sess.Name] = true
	}
	return live, nil
}

// step is one look at a session and what follows from it.
func (s *Shutdown) step(ctx context.Context, j *job, live map[string]bool) {
	if !live[j.name] {
		s.ended(j)
		return
	}
	pane, err := s.Tmux.CapturePane(ctx, j.name, true)
	if err != nil {
		// A session that ended since the list was read is seen gone on the next pass. Any other
		// failure kills nothing: the pane may hold unsent text (SD12, SD13).
		if !strings.Contains(err.Error(), "can't find") {
			j.done(failed, err.Error()+"; nothing was typed or stopped")
		}
		return
	}
	prefix, _, found := strings.Cut(j.name, "-")
	if !found {
		prefix = j.name
	}
	verdict := restore.Judge(prefix, true, screen.Plain(pane), restore.ContainerUp, restore.GoingOutcome{})
	switch verdict.State {
	case restore.StateDead:
		s.closeWindow(ctx, j, verdict.Detail)
		return
	case restore.StateWaitingAtPrompt:
		if verdict.Detail == composeStop {
			j.done(exited, noteCompose)
			return
		}
		s.hardStop(ctx, j, "it was at the launcher's "+verdict.Detail+" question, so claude was not running; its tmux session was killed")
		return
	case restore.StateOK, restore.StateStarting, restore.StateSettingGoing, restore.StateNotSetGoing:
	}
	if err := s.act(ctx, j, screen.ClaudeState(pane)); err != nil {
		j.done(failed, err.Error())
		return
	}
	if j.result != pending || s.Now().Before(j.deadline) {
		return
	}
	if j.typed {
		s.hardStop(ctx, j, "it did not end within "+seconds(s.Waits.Exit)+" of /exit"+noteKilledSuffix)
		return
	}
	s.hardStop(ctx, j, "its prompt was not ready within "+seconds(s.Waits.Prompt)+noteKilledSuffix)
}

// act is what claude's screen calls for: /exit at a ready prompt, Escape once on a busy one,
// nothing over unsent text, one more Enter when /exit was left in the box.
func (s *Shutdown) act(ctx context.Context, j *job, state screen.State) error {
	switch state {
	case screen.Ready:
		if !j.typed {
			return s.typeExit(ctx, j)
		}
	case screen.Busy:
		if !j.typed && !j.escaped {
			j.escaped = true
			return s.Tmux.SendEscape(ctx, j.name)
		}
	case screen.Typed:
		if !j.typed {
			j.done(failed, whyUnsent)
			return nil
		}
		if !j.reentered {
			j.reentered = true
			return s.Tmux.SendEnter(ctx, j.name)
		}
	case screen.NotDrawn:
	}
	return nil
}

// typeExit types /exit, then presses Enter on its own after the submit delay, as ccy-sessions
// types a line: an Enter arriving with the text is read as part of it.
func (s *Shutdown) typeExit(ctx context.Context, j *job) error {
	if err := s.Tmux.SendKeys(ctx, j.name, exitCommand); err != nil {
		return err
	}
	s.Sleep(s.Waits.Submit)
	if err := s.Tmux.SendEnter(ctx, j.name); err != nil {
		return err
	}
	j.typed = true
	j.deadline = s.Now().Add(s.Waits.Exit)
	return nil
}

// ended is a session whose tmux session has gone.
func (s *Shutdown) ended(j *job) {
	if j.typed {
		j.done(exited, "")
		return
	}
	j.done(exited, noteByItself)
}

// closeWindow closes a pane whose launcher has exited, as its Enter would (SD11).
func (s *Shutdown) closeWindow(ctx context.Context, j *job, detail string) {
	if detail != "launcher-exited" {
		j.done(failed, "its pane cannot be judged ("+detail+")")
		return
	}
	if err := s.Tmux.KillSession(ctx, j.name); err != nil && !errors.Is(err, tmuxccy.ErrSessionGone) {
		j.done(failed, err.Error())
		return
	}
	if j.typed {
		j.done(exited, noteEndedAfter)
		return
	}
	j.done(exited, noteEndedBefore)
}

// hardStop is ccy-sessions' End: the tmux session killed, then its record removed, since a
// killed pane never reaches the trampoline that would remove it (SD12).
func (s *Shutdown) hardStop(ctx context.Context, j *job, note string) {
	err := s.Tmux.KillSession(ctx, j.name)
	if errors.Is(err, tmuxccy.ErrSessionGone) {
		s.ended(j)
		return
	}
	if err != nil {
		j.done(failed, err.Error())
		return
	}
	store, err := s.Registry()
	if err == nil {
		err = store.Remove(j.name)
	}
	if err != nil {
		j.done(failed, "its tmux session was killed, but its session record could not be removed: "+err.Error())
		return
	}
	j.done(stopped, note)
}

// checkSeats reads again, until each is free or the wait is over, every seat that each ended or
// stopped session held (SD14). Nothing frees a seat by hand: the session's process ending does.
func (s *Shutdown) checkSeats(ctx context.Context, jobs []*job) {
	deadline := s.Now().Add(s.Waits.Release)
	for {
		var waiting []*job
		for _, j := range jobs {
			if (j.result == exited || j.result == stopped) && !j.released {
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
			j.done(failed, why)
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
		j.done(failed, "seat "+still+" is still held "+seconds(s.Waits.Release)+" after: something of the session still runs (podman ps)")
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
