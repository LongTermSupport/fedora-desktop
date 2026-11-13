// Package ccc is the tee form: a child's output, which holds a token, sent both to the
// terminal and to a buffer the token is read out of.
package ccc

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"

	"fix/internal/term"
)

// extractToken is a secret source by the rule's table.
func extractToken(output string) (string, bool) {
	line, found := strings.CutPrefix(output, "token: ")
	return line, found
}

type app struct {
	stderr io.Writer
	screen *term.SecretScreen
	run    func(stdout io.Writer) error
}

// signIn tees the child's output to the terminal: the token stays in the scrollback.
func (a *app) signIn() (string, error) { // want signIn:"returns a secret from ccc.extractToken"
	var out bytes.Buffer
	if err := a.run(io.MultiWriter(a.stderr, &out)); err != nil { // want `output holding a secret from ccc.extractToken teed to the terminal`
		return "", err
	}
	token, ok := extractToken(out.String())
	if !ok {
		return "", errors.New("no token")
	}
	return token, nil
}

// signInToStdout is the same tee to os.Stdout.
func (a *app) signInToStdout() (string, error) { // want signInToStdout:"returns a secret from ccc.extractToken"
	var out bytes.Buffer
	if err := a.run(io.MultiWriter(os.Stdout, &out)); err != nil { // want `output holding a secret from ccc.extractToken teed to the terminal`
		return "", err
	}
	token, ok := extractToken(out.String())
	if !ok {
		return "", errors.New("no token")
	}
	return token, nil
}

// signInOnTheScreen is the fix: the child's output goes to the secret screen.
func (a *app) signInOnTheScreen() (string, error) { // want signInOnTheScreen:"returns a secret from ccc.extractToken"
	var out bytes.Buffer
	if err := a.run(io.MultiWriter(a.screen, &out)); err != nil {
		return "", err
	}
	token, ok := extractToken(out.String())
	if !ok {
		return "", errors.New("no token")
	}
	return token, nil
}

// logged tees a child whose output nothing reads a secret from.
func (a *app) logged() (string, error) {
	var out bytes.Buffer
	err := a.run(io.MultiWriter(a.stderr, &out))
	return out.String(), err
}
