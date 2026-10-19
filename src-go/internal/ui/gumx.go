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

// Spin runs argv behind a spinner titled title and returns its failure as the
// *execx.ExitError of the spun command (gum spin exits with it).
func (g *Gum) Spin(ctx context.Context, title string, argv ...string) error {
	args := append([]string{"spin", "--title", title, "--"}, argv...)
	if _, err := g.run(ctx, args...); err != nil {
		return fmt.Errorf("gum spin: %w", err)
	}
	return nil
}

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
		Name: gumBin, Args: args, Env: gumEnv, Stdout: g.Stderr, Stderr: g.Stderr,
	})
	if err != nil {
		return fmt.Errorf("gum style: %w", err)
	}
	return nil
}

// Explain prints text wrapped to the terminal, indent columns in. A header gum prints is
// never wrapped, so the longer explanation before a question goes through here. The
// width is only layout: when the terminal's cannot be read (no terminal, tput missing) a common
// one serves.
func (g *Gum) Explain(ctx context.Context, text string, indent int) error {
	return g.Style(ctx, text, g.columns(ctx)-explainMargin, indent)
}

func (g *Gum) columns(ctx context.Context) int { return columns(ctx, g.Runner, g.Stdin, g.Stderr) }

// columns is the terminal's width, the one reader of it: tput cols through the runner, which
// finds the terminal on stdin or stderr. A width that cannot be read is fallbackColumns.
func columns(ctx context.Context, r execx.Runner, stdin io.Reader, stderr io.Writer) int {
	res, err := r.Run(ctx, execx.Cmd{Name: "tput", Args: []string{"cols"}, Stdin: stdin, Stderr: stderr})
	if err != nil {
		return fallbackColumns
	}
	cols, err := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
	if err != nil || cols <= 0 {
		return fallbackColumns
	}
	return cols
}
