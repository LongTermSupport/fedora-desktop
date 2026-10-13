package teams

import (
	"context"
	_ "embed"
)

//go:embed help.txt
var helpText string

// showHelp explains the table and the keys, then waits for the list to be read before it is
// drawn over the text.
func (a *App) showHelp(ctx context.Context) {
	a.print(a.Stderr, "\n")
	a.Term.Say("ccy-teams: the agent teams on this machine")
	a.print(a.Stderr, helpText)
	a.backToList(ctx)
}
