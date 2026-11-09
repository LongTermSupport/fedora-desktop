package badfailure

import (
	"errors"
	"fmt"
	"fix/internal/execx"
)

// returned carries the failure to the caller.
func returned(r execx.Runner, c execx.Cmd) error { // want returned:"returns a command's failure"
	_, err := r.Run(c)
	if err != nil {
		return err
	}
	return nil
}

// wrapped names it in the message.
func wrapped(c execx.Cmd) error { // want wrapped:"returns a command's failure"
	if err := execx.Probe(c); err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	return nil
}

// extracted reads the exit code out of it.
func extracted(r execx.Runner, c execx.Cmd) int {
	_, err := r.Run(c)
	var exit *execx.ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return 0
}

// worded keeps its text.
func worded(r execx.Runner, c execx.Cmd) string {
	if _, err := r.Run(c); err != nil {
		return "failed: " + err.Error()
	}
	return ""
}

// kept stores it for a later report.
func kept(r execx.Runner, c execx.Cmd, errs []error) []error {
	_, err := r.Run(c)
	return append(errs, err)
}

// sentinelThenCarried handles the refusal, and carries every other failure too.
func sentinelThenCarried(c execx.Cmd) error { // want sentinelThenCarried:"returns a command's failure"
	err := execx.Probe(c)
	if errors.Is(err, execx.ErrSecretInArgv) {
		return nil
	}
	return err
}

// reassignedInABranch may replace the failure, but returns whichever it holds.
func reassignedInABranch(r execx.Runner, c execx.Cmd) error { // want reassignedInABranch:"returns a command's failure"
	res, err := r.Run(c)
	if err == nil && res.ExitCode != 0 {
		err = fmt.Errorf("status %d", res.ExitCode)
	}
	return err
}

// eitherBranch runs one of two commands and carries whichever failed.
func eitherBranch(r execx.Runner, c execx.Cmd, first bool) error { // want eitherBranch:"returns a command's failure"
	var err error
	if first {
		_, err = r.Run(c)
	} else {
		_, err = r.Run(c)
	}
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	return nil
}

// joinedIntoItself carries the failure into its own next value: the right of an assignment is
// read before v is replaced.
func joinedIntoItself(r execx.Runner, c execx.Cmd, release func() error) error { // want joinedIntoItself:"returns a command's failure"
	_, err := r.Run(c)
	if err = errors.Join(err, release()); err != nil {
		return err
	}
	return nil
}

// noCommand's error is not a command's failure: NewName takes no command.
func noCommand() string {
	name, err := execx.NewName("x")
	if err != nil {
		return ""
	}
	return name
}
