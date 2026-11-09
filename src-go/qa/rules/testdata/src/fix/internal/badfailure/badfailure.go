// Package badfailure runs commands and keeps only whether they failed, not why.
package badfailure

import (
	"errors"
	"fix/internal/execx"
	"os"
)

// ask is the originating instance (Plan 00191, 2026-10-29): any failure of curl became the
// status 000, printed as "could not reach the API" and cached, the exit code and stderr lost.
// The refusal it returns is carried for that one sentinel only, and the later err is another
// assignment, so neither carries the run's other failures.
func ask(r execx.Runner, c execx.Cmd) (string, error) { // want ask:"returns a command's failure"
	res, err := r.Run(c) // want `a command's failure is only tested, never carried`
	code := "000"
	switch {
	case errors.Is(err, execx.ErrSecretInArgv):
		return "", err
	case err == nil:
		code = string(res.Stdout)
	}
	raw, err := os.ReadFile("/x")
	if err != nil {
		return "", err
	}
	return code + string(raw), nil
}

// tested only in its own if.
func tested(c execx.Cmd) string {
	if err := execx.Probe(c); err != nil { // want `a command's failure is only tested, never carried`
		return "failed"
	}
	return "ok"
}

// unread is assigned and never looked at before it is replaced.
func unread(r execx.Runner, c execx.Cmd) string {
	res, err := r.Run(c) // want `a command's failure is only tested, never carried`
	err = errors.New("replaced")
	return string(res.Stdout) + err.Error()
}

// sentinelOnly carries the failure only when it is the refusal, in an if.
func sentinelOnly(c execx.Cmd) error { // want sentinelOnly:"returns a command's failure"
	err := execx.Probe(c) // want `a command's failure is only tested, never carried`
	if errors.Is(err, execx.ErrSecretInArgv) {
		return err
	}
	return nil
}

// statement drops the whole result.
func statement(c execx.Cmd) {
	execx.Probe(c) // want `a command's failure is only tested, never carried`
}
