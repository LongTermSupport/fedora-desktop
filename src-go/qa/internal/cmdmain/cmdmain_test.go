package cmdmain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestWriteReportWritesTheFileWorldReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	if err := WriteReport(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v %v", info, err)
	}
	if err := WriteReport(filepath.Join(t.TempDir(), "no", "r.json"), nil); err == nil {
		t.Fatal("a report in a missing directory was written")
	}
}

// StdoutWasClosed tells a stdout closed at start (>&-) from one sent to /dev/null (>/dev/null).
func TestStdoutWasClosedTellsClosedFromDevNull(t *testing.T) {
	if mode := os.Getenv(childMode); mode != "" {
		if StdoutWasClosed() {
			os.Exit(7)
		}
		os.Exit(0)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for redirect, want := range map[string]int{">&-": 7, ">/dev/null": 0} {
		res, runErr := proc.OS{}.Run(context.Background(), proc.Cmd{
			Argv: []string{"bash", "-c", `exec "$0" -test.run='^TestStdoutWasClosedTellsClosedFromDevNull$' ` + redirect, self},
			Env:  []string{childMode + "=1"},
		})
		if runErr != nil || res.Code != want {
			t.Fatalf("%s: %v exit %d want %d\n%s", redirect, runErr, res.Code, want, strings.TrimSpace(res.Stderr))
		}
	}
}

const childMode = "QA_CMDMAIN_CHILD"
