package checkoutpin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const customDockerfile = "FROM claude-yolo:latest\nRUN dnf install -y golang\n"

// dockerfileRun is `checkout-pin dockerfile <unattended> <into>` in the rig's checkout, into a
// fresh private directory.
func (r *rig) dockerfileRun(t *testing.T, unattended string) (term.ExitCode, string) {
	t.Helper()
	into := t.TempDir()
	code := r.cmd().Dockerfile(context.Background(), []string{unattended, into})
	return code, into
}

// D1 (Plan 00266 review finding 7, the hazard): a custom Dockerfile is built only when its
// content is the content a person approved. Unattended, any other is exit 1 naming the file and
// the approve verb, and nothing is copied; with no file, nothing is printed and exit 0.
func TestDockerfileUnattendedTakesOnlyApprovedContent(t *testing.T) {
	r := newRig(t)
	if code, into := r.dockerfileRun(t, "true"); code != term.ExitOK || r.out.Len() != 0 || entries(t, into) != 0 {
		t.Fatalf("no Dockerfile: %v %q", code, r.out.String())
	}
	write(t, r.dir, Dockerfile, customDockerfile)
	code, into := r.dockerfileRun(t, "true")
	if code != term.ExitFailure || r.out.Len() != 0 || entries(t, into) != 0 ||
		!strings.Contains(r.err.String(), "ccy-teams approve dockerfile "+r.dir) || !strings.Contains(r.err.String(), Dockerfile) {
		t.Fatalf("unattended, nothing recorded: %v out %q err %q", code, r.out.String(), r.err.String())
	}
	r.answer = yes()
	if code := r.cmd().Approve(context.Background(), []string{"dockerfile"}); code != term.ExitOK || !strings.Contains(r.err.String(), "RUN dnf install -y golang") {
		t.Fatalf("approve dockerfile: %v %q", code, r.err.String())
	}
	r.answer = nil
	code, into = r.dockerfileRun(t, "true")
	copied := strings.TrimSuffix(r.out.String(), "\n")
	if code != term.ExitOK || copied != filepath.Join(into, "Dockerfile") {
		t.Fatalf("unattended, approved: %v out %q err %q", code, r.out.String(), r.err.String())
	}
	if got, err := os.ReadFile(copied); err != nil || string(got) != customDockerfile {
		t.Fatalf("the copy: %q %v", got, err)
	}
	if info, err := os.Stat(copied); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the copy's mode: %v %v", info, err)
	}
	write(t, r.dir, Dockerfile, customDockerfile+"ENTRYPOINT [\"/bin/sh\"]\n")
	if code, into := r.dockerfileRun(t, "true"); code != term.ExitFailure || entries(t, into) != 0 || !strings.Contains(r.err.String(), "changed since") {
		t.Fatalf("unattended, changed: %v %q", code, r.err.String())
	}
}

// D2: attended, content no person approved is shown line by line and asked about, default no.
// Only a yes records it and copies it; a no, or no terminal, copies nothing and records nothing.
func TestDockerfileAttendedShowsAndAsks(t *testing.T) {
	r := newRig(t)
	write(t, r.dir, Dockerfile, customDockerfile)
	if code, into := r.dockerfileRun(t, "false"); code != term.ExitFailure || entries(t, into) != 0 {
		t.Fatalf("no terminal: %v %q", code, r.err.String())
	}
	r.answer = no()
	code, into := r.dockerfileRun(t, "false")
	if code != term.ExitFailure || entries(t, into) != 0 || len(r.asked) != 1 ||
		!strings.Contains(r.err.String(), "RUN dnf install -y golang") || !strings.Contains(r.err.String(), "--disable-custom-docker") {
		t.Fatalf("a no: %v asked %v %q", code, r.asked, r.err.String())
	}
	r.answer = nil
	if code, into := r.dockerfileRun(t, "true"); code != term.ExitFailure || entries(t, into) != 0 {
		t.Fatalf("a no recorded the Dockerfile: %v", code)
	}
	r.answer = yes()
	if code, into := r.dockerfileRun(t, "false"); code != term.ExitOK || entries(t, into) != 1 || len(r.asked) != 2 {
		t.Fatalf("a yes: %v asked %v %q", code, r.asked, r.err.String())
	}
	if code, into := r.dockerfileRun(t, "false"); code != term.ExitOK || entries(t, into) != 1 || len(r.asked) != 2 {
		t.Fatalf("approved content was asked about again: %v asked %v", code, r.asked)
	}
}

// D3: the Dockerfile is read through an os.Root of the checkout: a link at its name is refused,
// and nothing is copied or asked.
func TestDockerfileRefusesALink(t *testing.T) {
	r := newRig(t)
	outside := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(outside, []byte(customDockerfile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.dir, ".claude/ccy"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.dir, Dockerfile)); err != nil {
		t.Fatal(err)
	}
	r.answer = yes()
	if code, into := r.dockerfileRun(t, "false"); code != term.ExitFailure || entries(t, into) != 0 || len(r.asked) != 0 || !strings.Contains(r.err.String(), "is a link") {
		t.Fatalf("a link: %v asked %v %q", code, r.asked, r.err.String())
	}
	if code := r.cmd().Approve(context.Background(), []string{"dockerfile"}); code != term.ExitFailure || len(r.asked) != 0 {
		t.Fatalf("approving a link: %v asked %v", code, r.asked)
	}
}

// D4: the arguments are whether nobody attends and an absolute directory to copy into.
func TestDockerfileUsage(t *testing.T) {
	r := newRig(t)
	for _, args := range [][]string{nil, {"true"}, {"maybe", "/tmp"}, {"true", "rel"}, {"true", "/tmp", "x"}} {
		if code := r.cmd().Dockerfile(context.Background(), args); code != term.ExitUsage {
			t.Errorf("Dockerfile(%q) = %v", args, code)
		}
	}
}

// entries is how many entries dir holds.
func entries(t *testing.T, dir string) int {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(list)
}
