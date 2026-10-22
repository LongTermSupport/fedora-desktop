package restore

import (
	"errors"
	"fmt"
)

// ErrRefused is a restore a rule said no to (exit 70).
var ErrRefused = errors.New("restore: refused")

// RefusedError is ErrRefused with what refused it.
type RefusedError struct{ Problem string }

func (e *RefusedError) Error() string { return e.Problem }

// Is makes a RefusedError ErrRefused.
func (e *RefusedError) Is(target error) bool { return target == ErrRefused }

func refusedf(format string, args ...any) error {
	return &RefusedError{Problem: fmt.Sprintf(format, args...)}
}
