package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// List is ccy-teams --list: the picker's table, with a heading, on stdout. A table with a part
// that could not be read is printed but is not a success; a homeserver that is down or not
// installed is a state, not a failure. It never runs sudo -v.
func (c *Collector) List(ctx context.Context) term.ExitCode {
	s, err := c.Collect(ctx)
	if err != nil {
		return term.ExitFailure
	}
	if len(s.Rows) == 0 {
		c.Out.Print(c.Stderr, "No agent teams on this machine yet. ccy-teams create starts one.\n")
		return term.ExitOK
	}
	if s.NeedSudo != "" {
		c.Out.Print(c.Stderr, "The members of"+s.NeedSudo+" are not shown: reading them needs root. "+
			"Run sudo -v first, or run play-agent-bus.yml once, which installs a rule that lets ccy-teams read them without a password.\n")
	}
	if err := c.printTable(s); err != nil {
		c.Term.PrintError("the table could not be written: " + err.Error())
		return term.ExitFailure
	}
	if s.Incomplete != "" || s.NeedSudo != "" || membersUnreadable(s) {
		return term.ExitFailure
	}
	return term.ExitOK
}

// printTable writes the heading and the rows to stdout: coloured on a terminal, otherwise
// plain with a blank line under the heading.
func (c *Collector) printTable(s *Snapshot) error {
	var b strings.Builder
	if c.Term.ColourOn(c.Stdout) {
		b.WriteString(term.Bold + HeadingText() + term.Reset + "\n")
		for _, r := range s.Rows {
			b.WriteString(r.Painted() + "\n")
		}
	} else {
		b.WriteString(HeadingText() + "\n\n")
		for _, r := range s.Rows {
			b.WriteString(r.Text() + "\n")
		}
	}
	if _, err := fmt.Fprint(c.Stdout, b.String()); err != nil {
		return fmt.Errorf("write table: %w", err)
	}
	return nil
}

// membersUnreadable is any team whose members need sudo or failed to read.
func membersUnreadable(s *Snapshot) bool {
	for _, why := range s.MembersUnknown {
		if why == WordNeedSudo || strings.HasPrefix(why, "read failed") {
			return true
		}
	}
	return false
}
