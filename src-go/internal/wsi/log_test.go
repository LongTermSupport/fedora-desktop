package wsi

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testLog struct {
	*logger

	errb *bytes.Buffer
}

func newTestLogger(t *testing.T, immediate bool) testLog {
	t.Helper()
	var errb bytes.Buffer
	file := filepath.Join(t.TempDir(), "share", "speech-to-text", "debug.log")
	at := time.Date(2026, 10, 15, 9, 8, 7, 0, time.UTC)
	return testLog{
		logger: &logger{stderr: &errb, file: file, now: func() time.Time { return at }, immediate: immediate},
		errb:   &errb,
	}
}

func readLog(t *testing.T, file string) string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestW7DebugWritesEachLineAtOnceWithItsStamp(t *testing.T) {
	l := newTestLogger(t, true)
	l.info("hello")
	l.debug("detail")
	l.errorLine("bad")
	if l.errb.String() != "[WSI] hello\n[DEBUG] detail\n[ERROR] bad\n" {
		t.Fatalf("stderr %q", l.errb.String())
	}
	want := "[2026-10-15T09:08:07] [WSI] hello\n[2026-10-15T09:08:07] [WSI] [DEBUG] detail\n[2026-10-15T09:08:07] [WSI] [ERROR] bad\n"
	if got := readLog(t, l.file); got != want {
		t.Fatalf("log %q", got)
	}
}

func TestW7OtherwiseLinesAreHeldUntilAnErrorFlushesThem(t *testing.T) {
	l := newTestLogger(t, false)
	l.info("one")
	l.debug("hidden")
	if l.errb.String() != "[WSI] one\n" {
		t.Fatalf("a debug line was shown without -d or -v: %q", l.errb.String())
	}
	if _, err := os.Stat(l.file); err == nil {
		t.Fatal("a held line reached the file")
	}
	l.errorLine("two")
	if n := l.flush(); n != 2 {
		t.Fatalf("flushed %d", n)
	}
	l.direct("after")
	if got := readLog(t, l.file); got != "[2026-10-15T09:08:07] [WSI] one\n[2026-10-15T09:08:07] [WSI] [ERROR] two\n[2026-10-15T09:08:07] [WSI] after\n" {
		t.Fatalf("log %q", got)
	}
	if n := l.flush(); n != 0 {
		t.Fatalf("a second flush found %d", n)
	}
}

func TestW7ALogOverOneMebibyteIsRotatedFirst(t *testing.T) {
	l := newTestLogger(t, true)
	if err := os.MkdirAll(filepath.Dir(l.file), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.file, []byte(strings.Repeat("x", 1048577)), 0o600); err != nil {
		t.Fatal(err)
	}
	l.info("fresh")
	if got := readLog(t, l.file); got != "[2026-10-15T09:08:07] [WSI] fresh\n" {
		t.Fatalf("log %q", got)
	}
	if info, err := os.Stat(l.file + ".old"); err != nil || info.Size() != 1048577 {
		t.Fatalf("old log: %v", err)
	}
}

func TestB12ALogThatCannotBeWrittenIsSaidOnceAndTheRunGoesOn(t *testing.T) {
	l := newTestLogger(t, true)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l.file = filepath.Join(blocker, "debug.log")
	l.info("a")
	l.info("b")
	if n := strings.Count(l.errb.String(), "cannot write the log"); n != 1 {
		t.Fatalf("said %d times: %q", n, l.errb.String())
	}
}
