package teams

import (
	"context"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// list is ccy-teams --list: the picker's table, with a heading, on stdout. A table with a part
// that could not be read is printed but is not a success; a homeserver that is down or not
// installed is a state, not a failure. It never runs sudo -v.
func (a *App) list(ctx context.Context) int {
	s, err := a.collect(ctx)
	if err != nil {
		return term.ExitFailure
	}
	if len(s.Rows) == 0 {
		a.print(a.Stderr, "No agent teams on this machine yet. ccy-teams create starts one.\n")
		return term.ExitOK
	}
	if s.NeedSudo != "" {
		a.print(a.Stderr, "The members of"+s.NeedSudo+" are not shown: reading them needs root. "+
			"Run sudo -v first, or run play-agent-bus.yml once, which installs a rule that lets ccy-teams read them without a password.\n")
	}
	if err := a.printTable(s); err != nil {
		a.Term.PrintError("the table could not be written: " + err.Error())
		return term.ExitFailure
	}
	if s.Incomplete != "" || s.NeedSudo != "" || membersUnreadable(s) {
		return term.ExitFailure
	}
	return term.ExitOK
}

// printTable writes the heading and the rows to stdout: coloured on a terminal, otherwise
// plain with a blank line under the heading.
func (a *App) printTable(s *Snapshot) error {
	var b strings.Builder
	if a.Term.ColourOn(a.Stdout) {
		b.WriteString(cBold + headingText() + cReset + "\n")
		for _, r := range s.Rows {
			b.WriteString(r.Painted() + "\n")
		}
	} else {
		b.WriteString(headingText() + "\n\n")
		for _, r := range s.Rows {
			b.WriteString(r.Text() + "\n")
		}
	}
	if _, err := fmt.Fprint(a.Stdout, b.String()); err != nil {
		return fmt.Errorf("write table: %w", err)
	}
	return nil
}

// membersUnreadable is any team whose members need sudo or failed to read.
func membersUnreadable(s *Snapshot) bool {
	for _, why := range s.MembersUnknown {
		if why == wordNeedSudo || strings.HasPrefix(why, "read failed") {
			return true
		}
	}
	return false
}
