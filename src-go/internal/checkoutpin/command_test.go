package checkoutpin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// rig is a Command over a scratch home, state directory and checkout, with an answer to give
// the approval question (nil: no terminal).
type rig struct {
	home, state, dir string
	answer           *bool
	asked            []string
	out, err         bytes.Buffer
	env              map[string]string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	home := checkout(t)
	r := &rig{home: home, state: filepath.Join(home, ".local/state/ccy"), dir: filepath.Join(home, "Projects/app")}
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	r.env = map[string]string{"HOME": home}
	return r
}

func (r *rig) cmd() Command {
	r.out.Reset()
	r.err.Reset()
	c := Command{
		Getenv: func(k string) string { return r.env[k] },
		Getwd:  func() (string, error) { return r.dir, nil },
		Stdout: &r.out, Stderr: &r.err,
		Now: func() time.Time { return testAt },
	}
	if r.answer != nil {
		c.Confirm = func(_ context.Context, q string) (bool, error) {
			r.asked = append(r.asked, q)
			return *r.answer, nil
		}
	}
	return c
}

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

// C1: no mounts file is no mounts; CCY_NO_PROJECT_MOUNTS=1 ignores one, saying so.
func TestProjectMountsWithoutAFile(t *testing.T) {
	r := newRig(t)
	if code := r.cmd().ProjectMounts(nil); code != term.ExitOK || r.out.Len() != 0 {
		t.Fatalf("no file: %v %q", code, r.out.String())
	}
	write(t, r.dir, Mounts, "/srv:/x\n")
	r.env["CCY_NO_PROJECT_MOUNTS"] = "1"
	c := r.cmd()
	if code := c.ProjectMounts(nil); code != term.ExitOK || r.out.Len() != 0 || !strings.Contains(r.err.String(), "CCY_NO_PROJECT_MOUNTS=1") {
		t.Fatalf("opted out: %v %q %q", code, r.out.String(), r.err.String())
	}
}

