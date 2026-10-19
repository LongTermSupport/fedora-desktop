// Package ui asks the person running a tool: gum for questions (confirm, input, choose,
// styled text) and fzf for the bordered picker every ccy tool shares. Both run through an
// execx.Runner, so a flow is tested against a Fake with the exact argv the real tools get.
package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// ErrCancelled is the person leaving a question (Ctrl-C, or Esc where gum treats it so).
var ErrCancelled = errors.New("cancelled")

// ErrLeave is the person leaving a picker: Esc, q, or the Exit row.
var ErrLeave = errors.New("left the picker")

// Accent is the one colour for what is active: a 0-15 terminal index, so the terminal's
// theme decides its shade (amber, 3).
const Accent = "3"

// gumEnv is the environment every gum call gets: the terminal's own text colour ("")
// everywhere, the accent for what is active, and gum's fixed dim-grey key-help line
// hidden (gum 0.17 offers no flag to recolour it).
var gumEnv = []string{
	"GUM_INPUT_HEADER_FOREGROUND=", "GUM_INPUT_PROMPT_FOREGROUND=" + Accent,
	"GUM_INPUT_PLACEHOLDER_FOREGROUND=", "GUM_INPUT_CURSOR_FOREGROUND=" + Accent,
	"GUM_CONFIRM_PROMPT_FOREGROUND=", "GUM_CONFIRM_SELECTED_FOREGROUND=0",
	"GUM_CONFIRM_SELECTED_BACKGROUND=" + Accent, "GUM_CONFIRM_UNSELECTED_FOREGROUND=",
	"GUM_CONFIRM_UNSELECTED_BACKGROUND=",
	"GUM_INPUT_SHOW_HELP=false", "GUM_CONFIRM_SHOW_HELP=false", "GUM_CHOOSE_SHOW_HELP=false",
	"GUM_SPIN_TITLE_FOREGROUND=", "GUM_SPIN_SPINNER_FOREGROUND=" + Accent,
	"GUM_CHOOSE_HEADER_FOREGROUND=", "GUM_CHOOSE_ITEM_FOREGROUND=",
	"GUM_CHOOSE_CURSOR_FOREGROUND=" + Accent, "GUM_CHOOSE_SELECTED_FOREGROUND=" + Accent,
}

// Gum asks with gum. Its prompts draw on the terminal through stdin and stderr; only the
// answer comes back on stdout.
type Gum struct {
	Runner execx.Runner
	Stdin  io.Reader
	Stderr io.Writer
	// Plain draws the heading without colour: NO_COLOR, or not a terminal.
	Plain bool
}

// exitCode is a command's non-zero exit, or -1 when the error is not an exit at all.
func exitCode(err error) int {
	if exit, ok := errors.AsType[*execx.ExitError](err); ok {
		return exit.Code
	}
	return -1
}

// Confirm is a yes/no under the heading h: gum exits 0 for yes and 1 for no (Esc is a no in
// gum confirm); any other exit (130 on Ctrl-C) cancels.
func (g *Gum) Confirm(ctx context.Context, h Heading, question string, defaultYes bool) (bool, error) {
	if err := h.check(); err != nil {
		return false, err
	}
	_, err := g.run(ctx, "confirm", fmt.Sprintf("--default=%t", defaultYes), h.above(question, !g.Plain, g.headingWidth(ctx)))
	switch code := exitCode(err); {
	case err == nil:
		return true, nil
	case code == 1:
		return false, nil
	case code > 0:
		return false, ErrCancelled
	default:
		return false, fmt.Errorf("gum confirm: %w", err)
	}
}

// Input is one line of text under the heading h, trimmed. gum 0.17 exits 1 on Esc and 130 on
// Ctrl-C; both cancel.
func (g *Gum) Input(ctx context.Context, h Heading, header, placeholder, value string) (string, error) {
	if err := h.check(); err != nil {
		return "", err
	}
	res, err := g.run(ctx, "input", "--header", h.above(header, !g.Plain, g.headingWidth(ctx)), "--placeholder", placeholder, "--value", value, "--width", "0")
	if err != nil {
		return "", cancelOrFail("gum input", err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// Choose is one of options under the heading h, starting on selected.
func (g *Gum) Choose(ctx context.Context, h Heading, header, selected string, options []string) (string, error) {
	if err := h.check(); err != nil {
		return "", err
	}
	args := append([]string{"choose", "--header", h.above(header, !g.Plain, g.headingWidth(ctx)), "--selected", selected}, options...)
	res, err := g.run(ctx, args...)
	if err != nil {
		return "", cancelOrFail("gum choose", err)
	}
	choice := strings.TrimSpace(string(res.Stdout))
	if choice == "" {
		return "", ErrCancelled
	}
	return choice, nil
}

// Style prints text wrapped to width with a left indent, on the terminal.
func (g *Gum) Style(ctx context.Context, text string, width, indent int) error {
	_, err := g.Runner.Run(ctx, execx.Cmd{
		Name: "gum",
		Args: []string{"style", "--width", strconv.Itoa(width), "--padding", "0 0 0 " + strconv.Itoa(indent), text},
		Env:  gumEnv, Stdout: g.Stderr, Stderr: g.Stderr,
	})
	if err != nil {
		return fmt.Errorf("gum style: %w", err)
	}
	return nil
}

func (g *Gum) run(ctx context.Context, args ...string) (execx.Result, error) {
	return g.Runner.Run(ctx, execx.Cmd{
		Name: "gum", Args: args, Env: gumEnv, Stdin: g.Stdin, Stderr: g.Stderr,
	})
}

func cancelOrFail(what string, err error) error {
	if exitCode(err) > 0 {
		return ErrCancelled
	}
	return fmt.Errorf("%s: %w", what, err)
}
