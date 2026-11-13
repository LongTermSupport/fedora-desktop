// Package flow is the kit: Tell is a terminal write by a fact the rule derives, ShowSecret is
// not, because it hands its text only to the secret screen.
package flow

import (
	"io"

	"fix/internal/term"
)

// Kit is the person-facing side of a run.
type Kit struct {
	Out     *term.Printer
	Stderr  io.Writer
	Secrets *term.SecretScreen
}

// Tell says one line to the person.
func (k *Kit) Tell(msg string) { k.Out.Print(k.Stderr, msg+"\n") } // want Tell:"writes parameters 0"

// ReportErr prints why something failed.
func (k *Kit) ReportErr(err error) { k.Out.Print(k.Stderr, err.Error()+"\n") } // want ReportErr:"writes parameters 0"

// ShowSecret shows text on the secret screen.
func (k *Kit) ShowSecret(text string) { k.Secrets.Show(text) }
