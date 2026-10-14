package picker

import (
	"context"
	_ "embed"
)

//go:embed help.txt
var helpText string

// showHelp explains the table and the keys, then waits for the list to be read before it is
// drawn over the text.
func (p *Picker) showHelp(ctx context.Context) {
	p.Kit.Out.Print(p.Kit.Stderr, "\n")
	p.Kit.Term.Say("ccy-teams: the agent teams on this machine")
	p.Kit.Out.Print(p.Kit.Stderr, helpText)
	p.Kit.BackToList(ctx)
}
