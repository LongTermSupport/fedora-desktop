package teams

import (
	"io"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// verbSeatResume is `ccy-teams internal seat-resume`, whose stdout is its payload.
const verbSeatResume = "seat-resume"

// The launcher's words seat-resume takes before `--`: ccy's --fresh and --rebind, and
// --relaunch, which the launcher adds for a restore, a team restart or the supervisor's
// relaunch (each resumes the session's own conversation by id).
const (
	flagFresh    = "--fresh"
	flagRebind   = "--rebind"
	flagRelaunch = "--relaunch"
)

// SeatResume is `ccy-teams internal seat-resume <seats> [--relaunch] <before> -- <claude
// args...>` (issue 417): which conversation a ccy launch that takes seats starts in.
// The launcher runs it right after the seat check, before any prompt, and appends
// `--resume <id>` to claude's arguments when it prints an id. Exit 64 refuses the launch.
//
//   - -c/--continue: refused (the owner's ruling Q67), naming the plain launch and --fresh.
//   - --fresh: a new conversation, nothing printed; refused with a resume, --session-id or
//     --rebind.
//   - a bare --resume or -r (Claude Code's picker, ruling Q70), or one whose value is not a
//     session id, and --session-id: left as they are; --rebind is refused with them.
//   - --resume <id>: left as it is when no named seat has a pointer or every pointer is that id;
//     another id is refused unless --rebind or --relaunch, and a relaunch is refused too when
//     that pointer is a repair (--set) newer than the session's record: the repair wins.
//   - every lookup here saves the pointers it moves (conversation.SaveSeats), because the
//     launch may reuse a session folder whose records the pointers are taken from.
//   - none of these: the conversation the seats agree on (conversation.ForSeats) is printed;
//     when none can be named the case is given with both ways on, the repair
//     (`ccy-teams seats conversation <seats> --set <id>`) and `ccy --teams <seats> --fresh`.
type SeatResume struct {
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Getwd          func() (string, error)
}

const seatResumeUsage = "ccy-teams internal seat-resume: usage: ccy-teams internal seat-resume <seats> " +
	"[--relaunch] <before> -- <claude arguments...>, <before> being how many of claude's arguments " +
	"came before the launcher's --; it prints, NUL separated, the new count and claude's arguments. See ccy-teams --help."

// Run is the command over its arguments (after `internal seat-resume`): `<seats> [--relaunch]
// <before> -- <args...>`, args being the launcher's arguments for claude of which the first
// <before> came before its `--`. It prints, NUL separated, the new count before the separator
// and claude's arguments: ccy's --fresh and --rebind taken out of the first <before>, and
// `--resume <id>` appended when the seats' conversation is resumed.
func (r *SeatResume) Run(args []string) term.ExitCode {
	relaunch := len(args) > 1 && args[1] == flagRelaunch
	if relaunch {
		args = append(args[:1:1], args[2:]...)
	}
	if len(args) < 3 || strings.HasPrefix(args[0], "-") || args[2] != "--" {
		return say(r.Stderr, term.ExitUsage, seatResumeUsage)
	}
	list, claude := args[0], args[3:]
	before, err := strconv.Atoi(args[1])
	if err != nil || before < 0 || before > len(claude) {
		return say(r.Stderr, term.ExitUsage, seatResumeUsage)
	}
	fresh, rebind, ccyWords := false, false, before
	kept := make([]string, 0, len(claude)+2)
	for i, w := range claude {
		switch {
		case i < ccyWords && w == flagFresh:
			fresh = true
		case i < ccyWords && w == flagRebind:
			rebind = true
		default:
			kept = append(kept, w)
			continue
		}
		before--
	}
	id, code := r.decide(list, registry.ReadSelectors(kept), fresh, rebind, relaunch)
	if code != term.ExitOK {
		return code
	}
	if id != "" {
		kept = append(kept, "--resume", id)
	}
	var out strings.Builder
	for _, w := range append([]string{strconv.Itoa(before)}, kept...) {
		out.WriteString(w + "\x00")
	}
	if _, err := io.WriteString(r.Stdout, out.String()); err != nil {
		return say(r.Stderr, term.ExitFailure, "ccy-teams internal seat-resume: could not print claude's arguments: "+err.Error())
	}
	return term.ExitOK
}

// decide is the id to resume ("" for none) and the exit code.
func (r *SeatResume) decide(list string, sel registry.Selectors, fresh, rebind, relaunch bool) (string, term.ExitCode) {
	if sel.Continues || fresh || rebind && sel.ID == "" || sel.Picker || sel.SessionID || sel.ID != "" {
		return "", r.chosen(list, sel, fresh, rebind, relaunch)
	}
	return r.recorded(list)
}

// chosen is a launch whose arguments choose the conversation themselves.
func (r *SeatResume) chosen(list string, sel registry.Selectors, fresh, rebind, relaunch bool) term.ExitCode {
	plain := "    ccy --teams " + list
	freshCmd := "    ccy --teams " + list + " --fresh"
	switch {
	case sel.Continues:
		return say(r.Stderr, term.ExitUsage, "ccy refuses --continue in a team seat: it resumes this checkout's newest conversation, which may be another seat's.\n"+
			"  The plain launch resumes the conversation recorded for these seats:\n"+plain+"\n"+
			"  or start a new one:\n"+freshCmd)
	case fresh && (sel.ID != "" || sel.Picker || sel.SessionID || rebind):
		return say(r.Stderr, term.ExitUsage, "ccy: --fresh starts a new conversation, so it cannot be given with --resume, --session-id or --rebind. Drop one.")
	case fresh:
		return say(r.Stderr, term.ExitOK, "ccy: --fresh: starting a new conversation for "+list+
			"; the conversation recorded for these seats is not resumed, and the new one is recorded as theirs once it starts.")
	case rebind && sel.ID == "":
		return say(r.Stderr, term.ExitUsage, "ccy: --rebind goes with --resume <id>: it points the seats at that conversation. Drop it, or name the conversation.")
	case sel.ID != "" && !sel.Picker && !sel.SessionID:
		return r.explicit(list, sel.ID, rebind, relaunch)
	}
	return term.ExitOK
}

// explicit is --resume <id>: refused when a named seat points at another conversation, unless
// rebinding or relaunching; a relaunch is refused too when that pointer is a repair (--set)
// newer than the session's record, which the lookup's harvest has already compared.
func (r *SeatResume) explicit(list, id string, rebinding, relaunch bool) term.ExitCode {
	wd, err := r.Getwd()
	if err != nil {
		return say(r.Stderr, term.ExitFailure, "ccy-teams internal seat-resume: the working directory: "+err.Error())
	}
	seats, err := conversation.SaveSeats(wd, list, conversation.Here(r.Getenv))
	if err != nil {
		return say(r.Stderr, term.ExitUsage, "ccy: the conversation recorded for "+list+" cannot be read: "+err.Error())
	}
	var other, repaired []conversation.SeatPointer
	for _, p := range seats.Pointers {
		if p.ID != id {
			other = append(other, p)
			if p.SetByHand() {
				repaired = append(repaired, p)
			}
		}
	}
	switch {
	case len(other) == 0:
		return term.ExitOK
	case relaunch && len(repaired) > 0:
		return say(r.Stderr, term.ExitUsage, "ccy refuses to relaunch "+list+" in its old conversation "+id+": a seat was pointed at another one by hand after the session recorded it, and the repair wins:\n"+
			conversation.Describe(repaired)+"\n  Launch the seats in the conversation they point at:\n    ccy --teams "+list)
	case rebinding || relaunch:
		return say(r.Stderr, term.ExitOK, "ccy: rebinding "+list+" to the conversation "+id+"; until now:\n"+conversation.Describe(other))
	}
	return say(r.Stderr, term.ExitUsage, "ccy refuses --resume "+id+" for "+list+": the seats' record names another conversation, "+
		"and resuming this one would move them onto it:\n"+conversation.Describe(other)+"\n"+
		"  Resume the recorded conversation with the plain launch:\n    ccy --teams "+list+"\n"+
		"  or, to move the seats onto "+id+" on purpose, add --rebind:\n    ccy --resume "+id+" --teams "+list+" --rebind")
}

// recorded is the plain launch: the conversation the seats agree on, or the refusal with both
// ways on.
func (r *SeatResume) recorded(list string) (string, term.ExitCode) {
	wd, err := r.Getwd()
	if err != nil {
		return "", say(r.Stderr, term.ExitFailure, "ccy-teams internal seat-resume: the working directory: "+err.Error())
	}
	seats, err := conversation.SaveSeats(wd, list, conversation.Here(r.Getenv))
	var got conversation.Seated
	if err == nil {
		got, err = seats.Resolve()
	}
	if err != nil {
		return "", say(r.Stderr, term.ExitUsage, "ccy: no conversation of "+list+" can be resumed: "+err.Error()+"\n"+
			"  Repair the record, pointing the seats at the right conversation (its transcript must be in this checkout):\n"+
			"    ccy-teams seats conversation "+list+" --set <id>\n"+
			"  or start a new conversation on purpose:\n    ccy --teams "+list+" --fresh")
	}
	note := "ccy: resuming the conversation recorded for " + list + ": " + got.ID
	if len(got.Unrecorded) > 0 {
		note += "\n  " + strings.Join(got.Unrecorded, ",") + " had none of its own and is recorded with it once the session starts"
	}
	note += "\n  (a new one instead: ccy --teams " + list + " --fresh)"
	return got.ID, say(r.Stderr, term.ExitOK, note)
}
