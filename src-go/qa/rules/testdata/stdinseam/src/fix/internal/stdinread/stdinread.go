// Package stdinread is the fixture's shared reader: the one package that may read stdin.
package stdinread

import (
	"io"
	"time"
)

// Options is one read.
type Options struct {
	Prog        string
	GiveUpAfter time.Duration
}

// Read reads stdin, which only this package may do.
func Read(stdin io.Reader, o Options) ([]byte, error) {
	if o.Prog == "" {
		return io.ReadAll(stdin)
	}
	return io.ReadAll(io.LimitReader(stdin, 10))
}
