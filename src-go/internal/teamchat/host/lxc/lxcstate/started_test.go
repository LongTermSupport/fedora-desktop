package lxcstate

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func key(t *testing.T, s string) paths.SeatKey {
	t.Helper()
	k, err := paths.ParseSeatKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// startedRoot is a host root with the admin state root made, as the play makes it.
func startedRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, paths.AdminStateRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

// No file is no seat started.
func TestReadStartedOfNoFileIsEmpty(t *testing.T) {
	s, err := ReadStarted(startedRoot(t))
	if err != nil || len(s.Seats) != 0 {
		t.Fatalf("%+v %v", s, err)
	}
}

// A seat added is kept once, sorted, in a root-only file; a removed one is gone, and removing
// one not there changes nothing.
func TestStartedAddAndRemove(t *testing.T) {
	root := startedRoot(t)
	a := StartedSeat{Container: box(t, "c2"), Key: key(t, "00000000000000a1")}
	b := StartedSeat{Container: box(t, "c1"), Key: key(t, "00000000000000b2")}
	s := Started{}.Add(a).Add(b).Add(a)
	if err := WriteStarted(root, s); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, paths.LXCStartedFile))
	if err != nil || info.Mode().Perm() != StartedMode {
		t.Fatalf("mode %v %v", info, err)
	}
	got, err := ReadStarted(root)
	if err != nil || !slices.Equal(got.Seats, []StartedSeat{b, a}) {
		t.Fatalf("%+v %v", got, err)
	}
	if !got.Has(a) || got.Remove(a).Has(a) || len(got.Remove(a).Remove(a).Seats) != 1 {
		t.Fatalf("remove %+v", got.Remove(a))
	}
}

// A file that is not what WriteStarted writes is refused, never replaced.
func TestReadStartedRefusesAnUnwrittenFile(t *testing.T) {
	for _, text := range []string{
		`{"v":2,"seats":[]}`,
		`{"v":1,"seats":[],"x":1}`,
		`{"v":1,"seats":[{"container":"c1","key":"bad key"}]}`,
		`{"v":1,"seats":[{"container":"c2","key":"00000000000000a1"},{"container":"c1","key":"00000000000000a1"}]}`,
		`{"v":1,"seats":[{"container":"c1","key":"00000000000000a1"},{"container":"c1","key":"00000000000000a1"}]}`,
	} {
		root := startedRoot(t)
		if err := os.WriteFile(filepath.Join(root, paths.LXCStartedFile), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadStarted(root); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", text, err)
		}
		if err := WriteStarted(root, Started{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s replaced: %v", text, err)
		}
	}
}
