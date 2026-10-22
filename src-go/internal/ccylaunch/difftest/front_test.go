package difftest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylaunch"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The Go front under test is this test binary too: copied into a launcher directory as
// claude-yolo, beside the bash launcher as claude-yolo.bash, it runs ccylaunch.Front as
// cmd/claude-yolo does, so the matrix judges the cut-over without a build of its own.
const frontName = "claude-yolo"

// frontVersion is how play-claude-yolo.yml reads the version it builds the front with.
var frontVersion = regexp.MustCompile(`(?m)^CCY_VERSION="([^"]+)"`)

func isFront() bool {
	return os.Getenv(sandboxEnv) != "" && filepath.Base(os.Args[0]) == frontName
}

// runFront is cmd/claude-yolo's run, with the version the play would give the build.
func runFront() int {
	self, err := os.Executable()
	if err != nil {
		return brokenFake(frontName, err)
	}
	version := ""
	// A launcher that cannot be read leaves the version empty: the front hands over and names it.
	if text, readErr := os.ReadFile(filepath.Join(filepath.Dir(self), ccylaunch.BashName)); readErr == nil {
		if m := frontVersion.FindSubmatch(text); m != nil {
			version = string(m[1])
		}
	}
	front := ccylaunch.Front{
		Version: version, Self: self, Args: os.Args[1:], Environ: os.Environ(), LookPath: exec.LookPath,
		Runner: execx.OS{}, Execer: execx.OS{}, Stdout: os.Stdout, Stderr: os.Stderr,
	}
	return int(front.Run(context.Background()))
}

// installFront lays a launcher directory out as the play does with the front: this test binary
// as claude-yolo, the checkout's bash launcher (changed by edit, when not nil) beside it as
// claude-yolo.bash, and lib/. It returns the front's path.
func installFront(t *testing.T, edit func(string) string) string {
	t.Helper()
	bash := installBash(t, edit)
	dir := filepath.Dir(bash)
	if err := os.Rename(bash, filepath.Join(dir, ccylaunch.BashName)); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	front := filepath.Join(dir, frontName)
	if err := os.WriteFile(front, raw, dirMode); err != nil {
		t.Fatal(err)
	}
	return front
}
