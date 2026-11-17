package carry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func newStageOS(t *testing.T) StageOS {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(root+paths.CarryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return StageOS{Root: root, StateOwnerUID: ownUID(t), CtlUID: os.Getuid(), CtlGID: os.Getgid(), OwnerGroup: func() (int, error) { return os.Getgid(), nil }}
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}

// SI8: the homeserver's state root is made as systemd makes it when absent, and taken as it is
// when it is a directory of the state owner; a link in its place is refused.
func TestStageMakesTheHomeserverRoot(t *testing.T) {
	s := newStageOS(t)
	for range 2 {
		if err := s.MakeHomeserverRoot(); err != nil {
			t.Fatal(err)
		}
	}
	if m := modeOf(t, s.Root+filepath.Dir(paths.HomeserverPrivateStateRoot)); m != os.ModeDir|0o700 {
		t.Errorf("the private root is %v", m)
	}
	if m := modeOf(t, s.Root+paths.HomeserverPrivateStateRoot); m != os.ModeDir|0o755 {
		t.Errorf("the homeserver root is %v", m)
	}
	s = newStageOS(t)
	if err := os.MkdirAll(s.Root+filepath.Dir(paths.HomeserverPrivateStateRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(s.Root+paths.CarryDir, s.Root+paths.HomeserverPrivateStateRoot); err != nil {
		t.Fatal(err)
	}
	if err := s.MakeHomeserverRoot(); err == nil {
		t.Fatal("a link in place of the homeserver root was taken")
	}
}

// SI8: a staged directory and file are made new, of their exact mode, never over an entry and
// never through a link; a rename never replaces an entry.
func TestStageMakesWithoutLinks(t *testing.T) {
	s := newStageOS(t)
	dir := paths.CarryDir + "/x"
	if err := s.Mkdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := s.Mkdir(dir); err == nil {
		t.Error("a directory was made over one")
	}
	part, err := s.Create(dir+"/f", 0o640)
	if err != nil {
		t.Fatal(err)
	}
	if err = part.Close(); err != nil {
		t.Fatal(err)
	}
	if m := modeOf(t, s.Root+dir+"/f"); m != 0o640 {
		t.Errorf("the file is %v", m)
	}
	if _, err = s.Create(dir+"/f", 0o600); err == nil {
		t.Error("a file was made over one")
	}
	if err = os.Symlink(s.Root+dir, s.Root+paths.CarryDir+"/link"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Create(paths.CarryDir+"/link/g", 0o600); err == nil {
		t.Error("a file was made through a link")
	}
	if err = s.Rename(dir, paths.CarryDir+"/link"); err == nil {
		t.Error("a rename replaced an entry")
	}
}

// SI10: a tree is removed without following a link, an absent one or one under an absent
// directory being done; a rename moves a tree to a free name.
func TestStageRemovesWithoutLinks(t *testing.T) {
	s := newStageOS(t)
	dir := paths.CarryDir + "/x"
	if err := os.MkdirAll(s.Root+dir+"/sub", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Root+dir+"/f", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := os.Symlink(s.Root+dir, s.Root+paths.CarryDir+"/link")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RemoveTree(paths.CarryDir + "/link"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(s.Root + dir + "/f"); err != nil {
		t.Fatalf("the removal of a link followed it: %v", err)
	}
	if err = s.Rename(dir, paths.CarryDir+"/y"); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncDir(paths.CarryDir); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = s.RemoveTree(paths.CarryDir + "/y"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = os.Lstat(s.Root + paths.CarryDir + "/y"); !os.IsNotExist(err) {
		t.Fatalf("the tree is left: %v", err)
	}
	if err = s.RemoveTree(paths.CarryDir + "/absent/z"); err != nil {
		t.Fatalf("a tree under an absent directory: %v", err)
	}
}
