package tarstate

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

func stateOf(t *testing.T, raw []byte) *State {
	t.Helper()
	s, err := Read(bytes.NewReader(raw), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// fileData is the content of a file member; the member must exist.
func fileData(t *testing.T, s *State, name string) string {
	t.Helper()
	data, ok := s.File(name)
	if !ok {
		t.Fatalf("no file member %q in %q", name, s.Names())
	}
	return string(data)
}

// walk lists every path below root (relative, slash separated), sorted.
func walk(t *testing.T, root string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel != "." {
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	return names
}

func TestExtractWritesTheValidatedStateExactly(t *testing.T) {
	s := stateOf(t, build(t, good()...))
	stage := t.TempDir()
	if err := s.Extract(stage, nil); err != nil {
		t.Fatal(err)
	}
	want := s.Names()
	slices.Sort(want)
	if got := walk(t, stage); !slices.Equal(got, want) {
		t.Fatalf("extracted %q, validated %q", got, want)
	}
	for _, name := range want {
		info, err := os.Lstat(filepath.Join(stage, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.IsDir() {
			if info.Mode().Perm() != 0o700 {
				t.Errorf("%s mode %o, want 0700", name, info.Mode().Perm())
			}
			continue
		}
		if info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
			t.Errorf("%s mode %v, want a 0600 regular file", name, info.Mode())
		}
		got, err := os.ReadFile(filepath.Clean(filepath.Join(stage, name)))
		if err != nil || string(got) != fileData(t, s, name) {
			t.Errorf("%s content %q, %v", name, got, err)
		}
	}
	info, err := os.Stat(stage)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("stage mode %v, %v; want 0700", info.Mode().Perm(), err)
	}
}

// The mode in the tar is never used: a world-writable member is extracted 0600.
func TestExtractIgnoresTheModesInTheTar(t *testing.T) {
	w := file("secrets/w", "k")
	w.hdr.Mode = 0o777
	x := file("room_id", "r")
	x.hdr.Mode = 0o666
	d := dir("secrets/open")
	d.hdr.Mode = 0o777
	s := stateOf(t, build(t, file("team.json", "{}"), dir("secrets"), w, x, d))
	stage := t.TempDir()
	if err := s.Extract(stage, nil); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]fs.FileMode{"secrets/w": 0o600, "room_id": 0o600, "secrets/open": 0o700} {
		info, err := os.Lstat(filepath.Join(stage, name))
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v, %v; want %o", name, info, err, want)
		}
	}
}

func TestExtractCreatesImpliedParents(t *testing.T) {
	s := stateOf(t, build(t, file("team.json", "{}"), dir("secrets"), file("secrets/a/b/c", "k")))
	stage := t.TempDir()
	if err := s.Extract(stage, nil); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"secrets/a", "secrets/a/b"} {
		info, err := os.Lstat(filepath.Join(stage, d))
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v, %v", d, info, err)
		}
	}
}

