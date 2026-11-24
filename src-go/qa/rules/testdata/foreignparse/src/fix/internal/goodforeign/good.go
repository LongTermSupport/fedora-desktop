// Package goodforeign reads foreign input tolerantly, and its own protocol strictly.
package goodforeign

import (
	"io"
	"net"
	"os"

	"fix/internal/stdinread"
	"fix/internal/teamchat/proto"
)

// Env is what a command is given.
type Env struct {
	Stdin  io.Reader
	Getenv func(string) string
}

// payload reads only the keys it uses, and skips the rest whatever its form.
func payload(e Env) (map[string]proto.Value, error) {
	data, err := stdinread.Read(e.Stdin, stdinread.Options{Prog: "hook"})
	if err != nil {
		return nil, err
	}
	return proto.ForeignObject(data, "session_id", "transcript_path")
}

// record is teamchat's own file, strict by its own rules.
func record(path string) (proto.Value, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return proto.Value{}, err
	}
	return proto.Parse(data)
}

// answer is the link's line on teamchat's own socket.
func answer(c net.Conn) (proto.Value, error) {
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	if err != nil {
		return proto.Value{}, err
	}
	return proto.Parse(buf[:n])
}

// size is a number read from stdin, not a document.
func size(e Env) int {
	data, err := stdinread.Read(e.Stdin, stdinread.Options{Prog: "size"})
	if err != nil {
		return 0
	}
	return len(data)
}

// named is a constant document, beside an environment variable that is not parsed.
func named(e Env) (proto.Value, error) {
	if e.Getenv("TEAM") == "" {
		return proto.Value{}, nil
	}
	return proto.Parse([]byte(`{"v":1}`))
}
