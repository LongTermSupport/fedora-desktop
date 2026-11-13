// Package login is the defect: a password issued by agent-bus written to the terminal on the
// normal screen, where it stays in the scrollback after the flow has moved on.
package login

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"fix/internal/agentbus"
	"fix/internal/teams/flow"
)

// Login is the Element and password flows.
type Login struct {
	Kit    *flow.Kit
	Bus    *agentbus.Client
	copied string // want copied:"holds a secret from agentbus.Client.HumanPassword"
}

// IssuePassword is the defect's shape: the password reaches showLogin, which prints it.
func (l *Login) IssuePassword(ctx context.Context, team, name string) error { // want IssuePassword:"writes parameters 1 2"
	password, err := l.Bus.HumanPassword(ctx, team, name)
	if err != nil {
		return err
	}
	l.showLogin(team, name, password) // want `secret from agentbus.Client.HumanPassword written to the terminal through login.Login.showLogin`
	return nil
}

// showLogin bolds the password and prints it beside the other sign-in details.
func (l *Login) showLogin(team, name, password string) { // want showLogin:"writes parameters 0 1 2"
	shown := "\x1b[1m" + password + "\x1b[0m"
	l.Kit.Out.Print(l.Kit.Stderr, fmt.Sprintf("  %-11s %s\n", "Username", name+team))
	l.Kit.Out.Print(l.Kit.Stderr, fmt.Sprintf("  %-11s %s\n", "Password", shown))
}

// told says the password through the kit's Tell, a write the rule knows by its fact.
func (l *Login) told(ctx context.Context) error {
	password, err := l.Bus.HumanPassword(ctx, "alpha", "owner")
	if err != nil {
		return err
	}
	l.Kit.Tell("Your password is " + password) // want `secret from agentbus.Client.HumanPassword written to the terminal through flow.Kit.Tell`
	return nil
}

// direct writes the password with fmt and os, to stderr and stdout.
func (l *Login) direct(ctx context.Context) error {
	password, err := l.Bus.HumanPassword(ctx, "alpha", "owner")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "password: %s\n", password) // want `secret from agentbus.Client.HumanPassword written to the terminal through fmt.Fprintf`
	fmt.Println(password)                              // want `secret from agentbus.Client.HumanPassword written to the terminal through fmt.Println`
	_, err = os.Stdout.WriteString(password + "\n")    // want `secret from agentbus.Client.HumanPassword written to the terminal through os.File.WriteString`
	return err
}

// inError carries the password in an error that is then reported.
func (l *Login) inError(ctx context.Context) error {
	password, err := l.Bus.HumanPassword(ctx, "alpha", "owner")
	if err != nil {
		return err
	}
	l.Kit.ReportErr(errors.New("refused " + password)) // want `secret from agentbus.Client.HumanPassword written to the terminal through flow.Kit.ReportErr`
	return nil
}

// stored keeps the password in a field, which a later method prints.
func (l *Login) stored(ctx context.Context) error {
	password, err := l.Bus.HumanPassword(ctx, "alpha", "owner")
	l.copied = password
	return err
}

func (l *Login) later() {
	l.Kit.Tell("still on the clipboard: " + l.copied) // want `secret from agentbus.Client.HumanPassword written to the terminal through flow.Kit.Tell`
}

// buffered builds the line in a buffer and writes the buffer.
func (l *Login) buffered(ctx context.Context) error {
	password, err := l.Bus.HumanPassword(ctx, "alpha", "owner")
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "password %s\n", password)
	_, err = l.Kit.Stderr.Write(buf.Bytes()) // want `secret from agentbus.Client.HumanPassword written to the terminal through io.Writer.Write`
	return err
}

// issued is a wrapper whose result is the secret: a caller printing it is caught too.
func (l *Login) issued(ctx context.Context) (string, error) { // want issued:"returns a secret from agentbus.Client.HumanPassword"
	return l.Bus.HumanPassword(ctx, "alpha", "owner")
}

func (l *Login) wrapped(ctx context.Context) error {
	password, err := l.issued(ctx)
	l.Kit.Tell(password) // want `secret from agentbus.Client.HumanPassword written to the terminal through flow.Kit.Tell`
	return err
}

// revealed is a secret type's Reveal, written out.
func (l *Login) revealed(tok Token) {
	l.Kit.Tell("token " + tok.Reveal()) // want `secret from login.Token.Reveal written to the terminal through flow.Kit.Tell`
}

// Token is a secret type: its value leaves it only through Reveal.
type Token struct{ s string }

// Reveal is the token's value.
func (t Token) Reveal() string { return t.s }
