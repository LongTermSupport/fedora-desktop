// Package badrefusal reads every declare.py refusal as one verdict on the answer just typed.
package badrefusal

import (
	"errors"
	"fmt"

	"fix/internal/declare"
)

// checked is ccy-teams lxc-add's original: any refusal is "Try again", so a host_vars entry
// that clashes with the answer, or a file that does not read, is asked again three times.
func checked(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	de, ok := errors.AsType[*declare.Error](err) // want `a \*declare.Error is read without its kind`
	if !ok {
		return false, err
	}
	fmt.Printf("  %s. Try again.\n", de.Error())
	return false, nil
}

// isRefusal is the create wizard's: a refusal of any kind is taken for a malformed value.
func isRefusal(err error) bool {
	var refusal *declare.Error
	return errors.As(err, &refusal) // want `a \*declare.Error is read without its kind`
}

// noDefault reads a refusal of the file as a refusal of the login name.
func noDefault(login string) string {
	if err := declare.Check(login); err != nil {
		if de, ok := err.(*declare.Error); ok { // want `a \*declare.Error is read without its kind`
			fmt.Printf("no default (%s)\n", de.Message())
			return ""
		}
	}
	return login
}

// switched is the same through a type switch.
func switched(err error) bool {
	switch err.(type) {
	case *declare.Error: // want `a \*declare.Error is read without its kind`
		return false
	}
	return true
}

// inALiteral is the checker written inline, as the questions pass it.
func inALiteral() func(string) (bool, error) {
	return func(answer string) (bool, error) {
		if _, ok := errors.AsType[*declare.Error](declare.Check(answer)); ok { // want `a \*declare.Error is read without its kind`
			return false, nil
		}
		return true, nil
	}
}

// cancelsThenReasks reads one kind, the interrupt, and still asks again on every other one, a
// refusal of the file included.
func cancelsThenReasks(err error) (bool, error) {
	if errors.Is(err, declare.ErrInterrupted) {
		return false, err
	}
	if de, ok := errors.AsType[*declare.Error](err); ok { // want `a \*declare.Error is read without its kind`
		fmt.Printf("  %s. Try again.\n", de.Error())
		return false, nil
	}
	return false, err
}

// printsCodeThenReasks reads the code only to say it, and asks again whatever it is.
func printsCodeThenReasks(err error) (bool, error) {
	if de, ok := errors.AsType[*declare.Error](err); ok { // want `a \*declare.Error is read without its kind`
		fmt.Printf("  exit %d: %s. Try again.\n", de.Code, de.Error())
		return false, nil
	}
	return false, err
}

// stopCode ends a run with an exit code, as the flows' own stop does.
type stopCode struct{ code int }

func (s *stopCode) Error() string { return "stopped" }

// stopsOnOneKindThenReasks hands the code on for an interrupt only, and asks again on every
// other kind, a refusal of the file included.
func stopsOnOneKindThenReasks(err error) (bool, error) {
	de, ok := errors.AsType[*declare.Error](err) // want `a \*declare.Error is read without its kind`
	if !ok {
		return false, err
	}
	if errors.Is(err, declare.ErrInterrupted) {
		return false, &stopCode{code: de.Code}
	}
	fmt.Printf("  %s. Try again.\n", de.Error())
	return false, nil
}

// asksTheWrongKind asks whether the file refused, so a crash of declare.py is asked again too.
func asksTheWrongKind(err error) bool {
	var de *declare.Error
	return errors.As(err, &de) && !de.Is(declare.ErrRefused) // want `a \*declare.Error is read without its kind`
}

// isUsageOfAnother reads a kind, but of a different error than the one it recovered.
func isUsageOfAnother(err, other error) bool {
	if _, ok := errors.AsType[*declare.Error](err); ok { // want `a \*declare.Error is read without its kind`
		return errors.Is(other, errors.ErrUnsupported)
	}
	return false
}
