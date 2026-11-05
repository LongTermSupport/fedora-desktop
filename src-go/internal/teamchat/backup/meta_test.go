package backup

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// selfUID is the test's own uid, the owner of every tree the tests make.
func selfUID(t *testing.T) uint32 {
	t.Helper()
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("no stat")
	}
	return st.Uid
}

func metaDir(root string, team paths.TeamID) string {
	return root + paths.HomeserverPrivateBackupMeta(team)
}

// plantMeta writes Tuwunel's meta file n with the given text, making the tree as needed.
func plantMeta(t *testing.T, root string, team paths.TeamID, name, text string) {
	t.Helper()
	dir := metaDir(root, team)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func treeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(root+paths.HomeserverPrivateStateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestScanMetaCountsOnlyDecimalRegularFiles(t *testing.T) {
	root := treeRoot(t)
	team := alpha(t)
	plantMeta(t, root, team, "1", "1792663930\n327\n")
	plantMeta(t, root, team, "12", "1792663999\n")
	plantMeta(t, root, team, "3", "not a time\n")
	plantMeta(t, root, team, "4", "1792663930")
	plantMeta(t, root, team, "07", "1792663930\n")
	plantMeta(t, root, team, "8.tmp", "1792663930\n")
	plantMeta(t, root, team, "0", "1792663930\n")
	dir := metaDir(root, team)
	if err := os.Symlink(filepath.Join(dir, "1"), filepath.Join(dir, "20")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "21"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "22"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := scanMeta(Deps{Root: root, StateOwnerUID: selfUID(t)}, team)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]int64{1: 1792663930, 12: 1792663999, 3: 0, 4: 0}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for n, sec := range want {
		if got[n] != sec {
			t.Fatalf("meta %d: got %d, want %d (%v)", n, got[n], sec, got)
		}
	}
}

func TestScanMetaMissingIsNone(t *testing.T) {
	root := treeRoot(t)
	got, err := scanMeta(Deps{Root: root, StateOwnerUID: selfUID(t)}, alpha(t))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	if err = os.MkdirAll(root+paths.HomeserverPrivateBackups(alpha(t)), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err = scanMeta(Deps{Root: root, StateOwnerUID: selfUID(t)}, alpha(t)); err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestScanMetaRefusesALinkOnTheWay(t *testing.T) {
	root := treeRoot(t)
	team := alpha(t)
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(elsewhere, "meta"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "meta", "9"), []byte("1792663930\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+paths.HomeserverPrivateState(team), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, root+paths.HomeserverPrivateBackups(team)); err != nil {
		t.Fatal(err)
	}
	if _, err := scanMeta(Deps{Root: root, StateOwnerUID: selfUID(t)}, team); err == nil {
		t.Fatal("followed a link")
	}
}

func TestScanMetaRefusesAStateRootNotOwned(t *testing.T) {
	root := treeRoot(t)
	if _, err := scanMeta(Deps{Root: root, StateOwnerUID: selfUID(t) + 1}, alpha(t)); err == nil || !strings.Contains(err.Error(), "owned") {
		t.Fatalf("err %v", err)
	}
	linked := t.TempDir()
	if err := os.MkdirAll(linked+"/var/lib/private", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root+paths.HomeserverPrivateStateRoot, linked+paths.HomeserverPrivateStateRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := scanMeta(Deps{Root: linked, StateOwnerUID: selfUID(t)}, alpha(t)); err == nil {
		t.Fatal("followed a linked state root")
	}
	if _, err := scanMeta(Deps{Root: t.TempDir(), StateOwnerUID: selfUID(t)}, alpha(t)); err == nil {
		t.Fatal("a missing state root was taken as no backups")
	}
}

func TestScanMetaRefusesAnOversizedMeta(t *testing.T) {
	root := treeRoot(t)
	plantMeta(t, root, alpha(t), "2", "1792663930\n"+strings.Repeat("x", 64<<10))
	if _, err := scanMeta(Deps{Root: root, StateOwnerUID: selfUID(t)}, alpha(t)); err == nil {
		t.Fatal("read past the cap")
	}
}
