package proc

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
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

func TestOSStreamsToTheWritersWhenGivenAndCapturesNothingThere(t *testing.T) {
	var out, errOut strings.Builder
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv:     []string{"sh", "-c", "echo out; echo err >&2; exit 4"},
		StdoutTo: &out,
		StderrTo: &errOut,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "out\n" || errOut.String() != "err\n" {
		t.Fatalf("streamed %q and %q", out.String(), errOut.String())
	}
	if res.Code != 4 || res.Stdout != "" || res.Stderr != "" {
		t.Fatalf("a streamed stream is not also captured: %+v", res)
	}
}

func TestOSStreamsOneStreamAndCapturesTheOther(t *testing.T) {
	var out strings.Builder
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv:     []string{"sh", "-c", "echo out; echo err >&2"},
		StdoutTo: &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "out\n" || res.Stderr != "err\n" || res.Stdout != "" {
		t.Fatalf("streamed %q, got %+v", out.String(), res)
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

// gone is whether the process no longer runs: it has no /proc entry, or it is a zombie that
// only waits to be reaped.
func gone(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	// The state letter follows the parenthesised command name.
	rest := string(data[bytes.LastIndexByte(data, ')')+1:])
	return strings.HasPrefix(strings.TrimSpace(rest), "Z")
}

// The command's background child holds its output pipe, so a kill of the shell alone would
// leave it running; the process group kill ends it. The child's pid is read from the output
// and watched in /proc, so the test needs no wall clock.
func TestOSKillsTheWholeProcessGroupOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	res, err := OS{}.Run(ctx, Cmd{Argv: []string{"sh", "-c", "sleep 30 & echo $!; sleep 30"}, Merge: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("not marked timed out: %+v", res)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(res.Stdout))
	if err != nil {
		t.Fatalf("the child's pid was not printed: %q", res.Stdout)
	}
	const polls = 500
	for range polls {
		if gone(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the child %d outlived the timeout", pid)
}

func TestOSCancelledIsNotATimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := OS{}.Run(ctx, Cmd{Argv: []string{"sleep", "30"}})
	if err == nil && res.TimedOut {
		t.Fatalf("a cancel was reported as a timeout: %+v", res)
	}
}
