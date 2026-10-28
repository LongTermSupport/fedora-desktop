package containerimages

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// tagsFormat is the listing `before` and `replaced` ask for (engine.RepositoryTags).
var tagsFormat = []string{"images", "--no-trunc", "--format", "{{.Repository}}:{{.Tag}} {{.ID}}"}

// shelf is one engine's store as the self-prune sees it: the repository's tags, and the image
// each container (running or stopped) was made from.
type shelf struct {
	t        *testing.T
	engine   string
	fake     *execx.Fake
	home     string
	env      map[string]string
	out, err bytes.Buffer
	// tags is the `images` listing, "<name> sha256:<id>" a line.
	tags string
	// users is each container's image id; inspect fails once when inspectRace is set.
	users       []string
	inspectRace bool
	// refuse is the ids whose removal the engine refuses, with its message.
	refuse   map[string]string
	scripted bool
}

func newShelf(t *testing.T) *shelf {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	markStoreClean(t, home)
	env := map[string]string{"XDG_RUNTIME_DIR": filepath.Join(t.TempDir(), "run")}
	return &shelf{t: t, engine: "podman", fake: &execx.Fake{}, home: home, env: env, refuse: map[string]string{}}
}

// buildLock is the path of a ccy image build lock in the shelf's runtime directory.
func (s *shelf) buildLock(name string) string {
	return filepath.Join(s.env["XDG_RUNTIME_DIR"], "ccy", "image-builds", name)
}

func (s *shelf) script() {
	if s.scripted {
		return
	}
	s.scripted = true
	e := s.engine
	s.fake.OnFunc(append([]string{e}, tagsFormat...), func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(s.tags)}, nil
	})
	ctrIDs := make([]string, 0, len(s.users))
	for i := range s.users {
		ctrIDs = append(ctrIDs, "ctr"+string(rune('a'+i)))
	}
	s.fake.On([]string{e, "ps", "--all", "--quiet", "--no-trunc"}, execx.Result{Stdout: []byte(strings.Join(ctrIDs, "\n") + "\n")})
	raced := false
	s.fake.OnFunc([]string{e, "container", "inspect"}, func(execx.Cmd) (execx.Result, error) {
		if s.inspectRace && !raced {
			raced = true
			return execx.Result{ExitCode: 125, Stderr: []byte("Error: no such container ctra\n")}, nil
		}
		var out []string
		for _, u := range s.users {
			out = append(out, "sha256:"+u)
		}
		return execx.Result{Stdout: []byte(strings.Join(out, "\n") + "\n")}, nil
	})
	s.fake.OnFunc([]string{e, "image", "rm"}, func(c execx.Cmd) (execx.Result, error) {
		if msg, ok := s.refuse[c.Args[len(c.Args)-1]]; ok {
			return execx.Result{ExitCode: 2, Stderr: []byte(msg)}, nil
		}
		return execx.Result{Stdout: []byte("Deleted: " + c.Args[len(c.Args)-1] + "\n")}, nil
	})
}

func (s *shelf) run(args ...string) term.ExitCode {
	s.t.Helper()
	s.script()
	app := App{Env: Env{
		Runner: s.fake,
		Getenv: func(k string) string { return s.env[k] },
		LookPath: func(name string) (string, error) {
			if name == "docker" && s.env["NO_DOCKER"] != "" {
				return "", exec.ErrNotFound
			}
			return "/usr/bin/" + name, nil
		},
		Now:    func() time.Time { return now },
		Home:   s.home,
		Stdout: &s.out,
		Stderr: &s.err,
	}}
	return app.Run(context.Background(), args)
}

// record is what `before` printed, written where `replaced` reads it.
func (s *shelf) record(text string) string {
	s.t.Helper()
	path := filepath.Join(s.t.TempDir(), "before")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		s.t.Fatal(err)
	}
	return path
}

func (s *shelf) removed() []string {
	var got []string
	for _, c := range s.fake.Calls() {
		if len(c.Args) >= 3 && c.Args[0] == "image" && c.Args[1] == "rm" {
			got = append(got, c.Args[2])
		}
	}
	return got
}

