// Package element reads a teamchat-owned document on stdin (the play writes it to teamchat's
// own spec), so its strict parse is the protocol's.
package element

import (
	"io"
	"os"

	"fix/internal/stdinread"
	"fix/internal/teamchat/proto"
)

func start(stdin io.Reader) (proto.Value, error) {
	data, err := stdinread.Read(stdin, stdinread.Options{Prog: "element"})
	if err != nil {
		return proto.Value{}, err
	}
	return proto.Parse(data)
}

// fromEnvironment is judged here as anywhere: only the package's stdin is its protocol's.
func fromEnvironment() (proto.Value, error) {
	return proto.Parse([]byte(os.Getenv("TEAMS"))) // want `an environment variable parsed by teamchat's strict wire parser`
}
