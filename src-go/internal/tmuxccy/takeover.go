package tmuxccy

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// GoneError is a session that has ended, with the whole sentence for the person (without the
// "ERROR: " label, which the caller adds with term.PrintError). It matches ErrSessionGone.
type GoneError struct {
	Message string
}

// Error is the message for the person.
func (e *GoneError) Error() string { return e.Message }

// Is matches ErrSessionGone.
func (e *GoneError) Is(target error) bool { return target == ErrSessionGone }

// IdleWords is "idle 40 s", "idle 12 min", "idle 3 h" or "idle 2 d" for an age in seconds,
// rounded down. A negative age (the clock moved back) reads as 0.
func IdleWords(secs int64) string {
	secs = max(secs, 0)
	switch {
	case secs < 60:
		return "idle " + strconv.FormatInt(secs, 10) + " s"
	case secs < 3600:
		return "idle " + strconv.FormatInt(secs/60, 10) + " min"
	case secs < 86400:
		return "idle " + strconv.FormatInt(secs/3600, 10) + " h"
	}
	return "idle " + strconv.FormatInt(secs/86400, 10) + " d"
}

// ClientWords is "<tty> (idle …), <tty> (idle …)" for terminals seen at now (Unix seconds).
func ClientWords(clients []Client, now int64) string {
	words := make([]string, 0, len(clients))
	for _, c := range clients {
		words = append(words, c.TTY+" ("+IdleWords(now-c.Activity)+")")
	}
	return strings.Join(words, ", ")
}

// OtherTerminals words the terminals a session is open on, empty when it is open on none.
// ErrSessionGone when the session has ended.
func (t *Tmux) OtherTerminals(ctx context.Context, name string) (string, error) {
	clients, err := t.Clients(ctx, name)
	if err != nil {
		return "", err
	}
	return ClientWords(clients, t.now().Unix()), nil
}

// TakeOver detaches every other terminal from the session, waits until list-clients shows none
// (the hook's view; see IsDetached) for up to waitTenths tenths of a second, and attaches this
// one through Attach, whose outcome it returns (including Kicked). Nothing is attached when it
// returns an error: a *GoneError (matching ErrSessionGone) when the session has ended, or ended
// while it was being taken over; Attach's refusal if the session was taken again just before the
// attach; any other error when tmux refuses the detach or the other terminal does not let go in
// time. The error text is the whole message for the person. It never kills the session or
// anything in it.
func (t *Tmux) TakeOver(ctx context.Context, name string, waitTenths int) (Outcome, error) {
	if name == "" {
		return Refused, errors.New("take over requires a session name")
	}
	clients, err := t.Clients(ctx, name)
	if errors.Is(err, ErrSessionGone) {
		return Refused, &GoneError{Message: "'" + name + "' has ended; there is nothing to take over."}
	}
	if err != nil {
		return Refused, err
	}
	if len(clients) > 0 {
		if waitErr := t.detachAndWait(ctx, name, clients, waitTenths); waitErr != nil {
			return Refused, waitErr
		}
	}
	return t.Attach(ctx, name)
}

// detachAndWait tells the other terminals to leave and waits for them to go.
func (t *Tmux) detachAndWait(ctx context.Context, name string, clients []Client, waitTenths int) error {
	words := ClientWords(clients, t.now().Unix())
	out, detachErr := execx.Combined(ctx, t.Runner, execx.Cmd{
		Name: tmuxBinary, Args: []string{"-L", t.socket(), "detach-client", "-s", "=" + name},
	})
	out = strings.TrimRight(out, "\n")
	if detachErr == nil {
		t.Term.Printf("Detached '%s' from %s; the session keeps running.\n", name, words)
	} else {
		// The other terminal may have left between the listing and the detach, and tmux then
		// fails the detach for want of a client. That is the outcome wanted, so it is only a
		// failure if a client is still there.
		t.Term.Printf("The detach from %s failed (%s); checking whether '%s' is free anyway.\n", words, out, name)
	}
	for waited := 0; ; waited++ {
		left, err := t.Clients(ctx, name)
		if errors.Is(err, ErrSessionGone) {
			return &GoneError{Message: "'" + name + "' ended while it was being taken over; nothing was attached."}
		}
		if err != nil {
			return err
		}
		if len(left) == 0 {
			return nil
		}
		if detachErr != nil {
			return fmt.Errorf("could not detach '%s' from %s: %s", name, words, out)
		}
		if waited >= waitTenths {
			return fmt.Errorf("'%s' was told to leave %s, but it is still open there after %d s. Nothing was attached; the session keeps running", name, words, waitTenths/10)
		}
		t.pause(100 * time.Millisecond)
	}
}