func (s *shelf) logText() string {
	s.t.Helper()
	text, err := os.ReadFile(filepath.Join(s.home, ".local", "state", "fedora-desktop", "container-images.log"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return string(text)
}

func TestBeforePrintsEachTagWithItsImage(t *testing.T) {
	s := newShelf(t)
	s.tags = "localhost/claude-yolo:latest sha256:" + id("a") + "\nlocalhost/claude-yolo:base sha256:" + id("a") +
		"\n<none>:<none> sha256:" + id("f") + "\nlocalhost/claude-yolo:proj sha256:" + id("b") + "\n"
	if code := s.run("before", "claude-yolo"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, s.err.String())
	}
	want := "localhost/claude-yolo:latest " + id("a") + "\nlocalhost/claude-yolo:base " + id("a") + "\nlocalhost/claude-yolo:proj " + id("b") + "\n"
	if s.out.String() != want {
		t.Fatalf("stdout %q, want %q", s.out.String(), want)
	}
	if !s.fake.Called("podman images --no-trunc --format {{.Repository}}:{{.Tag}} {{.ID}} claude-yolo") {
		t.Fatalf("calls %v", s.fake.Calls())
	}
}

func TestBeforeOfARepositoryWithNoImagePrintsNothing(t *testing.T) {
	s := newShelf(t)
	if code := s.run("before", "claude-yolo"); code != term.ExitOK || s.out.String() != "" {
		t.Fatalf("exit %d, stdout %q", code, s.out.String())
	}
}

// A rebuild of :latest re-tags :base too: both old ids were recorded under the one lock, and
// the one image they named goes once.
func TestTheImageABuildReplacedIsRemovedAndLogged(t *testing.T) {
	s := newShelf(t)
	before := s.record("localhost/claude-yolo:latest " + id("a") + "\nlocalhost/claude-yolo:base " + id("a") + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("b") + "\nlocalhost/claude-yolo:base sha256:" + id("b") + "\n"
	if code := s.run("replaced", "claude-yolo", before); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, s.err.String())
	}
	if got := s.removed(); len(got) != 1 || got[0] != id("a") {
		t.Fatalf("removed %v", got)
	}
	if !strings.Contains(s.err.String(), "removed image aaaaaaaaaaaa (was localhost/claude-yolo:latest, localhost/claude-yolo:base): replaced by this build") {
		t.Fatalf("stderr %s", s.err.String())
	}
	if s.out.String() != "" {
		t.Fatalf("stdout %q: the report is stderr's", s.out.String())
	}
	log := s.logText()
	if !strings.Contains(log, "\timage\taaaaaaaaaaaa\t"+id("a")+"\t") || !strings.Contains(log, "replaced by a build of claude-yolo") {
		t.Fatalf("log %q", log)
	}
}

// Agent teams run two containers of one project on one image; a stopped container still holds
// its image too.
func TestAnImageAContainerUsesIsKept(t *testing.T) {
	for name, users := range map[string][]string{
		"two containers of one project": {id("a"), id("a")},
		"a stopped container":           {id("c"), id("a")},
	} {
		t.Run(name, func(t *testing.T) {
			s := newShelf(t)
			s.users = users
			before := s.record("localhost/claude-yolo:proj " + id("a") + "\n")
			s.tags = "localhost/claude-yolo:proj sha256:" + id("b") + "\n"
			if code := s.run("replaced", "claude-yolo", before); code != term.ExitOK {
				t.Fatalf("exit %d: %s", code, s.err.String())
			}
			if got := s.removed(); len(got) != 0 {
				t.Fatalf("removed %v", got)
			}
			if !strings.Contains(s.err.String(), "kept image aaaaaaaaaaaa (was localhost/claude-yolo:proj): a container uses it") {
				t.Fatalf("stderr %s", s.err.String())
			}
			if s.logText() != "" {
				t.Fatalf("log %q", s.logText())
			}
		})
	}
}

// :base is a second tag of the image :latest's build made; when only :latest moves, the old
// image is still :base and stays.
func TestAnImageAnotherTagStillNamesIsKept(t *testing.T) {
	s := newShelf(t)
	before := s.record("localhost/claude-yolo:latest " + id("a") + "\nlocalhost/claude-yolo:base " + id("a") + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("b") + "\nlocalhost/claude-yolo:base sha256:" + id("a") + "\n"
	if code := s.run("replaced", "claude-yolo", before); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, s.err.String())
	}
	if got := s.removed(); len(got) != 0 {
		t.Fatalf("removed %v", got)
	}
	if !strings.Contains(s.err.String(), "kept image aaaaaaaaaaaa (was localhost/claude-yolo:latest, localhost/claude-yolo:base): still named by localhost/claude-yolo:base") {
		t.Fatalf("stderr %s", s.err.String())
	}
}

