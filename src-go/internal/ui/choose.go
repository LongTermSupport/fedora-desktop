package ui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// ChooseSpec is a plain fzf screen in the legacy style of the freeze tools: no border and no
// Exit row, just a height, a prompt and a header. Prompt carries its own trailing space.
type ChooseSpec struct {
	Prompt string
	Header string
	Height string
	Multi  bool
	Rows   []string
}

// Choose shows the rows under the heading h (drawn plain: this screen has no --ansi) and returns the lines chosen (more than one only when Multi). Esc,
// Ctrl-C and "nothing matched" are ErrLeave; any other fzf failure is an error, never a leave.
// An fzf that is not installed is ErrFzfMissing, so a caller can fall back to a numbered prompt.
func (p *Picker) Choose(ctx context.Context, h Heading, s ChooseSpec) ([]string, error) {
	if err := h.check(); err != nil {
		return nil, err
	}
	var args []string
	if s.Multi {
		args = append(args, "--multi")
	}
	args = append(args, "--height="+s.Height, "--reverse", "--prompt="+s.Prompt, "--header="+h.above(s.Header, false, p.headingWidth(ctx, plainMargin)))
	var stdin io.Reader = strings.NewReader(strings.Join(s.Rows, "\n") + "\n")
	res, err := p.Runner.Run(ctx, execx.Cmd{Name: "fzf", Args: args, Stdin: stdin, Stderr: p.Stderr})
	if err != nil {
		switch code := exitCode(err); {
		case code == fzfNoMatch, code == fzfInterrupted:
			return nil, ErrLeave
		case execx.IsNotInstalled(err):
			return nil, ErrFzfMissing
		}
		return nil, fmt.Errorf("fzf: %w", err)
	}
	out := strings.TrimSuffix(string(res.Stdout), "\n")
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
