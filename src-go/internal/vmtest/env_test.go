package vmtest

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The production environment's own pieces, which the flow tests replace with the kit's.

func TestNewEnvIsComplete(t *testing.T) {
	e := NewEnv()
	if e.Runner == nil || e.Stdin == nil || e.Stdout == nil || e.Stderr == nil || e.Getenv == nil ||
		e.Now == nil || e.Sleep == nil || e.FreePort == nil || e.Random == nil || e.GPGKeyDir != DefaultGPGKeyDir {
		t.Fatalf("an unset production dependency: %+v", e)
	}
}

// A sleep ends with its context, and says so.
func TestSleepEndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestFreeLoopbackPort(t *testing.T) {
	port, err := freeLoopbackPort()
	if err != nil || port <= 0 || port > 65535 {
		t.Fatalf("got %d %v", port, err)
	}
}

// The command's error kinds read as their own text.
func TestErrorKindsReadAsTheirText(t *testing.T) {
	if got := usagef("bad %s", "flag").Error(); got != "bad flag" {
		t.Fatalf("usage %q", got)
	}
	if got := (&exitStatusError{code: 3}).Error(); got != "exit 3" {
		t.Fatalf("status %q", got)
	}
}
