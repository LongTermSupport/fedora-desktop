// Package cmdmaintest holds the two checks every QA command's main package owes and shares:
// that it refuses to run with a closed stdout, and which report writer it wires. They are
// helpers for tests, so they may import testing.
package cmdmaintest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const childEnv = "QA_CMDMAINTEST_CHILD"

// RefusesClosedStdout runs run in a child of the test binary whose stdout was closed at start
// (>&-) and requires the cannot-run code and the refusal on stderr. Call it as the whole body of
// a test: the child re-enters the same test and exits with run's code.
func RefusesClosedStdout(t *testing.T, run func() int, wantCode int) {
	t.Helper()
	if os.Getenv(childEnv) == t.Name() {
		os.Exit(run())
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res, runErr := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: []string{"bash", "-c", `exec "$0" -test.run='^` + t.Name() + `$' >&-`, self},
		Env:  []string{childEnv + "=" + t.Name()},
	})
	if runErr != nil || res.Code != wantCode || !strings.Contains(res.Stderr, "ERROR: stdout is closed") {
		t.Fatalf("%v: exit %d, want %d\nstderr: %s", runErr, res.Code, wantCode, res.Stderr)
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
