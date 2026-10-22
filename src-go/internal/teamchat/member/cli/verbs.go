// Package cli is the member binary's command line (Plan 00209 SPEC-n3 CL1-CL6, B 2.2): B
// 2.2.1's verb table as data, its one parser (flags only after the verb, `--flag value` only,
// no global flags), run's launch-line parser, the one-team rule, and `version` and `help`. A
// verb a later slice builds exits 1 `internal` naming that slice (CL2).
package cli

import (
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// TeamUse is how a verb finds its team (B5, CL6).
type TeamUse int

// The team uses: none (lifecycle, version, help), one team (--team, the ledger or the only
// active team), or every active team (B 2.2.3).
const (
	TeamNone TeamUse = iota
	TeamOne
	TeamMulti
)

// Flag is one flag of a verb. Value is its placeholder, "" for a flag that takes none. Group
// names a set of flags of which exactly one is given.
type Flag struct {
	Name     string
	Value    string
	Required bool
	Repeat   bool
	Group    string
	check    *checker
}

// Arg is one positional argument of a verb, written <Name> in its usage unless Form says
// otherwise.
type Arg struct {
	Name     string
	Form     string
	Optional bool
	check    *checker
	missing  func() error
}

// Verb is one row of B 2.2.1's table.
type Verb struct {
	Name  string
	Args  []Arg
	Flags []Flag
	Team  TeamUse
	Agent bool
	Does  string
}

// checker is a value's grammar and the message of a value that fails it, whose one slot is
// the flag's or argument's name; the value itself is never quoted.
type checker struct {
	ok  func(string) bool
	msg exits.Template
}

func validTeam(s string) bool {
	_, err := paths.ParseTeamID(s)
	return err == nil
}

func validUser(s string) bool {
	_, err := paths.ParseGuestUser(s)
	return err == nil
}

func validCount(s string) bool {
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n >= 0 && strconv.FormatInt(n, 10) == s
}

func validList(s string) bool {
	return !slices.Contains(strings.Split(s, ","), "")
}

func oneOf(words ...string) func(string) bool {
	return func(s string) bool { return slices.Contains(words, s) }
}

// HookEvents are the hook events of B 2.8, as the kit's hooks.json names them.
var HookEvents = []string{"session-start", "user-prompt-submit", "stop", "session-end"}

// The history rooms.
var historyRooms = []string{"main", roomQuestions}

var (
	checkTeam   = &checker{validTeam, "--{} takes a team id"}
	checkUser   = &checker{validUser, "--{} takes a user name"}
	checkCount  = &checker{validCount, "--{} takes a whole number"}
	checkDir    = &checker{absDir, msgDir}
	checkList   = &checker{validList, "--{} takes a comma-separated list with no empty item"}
	checkKind   = &checker{func(s string) bool { return slices.Contains(Kinds, Kind(s)) }, "--{} is one of ccy, lxc, host, other"}
	checkRoom   = &checker{oneOf(historyRooms...), "--{} is main or questions"}
	checkAnswer = &checker{proto.IsAnswerVerb, "--{} is one of ack, nack, done, blocked"}
	checkBranch = &checker{proto.ValidBranch, "--{} takes a branch name"}
	checkEvent  = &checker{proto.ValidEventID, "<{}> is an event id: $ and 43 characters"}
	checkQ      = &checker{proto.ValidQuestion, "<{}> is a question as Q<n>, such as Q4"}
	checkHook   = &checker{oneOf(HookEvents...), "<{}> is one of session-start, user-prompt-submit, stop, session-end"}
)

// The verbs' names that the code names.
const (
	verbRun       = "run"
	verbVersion   = "version"
	verbHelp      = "help"
	verbAsk       = "ask"
	verbQuestions = "questions"
)

// The guest operations' verbs and seat set-aside, which runs names too.
const (
	verbGuestCheck  = "guest check"
	verbPlaceBundle = "guest place-bundle"
	verbMembership  = "guest membership"
	verbGuestRemove = "guest remove"
	verbSetAside    = "seat set-aside"
)

// The flags the code names.
const (
	flagTeam = "team"
	flagTo   = "to"
	flagToC  = "to-coordinator"
	flagAsk  = "ask"
	flagVerb = "verb"
	argVerb  = "verb"
)

const roomQuestions = "questions"

const (
	valueDir   = "DIR"
	groupTo    = "to"
	topicCodes = "exit-codes"
)

var teamFlag = Flag{Name: flagTeam, Value: "T", check: checkTeam}

func required(f Flag) Flag {
	f.Required = true
	return f
}

var (
	toFlags = []Flag{
		{Name: flagTo, Value: "N[,N...]", Group: groupTo, check: checkList},
		{Name: flagToC, Group: groupTo},
	}
	eventArg = Arg{Name: "event-id", check: checkEvent}
	userFlag = Flag{Name: "user", Value: "U", Required: true, check: checkUser}
)

// table is B 2.2.1's verbs in its order: the agent verbs, then the lifecycle ones. An agent verb
// with a team use gains --team when the table is read.
var table = []Verb{
	{Name: "recv", Team: TeamMulti, Agent: true, Does: "prints pending items up to a delivery's caps, then a more record if more pend"},
	{Name: "wait", Team: TeamMulti, Agent: true, Flags: []Flag{{Name: "timeout", Value: "S", check: checkCount}}, Does: "blocks until something is pending, then acts as recv; for sessions with no inbox socket"},
	{Name: "inbox", Team: TeamMulti, Agent: true, Does: "lists what is pending without delivering it (no bodies)"},
	{
		Name: "reply", Team: TeamOne, Agent: true, Args: []Arg{eventArg},
		Flags: []Flag{{Name: flagVerb, Value: "V", check: checkAnswer}, {Name: "ref", Value: "R"}, {Name: flagToC}},
		Does:  "answers one item; text on stdin. --verb answers a ping with a closed verb",
	},
	{Name: "say", Team: TeamOne, Agent: true, Flags: toFlags, Does: "starts a conversation with named members; text on stdin"},
	{
		Name: "ping", Team: TeamOne, Agent: true,
		Args:  []Arg{{Name: argVerb, missing: barePing}, {Name: "ref", Optional: true}},
		Flags: append(slices.Clone(toFlags), Flag{Name: "branch", Value: "B", check: checkBranch}),
		Does:  "sends a closed-verb ping; the verb is required",
	},
	{Name: "report", Team: TeamOne, Agent: true, Does: "a quiet report to the reports room; headline then body on stdin"},
	{
		Name: verbAsk, Team: TeamOne, Agent: true,
		Flags: []Flag{{Name: "asked-by", Value: "N,...", check: checkList}, {Name: "re", Value: "ID,...", check: checkList}, {Name: "remind-after", Value: "S", check: checkCount}},
		Does:  "the coordinator asks the team's humans a numbered question; text on stdin",
	},
	{
		Name: "thread", Team: TeamOne, Agent: true, Args: []Arg{{Name: "question", Form: "Q<n>", check: checkQ}},
		Flags: []Flag{{Name: flagAsk}, {Name: "close"}},
		Does:  "the coordinator posts in a question's thread (quiet, --ask to notify the humans, --close to close it)",
	},
	{Name: verbQuestions, Team: TeamMulti, Agent: true, Flags: []Flag{{Name: "all"}}, Does: "the coordinator's open questions and the human replies not yet answered"},
	{
		Name: "history", Team: TeamMulti, Agent: true,
		Flags: []Flag{{Name: "room", Value: "main|questions", check: checkRoom}, {Name: "limit", Value: "N", check: checkCount}},
		Does:  "what this member received and sent, read from the room, newest first",
	},
	{Name: "verify", Team: TeamOne, Agent: true, Args: []Arg{eventArg}, Does: "re-fetches one event from the homeserver: its verified kind, sender, team, room, full text and SHA-256"},
	{Name: "status", Team: TeamMulti, Agent: true, Does: "this session's teams, roles, members, side rooms, delivery state, open pings and runtime"},
	{Name: verbVersion, Agent: true, Does: "the binary's source digest and the record version it prints"},
	{
		Name: verbHelp, Agent: true,
		Args: []Arg{{Name: argVerb, Form: "<verb>|" + topicCodes, Optional: true}, {Name: "word", Form: "-", Optional: true}},
		Does: "the verbs and their flags, one verb's usage, or the exit-code table",
	},
	{Name: verbRun, Flags: launchFlags, Does: "the keeper: launches CMD as its child and stays its parent for the session's life"},
	{Name: "link", Flags: []Flag{{Name: "run", Value: valueDir, Required: true, check: checkDir}}, Does: "the link; started by the keeper only"},
	{Name: "hook", Args: []Arg{{Name: "event", Form: strings.Join(HookEvents, "|"), check: checkHook}}, Does: "one Claude Code hook, run from the plugin's hooks.json"},
	{Name: "status-line", Does: "the status line, run by Claude Code from the session's settings"},
	{Name: "reach", Flags: []Flag{required(teamFlag)}, Does: "an unauthenticated GET of the team homeserver's versions, so a route is proven from the member side"},
	{
		Name:  "seat disconnected",
		Flags: []Flag{{Name: "bundle", Value: valueDir, Required: true, check: checkDir}, {Name: "since", Value: "MS", Required: true, check: checkCount}, {Name: "seed", Value: "ID", Required: true}},
		Does:  "posts a seat's disconnected notice once, from a host-held bundle",
	},
	{Name: verbSetAside, Flags: []Flag{required(teamFlag), {Name: "state", Value: valueDir, check: checkDir}}, Does: "renames a seat's store.json aside under the seat lock"},
	{Name: verbGuestCheck, Flags: []Flag{userFlag, required(teamFlag)}, Does: "checks an LXC guest's user can run a member session; stdin the team's reach facts"},
	{Name: verbPlaceBundle, Flags: []Flag{userFlag}, Does: "places the seat bundle on stdin in the user's home"},
	{Name: verbMembership, Flags: []Flag{userFlag}, Does: "writes the desired membership on stdin as the user's desired.json"},
	{Name: verbGuestRemove, Flags: []Flag{userFlag, required(teamFlag)}, Does: "removes a team's seat files but seat.lock, under the seat lock"},
}

// Verbs is the table, each agent verb with its --team; a copy the caller may change.
func Verbs() []Verb {
	out := make([]Verb, len(table))
	for i, v := range table {
		v.Args = slices.Clone(v.Args)
		v.Flags = slices.Clone(v.Flags)
		if v.Agent && v.Team != TeamNone {
			v.Flags = append(v.Flags, teamFlag)
		}
		out[i] = v
	}
	return out
}

func lookup(name string) (Verb, bool) {
	for _, v := range Verbs() {
		if v.Name == name {
			return v, true
		}
	}
	return Verb{}, false
}

// Usage is a verb's one-line form, as B 2.2.1 spells it.
func Usage(v Verb) string {
	parts := []string{"teamchat", v.Name}
	for _, a := range v.Args {
		parts = append(parts, a.usage()...)
	}
	done := map[string]bool{}
	for _, f := range v.Flags {
		if f.Group != "" {
			if !done[f.Group] {
				done[f.Group] = true
				parts = append(parts, groupUsage(v.Flags, f.Group))
			}
			continue
		}
		parts = append(parts, f.usage())
	}
	if v.Name == verbRun {
		parts = append(parts, separator, "CMD")
	}
	return strings.Join(parts, " ")
}

func (a Arg) usage() []string {
	form := a.Form
	switch form {
	case "-":
		return nil
	case "":
		form = "<" + a.Name + ">"
	}
	if a.Optional {
		form = "[" + form + "]"
	}
	return []string{form}
}

func (f Flag) spelled() string {
	if f.Value == "" {
		return "--" + f.Name
	}
	return "--" + f.Name + " " + f.Value
}

func (f Flag) usage() string {
	s := f.spelled()
	if f.Repeat {
		s += "..."
	}
	if !f.Required {
		s = "[" + s + "]"
	}
	return s
}

func groupUsage(flags []Flag, group string) string {
	var forms []string
	for _, f := range flags {
		if f.Group == group {
			forms = append(forms, f.spelled())
		}
	}
	return strings.Join(forms, "|")
}

// sendVerbs are the verbs a ping can start a conversation with: A 8.1's verbs that are not
// answers, which reply sends.
func sendVerbs() []string {
	var verbs []string
	for verb := range proto.Verbs {
		if !proto.IsAnswerVerb(verb) {
			verbs = append(verbs, verb)
		}
	}
	slices.Sort(verbs)
	return verbs
}

const msgBarePing exits.Template = "ping needs its verb first: one of {}; teamchat status shows who is live"

func barePing() error {
	verbs := sendVerbs()
	fills := make([]exits.Fill, 0, len(verbs))
	for _, v := range verbs {
		fills = append(fills, exits.Name(v))
	}
	return usage(msgBarePing, exits.Join(", ", fills...))
}
