package client

import (
	"bytes"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// flagToCoordinator adds the coordinator to a send's addressees.
const flagToCoordinator = "to-coordinator"

const (
	msgTerminal exits.Template = "teamchat {} reads its text on stdin, which is a terminal; give the text on stdin, such as: echo text | teamchat {}"
	msgTextSize exits.Template = "the text on stdin is larger than 4096 bytes"
	msgEmpty    exits.Template = "the text on stdin is empty"
	msgUTF8     exits.Template = "the text on stdin is not UTF-8"
)

// sendArgs are a send verb's args and its text: say and a reply without --verb read stdin,
// ping and reply --verb never do, so an inherited stdin cannot block them (n3d-D15). The text's
// rules run here, before anything is sent; every other rule of a send is the link's.
func sendArgs(e cli.Env, inv cli.Invocation, op string) (any, *string, error) {
	switch op {
	case wire.OpSay:
		text, err := readText(e, inv)
		return wire.SayArgs{Team: inv.Team, To: addressees(inv), ToCoordinator: inv.Has(flagToCoordinator)}, text, err
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
	text, err := readText(e, inv)
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
func readText(e cli.Env, inv cli.Invocation) (*string, error) {
	if term.FileIsTerminal(e.Stdin) {
		return nil, exits.New(exits.CodeUsage, inv.Team, msgTerminal, exits.Words(inv.Verb.Name), exits.Words(inv.Verb.Name))
	}
	data, err := io.ReadAll(io.LimitReader(e.Stdin, proto.MaxText+2))
	if err != nil {
		return nil, exits.New(exits.CodeInternal, inv.Team, msgInternal, exits.Words("read its stdin"))
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
