// Package badforeign decodes input from outside teamchat's protocol with the strict wire parser.
package badforeign

import (
	"bytes"
	"io"
	"os"

	"fix/internal/stdinread"
	"fix/internal/teamchat/proto"
	"fix/internal/wrap"
)

// Env is what a command is given.
type Env struct {
	Stdin  io.Reader
	Getenv func(string) string
}

// payload is the defect: Claude Code's hook payload parsed whole by the wire parser.
func payload(e Env) (proto.Value, error) {
	data, err := stdinread.Read(e.Stdin, stdinread.Options{Prog: "hook"})
	if err != nil {
		return proto.Value{}, err
	}
	return proto.Parse(data) // want `stdin parsed by teamchat's strict wire parser`
}

// decode is a helper of the package that parses its argument strictly.
func decode(b []byte) (proto.Value, error) { return proto.Parse(b) } // want decode:"strictly parses parameters 0"

func throughHelper(e Env) (proto.Value, error) {
	data, err := stdinread.Read(e.Stdin, stdinread.Options{Prog: "hook"})
	if err != nil {
		return proto.Value{}, err
	}
	trimmed := bytes.TrimSpace(data)
	if v, perr := decode(trimmed); perr == nil { // want `stdin parsed by teamchat's strict wire parser`
		return v, nil
	}
	if v, perr := wrap.Decode(data); perr == nil { // want `stdin parsed by teamchat's strict wire parser`
		return v, nil
	}
	return proto.DecodeThing(data) // want `stdin parsed by teamchat's strict wire parser`
}

func aLine(e Env) (proto.Value, error) {
	line, rest, err := stdinread.ReadLine(e.Stdin, stdinread.Options{Prog: "hook"})
	if err != nil || rest == nil {
		return proto.Value{}, err
	}
	s := string(line)
	return proto.DecodeString(s) // want `stdin parsed by teamchat's strict wire parser`
}

func fromEnvironment(e Env) error {
	if _, err := proto.Parse([]byte(os.Getenv("PAYLOAD"))); err != nil { // want `an environment variable parsed by teamchat's strict wire parser`
		return err
	}
	raw, ok := os.LookupEnv("PAYLOAD")
	if !ok {
		return nil
	}
	if _, err := proto.Parse([]byte(raw)); err != nil { // want `an environment variable parsed by teamchat's strict wire parser`
		return err
	}
	var got string
	got = e.Getenv("PAYLOAD")
	_, err := proto.Parse([]byte(got)) // want `an environment variable parsed by teamchat's strict wire parser`
	return err
}

// readIn hands stdin back to its caller.
func readIn(e Env) []byte { // want readIn:"returns stdin"
	data, err := stdinread.Read(e.Stdin, stdinread.Options{Prog: "hook"})
	if err != nil {
		return nil
	}
	return data
}

func throughSource(e Env) (proto.Value, error) {
	in := readIn(e)
	if v, err := proto.Parse(in); err == nil { // want `stdin parsed by teamchat's strict wire parser`
		return v, nil
	}
	return proto.Parse(wrap.Reader{Stdin: e.Stdin}.Payload()) // want `stdin parsed by teamchat's strict wire parser`
}

func inAClosure(e Env) func() error {
	data := readIn(e)
	return func() error {
		_, err := proto.Parse(data) // want `stdin parsed by teamchat's strict wire parser`
		return err
	}
}
