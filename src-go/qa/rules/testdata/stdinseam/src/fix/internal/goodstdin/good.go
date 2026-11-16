// Package goodstdin passes stdin along under its own name, asks about it, and reads it only
// through the shared reader or the ui seam.
package goodstdin

import (
	"io"
	"os"
	"os/exec"

	"fix/internal/execx"
	"fix/internal/stdinread"
	"fix/internal/term"
	"fix/internal/ui"
)

// Env is a command's streams.
type Env struct {
	Stdin      io.Reader
	Stdout     io.Writer
	IsTerminal func(any) bool
	Request    struct{ Stdin *string }
}

func Run(args []string, stdin io.Reader) int {
	e := Env{Stdin: stdin}
	if e.Stdin == nil || e.IsTerminal(e.Stdin) || term.FileIsTerminal(os.Stdin) {
		return 2
	}
	data, err := stdinread.Read(e.Stdin, stdinread.Options{Prog: "tool"})
	if err != nil || len(data) == 0 || len(args) == 0 {
		return 1
	}
	return 0
}

func main() int { return Run(os.Args, os.Stdin) }

func Child(e Env) error {
	cmd := exec.Command("true")
	cmd.Stdin = e.Stdin
	if err := execx.Run(execx.Cmd{Name: "true", Stdin: e.Stdin}, nopCloser{}); err != nil {
		return err
	}
	return cmd.Run()
}

func Ask(e Env) (string, error) {
	l := &ui.Lines{In: e.Stdin, Out: e.Stdout}
	return l.Ask()
}

// A request's Stdin is a string, not a reader: nothing reads it.
func Request(e Env) int {
	if e.Request.Stdin == nil {
		return 0
	}
	return len(*e.Request.Stdin)
}

// Fd and Name ask about the file without reading it.
func Fd() (uintptr, string) { return os.Stdin.Fd(), os.Stdin.Name() }

type nopCloser struct{}

func (nopCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopCloser) Close() error                { return nil }
