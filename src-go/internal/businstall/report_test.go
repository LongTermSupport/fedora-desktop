package businstall

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestAFailureSaysItsMessageAndAnOrdinaryErrorExitsOne(t *testing.T) {
	if got := fail(term.ExitUsage, "bad %s", "thing").Error(); got != "bad thing" {
		t.Fatalf("Error() = %q", got)
	}
	h := newHarness(t)
	if code := h.app.report(errors.New("plain trouble")); code != term.ExitFailure || h.errb.String() != "agent-bus-install: plain trouble\n" {
		t.Fatalf("report = %d, stderr %q", code, h.errb.String())
	}
}

func TestTheSinkKeepsTheFirstWriteFailureAndReturnsEveryOne(t *testing.T) {
	s := &sink{w: failingWriter{}}
	if _, err := s.Write([]byte("x")); err == nil || s.err == nil {
		t.Fatalf("Write = %v, kept %v", err, s.err)
	}
	first := s.err
	if _, err := s.Write([]byte("y")); err == nil || !errors.Is(s.err, first) {
		t.Fatalf("the first failure was replaced: %v", s.err)
	}
}

func TestProductionLooksTheRealAccountsUp(t *testing.T) {
	cfg := Production(&bytes.Buffer{}, &bytes.Buffer{}, "digest")
	root, err := cfg.RootOwner()
	if err != nil || root.UID != 0 || root.GID != 0 {
		t.Fatalf("RootOwner = %+v, %v", root, err)
	}
	if _, err = cfg.SvcOwner(); err != nil && !strings.Contains(err.Error(), svcName) {
		t.Fatalf("SvcOwner failed without naming the account: %v", err)
	}
}
