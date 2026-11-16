// Package teamtest is a fixture test double: the fake console reads the stdin a test gives it.
package teamtest

import "io"

// Console is a fake.
type Console struct{ Stdin io.Reader }

// Typed is what the test typed.
func (c *Console) Typed() ([]byte, error) { return io.ReadAll(c.Stdin) }
