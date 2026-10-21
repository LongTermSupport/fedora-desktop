package restore

import (
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
)

// State is a restored session's state, as verify-restore prints it.
type State string

// The states a restored session can be in.
const (
	StateOK              State = "OK"
	StateStarting        State = "STARTING"
	StateSettingGoing    State = "SETTING-GOING"
	StateNotSetGoing     State = "NOT-SET-GOING"
	StateWaitingAtPrompt State = "WAITING-AT-PROMPT"
	StateDead            State = "DEAD"
)

// ContainerState is what the engine says about a ccy session's container.
type ContainerState string

// The container states.
const (
	// ContainerNone is a session with no engine client (cc runs claude on the host).
	ContainerNone ContainerState = "-"
	// ContainerUp is a container in the engine's running list.
	ContainerUp ContainerState = "up"
	// ContainerStarting is an engine client whose container is not yet listed.
	ContainerStarting ContainerState = "starting"
)

// OutcomeKind is what set-going did, or has yet to do, with a session.
type OutcomeKind uint8

// The outcomes. The zero value is OutcomeNone: set-going has nothing to say.
const (
	// OutcomeNone is a session that is not this restore's to set going.
	OutcomeNone OutcomeKind = iota
	// OutcomeSettingGoing is a session set-going has yet to finish with.
	OutcomeSettingGoing
	// OutcomeStarted is a session that took what was typed into it.
	OutcomeStarted
	// OutcomeFailed is a session set-going left alone or could not set going; Detail says why.
	OutcomeFailed
)

// GoingOutcome is what set-going did to one session, judged from its manifest entry.
type GoingOutcome struct {
	Kind   OutcomeKind
	Detail string // the reason, for OutcomeFailed only
}

// String is the going word the bash restore library speaks: "-", "setting-going", "started" or
// "failed:<detail>".
func (o GoingOutcome) String() string {
	switch o.Kind {
	case OutcomeNone:
		return "-"
	case OutcomeSettingGoing:
		return "setting-going"
	case OutcomeStarted:
		return "started"
	case OutcomeFailed:
		return "failed:" + o.Detail
	}
	return "-"
}

// Verdict is one restored session's state with the detail that goes after it, if any.
type Verdict struct {
	State  State
	Detail string
}

// String is "STATE" or "STATE detail", the form verify-restore prints.
func (v Verdict) String() string {
	if v.Detail == "" {
		return string(v.State)
	}
	return string(v.State) + " " + v.Detail
}

const (
	prefixCCY = "ccy"
	prefixCC  = "cc"
)

// Judge is one restored session's state from what its pane shows. The screen is the pane's
// visible text and is judged by its LAST non-blank line, where a prompt waiting for input sits,
// and then as a whole for Claude Code's conversation picker, which nobody restoring can answer.
// container is, for ccy, ContainerUp when the session's container is running, ContainerStarting
// when its engine client exists and the container is not yet listed, ContainerNone when there is
// no engine client; cc runs claude on the host and ignores it. going is what set-going did to
// the session (OutcomeNone when it has nothing to say): that only matters once the session is
// otherwise OK, so a session still at a prompt or starting reports that first.
func Judge(prefix string, live bool, pane string, container ContainerState, going GoingOutcome) Verdict {
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
	if screen.AtConversationPicker(pane) {
		return Verdict{StateWaitingAtPrompt, screen.ConversationPicker}
	}
	switch prefix {
	case prefixCC:
	case prefixCCY:
		if container != ContainerUp {
			return Verdict{State: StateStarting}
		}
	default:
		return Verdict{StateDead, "unknown-launcher-" + prefix}
	}
	return goingVerdict(going)
}

func goingVerdict(going GoingOutcome) Verdict {
	switch going.Kind {
	case OutcomeNone, OutcomeStarted:
		return Verdict{State: StateOK}
	case OutcomeSettingGoing:
		return Verdict{State: StateSettingGoing}
	case OutcomeFailed:
		return Verdict{StateNotSetGoing, going.Detail}
	}
	return Verdict{StateNotSetGoing, "unknown-state-" + going.String()}
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

// GoingState is what set-going did to one manifest entry, as Judge takes it. now, at and window
// are seconds; at is 0 for an entry with none.
//
//	none               OutcomeNone: not this restore's to set going
//	pending            OutcomeSettingGoing
//	untouched          OutcomeFailed with the entry's detail
//	compacting         OutcomeSettingGoing while set-going waits to type continue after the
//	                   compaction; failed "compact-not-started" if the /compact was not taken
//	                   within window
//	compact, continue  OutcomeStarted once it took input; OutcomeSettingGoing until window seconds
//	                   after at; then failed "<what>-not-started"
func GoingState(going Going, at int64, detail string, now, window int64, took Took) GoingOutcome {
	switch going {
	case GoingNone:
		return GoingOutcome{Kind: OutcomeNone}
	case GoingPending:
		return GoingOutcome{Kind: OutcomeSettingGoing}
	case GoingUntouched:
		if detail == "" {
			detail = "no-reason-recorded"
		}
		return GoingOutcome{OutcomeFailed, detail}
	case GoingCompacting:
		if took != TookYes && now-at >= window {
			return GoingOutcome{OutcomeFailed, "compact-not-started"}
		}
		return GoingOutcome{Kind: OutcomeSettingGoing}
	case GoingCompact, GoingContinue:
		switch {
		case took == TookYes:
			return GoingOutcome{Kind: OutcomeStarted}
		case now-at < window:
			return GoingOutcome{Kind: OutcomeSettingGoing}
		}
		return GoingOutcome{OutcomeFailed, string(going) + "-not-started"}
	case GoingKeep:
		// keep is only an input to Merge; an entry holding it is as unknown as any other word.
	}
	return GoingOutcome{OutcomeFailed, "unknown-going-" + string(going)}
}
