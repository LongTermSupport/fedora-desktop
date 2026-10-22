// Package exits is the member binary's exit codes and its error object (Plan 00209 SPEC-n3
// CL4, B 2.2.4): the table as data, each exit's closed set of codes and its class, and the
// error object every failing verb writes as the last line of stderr. A message is a fixed
// template filled with grammar-checked names only, so no file value and no secret is quoted.
package exits

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Class is what an agent does on an exit (B 2.2.4); the skill teaches the classes, not codes.
type Class string

// The classes. ClassLifecycle is exit 75's, which only the lifecycle verbs return.
const (
	ClassNone      Class = "-"
	ClassFix       Class = "fix"
	ClassWait      Class = "wait"
	ClassRead      Class = "read"
	ClassTell      Class = "tell a human"
	ClassLifecycle Class = "lifecycle verbs only"
)

func (c Class) String() string { return string(c) }

// Does is what an agent does on an exit of the class (B 2.2.4's class table).
func (c Class) Does() string {
	switch c {
	case ClassFix:
		return "change the message or the command; never retry it unchanged"
	case ClassWait:
		return "wait and try once more; do not loop"
	case ClassRead:
		return "read the records it printed (team-failed) and carry on"
	case ClassTell:
		return "stop using teamchat for this and tell a human; start nothing"
	case ClassLifecycle:
		return "returned to the host and launchers, never to an agent"
	case ClassNone:
	}
	return "-"
}

// Classes are the classes an agent meets, in B's order.
var Classes = []Class{ClassFix, ClassWait, ClassRead, ClassTell}

// The exits of B 2.2.4.
const (
	ExitInternal    term.ExitCode = term.ExitFailure
	ExitTimedOut    term.ExitCode = 4
	ExitPartial     term.ExitCode = 5
	ExitRefused     term.ExitCode = 10
	ExitRole        term.ExitCode = 11
	ExitForge       term.ExitCode = 13
	ExitRate        term.ExitCode = 14
	ExitDuplicate   term.ExitCode = 15
	ExitUnreachable term.ExitCode = 20
	ExitToken       term.ExitCode = 21
	ExitUntrusted   term.ExitCode = 22
	ExitNotFound    term.ExitCode = 23
	ExitUsage       term.ExitCode = term.ExitUsage
	ExitNoRuntime   term.ExitCode = 69
	ExitBusy        term.ExitCode = 75
	ExitConfig      term.ExitCode = 78
)

// The codes of B 2.2.4's table, each in exactly one exit's set; CodeLaunch is from run's launch
// failure (B 2.4.1 step 7).
const (
	CodeInternal        = "internal"
	CodeTimedOut        = "timed-out"
	CodePartial         = "partial"
	CodeSize            = "size"
	CodeSchema          = "schema"
	CodeSecret          = "secret"
	CodeTarget          = "target"
	CodeVerb            = "verb"
	CodeRef             = "ref"
	CodeRe              = "re"
	CodeBody            = "body"
	CodeEmpty           = "empty"
	CodeRole            = "role"
	CodeForge           = "forge"
	CodeProvenance      = "provenance"
	CodeRate            = "rate"
	CodeServerRate      = "server-rate"
	CodeReportRate      = "report-rate"
	CodeDuplicate       = "duplicate"
	CodeUnreachable     = "unreachable"
	CodeToken           = "token"
	CodeUntrusted       = "untrusted"
	CodeForbidden       = "forbidden"
	CodeNotFound        = "not-found"
	CodeUsage           = "usage"
	CodeAmbiguousTeam   = "ambiguous-team"
	CodeUnknownMember   = "unknown-member"
	CodeAmbiguousMember = "ambiguous-member"
	CodeNoSession       = "no-session"
	CodeNoLink          = "no-link"
	CodeLinkFailed      = "link-failed"
	CodeSeatHeld        = "seat-held"
	CodeKeeperRunning   = "keeper-running"
	CodeBundle          = "bundle"
	CodeMembership      = "membership"
	CodeStore           = "store"
	CodeForgeLogin      = "forge-login"
	CodeRuntime         = "runtime"
	CodeLaunch          = "launch"
)

// The guest operations' configuration codes (SPEC-n3 GU1, GU2): the caller is not the user
// named, or one of guest check's checks failed.
const (
	CodeIdentity = "identity"
	CodeHome     = "home"
	CodeGh       = "gh"
	CodeTmux     = "tmux"
	CodeClaude   = "claude"
	CodeKernel   = "kernel"
	CodeReach    = "reach"
)

// Row is one exit of the table.
type Row struct {
	Exit    term.ExitCode
	Meaning string
	Codes   []string
	Class   Class
	Next    string
}

