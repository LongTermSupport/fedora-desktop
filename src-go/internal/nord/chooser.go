package nord

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// headingWidth is what a heading wraps to when the terminal does not say its width (ui.Lines
// draws a zero Width two columns wide).
const headingWidth = 80

const (
	keyCancel     = "q"
	keySwitch     = "1"
	keyDisconnect = "2"
	needTerminal  = "the chooser needs an interactive terminal; see 'nord --help' for the commands."
)

// chooser is E22: a server to connect to, or, when connected, switch, disconnect or cancel.
func (r *run) chooser(ctx context.Context) term.ExitCode {
	if !r.env.IsTerminal(r.env.Stdin) {
		r.failf(needTerminal)
		return term.ExitFailure
	}
	tty, err := r.env.OpenTTY()
	if err != nil {
		r.failf(needTerminal)
		return term.ExitFailure
	}
	code := r.choose(ctx, r.lines(tty))
	if cerr := tty.Close(); cerr != nil {
		r.failf("close the terminal: %v", cerr)
		return term.ExitFailure
	}
	return code
}

func (r *run) lines(tty io.Reader) *ui.Lines {
	width := headingWidth
	if n := r.env.Columns(); n > 0 {
		width = n
	}
	colour := r.env.IsTerminal(r.env.Stderr) && r.env.Getenv("NO_COLOR") == ""
	return &ui.Lines{In: tty, Out: r.env.Stderr, Colour: colour, Width: width}
}

func (r *run) choose(ctx context.Context, lines *ui.Lines) term.ExitCode {
	creds, ok := r.check(ctx, needConnect)
	if !ok {
		return term.ExitFailure
	}
	all, err := r.configs()
	if err != nil {
		r.failf("%v.", err)
		return term.ExitFailure
	}
	if len(all) == 0 {
		r.failf("no VPN configs in %s/.", r.abbrev(r.path(configsRel)))
		r.say("")
		r.noConfigsHint(r.say)
		return term.ExitFailure
	}
	up, ok := r.active(ctx)
	if !ok {
		return term.ExitFailure
	}
	if len(up) == 0 {
		return r.pickServer(ctx, lines, ui.Here("nord", "Connect").About(plural(len(all), "server")), all, nil, creds)
	}
	connected := make([]string, len(up))
	for i, p := range up {
		connected[i] = p.name
	}
	about := "connected to " + strings.Join(connected, ", ")
	key, err := lines.Choose(ctx, ui.Here("nord").About(about), "Choose [1, 2, q]", []ui.Choice{
		{Key: keySwitch, Label: "Switch to another server"},
		{Key: keyDisconnect, Label: "Disconnect"},
		{Key: keyCancel, Label: "Cancel"},
	})
	if err != nil {
		return r.unanswered(ctx, err)
	}
	switch key {
	case keySwitch:
		return r.pickServer(ctx, lines, ui.Here("nord", "Switch").About(about), all, up, creds)
	case keyDisconnect:
		return r.bringDown(ctx, up)
	}
	r.say("Nothing changed.")
	return term.ExitOK
}

// pickServer is the numbered server menu, each connected one marked; a server chosen is
// connected to (E13-E15).
func (r *run) pickServer(ctx context.Context, lines *ui.Lines, h ui.Heading, all []config, up []nmProfile,
	creds credentials,
) term.ExitCode {
	choices := make([]ui.Choice, 0, len(all)+1)
	for i, c := range all {
		label := c.name
		if slices.ContainsFunc(up, func(p nmProfile) bool { return p.name == c.name }) {
			label = c.name + " (connected)"
		}
		choices = append(choices, ui.Choice{Key: strconv.Itoa(i + 1), Label: label})
	}
	choices = append(choices, ui.Choice{Key: keyCancel, Label: "Cancel"})
	keys := "1"
	if len(all) > 1 {
		keys = fmt.Sprintf("1-%d", len(all))
	}
	key, err := lines.Choose(ctx, h, "Choose a server ["+keys+", q]", choices)
	if err != nil {
		return r.unanswered(ctx, err)
	}
	if key == keyCancel {
		r.say("Nothing changed.")
		return term.ExitOK
	}
	n, err := strconv.Atoi(key)
	if err != nil || n < 1 || n > len(all) {
		r.failf("the menu returned a key it does not have: %q", key)
		return term.ExitFailure
	}
	return r.connectTo(ctx, all[n-1], creds)
}

// unanswered is a menu that ended without a choice (E22, E24).
func (r *run) unanswered(ctx context.Context, err error) term.ExitCode {
	var gaveUp *ui.GaveUpError
	switch {
	case ctx.Err() != nil:
		return term.ExitCancelled
	case errors.Is(err, ui.ErrInputEnded):
		r.say("The input ended before a choice; nothing changed.")
		return term.ExitCancelled
	case errors.As(err, &gaveUp):
		r.failf("no valid choice after %d tries; nothing changed.", gaveUp.Tries)
	default:
		r.failf("%v", err)
	}
	return term.ExitFailure
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
