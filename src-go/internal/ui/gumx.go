package ui

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The terminal width assumed when it cannot be read.
const (
	fallbackColumns = 80
	// explainMargin is what an explanation leaves free at the right edge.
	explainMargin = 4

	gumBin = "gum"
)

// Box draws lines in a rounded border on the terminal, the first of them bold when bold
// is set.
func (g *Gum) Box(ctx context.Context, bold bool, lines ...string) error {
	args := []string{"style"}
	if bold {
		args = append(args, "--bold")
	}
	args = append(args, "--border", "rounded", "--padding", "0 1")
	args = append(args, lines...)
	_, err := g.Runner.Run(ctx, execx.Cmd{
		Name: gumBin, Args: args, Env: gumEnv, Stdout: g.Stderr, Stderr: g.Stderr, EchoHeld: true,
	})
	if err != nil {
		return fmt.Errorf("gum style: %w", err)
	}
	return nil
}

// Explain prints text wrapped to the terminal, indent columns in. A header gum prints is
// never wrapped, so the longer explanation before a question goes through here. The
// width is only layout: when tput runs and cannot say (no terminal) a common one serves.
func (g *Gum) Explain(ctx context.Context, text string, indent int) error {
	cols, err := g.columns(ctx)
	if err != nil {
		return err
	}
	return g.Style(ctx, text, cols-explainMargin, indent)
}

func (g *Gum) columns(ctx context.Context) (int, error) {
	return columns(ctx, g.Runner, g.Stdin, g.Stderr)
}

// columns is the terminal's width, the one reader of it: tput cols through the runner, which
// finds the terminal on stdin or stderr. A tput that ran and failed (no terminal), or said no
// width, is fallbackColumns; one that could not run (missing, killed) is the error.
func columns(ctx context.Context, r execx.Runner, stdin io.Reader, stderr io.Writer) (int, error) {
	res, err := r.Run(ctx, execx.Cmd{Name: "tput", Args: []string{"cols"}, Stdin: stdin, Stderr: stderr, EchoHeld: true})
	if err != nil {
		if execx.ExitedNonZero(err) {
			return fallbackColumns, nil
		}
		return 0, fmt.Errorf("the terminal's width: %w", err)
	}
	if cols, convErr := strconv.Atoi(strings.TrimSpace(string(res.Stdout))); convErr == nil && cols > 0 {
		return cols, nil
	}
	return fallbackColumns, nil
}
