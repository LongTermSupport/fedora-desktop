// Package declare is the shape of src-go/internal/declare that project_refusal_kind judges:
// declare.py's refusal, whose exit code is its kind.
package declare

import "errors"

// The kinds of refusal.
var (
	ErrUsage       = errors.New("declare: usage")
	ErrRefused     = errors.New("declare: refused")
	ErrInterrupted = errors.New("declare: cancelled")
	ErrFailed      = errors.New("declare: failed")
)

// Error is a declare.py run that exited non-zero.
type Error struct {
	Code   int
	Stderr string
}

func (e *Error) Error() string { return e.Stderr }

// Message is the line without its prefix.
func (e *Error) Message() string { return e.Stderr }

// Is matches the kind of the exit code.
func (e *Error) Is(target error) bool {
	switch e.Code {
	case 64:
		return target == ErrUsage
	case 78:
		return target == ErrRefused
	}
	return target == ErrFailed
}

// Check is a check of a typed value, which may also judge it among the file's entries.
func Check(string) error { return nil }
