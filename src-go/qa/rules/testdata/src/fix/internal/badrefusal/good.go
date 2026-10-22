package badrefusal

import (
	"errors"
	"fmt"

	"fix/internal/declare"
)

// retriesOnlyUsage asks again for a malformed answer and stops on every other kind.
func retriesOnlyUsage(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	de, ok := errors.AsType[*declare.Error](err)
	if !ok || !errors.Is(err, declare.ErrUsage) {
		return false, err
	}
	fmt.Printf("  %s. Try again.\n", de.Error())
	return false, nil
}

// exitsWithItsCode hands the kind on whole as the exit code.
func exitsWithItsCode(err error) int {
	if de, ok := errors.AsType[*declare.Error](err); ok {
		return de.Code
	}
	return 1
}

// exitCode is an exit code type, as term.ExitCode is.
type exitCode int

// convertsItsCode hands the kind on whole, converted to the exit code's type.
func convertsItsCode(err error) exitCode {
	if de, ok := errors.AsType[*declare.Error](err); ok {
		return exitCode(de.Code)
	}
	return 1
}

// stop ends a run with an exit code, as the flows' own stop does.
type stop struct{ code exitCode }

func (s *stop) Error() string { return "stopped" }

// stopsWithItsCode ends the run, the code handed on inside the error it returns.
func stopsWithItsCode(err error) error {
	if de, ok := errors.AsType[*declare.Error](err); ok {
		fmt.Printf("%s\nRefused; nothing was changed.\n", de.Error())
		return &stop{code: exitCode(de.Code)}
	}
	return err
}

// asksItsKind asks the error itself whether the value is at fault.
func asksItsKind(err error) bool {
	var de *declare.Error
	return errors.As(err, &de) && de.Is(declare.ErrUsage)
}

// kindOnly never recovers the type: errors.Is is the kind already.
func kindOnly(err error) bool {
	return errors.Is(err, declare.ErrUsage)
}

// assignsThenExits recovers in an assignment and hands the code on as a later direct statement.
func assignsThenExits(err error) int {
	de, ok := errors.AsType[*declare.Error](err)
	if !ok {
		return 1
	}
	fmt.Printf("%s\n", de.Error())
	return de.Code
}

// exitsOnAnyCode hands the code on for every refusal that has one, as the chooser does.
func exitsOnAnyCode(err error) int {
	if de, ok := errors.AsType[*declare.Error](err); ok && de.Code != 0 {
		return de.Code
	}
	return 1
}

// switchedThenExits hands the code on from the case that names the type.
func switchedThenExits(err error) int {
	switch de := err.(type) {
	case *declare.Error:
		return de.Code
	}
	return 0
}
