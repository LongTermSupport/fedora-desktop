package emit

import (
	"bytes"
	"errors"
	"testing"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestLineTextAndWriteReachTheWriter(t *testing.T) {
	var buf bytes.Buffer
	e := New(&buf)
	e.Line("a")
	e.Text("b")
	if n, err := e.Write([]byte("c")); n != 1 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	if buf.String() != "a\nbc" || e.Err() != nil {
		t.Fatalf("%q %v", buf.String(), e.Err())
	}
}

func TestBlockPrintsTheTextOnceWithOneFinalNewline(t *testing.T) {
	var buf bytes.Buffer
	New(&buf).Block("usage: x\n\n  -h  help\n")
	if buf.String() != "usage: x\n\n  -h  help\n" {
		t.Fatalf("%q", buf.String())
	}
}

func TestTheFirstWriteErrorIsKept(t *testing.T) {
	e := New(failWriter{})
	e.Line("a")
	e.Text("b")
	if _, err := e.Write([]byte("c")); err == nil {
		t.Fatal("the write error was not returned")
	}
	if e.Err() == nil || e.Err().Error() != "disk full" {
		t.Fatalf("%v", e.Err())
	}
}

type code int

const (
	ok code = iota
	failed
	cannotRun
)

func TestFinishKeepsTheCodeWhenNothingFailedToWrite(t *testing.T) {
	var o, e bytes.Buffer
	if got := Finish(New(&o), New(&e), failed, cannotRun); got != failed || e.Len() != 0 {
		t.Fatalf("%d %q", got, e.String())
	}
}

func TestFinishTurnsAStdoutFailureIntoCannotRunAndSaysSo(t *testing.T) {
	var e bytes.Buffer
	out := New(failWriter{})
	out.Line("x")
	if got := Finish(out, New(&e), ok, cannotRun); got != cannotRun || e.String() != "ERROR: cannot write to stdout: disk full\n" {
		t.Fatalf("%d %q", got, e.String())
	}
}

func TestFinishTurnsAStderrFailureIntoCannotRun(t *testing.T) {
	var o bytes.Buffer
	errOut := New(failWriter{})
	errOut.Line("x")
	if got := Finish(New(&o), errOut, failed, cannotRun); got != cannotRun {
		t.Fatalf("%d", got)
	}
	if got := Finish(New(&o), errOut, ok, cannotRun); got != cannotRun {
		t.Fatalf("%d", got)
	}
	if o.Len() != 0 {
		t.Fatalf("%q", o.String())
	}
}