// A build that changed nothing (a full cache hit), and every other project's tag recorded with
// it, leave the same ids under the same tags: nothing to say, nothing removed.
func TestAnUnchangedImageIsPassedOverInSilence(t *testing.T) {
	s := newShelf(t)
	list := "localhost/claude-yolo:latest " + id("a") + "\nlocalhost/claude-yolo:other " + id("c") + "\n"
	before := s.record(list + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("a") + "\nlocalhost/claude-yolo:other sha256:" + id("c") + "\n"
	if code := s.run("replaced", "claude-yolo", before); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, s.err.String())
	}
	if len(s.removed()) != 0 || s.err.String() != "" {
		t.Fatalf("removed %v; stderr %q", s.removed(), s.err.String())
	}
	if s.fake.Called("podman ps") {
		t.Fatalf("asked for the containers with nothing to judge: %v", s.fake.Calls())
	}
}

// An old :latest a project image is built on has a child: the engine refuses, the launch goes
// on, and the daily prune removes it once nothing holds it.
func TestARemovalTheEngineRefusesIsReportedAndNotFatal(t *testing.T) {
	s := newShelf(t)
	s.refuse[id("a")] = "Error: image used by " + id("e") + ": image is in use by a container: consider listing external containers\n"
	before := s.record("localhost/claude-yolo:latest " + id("a") + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("b") + "\n"
	if code := s.run("replaced", "claude-yolo", before); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, s.err.String())
	}
	if !strings.Contains(s.err.String(), "could not remove image aaaaaaaaaaaa (was localhost/claude-yolo:latest)") ||
		!strings.Contains(s.err.String(), "image is in use") || !strings.Contains(s.err.String(), "daily prune") {
		t.Fatalf("stderr %s", s.err.String())
	}
	if s.logText() != "" {
		t.Fatalf("a refused removal was logged: %q", s.logText())
	}
}

// The locks are per image and the record is the whole repository: while another build of it
// runs (a project image built on the old :latest), nothing is removed, and the daily prune
// removes it later. The caller's own lock, named by --lock, is no other build.
func TestNothingIsRemovedWhileAnotherBuildOfTheRepositoryHoldsItsLock(t *testing.T) {
	s := newShelf(t)
	own := s.buildLock("claude-yolo_latest.lock")
	holdLock(t, own)
	holdLock(t, s.buildLock("claude-yolo_proj.lock"))
	before := s.record("localhost/claude-yolo:latest " + id("a") + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("b") + "\n"
	if code := s.run("replaced", "--lock", own, "claude-yolo", before); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, s.err.String())
	}
	if got := s.removed(); len(got) != 0 {
		t.Fatalf("removed %v while another build ran", got)
	}
	if !strings.Contains(s.err.String(), "kept image aaaaaaaaaaaa (was localhost/claude-yolo:latest): another ccy image build is running (claude-yolo_proj.lock is held); the daily prune") {
		t.Fatalf("stderr %s", s.err.String())
	}
	if s.logText() != "" {
		t.Fatalf("log %q", s.logText())
	}
}

func TestTheCallersOwnBuildLockDoesNotStopTheRemoval(t *testing.T) {
	s := newShelf(t)
	own := s.buildLock("claude-yolo_latest.lock")
	holdLock(t, own)
	before := s.record("localhost/claude-yolo:latest " + id("a") + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("b") + "\n"
	if code := s.run("replaced", "--lock", own, "claude-yolo", before); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, s.err.String())
	}
	if got := s.removed(); len(got) != 1 || got[0] != id("a") {
		t.Fatalf("removed %v; stderr %s", got, s.err.String())
	}
	s = newShelf(t)
	holdLock(t, s.buildLock("claude-yolo_latest.lock"))
	before = s.record("localhost/claude-yolo:latest " + id("a") + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("b") + "\n"
	if code := s.run("replaced", "claude-yolo", before); code != term.ExitOK || len(s.removed()) != 0 {
		t.Fatalf("with no --lock every held lock is another build's: exit %d, removed %v", code, s.removed())
	}
}

