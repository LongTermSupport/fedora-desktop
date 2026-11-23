package client

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// flagToCoordinator adds the coordinator to a send's addressees.
const (
	flagToCoordinator = "to-coordinator"
	flagWaitContact   = "wait-contact"
)

const (
	msgTerminal exits.Template = "teamchat {} reads its text on stdin, which is a terminal; give the text on stdin, such as: echo text | teamchat {}"
	msgEmpty    exits.Template = "the text on stdin is empty"
	msgUTF8     exits.Template = "the text on stdin is not UTF-8"
	msgNoInput  exits.Template = "teamchat {} read no text: {}; give the text on stdin, such as: echo text | teamchat {}"
)

// sendArgs are a send verb's args and its text: say and a reply without --verb read stdin,
// ping and reply --verb never do, so an inherited stdin cannot block them (n3d-D15). The text's
// rules run here, before anything is sent; every other rule of a send is the link's.
func sendArgs(e cli.Env, inv cli.Invocation, op string, clk stdinread.Clock) (any, readStdinText, error) {
	switch op {
	case wire.OpSay:
		text, err := readText(e, inv, clk)
		a := wire.SayArgs{Team: inv.Team, To: addressees(inv), ToCoordinator: inv.Has(flagToCoordinator), WaitContact: inv.Has(flagWaitContact)}
		return a, readStdinText{text: text}, err
	case wire.OpReport:
		text, err := readCapped(e, inv, clk, proto.MaxReportBody)
		return wire.ReportArgs{Team: inv.Team}, text, err
	case wire.OpPing:
		a := wire.PingArgs{
			Team: inv.Team, Verb: inv.Args[0], To: addressees(inv), ToCoordinator: inv.Has(flagToCoordinator),
			Ref: optional(inv.Args, 1), Branch: optionalFlag(inv, "branch"),
		}
		return a, readStdinText{}, nil
	}
	a := wire.ReplyArgs{
		Team: inv.Team, ID: inv.Args[0], Verb: optionalFlag(inv, "verb"), Ref: optionalFlag(inv, "ref"),
		ToCoordinator: inv.Has(flagToCoordinator),
	}
	if a.Verb != nil {
		return a, readStdinText{}, nil
	}
	text, err := readText(e, inv, clk)
	return a, readStdinText{text: text}, err
}

// readStdinText is a send's text as the request carries it, and the whole stdin's size when the
// text was cut at its read cap (0 otherwise), for the link's size refusal (#365).
type readStdinText struct {
	text  *string
	whole int64
}

// addressees are --to's names, none (an empty list, not null) without it.
func addressees(inv cli.Invocation) []string {
	to, given := inv.Flag("to")
	if !given {
		return []string{}
	}
	return strings.Split(to, ",")
}

// optionalFlag is the flag's value, nil when it is not given.
func optionalFlag(inv cli.Invocation, name string) *string {
	if v, given := inv.Flag(name); given {
		return &v
	}
	return nil
}

// optional is the i-th positional argument, nil when it is not given.
func optional(args []string, i int) *string {
	if i < len(args) {
		s := args[i]
		return &s
	}
	return nil
}

// readText is the text on stdin (n3d-D28): a terminal is refused before any read; then at most
// one byte past the cap and its newline is read, one trailing newline is removed, and the
// text must be 1 to proto.MaxText bytes of UTF-8.
func readText(e cli.Env, inv cli.Invocation, clk stdinread.Clock) (*string, error) {
	data, err := readStdin(e, inv, clk, exits.MaxMeasured+1)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimSuffix(data, []byte("\n"))
	switch {
	case len(data) > proto.MaxText:
		return nil, exits.TooLarge(exits.CodeSize, inv.Team, int64(len(data)), proto.MaxText)
	case len(data) == 0:
		return nil, exits.New(exits.CodeEmpty, inv.Team, msgEmpty)
	case !utf8.Valid(data):
		return nil, exits.New(exits.CodeSchema, inv.Team, msgUTF8)
	}
	text := string(data)
	return &text, nil
}

// readStdin is at most limit bytes of stdin; a terminal is refused before any read. It says on
// stderr once nothing has arrived for a second, and a stdin that stays silent for
// stdinread.PipedGiveUp (an open pipe nothing writes to) is 64 usage naming the bound.
func readStdin(e cli.Env, inv cli.Invocation, clk stdinread.Clock, limit int64) ([]byte, error) {
	verb := exits.Words(inv.Verb.Name)
	if term.FileIsTerminal(e.Stdin) {
		return nil, exits.New(exits.CodeUsage, inv.Team, msgTerminal, verb, verb)
	}
	data, err := stdinread.Read(e.Stdin, stdinread.Options{
		Prog: "teamchat " + inv.Verb.Name, Err: e.Stderr, Limit: limit, GiveUpAfter: stdinread.PipedGiveUp, Clock: clk,
	})
	if none, ok := errors.AsType[*stdinread.NoInputError](err); ok {
		return nil, exits.New(exits.CodeUsage, inv.Team, msgNoInput, verb, exits.Words(none.Error()), verb)
	}
	if err != nil {
		return nil, exits.New(exits.CodeInternal, inv.Team, msgInternal, exits.Words("read its stdin"))
	}
	return data, nil
}

// readCapped is a text by n3f-D1's rule with the cap given (a report's, a question's, a thread
// post's): one trailing newline is removed unless the text is cap + 2 bytes or more, which is
// over the cap whatever its end is, so it is sent whole up to cap + 2 bytes, with the whole
// stdin's size beside it when it was cut there (#365). It refuses no size and no empty text: the
// role check (11) comes before the size check (10 body), and only the link knows the role. Text
// that is not UTF-8 cannot travel in the request's JSON, so it is 10 schema, as say's.
func readCapped(e cli.Env, inv cli.Invocation, clk stdinread.Clock, limit int) (readStdinText, error) {
	data, err := readStdin(e, inv, clk, exits.MaxMeasured+1)
	if err != nil {
		return readStdinText{}, err
	}
	var whole int64
	switch {
	case len(data) > limit+2:
		whole, data = int64(len(data)), data[:limit+2]
	case len(data) < limit+2:
		data = bytes.TrimSuffix(data, []byte("\n"))
	}
	if !utf8.Valid(data) {
		return readStdinText{}, exits.New(exits.CodeSchema, inv.Team, msgUTF8)
	}
	text := string(data)
	return readStdinText{text: &text, whole: whole}, nil
}
