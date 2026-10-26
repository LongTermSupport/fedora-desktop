// Package setuprclone is the setup-rclone command (Plan 00192 SPEC-setup-rclone.md, E1-E23).
package setuprclone

import (
	"context"
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const playPath = "playbooks/imports/optional/common/play-rclone.yml"

const noHostsPhrase = "skipping: no hosts matched"

// Env is everything a run touches outside its own memory and the files it reads and writes.
type Env struct {
	Runner           execx.Runner
	Foreground       execx.Foregrounder
	Getenv           func(string) string
	LookPath         func(string) (string, error)
	InContainer      func() bool
	ReleaseFile      string
	Stdin            io.Reader
	Stdout, Stderr   io.Writer
	StderrIsTerminal bool
}

// NewEnv is the production environment.
func NewEnv() Env { return Env{} }

// App is one setup-rclone invocation.
type App struct{ Env Env }

// Run is the whole command; it returns the exit code.
func (a App) Run(_ context.Context, _ []string) term.ExitCode { return term.ExitOK }

type watch struct {
	w      io.Writer
	phrase string
}

func (s *watch) Write(p []byte) (int, error) { return len(p), nil }
func (s *watch) matched() bool               { return false }
