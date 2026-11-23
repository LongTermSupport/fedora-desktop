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

// assembled is what a faked kit assembly was given.
type assembled struct {
	into, kit, binary string
	calls             int
}

// fakeAssembly replaces the assembly and the running program for one test.
func fakeAssembly(t *testing.T, fail error) *assembled {
	t.Helper()
	got := &assembled{}
	wasAssemble, wasBinary := assembleKit, ownBinary
	t.Cleanup(func() { assembleKit, ownBinary = wasAssemble, wasBinary })
	ownBinary = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("the binary")), nil }
	assembleKit = func(into string, kit paths.Kit, binary io.Reader) error {
		data, err := io.ReadAll(binary)
		if err != nil {
			return err
		}
		got.into, got.kit, got.binary = into, kit.String(), string(data)
		got.calls++
		return fail
	}
	return got
}

// Plan 00209 DESIGN-host-cc-v2.md section 6 step 2: `kit assemble --into DIR` assembles this
// build's host kit, from the running program, into the directory named for its own digest.
func TestKitAssembleBuildsThisBuildsHostKitFromItself(t *testing.T) {
	got := fakeAssembly(t, nil)
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	want := paths.HostKitDir(d).String()
	var w world
	if code := w.run(digest, "kit", "assemble", "--into", want); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got.calls != 1 || got.into != want || got.kit != want || got.binary != "the binary" {
		t.Fatalf("assembled %+v, want into and kit %s from the running program", got, want)
	}
}

// Any other directory is refused before anything is written: the hooks name the kit's own path,
// so a kit assembled elsewhere would run another build's program.
func TestKitAssembleRefusesADirectoryNotThisBuilds(t *testing.T) {
	for _, into := range []string{"/usr/local/lib/teamchat/kit/current", "/tmp/kit", "relative/kit"} {
		got := fakeAssembly(t, nil)
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
	got := fakeAssembly(t, nil)
	var w world
	if code := w.run("", "kit", "assemble", "--into", "/usr/local/lib/teamchat/kit/x"); code != term.ExitFailure || got.calls != 0 {
		t.Errorf("no digest: exit %d, calls %d", code, got.calls)
	}
	fakeAssembly(t, errors.New("the kit is assembled into an existing, empty directory"))
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
}
