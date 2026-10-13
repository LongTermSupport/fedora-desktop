package restore

import (
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
)

// The states a restored session can be in, as verify-restore prints them.
const (
	StateOK              = "OK"
	StateStarting        = "STARTING"
	StateSettingGoing    = "SETTING-GOING"
	StateNotSetGoing     = "NOT-SET-GOING"
	StateWaitingAtPrompt = "WAITING-AT-PROMPT"
	StateDead            = "DEAD"
)

// Verdict is one restored session's state with the detail that goes after it, if any.
type Verdict struct {
	State  string
	Detail string
}

// String is "STATE" or "STATE detail", the form verify-restore prints.
func (v Verdict) String() string {
	if v.Detail == "" {
		return v.State
	}
	return v.State + " " + v.Detail
}

const (
	prefixCCY = "ccy"
	prefixCC  = "cc"
	goingDash = "-"

	goingSettingGoing = "setting-going"
)

// Judge is one restored session's state from what its pane shows. The screen is the pane's
// visible text and is judged by its LAST non-blank line, where a prompt waiting for input sits.
// container is, for ccy, "up" when the session's container is running, "starting" when its
// engine client exists and the container is not yet listed, "-" when there is no engine
// client; cc runs claude on the host and ignores it. going is ccy_restore_going_state's word
// ("-" when set-going has nothing to say): what set-going did only matters once the session is
// otherwise OK, so a session still at a prompt or starting reports that first.
func Judge(prefix string, live bool, pane, container, going string) Verdict {
	if !live {
		return Verdict{StateDead, "session-not-running"}
	}
	last := lastLine(pane)
	if strings.HasSuffix(last, screen.EndedText) {
		return Verdict{StateDead, "launcher-exited"}
	}
	for _, p := range screen.KnownPrompts() {
		if strings.HasPrefix(last, p.Text) {
			return Verdict{StateWaitingAtPrompt, p.Name}
		}
	}
	switch prefix {
	case prefixCC:
	case prefixCCY:
		if container != "up" {
			return Verdict{State: StateStarting}
		}
	default:
		return Verdict{StateDead, "unknown-launcher-" + prefix}
	}
	return goingVerdict(going)
}

func goingVerdict(going string) Verdict {
	switch {
	case going == goingDash, going == "started":
		return Verdict{State: StateOK}
	case going == goingSettingGoing:
		return Verdict{State: StateSettingGoing}
	case strings.HasPrefix(going, "failed:"):
		return Verdict{StateNotSetGoing, strings.TrimPrefix(going, "failed:")}
	}
	return Verdict{StateNotSetGoing, "unknown-state-" + going}
}

const shellSpace = " \t\n\v\f\r"

// lastLine is the last line of the pane that is not blank, with its leading and trailing
// whitespace removed.
func lastLine(pane string) string {
	last := ""
	for line := range strings.SplitSeq(pane, "\n") {
		if trimmed := strings.TrimRight(line, shellSpace); trimmed != "" {
			last = trimmed
		}
	}
	return strings.TrimLeft(last, shellSpace)
}

// Took is whether a conversation took input after a given moment: "yes", "no", or "-" when it
// was not asked.
type Took string

// The answers.
const (
	TookYes Took = "yes"
	TookNo  Took = "no"
	TookNA  Took = "-"
)

// GoingState is the going word Judge takes, from one manifest entry. now, at and window are
// seconds; at is 0 for an entry with none.
//
//	none               "-": not this restore's to set going
//	pending            "setting-going"
//	untouched          "failed:<detail>"
//	compacting         "setting-going" while set-going waits to type continue after the
//	                   compaction; "failed:compact-not-started" if the /compact was not taken
//	                   within window
//	compact, continue  "started" once it took input; "setting-going" until window seconds
//	                   after at; then "failed:<what>-not-started"
func GoingState(going Going, at int64, detail string, now, window int64, took Took) string {
	switch going {
	case GoingNone:
		return goingDash
	case GoingPending:
		return goingSettingGoing
	case GoingUntouched:
		if detail == "" {
			detail = "no-reason-recorded"
		}
		return "failed:" + detail
	case GoingCompacting:
		if took != TookYes && now-at >= window {
			return "failed:compact-not-started"
		}
		return goingSettingGoing
	case GoingCompact, GoingContinue:
		switch {
		case took == TookYes:
			return "started"
		case now-at < window:
			return goingSettingGoing
		}
		return "failed:" + string(going) + "-not-started"
	case GoingKeep:
		// keep is only an input to Merge; an entry holding it is as unknown as any other word.
	}
	return "failed:unknown-going-" + string(going)
}
