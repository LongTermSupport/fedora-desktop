package ccyversionbase

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	cwd      = "/repo"
	f44Tip   = "4444444444444444444444444444444444444444"
	f43Tip   = "3333333333333333333333333333333333333333"
	head     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	incoming = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	policy   = "cap = 10\nwarn_days = 3\nfail_days = 7\nexempt = [\"F44\", \"F42\", \"F43\"]\n"
)

// fake is git over a remote whose F44 and F43 tips are given (F42 is absent from origin). on
// says which commits each tip contains; before says which commit is an ancestor of which.
type fake struct {
	t       *testing.T
	merging bool
	on      map[string][]string
	before  map[string]string
	broken  string
	blank   string
	files   map[string]string
}

func (f *fake) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	key := strings.Join(c.Argv, " ")
	if c.Dir != cwd {
		f.t.Errorf("%q run in %q, not the checkout", key, c.Dir)
	}
	if res, faulty := f.fault(key); faulty {
		return res, nil
	}
	if res, isBlob := f.blob(key); isBlob {
		return res, nil
	}
	switch key {
	case "git rev-parse --verify --quiet MERGE_HEAD^{commit}":
		if f.merging {
			return proc.Result{Stdout: incoming + "\n"}, nil
		}
		return proc.Result{Code: 1}, nil
	case "git rev-parse --verify HEAD^{commit}":
		return proc.Result{Stdout: head + "\n"}, nil
	case "git rev-parse --verify --quiet refs/remotes/origin/F44^{commit}":
		return proc.Result{Stdout: f44Tip + "\n"}, nil
	case "git rev-parse --verify --quiet refs/remotes/origin/F43^{commit}":
		return proc.Result{Stdout: f43Tip + "\n"}, nil
	case "git rev-parse --verify --quiet refs/remotes/origin/F42^{commit}":
		return proc.Result{Code: 1}, nil
	}
	if pair, ok := strings.CutPrefix(key, "git merge-base --is-ancestor "); ok {
		commit, other, found := strings.Cut(pair, " ")
		if found && (slices.Contains(f.on[other], commit) || f.before[commit] == other) {
			return proc.Result{}, nil
		}
		return proc.Result{Code: 1}, nil
	}
	f.t.Errorf("unexpected command %q", key)
	return proc.Result{Code: 2, Stderr: "unexpected"}, nil
}

// fault is the answer of a command the test has broken (exit 128) or blanked (exit 0, no output).
func (f *fake) fault(key string) (proc.Result, bool) {
	switch {
	case f.broken != "" && strings.Contains(key, f.broken):
		return proc.Result{Code: 128, Stderr: "fatal: broken"}, true
	case f.blank != "" && strings.Contains(key, f.blank):
		return proc.Result{}, true
	}
	return proc.Result{}, false
}

// blob answers git's questions about a file at a revision, from files keyed "rev:path" (the
// index is ":path"); a key the map lacks names nothing.
func (f *fake) blob(key string) (proc.Result, bool) {
	if spec, ok := strings.CutPrefix(key, "git rev-parse --verify --quiet "); ok && strings.Contains(spec, ":") {
		if _, there := f.files[spec]; there {
			return proc.Result{Stdout: "blob " + spec + "\n"}, true
		}
		return proc.Result{Code: 1}, true
	}
	if spec, ok := strings.CutPrefix(key, "git cat-file blob blob "); ok {
		return proc.Result{Stdout: f.files[spec]}, true
	}
	return proc.Result{}, false
}

type rig struct {
	git      *fake
	out, err bytes.Buffer
	policy   string
}

func (r *rig) readFile(path string) ([]byte, error) {
	if path != filepath.Join(cwd, branchhygiene.PolicyFile) {
		return nil, fs.ErrNotExist
	}
	if r.policy == "" {
		return nil, errors.New("permission denied")
	}
	return []byte(r.policy), nil
}

func newRig(t *testing.T, merging bool, on map[string][]string) *rig {
	t.Helper()
	return &rig{git: &fake{t: t, merging: merging, on: on, before: map[string]string{}}, policy: policy}
}

func (r *rig) run(t *testing.T, args ...string) ExitCode {
	t.Helper()
	return Main(context.Background(), args, Env{
		Stdout: &r.out, Stderr: &r.err, Runner: r.git, Cwd: cwd, ReadFile: r.readFile,
	})
}

