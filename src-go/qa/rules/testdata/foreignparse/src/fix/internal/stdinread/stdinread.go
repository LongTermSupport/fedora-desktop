// Package stdinread is the fixture's shared stdin reader.
package stdinread

import (
	"bytes"
	"io"
)

// Options is one read.
type Options struct{ Prog string }

// Read is all of stdin.
func Read(stdin io.Reader, o Options) ([]byte, error) {
	if o.Prog == "" {
		return nil, io.EOF
	}
	return io.ReadAll(stdin)
}

// ReadLine is stdin's first line and the reader of the rest.
func ReadLine(stdin io.Reader, o Options) ([]byte, io.Reader, error) { // want ReadLine:"returns stdin"
	data, err := Read(stdin, o)
	if err != nil {
		return nil, nil, err
	}
	line, rest, found := bytes.Cut(data, []byte("\n"))
	if !found {
		return line, bytes.NewReader(nil), nil
	}
	return line, bytes.NewReader(rest), nil
}
