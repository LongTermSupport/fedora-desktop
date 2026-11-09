package containerimages

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// fakeFiles records what the re-own does, where only root could do it for real.
type fakeFiles struct {
	chowns  []string
	chmods  []string
	setcaps []string
	removed []string
	caps    map[string][]byte
	fail    string // a path whose chown fails
}

func (f *fakeFiles) ops() fileOps {
	return fileOps{
		lchown: func(path string, uid, gid int) error {
			if path == f.fail {
				return errors.New("operation not permitted")
			}
			f.chowns = append(f.chowns, path+" "+strconv.Itoa(uid)+":"+strconv.Itoa(gid))
			return nil
		},
		chmod: func(path string, mode fs.FileMode) error {
			f.chmods = append(f.chmods, path+" "+mode.String())
			return nil
		},
		getcap: func(path string) ([]byte, error) { return f.caps[path], nil },
		setcap: func(path string, v []byte) error {
			f.setcaps = append(f.setcaps, path+" "+string(v))
			return nil
		},
		removeAll: func(path string) error {
			f.removed = append(f.removed, path)
			return nil
		},
	}
}

// reownWorld is a store of fake entries under /s, as the walk would see them.
func reownWorld(entries map[string][]scanEntry) walkFunc {
	return func(root string, fn func(scanEntry) error) error {
		list, ok := entries[root]
		if !ok {
			return fs.ErrNotExist
		}
		for _, e := range list {
			if err := fn(e); err != nil {
				return err
			}
		}
		return nil
	}
}

func reownApp(t *testing.T, plan Plan, walk walkFunc, files *fakeFiles) (App, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.json")
	text, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(text))
	var out, errOut bytes.Buffer
	return App{Env: Env{Stdout: &out, Stderr: &errOut, EUID: 0, Walk: walk, Files: files.ops()}}, &out, &errOut, path
}

func samplePlan() Plan {
	return Plan{
		Store: "/home/user/.local/share/containers/storage", Owners: owners(), Old: oldRange,
		Reown:      []PlanStep{{Kind: "dir", Key: "overlay/L1", Name: "overlay/L1", Why: "image I1"}},
		RootRemove: []PlanStep{{Kind: kindPath, Key: "overlay/STRAY", Name: "overlay/STRAY", Why: "no record"}},
		Held:       []PlanStep{{Kind: "dir", Key: "overlay/L9", Name: "overlay/L9", Why: "under running container web"}},
	}
}

func sampleWalk() walkFunc {
	return reownWorld(map[string][]scanEntry{
		"/home/user/.local/share/containers/storage/overlay/L1": {
			{rel: ".", uid: 1000, gid: 1000, mode: fs.ModeDir | 0o755},
			{rel: "diff/var/cache/apt/archives/partial", uid: 100099, gid: 100103, mode: fs.ModeDir | 0o700},
			{rel: "diff/usr/bin/passwd", regular: true, uid: 100000, gid: 100000, mode: fs.ModeSetuid | 0o755},
			{rel: "diff/usr/bin/ping", regular: true, uid: 100000, gid: 1000, mode: 0o755},
			{rel: "diff/etc/hosts", regular: true, uid: 524288, gid: 524288, mode: 0o644},
		},
	})
}

