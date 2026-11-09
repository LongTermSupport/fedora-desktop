package execx

import "errors"

// Result is what a command left.
type Result struct {
	Stdout   []byte
	ExitCode int
}

// ExitError is a command that ran and exited non-zero.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return "exit" }

// ErrSecretInArgv is a command refused before it started.
var ErrSecretInArgv = errors.New("secret in argv")

// Runner runs a command.
type Runner interface {
	Run(c Cmd) (Result, error)
}

// Probe runs a command and says only whether it ran.
func Probe(c Cmd) error { return Run(c) }

// NewName takes no command, so its error is not a command's failure.
func NewName(s string) (string, error) { return s, nil }
