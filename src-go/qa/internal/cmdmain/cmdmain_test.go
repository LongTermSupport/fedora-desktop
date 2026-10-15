package cmdmain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestWriteReportWritesTheFileWorldReadable(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), "r.json")
	if err := WriteReport(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v %v", info, err)
	}
	if data, err := os.ReadFile(filepath.Clean(path)); err != nil || string(data) != "{}" {
		t.Fatalf("content %q %v", data, err)
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

func TestWriteReportPrivateWritesTheFileOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	if err := WriteReportPrivate(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info, err)
	}
	if data, err := os.ReadFile(filepath.Clean(path)); err != nil || string(data) != "{}" {
		t.Fatalf("content %q %v", data, err)
	}
	if err := WriteReportPrivate(filepath.Join(t.TempDir(), "no", "r.json"), nil); err == nil {
		t.Fatal("a report in a missing directory was written")
	}
}

// RefuseClosedStdout says why on the stream it is given and reports true only for a closed stdout.
func TestRefuseClosedStdoutNamesTheReasonOnlyWhenClosed(t *testing.T) {
	if os.Getenv(childMode) == "refuse" {
		if RefuseClosedStdout() {
			os.Exit(7)
		}
		os.Exit(0)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		redirect string
		code     int
		stderr   string
	}{
		{">&-", 7, "ERROR: stdout is closed (>&-): the verdict cannot be delivered\n"},
		{">/dev/null", 0, ""},
	}
	for _, c := range cases {
		res, runErr := proc.OS{}.Run(context.Background(), proc.Cmd{
			Argv: []string{"bash", "-c", `exec "$0" -test.run='^TestRefuseClosedStdoutNamesTheReasonOnlyWhenClosed$' ` + c.redirect, self},
			Env:  []string{childMode + "=refuse"},
		})
		if runErr != nil || res.Code != c.code || res.Stderr != c.stderr {
			t.Fatalf("%s: %v exit %d stderr %q", c.redirect, runErr, res.Code, res.Stderr)
		}
	}
}