func TestABuildLockItCannotProbeStopsTheRunBeforeARemoval(t *testing.T) {
	s := newShelf(t)
	if err := os.MkdirAll(s.buildLock("odd.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	before := s.record("localhost/claude-yolo:latest " + id("a") + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("b") + "\n"
	if code := s.run("replaced", "claude-yolo", before); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if len(s.removed()) != 0 || !strings.Contains(s.err.String(), "odd.lock") {
		t.Fatalf("removed %v; stderr %s", s.removed(), s.err.String())
	}
}

// A container that goes between the listing and the inspect (an agent team's --rm member) is
// a race, not a broken engine: the pair is asked once more.
func TestAContainerThatGoesMidQuestionIsAskedAgain(t *testing.T) {
	s := newShelf(t)
	s.users = []string{id("a")}
	s.inspectRace = true
	before := s.record("localhost/claude-yolo:proj " + id("a") + "\n")
	s.tags = "localhost/claude-yolo:proj sha256:" + id("b") + "\n"
	if code := s.run("replaced", "claude-yolo", before); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, s.err.String())
	}
	if len(s.removed()) != 0 {
		t.Fatalf("removed %v", s.removed())
	}
}

func TestListsTheEngineCannotGiveFailTheRunBeforeAnyRemoval(t *testing.T) {
	for name, prefix := range map[string][]string{
		"tags":       append([]string{"podman"}, tagsFormat...),
		"containers": {"podman", "ps"},
		"inspect":    {"podman", "container", "inspect"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newShelf(t)
			s.users = []string{id("c")}
			s.script()
			s.fake.On(prefix, execx.Result{ExitCode: 125, Stderr: []byte("Error: boom\n")})
			before := s.record("localhost/claude-yolo:latest " + id("a") + "\n")
			if code := s.run("replaced", "claude-yolo", before); code != term.ExitFailure {
				t.Fatalf("exit %d", code)
			}
			if len(s.removed()) != 0 || !strings.Contains(s.err.String(), "boom") {
				t.Fatalf("removed %v; stderr %s", s.removed(), s.err.String())
			}
		})
	}
	s := newShelf(t)
	s.script()
	s.fake.On(append([]string{"podman"}, tagsFormat...), execx.Result{ExitCode: 125, Stderr: []byte("Error: boom\n")})
	if code := s.run("before", "claude-yolo"); code != term.ExitFailure || s.out.String() != "" {
		t.Fatalf("before: exit %d, stdout %q", code, s.out.String())
	}
}

func TestARecordItCannotReadFailsTheRunBeforeAnyQuestion(t *testing.T) {
	for name, text := range map[string]string{
		"not an id":      "localhost/claude-yolo:latest --force\n",
		"a short id":     "localhost/claude-yolo:latest abc123\n",
		"no name and id": "justone\n",
	} {
		t.Run(name, func(t *testing.T) {
			s := newShelf(t)
			if code := s.run("replaced", "claude-yolo", s.record(text)); code != term.ExitFailure {
				t.Fatalf("exit %d", code)
			}
			if len(s.fake.Calls()) != 0 || !strings.Contains(s.err.String(), "line 1") {
				t.Fatalf("calls %v; stderr %s", s.fake.Calls(), s.err.String())
			}
		})
	}
	s := newShelf(t)
	if code := s.run("replaced", "claude-yolo", filepath.Join(t.TempDir(), "missing")); code != term.ExitFailure || len(s.fake.Calls()) != 0 {
		t.Fatalf("a missing record: exit %d, calls %v", code, s.fake.Calls())
	}
}

