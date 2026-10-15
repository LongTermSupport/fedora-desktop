package bash

import (
	"context"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// syntaxExit is the status bash -n gives a script that does not parse.
const syntaxExit = 2

// syntax runs bash -n on each file and returns the message of every file that does not parse. The
// message names the file as it was given, relative to the root; a script bash could not open or
// run is not a failing script but a run that could not complete.
func (g *gate) syntax(ctx context.Context, files []string) (map[string]string, error) {
	failed := map[string]string{}
	for _, rel := range files {
		res, err := g.runTool(ctx, syntaxTimeout, proc.Cmd{Argv: []string{toolBash, "-n", "--", rel}, Merge: true})
		if err != nil {
			return nil, fmt.Errorf("bash -n %s: %w", rel, err)
		}
		switch res.Code {
		case 0:
		case syntaxExit:
			msg := strings.TrimSpace(res.Stdout)
			if msg == "" {
				msg = "syntax error"
			}
			failed[rel] = msg
		default:
			return nil, fmt.Errorf("bash -n %s exited %d, which is not a syntax error: %s", rel, res.Code, strings.TrimSpace(res.Stdout))
		}
	}
	return failed, nil
}
