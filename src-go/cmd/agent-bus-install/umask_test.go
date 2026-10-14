package main

import (
	"os"
	"syscall"
	"testing"
)

// The installer's children (the package manager and its scriptlets, the account tool, zstd,
// the bundler) were run by the bash installer under the umask sudo and Ansible hand out.
// Every file the installer writes itself carries an explicit mode on its descriptor, so it
// needs no umask of its own, and one set process-wide would reach those children.
func TestTheInstallerLeavesTheUmaskItsChildrenInherit(t *testing.T) {
	restorePath(t)
	const inherited = 0o022
	old := syscall.Umask(inherited)
	t.Cleanup(func() { syscall.Umask(old) })
	oldArgs := os.Args
	os.Args = []string{"agent-bus-install", "--help"}
	t.Cleanup(func() { os.Args = oldArgs })
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	oldOut := os.Stdout
	os.Stdout = devnull
	t.Cleanup(func() {
		os.Stdout = oldOut
		if cerr := devnull.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	if code := execute(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := syscall.Umask(inherited); got != inherited {
		t.Fatalf("umask after the run is %#o, want %#o", got, inherited)
	}
}
