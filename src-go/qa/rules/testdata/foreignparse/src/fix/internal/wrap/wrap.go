// Package wrap parses its input strictly on another package's behalf, and reads stdin for it.
package wrap

import (
	"io"

	"fix/internal/stdinread"
	"fix/internal/teamchat/proto"
)

// Decode parses b strictly.
func Decode(b []byte) (proto.Value, error) { return proto.Parse(b) } // want Decode:"strictly parses parameters 0"

// Reader holds a stdin.
type Reader struct{ Stdin io.Reader }

// Payload is the stdin it holds.
func (r Reader) Payload() []byte { // want Payload:"returns stdin"
	data, err := stdinread.Read(r.Stdin, stdinread.Options{Prog: "wrap"})
	if err != nil {
		return nil
	}
	return data
}
