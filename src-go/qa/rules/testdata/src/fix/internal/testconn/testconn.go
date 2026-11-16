// Package testconn is the fixture's stand-in for internal/testconn.
package testconn

import (
	"io"
	"testing"
)

// Bound stands in for the real bound.
func Bound(tb testing.TB, c io.Closer) { tb.Helper() }