// B1: no merge in progress: the commit is judged against HEAD, as it always was.
func TestOutsideAMergeTheBaseIsHEAD(t *testing.T) {
	r := newRig(t, false, nil)
	if code := r.run(t); code != ExitOK || r.out.String() != "HEAD\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.out.String(), r.err.String())
	}
}

// B2: a merge with one parent on a protected line is judged against that parent, whichever side
// it is on: F44 merged into a work branch, or a work branch merged onto F44.
func TestAMergeIsJudgedAgainstItsParentOnTheProtectedLine(t *testing.T) {
	cases := []struct {
		name string
		on   map[string][]string
		want string
	}{
		{"F44 merged in", map[string][]string{f44Tip: {incoming}}, incoming},
		{"merged onto F44", map[string][]string{f44Tip: {head}}, head},
		{"onto an older F43", map[string][]string{f43Tip: {incoming}}, incoming},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, true, c.on)
			if code := r.run(t); code != ExitOK || r.out.String() != c.want+"\n" {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, r.out.String(), r.err.String())
			}
		})
	}
}

// B3: both parents on the protected line: the later one, which already holds the earlier.
func TestBothParentsOnTheLineTheLaterIsTheBase(t *testing.T) {
	r := newRig(t, true, map[string][]string{f44Tip: {head, incoming}})
	r.git.before[head] = incoming
	if code := r.run(t); code != ExitOK || r.out.String() != incoming+"\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.out.String(), r.err.String())
	}
}

// B3, the other way round: HEAD is the later parent.
func TestBothParentsOnTheLineHEADCanBeTheLater(t *testing.T) {
	r := newRig(t, true, map[string][]string{f44Tip: {head, incoming}})
	r.git.before[incoming] = head
	if code := r.run(t); code != ExitOK || r.out.String() != head+"\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.out.String(), r.err.String())
	}
}

// B4: two work branches merged: no parent is a release, so no bump is owed until the work lands
// on a protected line. An empty line says there is nothing to judge.
func TestAMergeOfTwoWorkBranchesHasNoBase(t *testing.T) {
	r := newRig(t, true, map[string][]string{})
	if code := r.run(t); code != ExitOK || r.out.String() != "\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.out.String(), r.err.String())
	}
}

// B5: two protected-line parents, neither holding the other (two lines merged): no one base,
// refused rather than guessed.
func TestTwoDivergedLineParentsAreRefused(t *testing.T) {
	r := newRig(t, true, map[string][]string{f44Tip: {head}, f43Tip: {incoming}})
	if code := r.run(t); code != ExitCannotRun || !strings.Contains(r.err.String(), "neither") {
		t.Fatalf("exit %d, stderr %q", code, r.err.String())
	}
}

// B6: a git that fails, or a policy that cannot be read, cannot judge: exit 2, never a base.
func TestAFailureCannotJudge(t *testing.T) {
	for name, mutate := range map[string]func(*rig){
		"git fails":            func(r *rig) { r.git.broken = "merge-base" },
		"HEAD unreadable":      func(r *rig) { r.git.broken = "HEAD^{commit}" },
		"a line unreadable":    func(r *rig) { r.git.broken = "origin/F43" },
		"policy unreadable":    func(r *rig) { r.policy = "" },
		"HEAD names nothing":   func(r *rig) { r.git.blank = "verify HEAD^{commit}" },
		"a tip prints nothing": func(r *rig) { r.git.blank = "origin/F44" },
		"a blob unreadable":    func(r *rig) { r.tree("", "1.0", "1.0"); r.git.broken = "cat-file" },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, true, map[string][]string{f44Tip: {incoming}})
			mutate(r)
			if code := r.run(t); code != ExitCannotRun || r.out.Len() != 0 {
				t.Fatalf("exit %d, stdout %q", code, r.out.String())
			}
		})
	}
}

func TestUsage(t *testing.T) {
	r := newRig(t, false, nil)
	if code := r.run(t, "--help"); code != ExitOK || !strings.Contains(r.out.String(), "usage: qa-ccy-version-base") {
		t.Fatalf("exit %d: %q", code, r.out.String())
	}
	r = newRig(t, false, nil)
	if code := r.run(t, "stray"); code != ExitUsage {
		t.Fatalf("exit %d", code)
	}
}

const (
	launcher   = "files/var/local/claude-yolo/claude-yolo"
	dockerfile = "files/var/local/claude-yolo/Dockerfile"
)