func TestALogThatCannotBeOpenedStopsTheRunBeforeARemoval(t *testing.T) {
	s := newShelf(t)
	if err := os.MkdirAll(filepath.Join(s.home, ".local", "state", "fedora-desktop", "container-images.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	before := s.record("localhost/claude-yolo:latest " + id("a") + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("b") + "\n"
	if code := s.run("replaced", "claude-yolo", before); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if len(s.removed()) != 0 || !strings.Contains(s.err.String(), "removal log") {
		t.Fatalf("removed %v; stderr %s", s.removed(), s.err.String())
	}
}

// While the store is not confirmed free of old-range files a removal could break it until root
// repairs it, so the self-prune removes nothing, says so once, and the launch goes on (R7).
func TestNothingIsReplacedWhileTheStoreIsNotConfirmedClean(t *testing.T) {
	s := newShelf(t)
	unconfirmStore(t, s.home)
	before := s.record("localhost/claude-yolo:latest " + id("a") + "\nlocalhost/claude-yolo:proj " + id("c") + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("b") + "\n"
	if code := s.run("replaced", "claude-yolo", before); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, s.err.String())
	}
	if got := s.removed(); len(got) != 0 {
		t.Fatalf("removed %v from an unconfirmed store", got)
	}
	want := "container-images: " + unconfirmedSays + "; nothing removed; run play-podman.yml\n"
	if s.err.String() != want {
		t.Fatalf("stderr %q, want %q", s.err.String(), want)
	}
	if s.logText() != "" {
		t.Fatalf("log %q", s.logText())
	}
}

func TestAMarkerThatCannotBeReadStopsTheSelfPruneBeforeARemoval(t *testing.T) {
	s := newShelf(t)
	if err := os.RemoveAll(filepath.Join(s.home, ".local")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.home, ".local"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.home, ".local", "state"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	before := s.record("localhost/claude-yolo:latest " + id("a") + "\n")
	s.tags = "localhost/claude-yolo:latest sha256:" + id("b") + "\n"
	if code := s.run("replaced", "claude-yolo", before); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if len(s.removed()) != 0 || !strings.Contains(s.err.String(), "store-clean marker") {
		t.Fatalf("removed %v; stderr %s", s.removed(), s.err.String())
	}
}

// The engine is the flag's, else CCY_CONTAINER_ENGINE's, else podman; docker answers the same
// questions.
func TestTheEngineIsTheFlagsElseTheVariablesElsePodman(t *testing.T) {
	s := newShelf(t)
	s.engine = "docker"
	s.env["CCY_CONTAINER_ENGINE"] = "docker"
	s.tags = "claude-yolo:latest sha256:" + id("a") + "\n"
	if code := s.run("before", "claude-yolo"); code != term.ExitOK || !s.fake.Called("docker images") {
		t.Fatalf("variable: exit %d; %v; %s", code, s.fake.Calls(), s.err.String())
	}
	s = newShelf(t)
	s.engine = "docker"
	before := s.record("claude-yolo:latest " + id("a") + "\n")
	s.tags = "claude-yolo:latest sha256:" + id("b") + "\n"
	if code := s.run("--engine", "docker", "replaced", "claude-yolo", before); code != term.ExitOK || !s.fake.Called("docker image rm "+id("a")) {
		t.Fatalf("flag: exit %d; %v; %s", code, s.fake.Calls(), s.err.String())
	}
	s = newShelf(t)
	s.env["CCY_CONTAINER_ENGINE"] = "docker"
	s.env["NO_DOCKER"] = "yes"
	if code := s.run("before", "claude-yolo"); code != term.ExitFailure || !strings.Contains(s.err.String(), "not installed") {
		t.Fatalf("missing docker: exit %d; %s", code, s.err.String())
	}
	s = newShelf(t)
	if code := s.run("--engine", "lxc", "before", "claude-yolo"); code != term.ExitUsage || len(s.fake.Calls()) != 0 {
		t.Fatalf("unknown engine: exit %d; %v", code, s.fake.Calls())
	}
}

func TestBeforeAndReplacedUsage(t *testing.T) {
	for _, args := range [][]string{
		{"before"},
		{"before", "a", "b"},
		{"replaced", "claude-yolo"},
		{"replaced", "a", "b", "c"},
		{"--engine"},
		{"--engine", "podman"},
		{"--engine", "podman", "prune"},
		{"before", ""},
		{"replaced", "", "f"},
		{"replaced", "--lock", "", "claude-yolo", "f"},
		{"replaced", "--lock", "l", "claude-yolo"},
		{"replaced", "claude-yolo", "f", "--lock", "l"},
	} {
		s := newShelf(t)
		if code := s.run(args...); code != term.ExitUsage {
			t.Errorf("%v: exit %d, want usage", args, code)
		}
		if len(s.fake.Calls()) != 0 {
			t.Errorf("%v: ran %v", args, s.fake.Calls())
		}
	}
	s := newShelf(t)
	if code := s.run("--help"); code != term.ExitOK || !strings.Contains(s.out.String(), "replaced [--lock <file>] <repository> <ids-file>") {
		t.Fatalf("--help: %s", s.out.String())
	}
}

func TestAnUnwritableStdoutFailsBefore(t *testing.T) {
	s := newShelf(t)
	s.tags = "claude-yolo:latest sha256:" + id("a") + "\n"
	s.script()
	app := App{Env: Env{
		Runner: s.fake, Getenv: func(string) string { return "" }, Now: func() time.Time { return now },
		LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil },
		Home:     s.home, Stdout: brokenWriter{}, Stderr: brokenWriter{},
	}}
	if code := app.Run(context.Background(), []string{"before", "claude-yolo"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	before := s.record("claude-yolo:latest " + id("c") + "\n")
	if code := app.Run(context.Background(), []string{"replaced", "claude-yolo", before}); code != term.ExitFailure {
		t.Fatalf("replaced with an unwritable report: exit %d", code)
	}
}
