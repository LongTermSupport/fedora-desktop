// Package wrapper runs a command for its callers and hands its failure back.
package wrapper

import (
	"fmt"
	"fix/internal/execx"
)

// State returns the run's error: a caller holds a command's failure.
func State(r execx.Runner, name string) (string, error) { // want State:"returns a command's failure"
	res, err := r.Run(execx.Cmd{Name: "systemctl", Args: []string{"is-active", name}})
	return string(res.Stdout), err
}

// Units wraps State's failure, so it is a command's failure too.
func Units(r execx.Runner) ([]string, error) { // want Units:"returns a command's failure"
	st, err := State(r, "x")
	if err != nil {
		return nil, fmt.Errorf("units: %w", err)
	}
	return []string{st}, nil
}

// Label runs nothing; its error is not a command's failure.
func Label(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("no name")
	}
	return name, nil
}