// C2 (the hazard): a mounts file no person approved is refused at every launch, attended or
// not, naming the file and the exact verb, and nothing is bound. Approved, it is bound; changed
// again, it is refused again.
func TestProjectMountsNeedsApproval(t *testing.T) {
	r := newRig(t)
	data := filepath.Join(r.home, "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, r.dir, Mounts, "~/data:/data:ro\n")
	if code := r.cmd().ProjectMounts(nil); code != term.ExitFailure || r.out.Len() != 0 {
		t.Fatalf("unapproved: %v %q", code, r.out.String())
	}
	for _, want := range []string{r.dir + "/" + Mounts, "ccy-teams approve mounts " + r.dir} {
		if !strings.Contains(r.err.String(), want) {
			t.Errorf("refusal %q lacks %q", r.err.String(), want)
		}
	}
	r.answer = yes()
	if code := r.cmd().Approve(context.Background(), []string{"mounts", r.dir}); code != term.ExitOK {
		t.Fatalf("approve: %v %s", code, r.err.String())
	}
	if len(r.asked) != 1 || !strings.Contains(r.err.String(), data+" → /data (ro)") {
		t.Fatalf("approve showed %q, asked %v", r.err.String(), r.asked)
	}
	if code := r.cmd().ProjectMounts(nil); code != term.ExitOK || r.out.String() != "-v\n"+data+":/data:ro\n" {
		t.Fatalf("approved: %v %q %s", code, r.out.String(), r.err.String())
	}
	write(t, r.dir, Mounts, "~/data:/data:rw\n")
	if code := r.cmd().ProjectMounts(nil); code != term.ExitFailure || !strings.Contains(r.err.String(), "changed since") {
		t.Fatalf("changed: %v %q", code, r.err.String())
	}
}

// C3: approval needs a person at a terminal and a yes; a no or no terminal records nothing, and
// a file that breaks a rule cannot be approved.
func TestApproveAsksAPerson(t *testing.T) {
	r := newRig(t)
	write(t, r.dir, Mounts, "/srv:/x\n")
	if code := r.cmd().Approve(context.Background(), []string{"mounts", r.dir}); code != term.ExitFailure || !strings.Contains(r.err.String(), "terminal") {
		t.Fatalf("no terminal: %v %q", code, r.err.String())
	}
	r.answer = no()
	if code := r.cmd().Approve(context.Background(), []string{"mounts", r.dir}); code != term.ExitFailure {
		t.Fatalf("a no: %v", code)
	}
	s, err := Locate(r.cmd().Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, found, err := s.read(r.dir, KindMounts); found || err != nil {
		t.Fatalf("recorded after a no: %v %v", found, err)
	}
	r.answer = yes()
	write(t, r.dir, Mounts, "~/.ssh:/x\n")
	if code := r.cmd().Approve(context.Background(), []string{"mounts", r.dir}); code != term.ExitFailure || len(r.asked) != 1 {
		t.Fatalf("a broken file: %v asked %v", code, r.asked)
	}
	for _, args := range [][]string{nil, {"other"}, {"mounts", r.dir, "x"}} {
		if code := r.cmd().Approve(context.Background(), args); code != term.ExitUsage {
			t.Errorf("Approve(%q) = %v", args, code)
		}
	}
}

// C4: a broken line, a source that is not there, and a source that is a link to a denied path
// are refused with the line named, even when approved.
func TestProjectMountsChecksTheDisk(t *testing.T) {
	r := newRig(t)
	ssh := filepath.Join(r.home, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(ssh, filepath.Join(r.home, "innocent")); err != nil {
		t.Fatal(err)
	}
	// A relocated state directory is the launcher's state all the same.
	r.env["CCY_STATE_DIR"] = filepath.Join(r.home, "state/ccy")
	for content, want := range map[string]string{
		"~/innocent:/x:ro\n": "(under " + ssh + ")",
		"~/absent:/x\n":      "does not exist",
		"/etc:/x\n":          "line 1",
		"~/state:/x\n":       "(it holds " + r.env["CCY_STATE_DIR"] + ")",
	} {
		write(t, r.dir, Mounts, content)
		r.answer = yes()
		if code := r.cmd().Approve(context.Background(), []string{"mounts", r.dir}); content != "~/innocent:/x:ro\n" && content != "~/absent:/x\n" && code != term.ExitFailure {
			t.Errorf("%q approved", content)
		}
		if code := r.cmd().ProjectMounts(nil); code != term.ExitFailure || !strings.Contains(r.err.String(), want) {
			t.Errorf("%q: %v %q, want %q", content, code, r.err.String(), want)
		}
	}
}

// C5 (the hazard): a compose file is recorded when a person starts it, and a launch nobody
// attends runs only the recorded content; with none recorded, or any file changed, it refuses
// naming the verb. No compose file is nothing to judge.
func TestComposePin(t *testing.T) {
	r := newRig(t)
	if code := r.cmd().Compose(context.Background(), []string{"true"}); code != term.ExitOK {
		t.Fatalf("no compose files: %v", code)
	}
	write(t, r.dir, "compose.yml", "services: {}\n")
	if code := r.cmd().Compose(context.Background(), []string{"true"}); code != term.ExitFailure || !strings.Contains(r.err.String(), "ccy-teams approve compose "+r.dir) {
		t.Fatalf("unattended, nothing recorded: %v %q", code, r.err.String())
	}
	r.answer = yes()
	if code := r.cmd().Compose(context.Background(), []string{"false"}); code != term.ExitOK {
		t.Fatalf("attended: %v %s", code, r.err.String())
	}
	r.answer = nil
	if code := r.cmd().Compose(context.Background(), []string{"true"}); code != term.ExitOK {
		t.Fatalf("unattended, recorded: %v %s", code, r.err.String())
	}
	write(t, r.dir, "compose.yml", "services:\n  x:\n    volumes: [/:/host]\n")
	if code := r.cmd().Compose(context.Background(), []string{"true"}); code != term.ExitFailure || !strings.Contains(r.err.String(), "changed since") {
		t.Fatalf("unattended, changed: %v %q", code, r.err.String())
	}
	r.answer = yes()
	if code := r.cmd().Approve(context.Background(), []string{"compose"}); code != term.ExitOK || !strings.Contains(r.err.String(), "compose.yml") {
		t.Fatalf("approve compose: %v %q", code, r.err.String())
	}
	if code := r.cmd().Compose(context.Background(), []string{"true"}); code != term.ExitOK {
		t.Fatalf("unattended, approved: %v %s", code, r.err.String())
	}
	for _, args := range [][]string{nil, {"maybe"}, {"true", "x"}} {
		if code := r.cmd().Compose(context.Background(), args); code != term.ExitUsage {
			t.Errorf("Compose(%q) = %v", args, code)
		}
	}
}

// C6 (finding 9): approving the compose files shows what is approved, each file's lines under
// its name: the compose files, the .env they read and each file they include, so the person
// reads the bytes recorded, not a list of names.
func TestApproveComposeShowsTheContent(t *testing.T) {
	r := newRig(t)
	write(t, r.dir, "compose.yml", "include: [base.yml]\nservices:\n  app:\n    volumes: [\"./data:/d\"]\n")
	write(t, r.dir, "base.yml", "services:\n  db:\n    image: postgres\n")
	write(t, r.dir, ".env", "DATA=/srv/data\n")
	r.answer = no()
	if code := r.cmd().Approve(context.Background(), []string{"compose"}); code != term.ExitFailure {
		t.Fatalf("approve: %v", code)
	}
	for _, want := range []string{"── compose.yml ──", `volumes: ["./data:/d"]`, "── base.yml ──", "image: postgres", "── .env ──", "DATA=/srv/data"} {
		if !strings.Contains(r.err.String(), want) {
			t.Errorf("approval screen lacks %q:\n%s", want, r.err.String())
		}
	}
}

// C7 (finding 3, the hazard): an attended launch whose compose files are not the approved
// content shows every line and asks, default no; only a yes records them. No terminal, or a no,
// starts nothing and records nothing; the approved content is started unasked.
func TestComposeAttendedShowsAndAsks(t *testing.T) {
	r := newRig(t)
	write(t, r.dir, "compose.yml", "services:\n  x:\n    volumes: [/home:/h]\n")
	if code := r.cmd().Compose(context.Background(), []string{"false"}); code != term.ExitFailure || !strings.Contains(r.err.String(), "ccy-teams approve compose "+r.dir) {
		t.Fatalf("no terminal: %v %q", code, r.err.String())
	}
	r.answer = no()
	if code := r.cmd().Compose(context.Background(), []string{"false"}); code != term.ExitFailure || len(r.asked) != 1 {
		t.Fatalf("a no: %v asked %v", code, r.asked)
	}
	if !strings.Contains(r.err.String(), "volumes: [/home:/h]") || !strings.Contains(r.err.String(), "no approval is recorded") {
		t.Fatalf("the question did not show the content and why: %q", r.err.String())
	}
	if code := r.cmd().Compose(context.Background(), []string{"true"}); code != term.ExitFailure {
		t.Fatalf("a no recorded the files: %v", code)
	}
	r.answer = yes()
	if code := r.cmd().Compose(context.Background(), []string{"false"}); code != term.ExitOK || len(r.asked) != 2 {
		t.Fatalf("a yes: %v asked %v %q", code, r.asked, r.err.String())
	}
	if code := r.cmd().Compose(context.Background(), []string{"false"}); code != term.ExitOK || len(r.asked) != 2 {
		t.Fatalf("approved content was asked about again: %v asked %v", code, r.asked)
	}
	write(t, r.dir, "compose.yml", "services:\n  x:\n    volumes: [/:/host]\n")
	r.answer = no()
	if code := r.cmd().Compose(context.Background(), []string{"false"}); code != term.ExitFailure || !strings.Contains(r.err.String(), "changed since") {
		t.Fatalf("changed, a no: %v %q", code, r.err.String())
	}
}
