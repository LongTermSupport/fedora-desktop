package cmdmain

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

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

// closedCode is the exit code the child runs below return for a stdout found closed.
const closedCode = 7

// codeIf is closedCode when closed holds, and 0 otherwise.
func codeIf(closed bool) int {
	if closed {
		return closedCode
	}
	return 0
}

// StdoutWasClosed tells a stdout closed at start (>&-) from one sent to /dev/null (>/dev/null).
func TestStdoutWasClosedSeesAClosedStdout(t *testing.T) {
	if res := cmdmaintest.Child(t, ">&-", func() int { return codeIf(StdoutWasClosed()) }); res.Code != closedCode {
		t.Fatalf(">&-: exit %d\n%s", res.Code, res.Stderr)
	}
}

func TestStdoutWasClosedPassesDevNull(t *testing.T) {
	if res := cmdmaintest.Child(t, ">/dev/null", func() int { return codeIf(StdoutWasClosed()) }); res.Code != 0 {
		t.Fatalf(">/dev/null: exit %d\n%s", res.Code, res.Stderr)
	}
}

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

// RefuseClosedStdout says why on stderr and reports true only for a closed stdout.
func TestRefuseClosedStdoutNamesTheReasonWhenClosed(t *testing.T) {
	res := cmdmaintest.Child(t, ">&-", func() int { return codeIf(RefuseClosedStdout()) })
	if res.Code != closedCode || res.Stderr != "ERROR: stdout is closed (>&-): the verdict cannot be delivered\n" {
		t.Fatalf("exit %d stderr %q", res.Code, res.Stderr)
	}
}

func TestRefuseClosedStdoutIsSilentForDevNull(t *testing.T) {
	res := cmdmaintest.Child(t, ">/dev/null", func() int { return codeIf(RefuseClosedStdout()) })
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("exit %d stderr %q", res.Code, res.Stderr)
	}
}
