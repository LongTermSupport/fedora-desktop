package execx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOSExecHonoursDirBeforeTheSyscall(t *testing.T) {
	t.Chdir(t.TempDir()) // registers the restore of the working directory Exec changes
	want, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var cwd string
	old := syscallExec
	syscallExec = func(string, []string, []string) error {
		var gerr error
		cwd, gerr = os.Getwd()
		return gerr
	}
	t.Cleanup(func() { syscallExec = old })
	if err := (OS{}).Exec(Cmd{Name: "/opt/launcher", Dir: want}); err != nil {
		t.Fatal(err)
	}
	if cwd != want {
		t.Fatalf("exec ran in %q, want %q", cwd, want)
	}
}

func TestOSExecWithAnUnenterableDirDoesNotExec(t *testing.T) {
	t.Chdir(t.TempDir())
	called := false
	old := syscallExec
	syscallExec = func(string, []string, []string) error { called = true; return nil }
	t.Cleanup(func() { syscallExec = old })
	err := (OS{}).Exec(Cmd{Name: "/opt/launcher", Dir: filepath.Join(t.TempDir(), "absent")})
	if err == nil || called || !strings.Contains(err.Error(), "enter") {
		t.Fatalf("called %v, err %v", called, err)
	}
}

func TestOSExecLooksTheProgramUpBeforeEnteringDir(t *testing.T) {
	t.Chdir(t.TempDir())
	err := (OS{}).Exec(Cmd{Name: "execx-no-such-program-anywhere", Dir: t.TempDir()})
	if !IsNotInstalled(err) {
		t.Fatalf("got %v", err)
	}
}
