package teams

import (
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// verbSeatConversation is `ccy-teams seats conversation <list>`.
const verbSeatConversation = "conversation"

// flagSet is `ccy-teams seats conversation <list> --set <id>`.
const flagSet = "--set"

// SeatConversation is `ccy-teams seats conversation <seat>@<team>[,...] [--set <id>]` (issues
// 409, 417): the conversation the seats of the working directory's checkout agree on
// (conversation.ForSeats), alone on stdout, which a plain `ccy --teams <list>` resumes; with
// --set, the repair: every seat named points at that conversation (conversation.SetSeats), and
// the id is printed. Every refusal is on stderr, exit 1; malformed arguments exit 64.
type SeatConversation struct {
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Getwd          func() (string, error)
	NowMS          func() int64
}

// Run is the command over its arguments (after `seats conversation`).
func (c *SeatConversation) Run(args []string) term.ExitCode {
	const prefix = "ccy-teams seats conversation: "
	list, id := "", ""
	for i, a := range args {
		switch {
		case i == 0:
			list = a
		case i == 1 && a == flagSet && len(args) == 3:
		case i == 2 && len(args) == 3:
			id = a
		default:
			list = ""
		}
	}
	set := len(args) == 3
	if list == "" || set && id == "" {
		return say(c.Stderr, term.ExitUsage, prefix+"takes one list, and --set <id> to repair it: "+
			"ccy-teams seats conversation <seat>@<team>[,<seat>@<team>...] [--set <id>]. See ccy-teams --help.")
	}
	wd, err := c.Getwd()
	if err != nil {
		return say(c.Stderr, term.ExitFailure, prefix+"the working directory: "+err.Error())
	}
	where := conversation.Here(c.Getenv)
	if set {
		if err := conversation.SetSeats(wd, list, id, where, c.NowMS()); err != nil {
			return say(c.Stderr, term.ExitFailure, prefix+err.Error())
		}
		if code := say(c.Stderr, term.ExitOK, prefix+list+" now resume "+id+": a plain ccy --teams "+list+" resumes it."); code != term.ExitOK {
			return code
		}
	} else {
		got, err := conversation.ForSeats(wd, list, where)
		if err != nil {
			return say(c.Stderr, term.ExitFailure, prefix+err.Error())
		}
		id = got.ID
	}
	if _, err := io.WriteString(c.Stdout, id+"\n"); err != nil {
		return say(c.Stderr, term.ExitFailure, prefix+"could not print the id: "+err.Error())
	}
	return term.ExitOK
}

// say prints msg on stderr and gives code; a stderr that cannot be written is a plain failure.
func say(stderr io.Writer, code term.ExitCode, msg string) term.ExitCode {
	if _, err := io.WriteString(stderr, msg+"\n"); err != nil {
		return term.ExitFailure
	}
	return code
}
