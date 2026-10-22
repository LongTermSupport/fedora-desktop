package restore

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// layout makes each named entry of the state directory: "d" a directory, "f" a file, "l" a
// link to /.
func layout(t *testing.T, state string, entries map[string]string) {
	t.Helper()
	for name, what := range entries {
		switch what {
		case "d":
			writeFile(t, state+"/"+name+"/marker", name)
		case "f":
			writeFile(t, state+"/"+name, name)
		case "l":
			if err := os.Symlink("/", state+"/"+name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// marker is the name of the entry whose content now sits at name, "" for none.
func marker(t *testing.T, state, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(state + "/" + name + "/marker"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// RS16's table, every row in both directions; a repeat is harmless.
func TestRenamesTable(t *testing.T) {
	const r7 = "db.restore-7"
	cases := []struct {
		name   string
		start  map[string]string
		toward Toward
		want   map[string]string // where each original entry ends: final name -> marker
	}{
		{"d r swapped", map[string]string{"db": "d", r7: "d"}, Swapped, map[string]string{"db": r7, "db.previous": "db"}},
		{"p r swapped", map[string]string{"db.previous": "d", r7: "d"}, Swapped, map[string]string{"db": r7, "db.previous": "db.previous"}},
		{"d p swapped", map[string]string{"db": "d", "db.previous": "d"}, Swapped, map[string]string{"db": "db", "db.previous": "db.previous"}},
		{"d r unswapped", map[string]string{"db": "d", r7: "d"}, Unswapped, map[string]string{"db": "db", r7: r7}},
		{"p r unswapped", map[string]string{"db.previous": "d", r7: "d"}, Unswapped, map[string]string{"db": "db.previous", r7: r7}},
		{"d p unswapped", map[string]string{"db": "d", "db.previous": "d"}, Unswapped, map[string]string{"db": "db.previous", r7: "db"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, state := osTree(t)
			layout(t, state, c.start)
			for range 2 {
				if err := Renames(o, opsTeam(t), 7, c.toward); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"db", "db.previous", r7} {
				if got := marker(t, state, name); got != c.want[name] {
					t.Errorf("%s holds %q, want %q", name, got, c.want[name])
				}
				if c.want[name] == "" && exists(t, state+"/"+name) {
					t.Errorf("%s is left", name)
				}
			}
		})
	}
}

// Every other state is refused with a listing of what is there, and nothing is renamed.
func TestRenamesRefusals(t *testing.T) {
	cases := []struct {
		start   map[string]string
		listing string
	}{
		{map[string]string{"db": "d", "db.previous": "d", "db.restore-7": "d"}, "db=directory db.previous=directory db.restore-7=directory"},
		{map[string]string{"db": "d"}, "db=directory db.previous=absent db.restore-7=absent"},
		{map[string]string{"db.previous": "d"}, "db=absent db.previous=directory db.restore-7=absent"},
		{map[string]string{"db.restore-7": "d"}, "db=absent db.previous=absent db.restore-7=directory"},
		{map[string]string{}, "db=absent db.previous=absent db.restore-7=absent"},
		{map[string]string{"db": "d", "db.restore-7": "f"}, "db=directory db.previous=absent db.restore-7=file"},
		{map[string]string{"db": "l", "db.restore-7": "d"}, "db=other db.previous=absent db.restore-7=directory"},
	}
	for _, c := range cases {
		for _, toward := range []Toward{Swapped, Unswapped} {
			o, state := osTree(t)
			layout(t, state, c.start)
			err := Renames(o, opsTeam(t), 7, toward)
			var stuck *StuckError
			if !errors.As(err, &stuck) || stuck.Listing != c.listing {
				t.Errorf("%v toward %v = %v, want the listing %q", c.start, toward, err, c.listing)
				continue
			}
			if !strings.Contains(err.Error(), c.listing) {
				t.Errorf("the refusal does not name the listing: %v", err)
			}
			for name := range c.start {
				if !exists(t, state+"/"+name) {
					t.Errorf("%s was moved", name)
				}
			}
		}
	}
}

// killFS is an FS that dies, as a kill would, at its k-th rename.
type killFS struct {
	FS

	mu            sync.Mutex
	renames, kill int
}

func (k *killFS) Rename(team paths.TeamID, from, to string) error {
	k.mu.Lock()
	k.renames++
	n := k.renames
	k.mu.Unlock()
	if n == k.kill {
		runtime.Goexit()
	}
	return k.FS.Rename(team, from, to)
}

// inGoroutine runs fn as a process that may be killed by runtime.Goexit, and says whether it
// returned.
func inGoroutine(fn func()) bool {
	done := make(chan bool, 1)
	go func() {
		returned := false
		defer func() { done <- returned }()
		fn()
		returned = true
	}()
	return <-done
}

// A kill between the two renames leaves the p r row, and the next run completes it, in both
// directions.
func TestRenamesKillBetweenTheTwo(t *testing.T) {
	for _, c := range []struct {
		toward Toward
		start  map[string]string
		db     string
	}{
		{Swapped, map[string]string{"db": "d", "db.restore-7": "d"}, "db.restore-7"},
		{Unswapped, map[string]string{"db": "d", "db.previous": "d"}, "db.previous"},
	} {
		o, state := osTree(t)
		layout(t, state, c.start)
		k := &killFS{FS: o, kill: 2}
		if inGoroutine(func() { t.Errorf("returned: %v", Renames(k, opsTeam(t), 7, c.toward)) }) {
			t.Fatal("the kill did not stop the renames")
		}
		if exists(t, state+"/db") || !exists(t, state+"/db.previous") || !exists(t, state+"/db.restore-7") {
			t.Fatalf("toward %v: the kill did not leave p r", c.toward)
		}
		if err := Renames(o, opsTeam(t), 7, c.toward); err != nil {
			t.Fatal(err)
		}
		if got := marker(t, state, "db"); got != c.db {
			t.Errorf("toward %v: db holds %q, want %q", c.toward, got, c.db)
		}
	}
}

// A rename the filesystem refuses is returned.
func TestRenamesFailure(t *testing.T) {
	o, state := osTree(t)
	layout(t, state, map[string]string{"db": "d", "db.restore-7": "d"})
	if err := Renames(failFS{FS: o}, opsTeam(t), 7, Swapped); err == nil || errors.As(err, new(*StuckError)) {
		t.Errorf("a failed rename = %v", err)
	}
	o.StateOwnerUID++
	if err := Renames(o, opsTeam(t), 7, Swapped); err == nil {
		t.Error("an untrusted state root was read")
	}
}

type failFS struct{ FS }

func (failFS) Rename(paths.TeamID, string, string) error { return errors.New("no space") }

// RS17: a start needs db a directory and no restore.block.
func TestStartable(t *testing.T) {
	o, state := osTree(t)
	ops := opsTeam(t)
	if err := Startable(o, ops); err == nil {
		t.Error("a start without db was allowed")
	}
	layout(t, state, map[string]string{"db": "l"})
	if err := Startable(o, ops); err == nil {
		t.Error("a start with db a link was allowed")
	}
	if err := os.Remove(state + "/db"); err != nil {
		t.Fatal(err)
	}
	layout(t, state, map[string]string{"db": "d"})
	if err := Startable(o, ops); err != nil {
		t.Errorf("a start with db was refused: %v", err)
	}
	if err := os.Symlink("/nowhere", o.Root+paths.RestoreBlock(ops)); err != nil {
		t.Fatal(err)
	}
	if err := Startable(o, ops); err == nil || !strings.Contains(err.Error(), "restore.block") {
		t.Errorf("a start beside a block (a dangling link) = %v", err)
	}
	o.StateOwnerUID++
	if err := Startable(o, ops); err == nil {
		t.Error("an untrusted state root was read")
	}
}

func TestOSRootKind(t *testing.T) {
	o, state := osTree(t)
	ops := opsTeam(t)
	if k, err := o.RootKind(paths.RestoreBlock(ops)); err != nil || k != KindAbsent {
		t.Errorf("absent = %v, %v", k, err)
	}
	if k, err := o.RootKind(paths.AdminState(ops)); err != nil || k != KindDirectory {
		t.Errorf("a directory = %v, %v", k, err)
	}
	writeFile(t, o.Root+paths.RestoreBlock(ops), "")
	if k, err := o.RootKind(paths.RestoreBlock(ops)); err != nil || k != KindFile {
		t.Errorf("a file = %v, %v", k, err)
	}
	if k, err := o.RootKind(paths.RestoreBlock(ops) + "/x"); err == nil {
		t.Errorf("a path under a file = %v", k)
	}
	if err := os.Symlink("/", state+"/link"); err != nil {
		t.Fatal(err)
	}
	if k, err := o.RootKind(paths.HomeserverPrivateState(ops) + "/link"); err != nil || k != KindOther {
		t.Errorf("a link = %v, %v", k, err)
	}
}
