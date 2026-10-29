package badfailure

import (
	"errors"
	"fix/internal/execx"
	"fix/internal/wrapper"
)

// up reads a systemctl that could not run as "not up": the failure is behind a wrapper.
func up(r execx.Runner) bool {
	st, err := wrapper.State(r, "hs") // want `a command's failure is only tested, never carried`
	return err == nil && st == "active"
}

// listed guesses the cause of a failure two wrappers deep.
func listed(r execx.Runner) string {
	if _, err := wrapper.Units(r); err != nil { // want `a command's failure is only tested, never carried`
		return "systemctl could not list the units"
	}
	return ""
}

// exitCodeOf reads the exit code out of a failure (errors.As extracts its cause).
func exitCodeOf(err error) int {
	var exit *execx.ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return -1
}

// failed hands the failure to a function that reads it, so it is carried: whether that function
// keeps enough of it is past what the rule can see (the catalogue says so).
func failed(r execx.Runner, c execx.Cmd) bool {
	_, err := r.Run(c)
	return exitCodeOf(err) > 0
}

// carriedThroughWrapper says the wrapper's failure.
func carriedThroughWrapper(r execx.Runner) string {
	if _, err := wrapper.Units(r); err != nil {
		return "units: " + err.Error()
	}
	return ""
}

// notACommand's error is no command's failure.
func notACommand() string {
	if _, err := wrapper.Label(""); err != nil {
		return "no label"
	}
	return ""
}
