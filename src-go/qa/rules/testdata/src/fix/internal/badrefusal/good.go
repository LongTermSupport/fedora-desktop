package badrefusal

import (
	"errors"
	"fmt"

	"fix/internal/declare"
	"fix/internal/term"
)

// translatesItsCode hands the kind on whole through the tools' translation of a child's code.
func translatesItsCode(err error) term.ExitCode {
	if de, ok := errors.AsType[*declare.Error](err); ok {
		return term.FromChild(de.Code)
	}
	return term.ExitFailure
}

// translatedStop hands it on translated inside the value that ends the run.
type translatedStop struct{ code term.ExitCode }

func (s *translatedStop) Error() string { return "stopped" }

func stopsWithTranslatedCode(err error) error {
	if de, ok := errors.AsType[*declare.Error](err); ok {
		return &translatedStop{term.FromChild(de.Code)}
	}
	return err
}

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

// guardsThenExits recovers in a negated if and hands the code on after it.
func guardsThenExits(err error) int {
	var de *declare.Error
	if !errors.As(err, &de) {
		return 1
	}
	return de.Code
}

// asInTheInit recovers with errors.As in the if's Init and tests its result alone.
func asInTheInit(err error) int {
	var de *declare.Error
	if ok := errors.As(err, &de); ok {
		return de.Code
	}
	return 1
}
