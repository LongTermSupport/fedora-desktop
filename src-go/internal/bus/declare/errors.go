package declare

import (
	"errors"
	"fmt"
)

// Kind is the exit class of a declare.py run that did not answer.
type Kind int

// The exit classes: declare.py's UsageError (64) and DeclareError (78), and its uncaught
// exception, a Python traceback (1). The crash class is where v1 failed on a value no v1
// writer produces; the reader refuses there with a message instead (SPEC-view.md B-S3).
const (
	KindCrash   Kind = 1
	KindUsage   Kind = 64
	KindRefused Kind = 78
)

// Sentinels an *Error matches under errors.Is: its kind, and ErrVaulted for v1's VaultedError.
var (
	ErrCrash   = errors.New("declare: unreadable")
	ErrUsage   = errors.New("declare: usage mistake")
	ErrRefused = errors.New("declare: refused")
	ErrVaulted = errors.New("declare: vaulted")
)

// Error is a refusal: its exit class and declare.py's message, without the "declare: "
// prefix v1 printed before it. Vaulted marks v1's VaultedError, a refusal (78) that the
// description and settings fields tell apart from the others.
type Error struct {
	Kind    Kind
	Vaulted bool
	Message string
}

// Error implements error: the message alone.
func (e *Error) Error() string { return e.Message }

// Code is the exit code declare.py gave for this refusal.
func (e *Error) Code() int { return int(e.Kind) }

// Is matches the sentinel of the error's kind, and ErrVaulted for a vaulted value.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrVaulted:
		return e.Vaulted
	case ErrCrash:
		return e.Kind == KindCrash
	case ErrUsage:
		return e.Kind == KindUsage
	case ErrRefused:
		return e.Kind == KindRefused
	}
	return false
}

func usagef(format string, a ...any) error {
	return &Error{Kind: KindUsage, Message: fmt.Sprintf(format, a...)}
}

func refusedf(format string, a ...any) error {
	return &Error{Kind: KindRefused, Message: fmt.Sprintf(format, a...)}
}

func vaultedf(format string, a ...any) error {
	return &Error{Kind: KindRefused, Vaulted: true, Message: fmt.Sprintf(format, a...)}
}

func crashf(format string, a ...any) error {
	return &Error{Kind: KindCrash, Message: fmt.Sprintf(format, a...)}
}

// asError is err as an *Error.
func asError(err error) (*Error, bool) {
	return errors.AsType[*Error](err)
}

// isRefusal is whether err is a refusal (78), a vaulted value included: what v1 caught as
// DeclareError where one entry must not end a listing.
func isRefusal(err error) bool {
	e, ok := asError(err)
	return ok && e.Kind == KindRefused
}

// isVaulted is whether err is v1's VaultedError.
func isVaulted(err error) bool {
	e, ok := asError(err)
	return ok && e.Vaulted
}
