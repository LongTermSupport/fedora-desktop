package cli

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Mode is a launch's kind: Claude Code, or a plain command (B 2.4.1 step 5).
type Mode int

// The two launch modes; exactly one is given.
const (
	LaunchClaude Mode = iota + 1
	LaunchPlain
)

// Kind is the member kind a launch states, so a failed link's recovery is the kind's own.
type Kind string

// The member kinds of B 2.2.1.
const (
	KindCCY   Kind = "ccy"
	KindLXC   Kind = "lxc"
	KindHost  Kind = "host"
	KindOther Kind = "other"
)

// Kinds is every member kind, in B's order.
var Kinds = []Kind{KindCCY, KindLXC, KindHost, KindOther}

// SeatRef is one `--seat T/S` of the acceptance probe's launch.
type SeatRef struct {
	Team paths.TeamID
	Seat paths.SeatName
}

// Launch is `run`'s parsed command line (SPEC-n3 CL5). Claude is the index in Cmd of Claude
// Code's argument under LaunchClaude, -1 under LaunchPlain. Kit is the re-exec form's kit
// directory (B 2.4.1 step 2), "" when run resolves the kit itself.
type Launch struct {
	Mode           Mode
	Kind           Kind
	Desired        string
	State          string
	Kit            string
	Seats          []SeatRef
	ResumeRecorded bool
	Cmd            []string
	Claude         int
}

// The launch flags.
const (
	flagClaude         = "claude"
	flagPlain          = "plain"
	flagKind           = "kind"
	flagDesired        = "desired"
	flagState          = "state"
	flagSeat           = "seat"
	flagResumeRecorded = "resume-recorded"
	flagKit            = "kit"
)

const groupMode = "mode"

// launchFlags are run's flags; it parses them itself, since everything after `--` is CMD.
var launchFlags = []Flag{
	{Name: flagClaude, Group: groupMode},
	{Name: flagPlain, Group: groupMode},
	{Name: flagKind, Value: "ccy|lxc|host|other", Required: true, check: checkKind},
	{Name: flagDesired, Value: valueDir, check: checkDir},
	{Name: flagSeat, Value: "T/S", Repeat: true},
	{Name: flagState, Value: valueDir, check: checkDir},
	{Name: flagResumeRecorded},
	{Name: flagKit, Value: valueDir, check: checkDir},
}

const separator = "--"

// The messages of a refused launch line.
const (
	msgLaunchNoSeparator   exits.Template = "run takes its flags, then --, then the command: teamchat run --claude|--plain --kind K [flags] -- CMD"
	msgLaunchNoCmd         exits.Template = "run has no command after --"
	msgLaunchStrayArgument exits.Template = "run takes no argument before --; the command follows --"
	msgLaunchSeatState     exits.Template = "--seat is only for a launch with --state and no --desired"
	msgLaunchSeatForm      exits.Template = "--seat is T/S: a team id, a slash, a seat name"
	msgLaunchSeatTwice     exits.Template = "--seat names team {} twice"
	msgLaunchNoClaude      exits.Template = "under --claude the command must have an argument named claude"
	msgLaunchOverridden    exits.Template = "the command holds {}, which run appends itself; take it out"
	msgLaunchSeparator     exits.Template = "the command may hold one bare --, only directly before claude"
	msgLaunchResume        exits.Template = "--resume-recorded and claude's own {} cannot both be given"
	msgLaunchResumePlain   exits.Template = "--resume-recorded is only for a --claude launch"
	msgDir                 exits.Template = "--{} takes an absolute, clean directory other than /"
)

func usage(t exits.Template, fills ...exits.Fill) error {
	return exits.New(exits.CodeUsage, nil, t, fills...)
}

// ParseLaunch parses run's arguments, those after `run` (CL5, B 2.4.1 steps 2, 5 and 6).
func ParseLaunch(args []string) (Launch, error) {
	sep := slices.Index(args, separator)
	if sep < 0 {
		return Launch{}, usage(msgLaunchNoSeparator)
	}
	l := Launch{Cmd: slices.Clone(args[sep+1:]), Claude: -1}
	if len(l.Cmd) == 0 {
		return Launch{}, usage(msgLaunchNoCmd)
	}
	got, err := parseFlags(verbRun, launchFlags, args[:sep])
	if err != nil {
		return Launch{}, err
	}
	if len(got.args) > 0 {
		return Launch{}, usage(msgLaunchStrayArgument)
	}
	if err := l.fromFlags(got); err != nil {
		return Launch{}, err
	}
	if l.Mode == LaunchPlain {
		if l.ResumeRecorded {
			return Launch{}, usage(msgLaunchResumePlain)
		}
		return l, nil
	}
	if err := l.checkClaudeCmd(); err != nil {
		return Launch{}, err
	}
	return l, nil
}

func (l *Launch) fromFlags(got parsed) error {
	l.Mode = LaunchPlain
	if got.has(flagClaude) {
		l.Mode = LaunchClaude
	}
	l.Kind = Kind(got.value(flagKind))
	l.ResumeRecorded = got.has(flagResumeRecorded)
	l.Desired, l.State, l.Kit = got.value(flagDesired), got.value(flagState), got.value(flagKit)
	return l.seats(got.values[flagSeat])
}

func (l *Launch) seats(refs []string) error {
	if len(refs) == 0 {
		return nil
	}
	if l.State == "" || l.Desired != "" {
		return usage(msgLaunchSeatState)
	}
	for _, ref := range refs {
		teamText, seatText, ok := strings.Cut(ref, "/")
		team, terr := paths.ParseTeamID(teamText)
		seat, serr := paths.ParseSeatName(seatText)
		if !ok || terr != nil || serr != nil {
			return usage(msgLaunchSeatForm)
		}
		if slices.ContainsFunc(l.Seats, func(s SeatRef) bool { return s.Team == team }) {
			return usage(msgLaunchSeatTwice, exits.Team(team))
		}
		l.Seats = append(l.Seats, SeatRef{Team: team, Seat: seat})
	}
	return nil
}

// claudeFlagsAppended are the flags run appends after a Claude Code CMD, which CMD must not
// hold anywhere (B 2.4.1 step 5).
var claudeFlagsAppended = []string{"--settings", "--plugin-dir"}

// resumeFlags are Claude Code's own resume flags, which --resume-recorded would duplicate.
var resumeFlags = []string{"--resume", "--continue", "-r", "-c"}

// flagName is an argument's flag name, without any `=value`.
func flagName(a string) string {
	return strings.SplitN(a, "=", 2)[0]
}

func (l *Launch) checkClaudeCmd() error {
	l.Claude = slices.IndexFunc(l.Cmd, func(a string) bool { return filepath.Base(a) == "claude" })
	if l.Claude < 0 {
		return usage(msgLaunchNoClaude)
	}
	for i, a := range l.Cmd {
		name := flagName(a)
		if slices.Contains(claudeFlagsAppended, name) {
			return usage(msgLaunchOverridden, exits.Name(name))
		}
		if a == separator && i != l.Claude-1 {
			return usage(msgLaunchSeparator)
		}
		if l.ResumeRecorded && i > l.Claude && slices.Contains(resumeFlags, name) {
			return usage(msgLaunchResume, exits.Name(name))
		}
	}
	return nil
}

// absDir is whether p is an absolute, clean directory path other than the root.
func absDir(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && p != "/"
}