var table = []Row{
	{term.ExitOK, `done (including "nothing pending")`, nil, ClassNone, "-"},
	{ExitInternal, "internal error (a bug; never returned on purpose)", []string{CodeInternal}, ClassTell, "tell a human"},
	{ExitTimedOut, "wait timed out with nothing", []string{CodeTimedOut}, ClassWait, "run wait again if you still wait"},
	{ExitPartial, "a multi-team verb succeeded for some teams only", []string{CodePartial}, ClassRead, "read the team-failed records"},
	{ExitRefused, "refused by the protocol rules", []string{CodeSize, CodeSchema, CodeSecret, CodeTarget, CodeVerb, CodeRef, CodeRe, CodeBody, CodeEmpty}, ClassFix, "fix the message; do not retry it unchanged"},
	{ExitRole, "refused by role", []string{CodeRole}, ClassFix, "ask the coordinator instead"},
	{ExitForge, "a reference did not resolve at the forge, or its provenance failed", []string{CodeForge, CodeProvenance}, ClassFix, "push first, or reference what exists"},
	{ExitRate, "rate limited", []string{CodeRate, CodeServerRate, CodeReportRate}, ClassWait, "wait; do not loop"},
	{ExitDuplicate, "the same message again within the duplicate window", []string{CodeDuplicate}, ClassFix, "it was already sent; do not resend"},
	{ExitUnreachable, "the homeserver did not answer", []string{CodeUnreachable}, ClassWait, "tell a human if it persists; the link keeps retrying on its own"},
	{ExitToken, "the member's token was refused", []string{CodeToken}, ClassTell, "tell a human"},
	{ExitUntrusted, "a room is missing or not trusted, or the homeserver forbade it", []string{CodeUntrusted, CodeForbidden}, ClassTell, "run status; tell a human"},
	{ExitNotFound, "the event or question named does not exist", []string{CodeNotFound}, ClassFix, "check the id"},
	{ExitUsage, "usage error", []string{CodeUsage, CodeAmbiguousTeam, CodeUnknownMember, CodeAmbiguousMember}, ClassFix, "read the message, which lists the choices"},
	{ExitNoRuntime, "no runtime: not in a member session, or its link is not answering", []string{CodeNoSession, CodeNoLink, CodeLinkFailed}, ClassTell, "pass the message, which names the recovery, to a human; do not start anything yourself"},
	{ExitBusy, "busy: the seat is held by another session, or a keeper already runs", []string{CodeSeatHeld, CodeKeeperRunning}, ClassLifecycle, "(lifecycle verbs only)"},
	{ExitConfig, "configuration refused (bundle, desired membership, store, launch, a guest's identity or check)", []string{CodeBundle, CodeMembership, CodeStore, CodeForgeLogin, CodeRuntime, CodeLaunch, CodeIdentity, CodeHome, CodeGh, CodeTmux, CodeClaude, CodeKernel, CodeReach}, ClassTell, "run status; tell a human"},
}

// Table is B 2.2.4's table, a copy the caller may change.
func Table() []Row {
	rows := make([]Row, len(table))
	for i, r := range table {
		r.Codes = slices.Clone(r.Codes)
		rows[i] = r
	}
	return rows
}

// ExitOf is the exit whose set holds code.
func ExitOf(code string) (term.ExitCode, bool) {
	for _, r := range table {
		if slices.Contains(r.Codes, code) {
			return r.Exit, true
		}
	}
	return 0, false
}

// Error is a verb's failure as B 2.2.2's error object. Make one with New, so its exit matches
// its code and its message is a filled template.
type Error struct {
	Exit    term.ExitCode
	Code    string
	Team    *paths.TeamID
	Message string
}

func (e *Error) Error() string { return e.Message }

// Template is a message with one {} per fill. It is a constant of the caller's: a string
// variable does not convert to it without saying so.
type Template string

// Fill is one checked name for a template; a value failing its grammar shows as "?".
type Fill struct{ text string }

const unshown = "?"

var (
	nameGrammar  = regexp.MustCompile(`^[A-Za-z0-9$@#/_.:+=,-]{1,255}$`)
	wordsGrammar = regexp.MustCompile(`^[a-z0-9-]+(?: [a-z0-9-]+)*$`)
)

func checked(ok bool, s string) Fill {
	if !ok {
		return Fill{unshown}
	}
	return Fill{s}
}

// Name is an identifier, a handle, an event id, a flag or a path: no space, quote or control.
func Name(s string) Fill { return checked(nameGrammar.MatchString(s), s) }

// Words is a verb or slice name: lowercase words, one space between them.
func Words(s string) Fill { return checked(len(s) <= 64 && wordsGrammar.MatchString(s), s) }

// Description is a team's description, checked by A's grammar for one.
func Description(s string) Fill { return checked(proto.ValidDescription(s), s) }

// Team is a parsed team id.
func Team(t paths.TeamID) Fill { return Fill{t.String()} }

// Count is a number.
func Count(n int64) Fill { return Fill{strconv.FormatInt(n, 10)} }

const slot = "{}"

