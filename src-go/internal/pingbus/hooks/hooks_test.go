package hooks

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The stand-in answers nothing and fails, so no caller can mistake it for the port.
func TestTheStandInFailsAndWritesNothing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	at := time.UnixMilli(1)
	in := Input{Event: "prompt", Stdin: []byte("{}"), Env: map[string]string{}, Now: func() time.Time { return at }}
	if code := Run(context.Background(), in, &stdout, &stderr); code != term.ExitFailure {
		t.Errorf("exit %d, want %d", code, term.ExitFailure)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("wrote stdout %q, stderr %q; want nothing", stdout.Bytes(), stderr.Bytes())
	}
	if !Placeholder {
		t.Error("the stand-in must say it is one")
	}
	if InputMax != 1<<20 {
		t.Errorf("InputMax %d, want 1 MiB", InputMax)
	}
}
