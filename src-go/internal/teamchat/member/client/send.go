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
	msgTextSize exits.Template = "the text on stdin is larger than 4096 bytes"
	msgEmpty    exits.Template = "the text on stdin is empty"
	msgUTF8     exits.Template = "the text on stdin is not UTF-8"
	msgNoInput  exits.Template = "teamchat {} read no text: {}; give the text on stdin, such as: echo text | teamchat {}"
)

// sendArgs are a send verb's args and its text: say and a reply without --verb read stdin,
// ping and reply --verb never do, so an inherited stdin cannot block them (n3d-D15). The text's
// rules run here, before anything is sent; every other rule of a send is the link's.
func sendArgs(e cli.Env, inv cli.Invocation, op string, clk stdinread.Clock) (any, *string, error) {
	switch op {
	case wire.OpSay:
		text, err := readText(e, inv, clk)
		a := wire.SayArgs{Team: inv.Team, To: addressees(inv), ToCoordinator: inv.Has(flagToCoordinator), WaitContact: inv.Has(flagWaitContact)}
		return a, text, err
	case wire.OpReport:
		text, err := readReport(e, inv, clk)
		return wire.ReportArgs{Team: inv.Team}, text, err
	case wire.OpPing:
		a := wire.PingArgs{
			Team: inv.Team, Verb: inv.Args[0], To: addressees(inv), ToCoordinator: inv.Has(flagToCoordinator),
			Ref: optional(inv.Args, 1), Branch: optionalFlag(inv, "branch"),
		}
		return a, nil, nil
	}
	a := wire.ReplyArgs{
		Team: inv.Team, ID: inv.Args[0], Verb: optionalFlag(inv, "verb"), Ref: optionalFlag(inv, "ref"),
		ToCoordinator: inv.Has(flagToCoordinator),
	}
	if a.Verb != nil {
		return a, nil, nil
	}
	text, err := readText(e, inv, clk)
	return a, text, err
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
	data, err := readStdin(e, inv, clk, proto.MaxText+2)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimSuffix(data, []byte("\n"))
	switch {
	case len(data) > proto.MaxText:
		return nil, exits.New(exits.CodeSize, inv.Team, msgTextSize)
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

// readReport is a report's text by n3f-D1's rule with the cap proto.MaxReportBody: at most cap + 2
// bytes are read, and one trailing newline is removed unless the read reached cap + 2 bytes,
// which is over the cap whatever its end is, so it is sent whole. It refuses no size and no
// empty text: RP3's role check (11) comes before its size check (10 body), and only the link
// knows the role. Text that is not UTF-8 cannot travel in the request's JSON, so it is 10
// schema, as say's.
func readReport(e cli.Env, inv cli.Invocation, clk stdinread.Clock) (*string, error) {
	data, err := readStdin(e, inv, clk, proto.MaxReportBody+2)
	if err != nil {
		return nil, err
	}
	if len(data) < proto.MaxReportBody+2 {
		data = bytes.TrimSuffix(data, []byte("\n"))
	}
	if !utf8.Valid(data) {
		return nil, exits.New(exits.CodeSchema, inv.Team, msgUTF8)
	}
	text := string(data)
	return &text, nil
}
