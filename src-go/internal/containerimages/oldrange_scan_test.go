package containerimages

import (
	"bytes"
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestDirKeyIsTheStoreDirectoryAPathBelongsTo(t *testing.T) {
	for rel, want := range map[string]string{
		"overlay/abc/diff/var/cache/apt/archives/partial": "overlay/abc",
		"overlay/abc":                      "overlay/abc",
		"volumes/db-data/_data/base/1":     "volumes/db-data",
		"overlay":                          "overlay",
		"overlay-images/images.json":       "overlay-images/images.json",
		"overlay-containers/c1/userdata/x": "overlay-containers/c1",
	} {
		if got := dirKey(rel); got != want {
			t.Errorf("dirKey(%q) = %q, want %q", rel, got, want)
		}
	}
}

// O2: per directory, the old-range paths (uid outside root, the user and the current range),
// their distinct owners, a few samples, and the size and data of everything under it.
func TestAggregateSumsEachDirectory(t *testing.T) {
	a := newAggregator(owners())
	for _, e := range []scanEntry{
		{rel: "overlay/L1", uid: 1000, gid: 1000},
		{rel: "overlay/L1/diff/var/cache/apt/archives/partial", uid: 100099, gid: 100103},
		{rel: "overlay/L1/diff/etc/passwd", regular: true, uid: 1000, gid: 1000, size: 700},
		{rel: "overlay/L2/diff/bin/sh", regular: true, uid: 524288, gid: 524288, size: 100},
		{rel: "volumes/v/_data/db", regular: true, uid: 100998, gid: 100998, size: 4096},
		{rel: "volumes/v/_data/db2", regular: true, uid: 100998, gid: 100998, size: 0},
		{rel: "volumes/empty/_data", uid: 100998, gid: 100998},
	} {
		a.add(e)
	}
	got := a.result()
	if len(got) != 3 {
		t.Fatalf("got %d directories, want the 3 with old-range paths: %+v", len(got), got)
	}
	want := []StoreDir{
		{Key: "overlay/L1", Paths: 1, UIDs: []int64{100099}, GIDs: []int64{100103}, Samples: []string{"overlay/L1/diff/var/cache/apt/archives/partial"}, Size: 700, Data: true},
		{Key: "volumes/empty", Paths: 1, UIDs: []int64{100998}, GIDs: []int64{100998}, Samples: []string{"volumes/empty/_data"}},
		{Key: "volumes/v", Paths: 2, UIDs: []int64{100998}, GIDs: []int64{100998}, Samples: []string{"volumes/v/_data/db", "volumes/v/_data/db2"}, Size: 4096, Data: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("directories = %+v, want %+v", got, want)
	}
}

func TestAggregateKeepsOnlyAFewSamples(t *testing.T) {
	a := newAggregator(owners())
	for i := range 20 {
		a.add(scanEntry{rel: "overlay/L/diff/f" + strconv.Itoa(i), uid: 100000 + int64(i), gid: 100000})
	}
	got := a.result()
	if len(got) != 1 || got[0].Paths != 20 || len(got[0].Samples) != maxSamples || len(got[0].UIDs) != 20 {
		t.Fatalf("got %+v", got)
	}
}

// The real walk, on a fixture tree: every path under the root once, the root itself not, with
// its owner and size.
func TestWalkStoreSeesEveryPathWithItsOwner(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "overlay", "L1", "diff", "etc", "hosts"), "127.0.0.1 localhost\n")
	if err := os.MkdirAll(filepath.Join(root, "volumes", "v", "_data"), 0o750); err != nil {
		t.Fatal(err)
	}
	var seen []scanEntry
	if err := walkStore(root, func(e scanEntry) error { seen = append(seen, e); return nil }); err != nil {
		t.Fatal(err)
	}
	rels := make([]string, 0, len(seen))
	for _, e := range seen {
		rels = append(rels, e.rel)
		if e.uid != int64(os.Getuid()) || e.gid != int64(os.Getgid()) {
			t.Errorf("%s read as owned by %d:%d", e.rel, e.uid, e.gid)
		}
		if e.rel == "overlay/L1/diff/etc/hosts" && (!e.regular || e.size != 20) {
			t.Errorf("the file read as %+v", e)
		}
	}
	want := []string{".", "overlay", "overlay/L1", "overlay/L1/diff", "overlay/L1/diff/etc", "overlay/L1/diff/etc/hosts", "volumes", "volumes/v", "volumes/v/_data"}
	if !slices.Equal(rels, want) {
		t.Fatalf("walked %v, want %v", rels, want)
	}
}

