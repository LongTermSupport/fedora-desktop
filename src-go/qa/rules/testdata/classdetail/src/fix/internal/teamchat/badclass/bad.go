// Package badclass prints a failed call's class where its detail is available.
package badclass

import (
	"errors"
	"fmt"
	"log"
	"os"

	"fix/internal/teamchat/matrix"
	"fix/internal/teamchat/status"
)

type team struct{ log *log.Logger }

// The defect of fedora-desktop#265: the down line names the class alone.
func (t *team) backoff(err error) {
	t.log.Printf("down: %s", matrix.Class(err)) // want `a failed call's class printed without its detail`
}

func formatted(err error) string {
	return fmt.Sprintf("stopped: %s", matrix.Class(err)) // want `a failed call's class printed without its detail`
}

func concatenated(err error) error {
	return errors.New("the notice: " + matrix.Class(err)) // want `a failed call's class printed without its detail`
}

func toStderr(err error) {
	fmt.Fprintln(os.Stderr, matrix.Class(err)) // want `a failed call's class printed without its detail`
}

// A helper that hands the class back is a class-only line wherever it is printed.
func describe(err error) string {
	if e, ok := errors.AsType[*matrix.Error](err); ok {
		return matrix.Class(e) // want `a failed call's class printed without its detail`
	}
	return err.Error()
}

// The function taken as a value is the class all the same.
func asValue() func(error) string {
	return matrix.Class // want `a failed call's class printed without its detail`
}

// A record of a type classRecords does not list is a line a person reads.
func otherRecord(err error) status.Other {
	return status.Other{What: matrix.Class(err)} // want `a failed call's class printed without its detail`
}

// A comparison is not concatenation: the record exemption does not reach through it.
func compared(err error) status.Problem {
	return status.Problem{What: fmt.Sprint(matrix.Class(err) == "x")} // want `a failed call's class printed without its detail`
}

// The bounded record carries the class: its decoder refuses more.
func problem(err error) status.Problem {
	return status.Problem{What: "the view: " + matrix.Class(err), Fix: "health"}
}

func positionalProblem(err error) status.Problem {
	return status.Problem{"the view: " + matrix.Class(err), "health"}
}

// The cause is the fix, and a method named Class is another function.
func good(t *team, err error) {
	t.log.Printf("down: %s", matrix.Cause(err))
	var e *matrix.Error
	if errors.As(err, &e) {
		t.log.Printf("%s", e.Class())
	}
}
