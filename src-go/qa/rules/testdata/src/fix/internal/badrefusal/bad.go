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

// isUsageOfAnother reads a kind, but of a different error than the one it recovered.
func isUsageOfAnother(err, other error) bool {
	if _, ok := errors.AsType[*declare.Error](err); ok { // want `a \*declare.Error is read without its kind`
		return errors.Is(other, errors.ErrUnsupported)
	}
	return false
}
