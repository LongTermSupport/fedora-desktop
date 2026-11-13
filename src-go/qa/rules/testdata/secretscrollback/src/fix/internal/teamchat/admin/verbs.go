// Package admin is teamchat-admin's member verb: its answer document carries an issued
// credential's bundle, encoded through types the rule does not follow, so the field the
// document is held in is the source.
package admin

import (
	"io"

	"fix/internal/term"
)

type work struct {
	doc    []byte
	issued bool
}

// Env is the verb's streams.
type Env struct{ Stdout io.Writer }

func answer(e Env, payload string) error { // want answer:"writes parameters 1"
	_, err := io.WriteString(e.Stdout, payload)
	return err
}

func memberWork() work { return work{doc: []byte(`{"v":1}`), issued: true} }

func bad(e Env) error {
	w := memberWork()
	return answer(e, string(w.doc)+"\n") // want `secret from admin.work.doc written to the terminal through admin.answer`
}

func good(e Env) {
	w := memberWork()
	screen := &term.SecretScreen{W: e.Stdout}
	screen.Show(string(w.doc) + "\n")
}

func changed(e Env) error { return answer(e, "CHANGED\tteam\n") }

var _ = []any{bad, good, changed}
