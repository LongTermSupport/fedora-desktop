package teams

import (
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// verbSeatConversation is `ccy-teams seats conversation <list>`.
const verbSeatConversation = "conversation"

const seatConversationUsage = "ccy-teams seats conversation <seat>@<team>[,<seat>@<team>...] [--set <id>]"

// SeatConversation is `ccy-teams seats conversation <seat>@<team>[,...]` (issues 409 and 417): the
// conversation the seats of the working directory's checkout resume (conversation.ForSeats), alone
// on stdout. Every refusal is on stderr, exit 1, with the two ways on. With `--set <id>` it is the
// repair: the seats' records point at id, whose transcript must be in this checkout, and the next
// plain `ccy --teams <list>` resumes it.
type SeatConversation struct {
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Getwd          func() (string, error)
	NowMS          func() int64
}

// Run is the command over its arguments (after `seats conversation`).
func (c *SeatConversation) Run(args []string) term.ExitCode {
	const prefix = "ccy-teams seats conversation: "
	set := ""
	if list, value, found := cutSet(args); found && registry.SessionID(value) {
		args, set = list, value
	}
	if len(args) != 1 {
		return c.say(term.ExitUsage, prefix+"takes one list, and --set with a Claude session id: "+seatConversationUsage+". See ccy-teams --help.")
	}
	wd, err := c.Getwd()
	if err != nil {
		return c.say(term.ExitFailure, prefix+"the working directory: "+err.Error())
	}
	if set != "" {
		return c.set(wd, args[0], set)
	}
	got, err := conversation.ForSeats(wd, args[0], conversation.Here(c.Getenv))
	if err != nil {
		return c.say(term.ExitFailure, prefix+err.Error()+"\n"+conversation.WaysOn(args[0]))
	}
	if _, err := io.WriteString(c.Stdout, got.ID+"\n"); err != nil {
		return c.say(term.ExitFailure, prefix+"could not print the id: "+err.Error())
	}
	return term.ExitOK
}

// cutSet is args as `<list> --set <value>`: the list alone, and the value.
func cutSet(args []string) ([]string, string, bool) {
	if len(args) != 3 || args[1] != "--set" {
		return args, "", false
	}
	return args[:1], args[2], true
}

// set is --set: the seats' records point at id, once its transcript is found here.
func (c *SeatConversation) set(wd, list, id string) term.ExitCode {
	const prefix = "ccy-teams seats conversation --set: "
	path, err := conversation.TranscriptOf(wd, id)
	if err != nil {
		return c.say(term.ExitFailure, prefix+err.Error())
	}
	if err := conversation.RecordSeats(wd, list, id, conversation.SourceSet, c.NowMS()); err != nil {
		return c.say(term.ExitFailure, prefix+err.Error())
	}
	if _, err := io.WriteString(c.Stdout, id+"\n"); err != nil {
		return c.say(term.ExitFailure, prefix+"could not print the id: "+err.Error())
	}
	return c.say(term.ExitOK, prefix+"the seats "+list+" now resume "+id+" ("+path+"): ccy --teams "+list)
}

// say prints msg on stderr and gives code; a stderr that cannot be written is a plain failure.
func (c *SeatConversation) say(code term.ExitCode, msg string) term.ExitCode {
	if _, err := io.WriteString(c.Stderr, msg+"\n"); err != nil {
		return term.ExitFailure
	}
	return code
}
