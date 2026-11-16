// Package ui is the fixture's prompt seam: its line reader reads the stdin it is given, having
// refused a non-terminal first.
package ui

import (
	"bufio"
	"io"
)

// Lines prompts a person.
type Lines struct {
	In  io.Reader
	Out io.Writer
}

// Ask reads one answer.
func (l *Lines) Ask() (string, error) {
	return bufio.NewReader(l.In).ReadString('\n')
}
