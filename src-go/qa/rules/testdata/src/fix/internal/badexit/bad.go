// Package badexit hands another program's exit code on as the tool's own.
package badexit

import (
	"errors"

	"fix/internal/term"
)

// CommandError is a wrapper that exited non-zero, as agentbus.CommandError is.
type CommandError struct {
	Tool string
	Code int
}

func (e *CommandError) Error() string { return e.Tool }

// ExitError is a value-receiver error with the code, as some adapters have.
type ExitError struct{ Code int }

func (e ExitError) Error() string { return "exited" }

// listFailed is ccy-teams choose-seat's original: agent-bus seat choose's 78 became ccy's
// exit, which the owner's prompt called "Unknown error code".
func listFailed(err error) term.ExitCode {
	if cmd, ok := errors.AsType[*CommandError](err); ok {
		return term.ExitCode(cmd.Code) // want `another program's exit code becomes this tool's own`
	}
	return term.ExitFailure
}

// stop carries a code to the end of a run, as the create wizard's does.
type stop struct{ code term.ExitCode }

func (s *stop) Error() string { return "stopped" }

func carried(err error) error {
	if cmd, ok := errors.AsType[*CommandError](err); ok {
		return &stop{term.ExitCode(cmd.Code)} // want `another program's exit code becomes this tool's own`
	}
	return err
}

// viaVariable reads the code into a local first.
func viaVariable(err error) term.ExitCode {
	var ee ExitError
	if !errors.As(err, &ee) {
		return term.ExitFailure
	}
	code := ee.Code
	return term.ExitCode(code) // want `another program's exit code becomes this tool's own`
}

// arithmetic hides the field in an expression.
func arithmetic(ee ExitError) term.ExitCode {
	return term.ExitCode(ee.Code + 0) // want `another program's exit code becomes this tool's own`
}

// aliased converts through an alias of the type.
type exit = term.ExitCode

func aliased(cmd *CommandError) exit {
	return exit(cmd.Code) // want `another program's exit code becomes this tool's own`
}

// inParens converts a parenthesised read.
func inParens(cmd *CommandError) term.ExitCode {
	return term.ExitCode((cmd.Code)) // want `another program's exit code becomes this tool's own`
}
