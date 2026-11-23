package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// assembled is what a faked kit placement was given.
type assembled struct {
	kit, binary string
	calls       int
}

// fakeAssembly replaces the placement and the running program for one test; the placement
// reports placed, or fails with fail.
func fakeAssembly(t *testing.T, placed bool, fail error) *assembled {
	t.Helper()
	got := &assembled{}
	wasPlace, wasBinary := placeKit, ownBinary
	t.Cleanup(func() { placeKit, ownBinary = wasPlace, wasBinary })
	ownBinary = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("the binary")), nil }
	placeKit = func(kit paths.Kit, binary io.Reader) (bool, error) {
		data, err := io.ReadAll(binary)
		if err != nil {
			return false, err
		}
		got.kit, got.binary = kit.String(), string(data)
		got.calls++
		return placed && fail == nil, fail
	}
	return got
}

// Plan 00209 DESIGN-host-cc-v2.md section 6 step 2: `kit assemble --into DIR` places this
// build's host kit, from the running program, whole at the directory named for its own digest
// (kit.Place: assembled beside it and renamed into place), and says so on stdout.
func TestKitAssembleBuildsThisBuildsHostKitFromItself(t *testing.T) {
	got := fakeAssembly(t, true, nil)
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	want := paths.HostKitDir(d).String()
	var w world
	if code := w.run(digest, "kit", "assemble", "--into", want); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got.calls != 1 || got.kit != want || got.binary != "the binary" {
		t.Fatalf("assembled %+v, want the kit %s from the running program", got, want)
	}
	if w.out.String() != "assembled "+want+"\n" {
		t.Errorf("stdout %q, want the kit assembled", w.out.String())
	}
}

// A whole kit already in place is left alone, and stdout says so, so the play reports no change.
func TestKitAssembleSaysAWholeKitIsLeftAlone(t *testing.T) {
	got := fakeAssembly(t, false, nil)
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	want := paths.HostKitDir(d).String()
	var w world
	if code := w.run(digest, "kit", "assemble", "--into", want); code != term.ExitOK || got.calls != 1 {
		t.Fatalf("exit %d, calls %d: %s", code, got.calls, w.err.String())
	}
	if w.out.String() != "whole "+want+"\n" {
		t.Errorf("stdout %q, want the kit left whole", w.out.String())
	}
}

// Any other directory is refused before anything is written: the hooks name the kit's own path,
// so a kit assembled elsewhere would run another build's program.
func TestKitAssembleRefusesADirectoryNotThisBuilds(t *testing.T) {
	for _, into := range []string{"/usr/local/lib/teamchat/kit/current", "/tmp/kit", "relative/kit"} {
		got := fakeAssembly(t, true, nil)
		var w world
		if code := w.run(digest, "kit", "assemble", "--into", into); code != exits.ExitUsage {
			t.Errorf("%s: exit %d, want usage", into, code)
		}
		if got.calls != 0 {
			t.Errorf("%s: assembled", into)
		}
	}
}

// A build with no digest cannot name its kit; a failed assembly fails the verb.
func TestKitAssembleFailsWithNoDigestOrAFailedAssembly(t *testing.T) {
	got := fakeAssembly(t, true, nil)
	var w world
	if code := w.run("", "kit", "assemble", "--into", "/usr/local/lib/teamchat/kit/x"); code != term.ExitFailure || got.calls != 0 {
		t.Errorf("no digest: exit %d, calls %d", code, got.calls)
	}
	fakeAssembly(t, true, errors.New("the kit is assembled into an existing, empty directory"))
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	var w2 world
	if code := w2.run(digest, "kit", "assemble", "--into", paths.HostKitDir(d).String()); code == term.ExitOK {
		t.Error("a failed assembly exited 0")
	}
	if !strings.Contains(w2.err.String(), "empty directory") {
		t.Errorf("stderr %q does not say why", w2.err.String())
	}
	if w2.out.Len() != 0 {
		t.Errorf("a failed assembly wrote %q to stdout", w2.out.String())
	}
}