// New is the error for code, whose exit is its set's; it panics on an unknown code or a fill
// count that differs from the template's slots, both mistakes in the caller's code.
func New(code string, team *paths.TeamID, t Template, fills ...Fill) *Error {
	exit, ok := ExitOf(code)
	if !ok {
		panic("exits: unknown code " + code)
	}
	return &Error{Exit: exit, Code: code, Team: team, Message: Format(t, fills...).text}
}

// Format is a template filled as one fill, for a part of a message such as one choice in a
// list; it panics as New does on a fill count that differs from the slots.
func Format(t Template, fills ...Fill) Fill {
	parts := strings.Split(string(t), slot)
	if len(parts) != len(fills)+1 {
		panic(fmt.Sprintf("exits: template %q has %d slots, given %d fills", t, len(parts)-1, len(fills)))
	}
	var b strings.Builder
	for i, p := range parts {
		b.WriteString(p)
		if i < len(fills) {
			b.WriteString(fills[i].text)
		}
	}
	return Fill{b.String()}
}

// Join is fills as one, sep between each.
func Join(sep Template, fills ...Fill) Fill {
	texts := make([]string, len(fills))
	for i, f := range fills {
		texts[i] = f.text
	}
	return Fill{strings.Join(texts, string(sep))}
}

// internalMessage is the error object's message for a failure no verb named.
const internalMessage Template = "teamchat failed inside; this is a bug: tell a human"

// Write writes err to stderr and is its exit: nothing for nil; an *Error as its object; any
// other error as one diagnostic line, its text with controls shown as spaces, then an
// `internal` object. The object is always the last line. A failed write keeps the exit.
func Write(stderr io.Writer, err error) term.ExitCode {
	if err == nil {
		return term.ExitOK
	}
	e, ok := errors.AsType[*Error](err)
	if !ok {
		diag := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, err.Error())
		e = New(CodeInternal, nil, internalMessage)
		if _, werr := io.WriteString(stderr, "teamchat: "+diag+"\n"); werr != nil {
			return e.Exit
		}
	}
	line := append(object(e).Compact(), '\n')
	if _, werr := stderr.Write(line); werr != nil {
		return e.Exit
	}
	return e.Exit
}

func object(e *Error) proto.Value {
	team := proto.Null()
	if e.Team != nil {
		team = proto.Str(e.Team.String())
	}
	return proto.Obj(
		proto.M(keyKind, proto.Str(kindError)),
		proto.M(keyV, proto.Int(1)),
		proto.M(keyExit, proto.Int(int64(e.Exit))),
		proto.M(keyCode, proto.Str(e.Code)),
		proto.M(keyTeam, team),
		proto.M(keyMessage, proto.Str(e.Message)),
	)
}

const (
	kindError  = "error"
	keyKind    = "kind"
	keyV       = "v"
	keyExit    = "exit"
	keyCode    = "code"
	keyTeam    = "team"
	keyMessage = "message"
)

var errObject = errors.New("not a teamchat error object")

// Decode reads one error object line strictly (SPEC-n3 TB2.4): exactly its six keys, kind
// `error`, v 1, an exit of the table and a code of that exit's set, a team id or null, a string
// message. A refusal quotes nothing of the line.
func Decode(line []byte) (Error, error) {
	v, err := proto.Parse(line)
	if err != nil || v.Kind() != proto.KindObject || len(v.Members()) != 6 {
		return Error{}, errObject
	}
	kind, okKind := member(v, keyKind, proto.KindString)
	ver, okV := member(v, keyV, proto.KindInt)
	exit, okExit := member(v, keyExit, proto.KindInt)
	code, okCode := member(v, keyCode, proto.KindString)
	msg, okMsg := member(v, keyMessage, proto.KindString)
	if !okKind || kind.Str() != kindError || !okV || ver.Int() != 1 || !okExit || !okCode || !okMsg {
		return Error{}, errObject
	}
	if want, ok := ExitOf(code.Str()); !ok || int64(want) != exit.Int() {
		return Error{}, fmt.Errorf("%w: an exit and code outside the table", errObject)
	}
	e := Error{Exit: term.ExitCode(exit.Int()), Code: code.Str(), Message: msg.Str()}
	if err := e.decodeTeam(v); err != nil {
		return Error{}, err
	}
	return e, nil
}

func member(v proto.Value, key string, kind proto.Kind) (proto.Value, bool) {
	m, ok := v.Get(key)
	return m, ok && m.Kind() == kind
}

func (e *Error) decodeTeam(v proto.Value) error {
	team, ok := v.Get(keyTeam)
	switch {
	case ok && team.Kind() == proto.KindNull:
		return nil
	case ok && team.Kind() == proto.KindString:
		id, err := paths.ParseTeamID(team.Str())
		if err != nil {
			return fmt.Errorf("%w: a team that is not a team id", errObject)
		}
		e.Team = &id
		return nil
	}
	return errObject
}
