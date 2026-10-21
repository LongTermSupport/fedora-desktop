// Package cmdmaintest holds the two checks every QA command's main package owes and shares:
// that it refuses to run with a closed stdout, and which report writer it wires. They are
// helpers for tests, so they may import testing.
//
// The closed-stdout check runs the command in a child of the test binary, and the child's exit
// code must be the command's, so a package that uses Child or RefusesClosedStdout declares
//
//	func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }
//
// and the process ends only there.
package cmdmaintest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const childEnv = "QA_CMDMAINTEST_CHILD"

// childMark starts the stderr line Main ends a child with, naming its exit code, so the parent
// tells a child Main ended from one whose package declares no TestMain.
const childMark = "cmdmaintest: child exit "

// childCode is the code the child's run returned, once Child has run it.
var childCode = -1

// childLost is a child's exit code when the test it was started for never reached its run.
const childLost = 125

// Main is the TestMain body of a package whose tests use Child: an ordinary test binary
// returns m.Run's code; a child returns the code of the run it was started for.
func Main(m *testing.M) int {
	code := m.Run()
	if os.Getenv(childEnv) == "" {
		return code
	}
	if childCode < 0 {
		fmt.Fprintln(os.Stderr, "cmdmaintest: the child test never reached its run")
		return childLost
	}
	fmt.Fprintf(os.Stderr, "%s%d\n", childMark, childCode)
	return childCode
}

// Child runs run in a child of the test binary started by bash with the redirect given (">&-"
// closes stdout, ">/dev/null" sends it to the null device), and returns the child's exit code
// and output, Main's closing line taken off. Call it once per test: the child re-enters the same
// test, runs run, and skips the rest of it.
func Child(t *testing.T, redirect string, run func() int) proc.Result {
	t.Helper()
	if os.Getenv(childEnv) == t.Name() {
		childCode = run()
		t.SkipNow()
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: []string{"bash", "-c", `exec "$0" -test.run='^` + t.Name() + `$' ` + redirect, self},
		Env:  []string{childEnv + "=" + t.Name()},
	})
	if err != nil {
		t.Fatal(err)
	}
	stderr, found := strings.CutSuffix(res.Stderr, childMark+strconv.Itoa(res.Code)+"\n")
	if !found {
		t.Fatalf("the child did not end through cmdmaintest.Main (no TestMain?): exit %d\nstderr: %s", res.Code, res.Stderr)
	}
	res.Stderr = stderr
	return res
}

// RefusesClosedStdout runs run in a child whose stdout was closed at start (>&-) and requires
// the cannot-run code and the refusal on stderr. Call it as the whole body of a test.
func RefusesClosedStdout(t *testing.T, run func() int, wantCode int) {
	t.Helper()
	res := Child(t, ">&-", run)
	if res.Code != wantCode || !strings.Contains(res.Stderr, "ERROR: stdout is closed") {
		t.Fatalf("exit %d, want %d\nstderr: %s", res.Code, wantCode, res.Stderr)
	}
}

// WritesReportWithMode requires that write puts the report down with exactly this permission
// under a umask that hides nothing, so a swap between the world-readable and the private writer
// fails.
func WritesReportWithMode(t *testing.T, write func(path string, data []byte) error, want os.FileMode) {
	t.Helper()
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), "report.json")
	if err := write(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("report mode %v, want %v", info.Mode().Perm(), want)
	}
}