func TestExtractSetsTheOwner(t *testing.T) {
	cur, err := fsx.CurrentOwner()
	if err != nil {
		t.Fatal(err)
	}
	s := stateOf(t, build(t, good()...))
	stage := t.TempDir()
	if err := s.Extract(stage, &cur); err != nil {
		t.Fatal(err)
	}
	for _, name := range append(s.Names(), ".") {
		info, err := os.Lstat(filepath.Join(stage, name))
		if err != nil {
			t.Fatal(err)
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(st.Uid) != cur.UID || int(st.Gid) != cur.GID {
			t.Errorf("%s owner %v, want %+v", name, info.Sys(), cur)
		}
	}
}

func TestExtractRefusesABadStage(t *testing.T) {
	s := stateOf(t, build(t, good()...))
	base := t.TempDir()

	t.Run("missing", func(t *testing.T) {
		if err := s.Extract(filepath.Join(base, "absent"), nil); err == nil {
			t.Fatal("extracted into a directory that does not exist")
		}
	})
}

func TestExtractRefusesAFileAsStage(t *testing.T) {
	s := stateOf(t, build(t, good()...))
	base := t.TempDir()
	t.Run("a file", func(t *testing.T) {
		f := filepath.Join(base, "file")
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := s.Extract(f, nil); err == nil {
			t.Fatal("extracted into a file")
		}
	})
}

func TestExtractRefusesANonEmptyStage(t *testing.T) {
	s := stateOf(t, build(t, good()...))
	base := t.TempDir()
	t.Run("not empty", func(t *testing.T) {
		stage := filepath.Join(base, "full")
		if err := os.Mkdir(stage, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "keep"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := s.Extract(stage, nil)
		if err == nil || !strings.Contains(err.Error(), "not empty") {
			t.Fatalf("error %v", err)
		}
		if got := walk(t, stage); !slices.Equal(got, []string{"keep"}) {
			t.Fatalf("stage changed: %q", got)
		}
	})
}

func TestExtractRefusesASymlinkAsStage(t *testing.T) {
	s := stateOf(t, build(t, good()...))
	base := t.TempDir()
	t.Run("a symlink to a directory", func(t *testing.T) {
		target := filepath.Join(base, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(base, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		err := s.Extract(link, nil)
		if err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("error %v", err)
		}
		if got := walk(t, target); len(got) != 0 {
			t.Fatalf("wrote through the symlink: %q", got)
		}
	})
}

// State has unexported fields, so a hostile name cannot be put in one from outside; this
// builds one by hand to show that Extract still cannot leave the stage.
func TestExtractCannotLeaveTheStageEvenForAnUnvalidatedName(t *testing.T) {
	for _, name := range []string{"../escape", "secrets/../../escape", "/tmp/escape-absolute", "a/../../escape"} {
		t.Run(name, func(t *testing.T) {
			outside := t.TempDir()
			stage := filepath.Join(outside, "stage")
			if err := os.Mkdir(stage, 0o700); err != nil {
				t.Fatal(err)
			}
			s := &State{entries: []entry{{name: name, data: []byte("x")}}, index: map[string]int{name: 0}}
			if err := s.Extract(stage, nil); err == nil {
				t.Fatal("extracted a name that leaves the stage")
			}
			if _, err := os.Lstat(filepath.Join(outside, "escape")); err == nil {
				t.Fatal("a file escaped the stage")
			}
			if _, err := os.Lstat("/tmp/escape-absolute"); err == nil {
				t.Fatal("an absolute name was written")
			}
		})
	}
}

// Two entries for one path (only possible in a hand-built State) never overwrite: files
// are created exclusively.
func TestExtractCreatesFilesExclusively(t *testing.T) {
	s := &State{entries: []entry{{name: "f", data: []byte("1")}, {name: "f", data: []byte("2")}}, index: map[string]int{"f": 1}}
	stage := t.TempDir()
	if err := s.Extract(stage, nil); err == nil {
		t.Fatal("second create of the same path succeeded")
	}
}

// The extracted set is the validated set for the tars whose headers two parsers read
// differently as well: a long name, and a second archive after the end marker.
func TestExtractedIsValidatedForAwkwardHeaders(t *testing.T) {
	long := "secrets/" + strings.Repeat("l", 130)
	raw := append(build(t, file("team.json", "{}"), dir("secrets"), file(long, "k")), build(t, file("etc/evil", "x"))...)
	s := stateOf(t, raw)
	stage := t.TempDir()
	if err := s.Extract(stage, nil); err != nil {
		t.Fatal(err)
	}
	want := s.Names()
	slices.Sort(want)
	if got := walk(t, stage); !slices.Equal(got, want) {
		t.Fatalf("extracted %q, validated %q", got, want)
	}
	if _, err := os.Lstat(filepath.Join(stage, "etc")); err == nil {
		t.Fatal("the appended archive was extracted")
	}
}
