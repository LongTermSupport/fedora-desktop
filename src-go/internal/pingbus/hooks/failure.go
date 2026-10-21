package hooks

import (
	"errors"
	"fmt"
	"slices"
	"syscall"
)

// The failure classes, worst first (H9, hooks.py FAILURE_CLASSES without relay: HB2).
const (
	classInternal  = "internal"
	classConfig    = "config"
	classState     = "state"
	classUntrusted = "untrusted"
	classInject    = "inject"
	classMissing   = "missing"
)

var failureClasses = []string{classInternal, classConfig, classState, classUntrusted, classInject, classMissing}

// worst is the worst class among found ("" is none), or "" when none is a class.
func worst(found ...string) string {
	for _, class := range failureClasses {
		if slices.Contains(found, class) {
			return class
		}
	}
	return ""
}

// The Python exception classes the hooks catch by name. Each keeps the message v1 would carry;
// the wording may differ (HB12), the class may not.
type (
	// configError is config.ConfigError (and the UsageError of a team selection).
	configError struct{ msg string }
	// stateError is inbox.StateError: a state file that is unusable.
	stateError struct{ msg string }
	// untrustedError is protocol.Untrusted: a team record that is not trusted.
	untrustedError struct{ msg string }
	// membershipError is membership.MembershipError: a refused membership file.
	membershipError struct{ msg string }
	// internalError is an exception no hook catches by name: an `internal` failure (H11) that
	// names its kind.
	internalError struct{ kind string }
)

func (e *configError) Error() string     { return e.msg }
func (e *stateError) Error() string      { return e.msg }
func (e *untrustedError) Error() string  { return e.msg }
func (e *membershipError) Error() string { return e.msg }
func (e *internalError) Error() string   { return e.kind }

func configErr(format string, args ...any) error {
	return &configError{fmt.Sprintf(format, args...)}
}

func stateErr(format string, args ...any) error {
	return &stateError{fmt.Sprintf(format, args...)}
}

func untrustedErr(format string, args ...any) error {
	return &untrustedError{fmt.Sprintf(format, args...)}
}

func membershipErr(format string, args ...any) error {
	return &membershipError{fmt.Sprintf(format, args...)}
}

func internalErr(kind string) error { return &internalError{kind} }

// strerror is Python's `exc.strerror or exc`: an OS error's errno text, else the error's text.
func strerror(err error) string {
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		return errno.Error()
	}
	return err.Error()
}
