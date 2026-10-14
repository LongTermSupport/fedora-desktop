package freeze

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestListMixedInventoryIsOnStdoutOnly(t *testing.T) {
	h := newHarness(t, vocabs[0])
	if err := h.s.List(); err != nil {
		t.Fatal(err)
	}
	sp := func(n int) string { return strings.Repeat(" ", n) }
	row := func(name, state, mark, extra string) string {
		return "  " + name + sp(34-len(name)) + " " + state + sp(8-len(state)) + " " + mark + sp(4-len(mark)) + " " + extra + "\n"
	}
	header := "  NAME" + sp(30) + " STATE" + sp(3) + " MARK EXTRA\n"
	want := "=== frozen (3) ===\n" + header +
		row("alpha", "paused", "m0", "e-alpha") + row("bravo", "paused", "m1", "e-bravo") + row("delta", "paused", "m3", "e-delta") +
		"\n  Thaw them all with: testfreeze thaw --all\n\n" +
		"=== running (2) ===\n" + header +
		row("charlie", "running", "m2", "e-charlie") + row("echo", "running", "m4", "e-echo")
	mustEq(t, "stdout", h.out.String(), want)
	mustEq(t, "stderr", h.err.String(), "")
}

func TestListEmptyInventoryShowsNoneTwice(t *testing.T) {
	h := newHarness(t, vocabs[0])
	h.eng.entries = nil
	if err := h.s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := h.s.List(); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "stdout", h.out.String(), "=== frozen (0) ===\n  (none)\n\n=== running (0) ===\n  (none)\n")
}

// "frozen" is the frozen state; everything else counts as running, since an inventory holds
// only those two states.
func TestListCountsAnyNonFrozenStateAsRunning(t *testing.T) {
	h := newHarness(t, vocabs[1])
	h.eng.entries = []Entry{{Name: "x", State: "WEIRD", Data: extraOf{"m0", "e"}}}
	if err := h.s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := h.s.List(); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "running", h.out.String(), "=== running (1) ===")
	mustContain(t, "frozen", h.out.String(), "=== frozen (0) ===\n  (none)\n")
}

// The engine may append a closing note (the LXC "no running or frozen containers" sentence).
func TestListAppendsTheEnginesFooter(t *testing.T) {
	h := newHarness(t, vocabs[0])
	h.eng.footer = "\n  nothing here.\n"
	if err := h.s.List(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(h.out.String(), "e-echo\n\n  nothing here.\n") {
		t.Fatalf("footer missing: %q", h.out.String())
	}
}

// A footer is optional: an engine without ListFooter lists without one.
type noFooter struct{ Engine }

func TestListWithoutAFooterEngine(t *testing.T) {
	h := newHarness(t, vocabs[0])
	h.s.Engine = noFooter{h.eng}
	if err := h.s.List(); err != nil {
		t.Fatal(err)
	}
	mustLack(t, "no footer", h.out.String(), "nothing here")
}

// A failed write to the payload stream is surfaced, never swallowed.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestListSurfacesAWriteFailure(t *testing.T) {
	h := newHarness(t, vocabs[0])
	s := NewSession(testConfig(vocabs[0]), Env{Out: failingWriter{}, Err: &bytes.Buffer{}}, h.eng, h.chooser)
	if err := s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	err := s.List()
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("got %v", err)
	}
}
