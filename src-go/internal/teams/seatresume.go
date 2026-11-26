package teams

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// verbSeatResume is `ccy-teams internal seat-resume`, the launcher's.
const verbSeatResume = "seat-resume"

const seatResumeUsage = "internal seat-resume [--fresh] [--rebind] [--relaunch=<why or empty>] [--given=<list as given>] <seats> -- [claude arguments...]"

// SeatResume is `ccy-teams internal seat-resume` (issue 417): which conversation a seated ccy
// launch runs, decided from claude's arguments and the seats' records, and the claude arguments
// that carry it, each followed by a NUL on stdout (none: the arguments already say it). Every
// refusal is on stderr, exit 64:
//
//   - no selector: the seats' recorded conversation (conversation.ForSeats) is resumed
//     (`--resume <id>`) and recorded for the named seats that had none; none can be named is
//     refused in its case, naming the repair and --fresh;
//   - --fresh: a new conversation with an id chosen here (`--session-id <id>`), recorded for
//     every seat, so the next plain launch resumes it; refused with any selector;
//   - --resume <id> (or --session-id <id>): taken and recorded when it is the seats' recorded
//     conversation, when they have none that can be resumed, with --rebind, or on a relaunch (a
//     restore, a team restart or the supervisor's: it resumes the session's own conversation);
//     another id is refused, naming --rebind and the plain launch;
//   - --continue: refused (the owner's ruling Q67), naming the plain launch and --fresh;
//   - the bare --resume picker (Q70): left alone, nothing recorded;
//   - --given=<list>, the --teams list as the person gave it (issue 423): when no seat of the
//     launch is named in it, the seats were picked in the chooser, so a plain launch whose seats
//     no session ever used (conversation.CaseNoSession) starts fresh, said, instead of the
//     refusal; a picked seat with a record resumes it, and a named seat keeps the refusal.
type SeatResume struct {
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Getwd          func() (string, error)
	NowMS          func() int64
	// NewID is a new conversation id, for --fresh.
	NewID func() (string, error)
}

// seatLaunch is one launch's request, as the launcher passes it.
type seatLaunch struct {
	fresh, rebind, relaunch bool
	list                    string
	// given is the --teams list as the person gave it, when hasGiven (--given).
	given    string
	hasGiven bool
	sel      registry.Selection
	// own is claude's arguments before the launcher's `--` less ccy's --fresh and --rebind,
	// which reach them through the launcher's parse; rest is those after it.
	own, rest []string
}

// errSeatUsage is a request the launcher cannot mean, or one refused: exit 64.
var errSeatUsage = errors.New("seat resume refused")

// Run is the verb over its arguments (after `internal seat-resume`).
func (r *SeatResume) Run(args []string) term.ExitCode {
	req, err := parseSeatLaunch(args)
	if err != nil {
		return r.refuse(err)
	}
	wd, err := r.Getwd()
	if err != nil {
		return r.refuse(fmt.Errorf("the working directory: %w", err))
	}
	words, err := r.decide(wd, req)
	if err != nil {
		return r.refuse(err)
	}
	var out strings.Builder
	for _, w := range slices.Concat([]string{strconv.Itoa(len(req.own))}, req.own, req.rest, words) {
		out.WriteString(w + "\x00")
	}
	if _, err := io.WriteString(r.Stdout, out.String()); err != nil {
		return r.refuse(fmt.Errorf("could not print the arguments: %w", err))
	}
	return term.ExitOK
}

// refuse says err; a refusal of the request is 64, anything else 1.
func (r *SeatResume) refuse(err error) term.ExitCode {
	code := term.ExitFailure
	if errors.Is(err, errSeatUsage) {
		code = term.ExitUsage
	}
	msg := strings.TrimPrefix(err.Error(), errSeatUsage.Error()+": ")
	if _, werr := io.WriteString(r.Stderr, "ccy: "+msg+"\n"); werr != nil {
		return term.ExitFailure
	}
	return code
}

func usageErr(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errSeatUsage}, a...)...)
}

func parseSeatLaunch(args []string) (seatLaunch, error) {
	var req seatLaunch
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		return req, usageErr("takes %s", seatResumeUsage)
	}
	claude := args[sep+1:]
	before := len(claude)
	for _, a := range args[:sep] {
		switch {
		case a == registry.FlagFresh:
			req.fresh = true
		case a == registry.FlagRebind:
			req.rebind = true
		case strings.HasPrefix(a, "--given="):
			req.given, req.hasGiven = strings.TrimPrefix(a, "--given="), true
		case strings.HasPrefix(a, "--relaunch="):
			req.relaunch = strings.TrimPrefix(a, "--relaunch=") != ""
		case strings.HasPrefix(a, "--before="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--before="))
			if err != nil || n < 0 || n > len(claude) {
				return req, usageErr("%q is not a count of claude's %d arguments", a, len(claude))
			}
			before = n
		case strings.HasPrefix(a, "-") || req.list != "":
			return req, usageErr("%q: takes %s", a, seatResumeUsage)
		default:
			req.list = a
		}
	}
	if req.list == "" {
		return req, usageErr("no seats are named: takes %s", seatResumeUsage)
	}
	if _, err := membership.ParseSeats(req.list); err != nil {
		return req, usageErr("the seats %q: %w", req.list, err)
	}
	for _, a := range claude[:before] {
		switch a {
		case registry.FlagFresh:
			req.fresh = true
		case registry.FlagRebind:
			req.rebind = true
		default:
			req.own = append(req.own, a)
		}
	}
	req.rest = claude[before:]
	req.sel = registry.Selectors(append(slices.Clone(req.own), req.rest...))
	return req, nil
}

