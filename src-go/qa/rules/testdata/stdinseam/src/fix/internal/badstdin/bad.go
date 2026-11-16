// Package badstdin reads stdin by every route the rule knows, outside the shared reader.
package badstdin

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
)

// Env is a command's streams.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
}

// The issue #260 shape: a verb's text read whole from the env's stdin, which an open pipe holds
// for ever.
func ReadText(e Env, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(e.Stdin, limit)) // want `io.LimitReader reads stdin outside internal/stdinread`
}

func ReadAllStdin() ([]byte, error) {
	return io.ReadAll(os.Stdin) // want `io.ReadAll reads stdin outside internal/stdinread`
}

func Prompt(stdin io.Reader) (string, error) {
	return bufio.NewReader(stdin).ReadString('\n') // want `bufio.NewReader reads stdin outside internal/stdinread`
}

func Scan(e *Env) bool {
	return bufio.NewScanner(e.Stdin).Scan() // want `bufio.NewScanner reads stdin outside internal/stdinread`
}

func Decode(e Env, v any) error {
	return json.NewDecoder(e.Stdin).Decode(v) // want `json.NewDecoder reads stdin outside internal/stdinread`
}

func CopyOut(e Env) error {
	_, err := io.Copy(e.Stdout, e.Stdin) // want `io.Copy reads stdin outside internal/stdinread`
	return err
}

func Full(stdin io.Reader, p []byte) error {
	_, err := io.ReadFull(stdin, p) // want `io.ReadFull reads stdin outside internal/stdinread`
	return err
}

func Direct(p []byte) (int, error) {
	return os.Stdin.Read(p) // want `Read reads stdin outside internal/stdinread`
}

func Tee(e Env) io.Reader {
	return io.TeeReader(e.Stdin, e.Stdout) // want `io.TeeReader reads stdin outside internal/stdinread`
}

func Many(e Env) io.Reader {
	return io.MultiReader(e.Stdin) // want `io.MultiReader reads stdin outside internal/stdinread`
}

// Place reads what it is handed: the reader's own name hides that it is stdin.
func Place(r io.Reader) ([]byte, error) { return io.ReadAll(r) }

func Handed(e Env) ([]byte, error) {
	return Place(e.Stdin) // want `stdin handed to Place as its r, a reader of another name`
}

func Renamed(e Env) ([]byte, error) {
	in := e.Stdin // want `stdin held as in, a reader of another name`
	return io.ReadAll(in)
}

// Lines is not the ui seam's: its In is read here.
type Lines struct{ In io.Reader }

func NewLines(e Env) *Lines {
	return &Lines{In: e.Stdin} // want `stdin held as In, a reader of another name`
}

func Reassigned(l *Lines) {
	l.In = os.Stdin // want `stdin held as In, a reader of another name`
}
