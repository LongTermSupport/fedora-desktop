package stdinread

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// lineOptions are a ReadLine's options with nothing said and no terminal.
func lineOptions(limit int64) Options {
	return Options{Limit: limit, Err: io.Discard, Clock: clock.NewFake(0, 0), IsTerminal: notTerminal}
}

// ReadLine is the first line, without its end, and the rest of the input after it, unread.
func TestReadLineIsTheLineAndTheRestAfterIt(t *testing.T) {
	line, rest, err := ReadLine(strings.NewReader("request\nbody\nmore"), lineOptions(64))
	if err != nil || string(line) != "request" {
		t.Fatalf("%q %v", line, err)
	}
	after, err := io.ReadAll(rest)
	if err != nil || string(after) != "body\nmore" {
		t.Fatalf("rest %q %v", after, err)
	}
}

// Input that ends without a line end is the line, and the rest is empty.
func TestReadLineOfInputWithNoLineEnd(t *testing.T) {
	line, rest, err := ReadLine(strings.NewReader("request"), lineOptions(64))
	if err != nil || string(line) != "request" {
		t.Fatalf("%q %v", line, err)
	}
	after, err := io.ReadAll(rest)
	if err != nil || len(after) != 0 {
		t.Fatalf("rest %q %v", after, err)
	}
}

// At most Limit bytes of the line are read: a caller with a cap gives one more and judges a line
// that long as over it. What follows stays in the rest.
func TestReadLineReadsAtMostTheLimit(t *testing.T) {
	line, rest, err := ReadLine(strings.NewReader("abcdef\nx"), lineOptions(4))
	if err != nil || string(line) != "abcd" {
		t.Fatalf("%q %v", line, err)
	}
	after, err := io.ReadAll(rest)
	if err != nil || string(after) != "ef\nx" {
		t.Fatalf("rest %q %v", after, err)
	}
	line, _, err = ReadLine(strings.NewReader("abc\n"), lineOptions(4))
	if err != nil || string(line) != "abc" {
		t.Fatalf("a line within the limit %q %v", line, err)
	}
}

// An open pipe nothing writes to is noticed and given up, as Read's is.
func TestReadLineOfAnOpenPipeIsNoticedThenGivenUp(t *testing.T) {
	f := clock.NewFake(0, 0)
	var errs syncBuffer
	got := make(chan error, 1)
	go func() {
		_, _, err := ReadLine(silent(t), Options{Prog: "ctl-child", Err: &errs, Clock: f, GiveUpAfter: time.Minute, IsTerminal: notTerminal, Limit: 64})
		got <- err
	}()
	due(t, f, NoticeAfter)
	due(t, f, time.Minute-NoticeAfter)
	var none *NoInputError
	if err := <-got; !errors.As(err, &none) {
		t.Fatalf("%v", err)
	}
	if !strings.HasPrefix(errs.String(), "ctl-child: waiting for input on stdin") {
		t.Fatalf("stderr %q", errs.String())
	}
}

// A failed read and a refused terminal are errors.
func TestReadLineFailures(t *testing.T) {
	if _, _, err := ReadLine(failing{errors.New("eio")}, lineOptions(64)); err == nil || err.Error() != "read stdin: eio" {
		t.Fatalf("a failed read %v", err)
	}
	o := lineOptions(64)
	o.RefuseTerminal = true
	o.IsTerminal = func(any) bool { return true }
	if _, _, err := ReadLine(strings.NewReader("x\n"), o); !errors.Is(err, ErrTerminal) {
		t.Fatalf("a terminal %v", err)
	}
}
