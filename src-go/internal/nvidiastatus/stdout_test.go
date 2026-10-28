package nvidiastatus

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os/exec"
	"testing"
	"testing/fstest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// runInto runs the report over a world whose every command fails to start, writing stdout to
// out; the report still renders, so a broken stdout is met.
func runInto(t *testing.T, out io.Writer) (term.ExitCode, string) {
	t.Helper()
	var errOut bytes.Buffer
	fake := &execx.Fake{}
	fake.OnFunc([]string{}, func(execx.Cmd) (execx.Result, error) { return execx.Result{}, errStart })
	env := Env{
		Runner:   fake,
		Getenv:   func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", exec.ErrNotFound },
		Stat:     func(string) (fs.FileInfo, error) { return nil, fs.ErrNotExist },
		Proc:     fstest.MapFS{},
		Stdout:   out,
		Stderr:   &errOut,
	}
	code := App{Env: env}.Run(context.Background(), nil)
	return code, errOut.String()
}