// chosen is whether every seat of the launch was picked in the chooser: --given was passed and
// names none of them.
func (req seatLaunch) chosen() bool {
	if !req.hasGiven {
		return false
	}
	given := strings.Split(strings.Join(strings.Fields(req.given), ""), ",")
	for item := range strings.SplitSeq(req.list, ",") {
		if slices.Contains(given, item) {
			return false
		}
	}
	return true
}

// decide is the launch's conversation, as claude arguments to add.
func (r *SeatResume) decide(wd string, req seatLaunch) ([]string, error) {
	plain := membership.Launch("ccy", req.list)
	switch {
	case req.sel.Continue:
		return nil, usageErr("refuses --continue in a team seat: it resumes this checkout's newest conversation, which may be another seat's.\n"+
			"  Resume the conversation the seats recorded:\n    %s\n  Or start a new one on purpose:\n    %s --fresh", plain, plain)
	case req.sel.Count() > 1:
		return nil, usageErr("claude's arguments select more than one conversation; give one")
	case req.fresh && req.rebind:
		return nil, usageErr("--fresh starts a new conversation and --rebind moves the seats onto a named one; give one")
	case req.fresh && req.sel.Count() > 0:
		return nil, usageErr("--fresh starts a new conversation, and claude's arguments already select one; drop one")
	case req.rebind && req.sel.Resume == "" && req.sel.SessionID == "":
		return nil, usageErr("--rebind moves the seats onto the conversation --resume <id> names, and none is named")
	case req.sel.Picker:
		return nil, nil
	case req.fresh:
		return r.fresh(wd, req)
	case req.sel.Resume != "" || req.sel.SessionID != "":
		return nil, r.named(wd, req)
	}
	return r.recorded(wd, req)
}

// fresh is --fresh: a new id, recorded for every seat.
func (r *SeatResume) fresh(wd string, req seatLaunch) ([]string, error) {
	id, err := r.NewID()
	if err != nil {
		return nil, fmt.Errorf("a new conversation id: %w", err)
	}
	if err := conversation.RecordSeats(wd, req.list, id, conversation.SourceFresh, r.NowMS()); err != nil {
		return nil, err
	}
	if err := r.say("ccy: --fresh: a new conversation %s for %s; a plain ccy --teams %s resumes it.", id, req.list, req.list); err != nil {
		return nil, err
	}
	return []string{"--session-id", id}, nil
}

// named is --resume <id> or --session-id <id>, checked against the seats' record.
func (r *SeatResume) named(wd string, req seatLaunch) error {
	id := req.sel.Resume + req.sel.SessionID
	if !registry.SessionID(id) {
		return usageErr("%q is not a Claude session id: a seat resumes a conversation by its id", id)
	}
	got, err := conversation.ForSeats(wd, req.list, conversation.Here(r.Getenv))
	var seatErr *conversation.SeatError
	switch {
	case err != nil && !errors.As(err, &seatErr):
		return fmt.Errorf("%w\n%s", err, conversation.WaysOn(req.list))
	case err == nil && got.ID == id:
		return r.record(wd, strings.Join(got.Unrecorded, ","), id, conversation.SourceLaunch)
	case err == nil && !req.rebind && !req.relaunch:
		return usageErr("the seats %s recorded the conversation %s, and this launch names %s.\n"+
			"  Resume theirs:                ccy --teams %s\n"+
			"  Move the seats onto %s: add --rebind", req.list, got.ID, id, req.list, id)
	}
	source := conversation.SourceLaunch
	switch {
	case req.relaunch:
		source = conversation.SourceRelaunch
	case req.rebind:
		source = conversation.SourceRebind
	}
	return r.record(wd, req.list, id, source)
}

// recorded is a plain launch: the seats' recorded conversation, or the refusal in its case.
func (r *SeatResume) recorded(wd string, req seatLaunch) ([]string, error) {
	got, err := conversation.ForSeats(wd, req.list, conversation.Here(r.Getenv))
	var seatErr *conversation.SeatError
	switch {
	case errors.As(err, &seatErr) && seatErr.Case == conversation.CaseNoSession && req.chosen():
		if err := r.say("ccy: the seats %s, chosen in the list, were never used: a new conversation starts.", req.list); err != nil {
			return nil, err
		}
		return r.fresh(wd, req)
	case errors.As(err, &seatErr):
		return nil, usageErr("no conversation of the seats %s can be resumed: %w\n%s", req.list, err, conversation.WaysOn(req.list))
	case err != nil:
		return nil, fmt.Errorf("%w\n%s", err, conversation.WaysOn(req.list))
	}
	if err := r.record(wd, strings.Join(got.Unrecorded, ","), got.ID, conversation.SourceLaunch); err != nil {
		return nil, err
	}
	if err := r.say("ccy: resuming the conversation %s that the seats %s recorded (ccy --teams %s --fresh starts a new one).", got.ID, req.list, req.list); err != nil {
		return nil, err
	}
	return []string{"--resume", got.ID}, nil
}

// record writes the seat records of list (none when it is empty).
func (r *SeatResume) record(wd, list, id, source string) error {
	if list == "" {
		return nil
	}
	return conversation.RecordSeats(wd, list, id, source, r.NowMS())
}

func (r *SeatResume) say(format string, a ...any) error {
	_, err := fmt.Fprintf(r.Stderr, format+"\n", a...)
	return err
}

// NewConversationID is a random version 4 UUID, the form Claude Code's --session-id takes.
func NewConversationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("could not read random bytes: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
