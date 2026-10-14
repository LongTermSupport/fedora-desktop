package proc

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOSRunsACommandAndSplitsTheStreams(t *testing.T) {
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv: []string{"sh", "-c", "echo out; echo err >&2; exit 3"},
		Dir:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 3 || res.Stdout != "out\n" || res.Stderr != "err\n" || res.TimedOut {
		t.Fatalf("got %+v", res)
	}
}

func TestOSMergesTheStreamsInOrderWhenAsked(t *testing.T) {
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv:  []string{"sh", "-c", "echo one; echo two >&2; echo three"},
		Merge: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "one\ntwo\nthree\n" || res.Stderr != "" {
		t.Fatalf("got %+v", res)
	}
}

func TestOSPassesTheExtraEnvironment(t *testing.T) {
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv: []string{"sh", "-c", "printf %s \"$QA_RUN_PROBE\""},
		Env:  []string{"QA_RUN_PROBE=hello"},
	})
	if err != nil || res.Stdout != "hello" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestOSReportsAProgramThatCannotStart(t *testing.T) {
	if _, err := (OS{}).Run(context.Background(), Cmd{Argv: []string{"/no/such/program"}}); err == nil {
		t.Fatal("a missing program was not an error")
	}
	if _, err := (OS{}).Run(context.Background(), Cmd{}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("an empty argv: %v", err)
	}
}

func TestOSKillsTheWholeProcessGroupOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := OS{}.Run(ctx, Cmd{Argv: []string{"sh", "-c", "sleep 30 & sleep 30"}, Merge: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("not marked timed out: %+v", res)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("the children outlived the timeout: %v", time.Since(start))
	}
}

func TestOSCancelledIsNotATimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := OS{}.Run(ctx, Cmd{Argv: []string{"sleep", "30"}})
	if err == nil && res.TimedOut {
		t.Fatalf("a cancel was reported as a timeout: %+v", res)
	}
}
