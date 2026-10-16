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

// restIndent starts the line under a row of a narrow table, with the columns moved off it.
const restIndent = "    "

// printTable writes the heading and the rows to stdout, laid out together: coloured on a
// terminal, otherwise plain with a blank line under the heading. On a terminal too narrow for
// every column, each row is followed by the columns moved off it, indented.
func (c *Collector) printTable(s *Snapshot) error {
	width := 0
	if c.Term.IsTerminal(c.Stdout) {
		width = c.Columns(c.Stdout)
	}
	table := Layout(s.Rows, width)
	colour := c.Term.ColourOn(c.Stdout)
	var b strings.Builder
	if colour {
		b.WriteString(term.Bold + table.Heading() + term.Reset + "\n")
	} else {
		b.WriteString(table.Heading() + "\n\n")
	}
	for _, r := range s.Rows {
		if colour {
			b.WriteString(table.Painted(r) + "\n")
		} else {
			b.WriteString(table.Text(r) + "\n")
		}
		if rest := table.Rest(r); rest != "" {
			b.WriteString(restIndent + rest + "\n")
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
