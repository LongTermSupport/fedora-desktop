package execx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type cancelRun struct {
	out  string
	err  error
	took time.Duration
}

// runThenCancel runs script and cancels the context once the child has printed "ready", so
// the cancel never lands before the child's trap is installed.
func runThenCancel(t *testing.T, r OS, script string) cancelRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, len("ready\n"))
		if _, err := io.ReadFull(pr, buf); err == nil {
			cancel()
		}
		if _, err := io.Copy(io.Discard, pr); err != nil {
			return
		}
	}()
	var out bytes.Buffer
	start := time.Now()
	_, err := r.Run(ctx, Cmd{Name: "sh", Args: []string{"-c", script}, Stdout: io.MultiWriter(&out, pw)})
	took := time.Since(start)
	if cerr := pw.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	<-done
	return cancelRun{out: out.String(), err: err, took: took}
}

func TestOSRunCancelSendsSIGTERMSoTheChildCanRestoreTheTerminal(t *testing.T) {
	got := runThenCancel(t, OS{}, `trap 'echo restored; exit 0' TERM; echo ready; while :; do sleep 0.05; done`)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", got.err)
	}
	if !strings.Contains(got.out, "restored") {
		t.Fatalf("the child got no catchable signal: %q", got.out)
	}
}

func TestOSRunCancelEscalatesWhenTheChildIgnoresTERMAndHoldsThePipe(t *testing.T) {
	got := runThenCancel(t, OS{Grace: 300 * time.Millisecond}, `trap '' TERM; echo ready; sleep 30 & while :; do sleep 0.05; done`)
	if got.err == nil {
		t.Fatal("want an error from the cancelled command")
	}
	if got.took > 10*time.Second {
		t.Fatalf("cancel did not escalate: took %v", got.took)
	}
}

func TestOSRunWithoutCancelIgnoresGrace(t *testing.T) {
	res, err := OS{Grace: time.Millisecond}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "sleep 0.1; echo done"}})
	if err != nil || string(res.Stdout) != "done\n" {
		t.Fatalf("got %q, %v", res.Stdout, err)
	}
}