// O10: without --yes nothing changes; the dry run names each directory, what it would shift
// and what it would remove.
func TestReownDryRunChangesNothing(t *testing.T) {
	files := &fakeFiles{}
	a, out, errOut, path := reownApp(t, samplePlan(), sampleWalk(), files)
	if code := a.Run(t.Context(), []string{"old-range", "reown", "--plan", path}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if len(files.chowns)+len(files.removed)+len(files.chmods) != 0 {
		t.Fatalf("a dry run changed %v %v %v", files.chowns, files.removed, files.chmods)
	}
	for _, want := range []string{"would re-own overlay/L1: 3 paths", "would remove overlay/STRAY", "held, left: overlay/L9", "a dry run"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
}

// O10: --yes shifts each old-range path to the same offset in the current range, the gid too
// when it is the old range's, puts back a setuid bit and a file capability the chown cleared,
// leaves every other path alone, and removes what root was told to.
func TestReownShiftsAndPutsBackWhatChownClears(t *testing.T) {
	files := &fakeFiles{caps: map[string][]byte{"/home/user/.local/share/containers/storage/overlay/L1/diff/usr/bin/ping": []byte("cap_net_raw")}}
	a, out, errOut, path := reownApp(t, samplePlan(), sampleWalk(), files)
	if code := a.Run(t.Context(), []string{"old-range", "reown", "--plan", path, "--yes"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	wantChowns := []string{
		"/home/user/.local/share/containers/storage/overlay/L1/diff/var/cache/apt/archives/partial 524387:524391",
		"/home/user/.local/share/containers/storage/overlay/L1/diff/usr/bin/passwd 524288:524288",
		"/home/user/.local/share/containers/storage/overlay/L1/diff/usr/bin/ping 524288:1000",
	}
	if !slices.Equal(files.chowns, wantChowns) {
		t.Errorf("chowns = %v, want %v", files.chowns, wantChowns)
	}
	if !slices.Equal(files.chmods, []string{"/home/user/.local/share/containers/storage/overlay/L1/diff/usr/bin/passwd urwxr-xr-x"}) {
		t.Errorf("chmods = %v", files.chmods)
	}
	if !slices.Equal(files.setcaps, []string{"/home/user/.local/share/containers/storage/overlay/L1/diff/usr/bin/ping cap_net_raw"}) {
		t.Errorf("setcaps = %v", files.setcaps)
	}
	if !slices.Equal(files.removed, []string{"/home/user/.local/share/containers/storage/overlay/STRAY"}) {
		t.Errorf("removed = %v", files.removed)
	}
	if !strings.Contains(out.String(), "re-owned overlay/L1: 3 paths") || !strings.Contains(out.String(), "removed overlay/STRAY") {
		t.Errorf("output:\n%s", out)
	}
}

// A path the shift cannot place stops the run there, by name (fail fast); so does a failed chown.
func TestReownStopsAtThePathItCannotShift(t *testing.T) {
	walk := reownWorld(map[string][]scanEntry{"/home/user/.local/share/containers/storage/overlay/L1": {{rel: "x", uid: 65534, gid: 65534, mode: 0o644, regular: true}}})
	files := &fakeFiles{}
	a, _, errOut, path := reownApp(t, samplePlan(), walk, files)
	if code := a.Run(t.Context(), []string{"old-range", "reown", "--plan", path, "--yes"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errOut.String(), "/home/user/.local/share/containers/storage/overlay/L1/x") || len(files.removed) != 0 {
		t.Fatalf("stderr %q, removed %v", errOut, files.removed)
	}
	files = &fakeFiles{fail: "/home/user/.local/share/containers/storage/overlay/L1/diff/usr/bin/passwd"}
	a, _, errOut, path = reownApp(t, samplePlan(), sampleWalk(), files)
	if code := a.Run(t.Context(), []string{"old-range", "reown", "--plan", path, "--yes"}); code != term.ExitFailure || !strings.Contains(errOut.String(), "not permitted") {
		t.Fatalf("a failed chown: exit %d, %s", code, errOut)
	}
}

// O13: the plan root carries out is checked first: root only, the store's own directories only,
// an old range that turns no mapped file into another.
func TestReownRefusesAPlanItCannotTrust(t *testing.T) {
	cases := map[string]func(p *Plan, a *App){
		"not root":            func(_ *Plan, a *App) { a.Env.EUID = 1000 },
		"relative store":      func(p *Plan, _ *App) { p.Store = "s" },
		"escaping key":        func(p *Plan, _ *App) { p.Reown[0].Key = "overlay/../../etc" },
		"a key too deep":      func(p *Plan, _ *App) { p.Reown[0].Key = "overlay/L1/diff" },
		"remove a record dir": func(p *Plan, _ *App) { p.RootRemove[0].Key = "overlay-images/x" },
		"old over current":    func(p *Plan, _ *App) { p.Old = Range{Start: 524288, Count: 5} },
	}
	for name, spoil := range cases {
		p := samplePlan()
		files := &fakeFiles{}
		a, _, errOut, path := reownApp(t, p, sampleWalk(), files)
		spoil(&p, &a)
		text, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, string(text))
		if code := a.Run(t.Context(), []string{"old-range", "reown", "--plan", path, "--yes"}); code != term.ExitFailure {
			t.Errorf("%s: exit %d", name, code)
		}
		if len(files.chowns)+len(files.removed) != 0 {
			t.Errorf("%s: changed %v %v (stderr %s)", name, files.chowns, files.removed, errOut)
		}
	}
}

// The real file operations, on a fixture file: a chown to the file's own owner clears a
// setuid bit as any chown does, and reownPath puts it back.
func TestReownPathPutsBackTheSetuidBitForReal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool")
	writeFile(t, path, "#!/bin/sh\n")
	if err := os.Chmod(path, 0o755|fs.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	e := scanEntry{rel: "tool", regular: true, mode: 0o755 | fs.ModeSetuid}
	if err := reownPath(realFileOps(), path, e, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&fs.ModeSetuid == 0 || info.Mode().Perm() != 0o755 {
		t.Fatalf("mode after the re-own = %v", info.Mode())
	}
	caps, err := realFileOps().getcap(path)
	if err != nil || caps != nil {
		t.Fatalf("a file with no capability read as %q, %v", caps, err)
	}
}