func TestWalkStoreFailsOnAMissingStore(t *testing.T) {
	if err := walkStore(filepath.Join(t.TempDir(), "nope"), func(scanEntry) error { return nil }); err == nil {
		t.Fatal("a missing store walked")
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// scanApp is an App whose user joe has home under a temporary directory, /etc there too.
func scanApp(t *testing.T) (App, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home", "joe")
	etc := filepath.Join(dir, "etc")
	writeFile(t, filepath.Join(etc, "subuid"), subuidText)
	writeFile(t, filepath.Join(etc, "subgid"), subuidText)
	store := filepath.Join(home, ".local", "share", "containers", "storage")
	writeFile(t, filepath.Join(store, "overlay", "L1", "diff", "f"), "x")
	var out, errOut bytes.Buffer
	return App{Env: Env{
		Stdout: &out, Stderr: &errOut, Etc: etc, EUID: 0,
		LookupUser: func(name string) (*user.User, error) {
			if name != "joe" {
				return nil, user.UnknownUserError(name)
			}
			return &user.User{Username: "joe", Uid: "1000", Gid: "1000", HomeDir: home}, nil
		},
	}}, &out, &errOut, store
}

func TestScanPrintsTheStoreTheOwnersAndTheDirectories(t *testing.T) {
	a, out, errOut, store := scanApp(t)
	// The test's own files are foreign to joe, whose uid is 1000 and range 524288+65536,
	// unless the test runs as one of those.
	if code := a.Run(t.Context(), []string{"old-range", "scan", "--user", "joe"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var s Scan
	if err := json.Unmarshal(out.Bytes(), &s); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if s.Store != store || s.User != "joe" || s.Owners != owners() {
		t.Fatalf("scan header = %+v", s)
	}
	uid := int64(os.Getuid())
	if owners().foreignUID(uid) && (len(s.Dirs) != 1 || s.Dirs[0].Key != "overlay/L1") {
		t.Fatalf("dirs = %+v", s.Dirs)
	}
}

func TestScanRefusesWhatItCannotJudge(t *testing.T) {
	cases := map[string]func(a *App, store string){
		"not root": func(a *App, _ string) { a.Env.EUID = 1000 },
		"unknown user": func(a *App, _ string) {
			a.Env.LookupUser = func(n string) (*user.User, error) { return nil, user.UnknownUserError(n) }
		},
		"no subuid": func(a *App, _ string) { a.Env.Etc = t.TempDir() },
		"moved store": func(_ *App, store string) {
			writeFile(t, filepath.Join(store, "..", "..", "..", "..", ".config", "containers", "storage.conf"), "[storage]\ngraphroot = \"/srv/x\"\n")
		},
		"system moved": func(a *App, _ string) {
			writeFile(t, filepath.Join(a.Env.Etc, "containers", "storage.conf"), " rootless_storage_path = \"/srv\"\n")
		},
		"no store": func(_ *App, store string) { mustRemoveAll(t, store) },
		"bad uid": func(a *App, _ string) {
			a.Env.LookupUser = func(string) (*user.User, error) { return &user.User{Uid: "x", Gid: "1"}, nil }
		},
	}
	for name, spoil := range cases {
		a, out, errOut, store := scanApp(t)
		spoil(&a, store)
		if code := a.Run(t.Context(), []string{"old-range", "scan", "--user", "joe"}); code != term.ExitFailure {
			t.Errorf("%s: exit %d", name, code)
		}
		if out.Len() != 0 || !strings.Contains(errOut.String(), "container-images: ") {
			t.Errorf("%s: stdout %q, stderr %q", name, out, errOut)
		}
	}
}

func mustRemoveAll(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}
