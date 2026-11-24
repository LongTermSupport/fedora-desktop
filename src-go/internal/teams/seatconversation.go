package teams

import (
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// verbSeatConversation is `ccy-teams seats conversation <list>`.
const verbSeatConversation = "conversation"

// SeatConversation is `ccy-teams seats conversation <seat>@<team>[,...]` (issue 409): the
// conversation recorded by the session of the working directory's checkout that held those
// seats, alone on stdout, for `ccy --resume <id> --teams <list>`. The launcher's --continue
// refusal runs it. Every refusal (conversation.ForSeats') is on stderr, exit 1.
type SeatConversation struct {
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Getwd          func() (string, error)
}

// Run is the command over its arguments (after `seats conversation`).
func (c *SeatConversation) Run(args []string) term.ExitCode {
	const prefix = "ccy-teams seats conversation: "
	if len(args) != 1 {
		return c.say(term.ExitUsage, prefix+"takes one list: ccy-teams seats conversation <seat>@<team>[,<seat>@<team>...]. See ccy-teams --help.")
	}
	wd, err := c.Getwd()
	if err != nil {
		return c.say(term.ExitFailure, prefix+"the working directory: "+err.Error())
	}
	got, err := conversation.ForSeats(wd, args[0], conversation.Here(c.Getenv))
	if err != nil {
		return c.say(term.ExitFailure, prefix+err.Error())
	}
	if _, err := io.WriteString(c.Stdout, got.ID+"\n"); err != nil {
		return c.say(term.ExitFailure, prefix+"could not print the id: "+err.Error())
	}
	return term.ExitOK
}

// say prints msg on stderr and gives code; a stderr that cannot be written is a plain failure.
func (c *SeatConversation) say(code term.ExitCode, msg string) term.ExitCode {
	if _, err := io.WriteString(c.Stderr, msg+"\n"); err != nil {
		return term.ExitFailure
	}
	return code
}