func versions(ccy, image string) (string, string) {
	return "#!/usr/bin/env bash\nCCY_VERSION=\"" + ccy + "\"  # a note\nCCY_VERSION=\"9.9.9\"\n",
		"FROM scratch\nLABEL claude-yolo-version=\"" + image + "\"\n"
}

// tree puts the launcher and Dockerfile at rev with the given versions.
func (r *rig) tree(rev, ccy, image string) {
	if r.git.files == nil {
		r.git.files = map[string]string{}
	}
	r.git.files[rev+":"+launcher], r.git.files[rev+":"+dockerfile] = versions(ccy, image)
}

// B7: a merge's staged versions are at least every parent's: a version lower than a parent's
// would ship that parent's release under an older name. The first version line is the one read.
func TestAMergeBelowAParentIsRefused(t *testing.T) {
	cases := []struct {
		name               string
		staged, head, inc  [2]string
		refused, mentioned string
	}{
		{"above both", [2]string{"3.122.10", "2.84.20"}, [2]string{"3.122.9", "2.84.20"}, [2]string{"3.122.2", "2.84.3"}, "", ""},
		{"equal to the higher", [2]string{"3.122.9", "2.84.20"}, [2]string{"3.122.9", "2.84.20"}, [2]string{"3.122.2", "2.84.20"}, "", ""},
		{"ccy below the incoming", [2]string{"3.122.9", "2.84.20"}, [2]string{"3.122.9", "2.84.20"}, [2]string{"3.122.10", "2.84.20"}, "CCY_VERSION 3.122.9", incoming},
		{"container below HEAD", [2]string{"3.122.9", "2.84.3"}, [2]string{"3.122.9", "2.84.20"}, [2]string{"3.122.9", "2.84.3"}, "claude-yolo-version 2.84.3", head},
		{"a longer version is later", [2]string{"3.122", "2.84"}, [2]string{"3.122.1", "2.84"}, [2]string{"3.122", "2.84"}, "CCY_VERSION 3.122", head},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, true, map[string][]string{f44Tip: {incoming}})
			r.tree("", c.staged[0], c.staged[1])
			r.tree(head, c.head[0], c.head[1])
			r.tree(incoming, c.inc[0], c.inc[1])
			code := r.run(t)
			if c.refused == "" {
				if code != ExitOK || r.out.String() != incoming+"\n" {
					t.Fatalf("exit %d, stdout %q, stderr %q", code, r.out.String(), r.err.String())
				}
				return
			}
			if code != ExitRefused || r.out.Len() != 0 || !strings.Contains(r.err.String(), c.refused) || !strings.Contains(r.err.String(), c.mentioned) {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, r.out.String(), r.err.String())
			}
		})
	}
}

// B7 holds for a merge of two work branches too, the one rule such a merge has.
func TestTwoWorkBranchesBelowAParentAreRefused(t *testing.T) {
	r := newRig(t, true, map[string][]string{})
	r.tree("", "1.0.1", "1.0")
	r.tree(head, "1.0.1", "1.0")
	r.tree(incoming, "1.0.2", "1.0")
	if code := r.run(t); code != ExitRefused {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.out.String(), r.err.String())
	}
}

// A file a side lacks, or a file with no version line, gives that side no version to be below.
func TestAParentWithoutTheFileSetsNoFloor(t *testing.T) {
	r := newRig(t, true, map[string][]string{f44Tip: {incoming}})
	r.tree("", "1.0.0", "1.0")
	r.tree(head, "9.0.0", "9.0")
	delete(r.git.files, head+":"+launcher)
	r.git.files[head+":"+dockerfile] = "FROM scratch\n"
	if code := r.run(t); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, r.err.String())
	}
}

// A version that is not dotted numbers cannot be ordered: exit 2, never a guess.
func TestAnUnorderableVersionCannotJudge(t *testing.T) {
	for _, bad := range []string{"3.x.1", "", "3..1"} {
		r := newRig(t, true, map[string][]string{f44Tip: {incoming}})
		r.tree("", "1.0.0", "1.0")
		r.tree(head, bad, "1.0")
		if code := r.run(t); code != ExitCannotRun || r.out.Len() != 0 {
			t.Fatalf("%q: exit %d, stdout %q", bad, code, r.out.String())
		}
	}
}
