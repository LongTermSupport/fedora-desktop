package teamsync

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func testSeat(t *testing.T) paths.Seat {
	t.Helper()
	l, err := paths.HomeLayout(t.TempDir(), t.TempDir())
	check(t, err)
	s := l.Seat(alphaID)
	check(t, os.MkdirAll(s.Dir(), 0o700))
	return s
}

// LN9: a rename into place of seat.json or token is a change; Close ends the watch.
func TestWatchSeat(t *testing.T) {
	s := testSeat(t)
	w, err := WatchSeat(s)
	check(t, err)
	tmp := filepath.Join(s.Dir(), "token.tmp")
	check(t, os.WriteFile(tmp, []byte("x"), 0o600))
	check(t, os.Rename(tmp, s.Token()))
	select {
	case <-w.Changes():
	case <-t.Context().Done():
		t.Fatal("no change for a token placed")
	}
	check(t, w.Close())
	if _, open := <-w.done; open {
		t.Error("the reader did not end")
	}
}

func TestWatchSeatRefusesNoDirectory(t *testing.T) {
	s, err := paths.SeatAt(filepath.Join(t.TempDir(), "missing", teamName))
	check(t, err)
	if _, err := WatchSeat(s); err == nil {
		t.Error("a missing seat directory was watched")
	}
}

// inotifyEvent is one event as the kernel writes it, the name NUL-padded to nameRoom.
func inotifyEvent(mask uint32, name string) []byte {
	const nameRoom = 32
	b := make([]byte, unix.SizeofInotifyEvent, unix.SizeofInotifyEvent+nameRoom)
	binary.NativeEndian.PutUint32(b[4:], mask)
	if name == "" {
		return b
	}
	binary.NativeEndian.PutUint32(b[12:], nameRoom)
	padded := make([]byte, nameRoom)
	copy(padded, name)
	return append(b, padded...)
}

// Only seat.json, token and an overflow are changes; other names and a short read are not.
func TestRelevant(t *testing.T) {
	names := map[string]bool{"seat.json": true, "token": true}
	cases := []struct {
		name string
		buf  []byte
		want bool
	}{
		{"seat.json", inotifyEvent(unix.IN_MOVED_TO, "seat.json"), true},
		{"token after another", append(inotifyEvent(unix.IN_CREATE, "store.json"), inotifyEvent(unix.IN_CLOSE_WRITE, "token")...), true},
		{"another name", inotifyEvent(unix.IN_CREATE, "seat.json.tmp"), false},
		{"an overflow", inotifyEvent(unix.IN_Q_OVERFLOW, ""), true},
		{"a short read", []byte{1, 2, 3}, false},
		{"a name past the read", inotifyEvent(unix.IN_CREATE, "token")[:unix.SizeofInotifyEvent+2], false},
	}
	for _, c := range cases {
		if got := relevant(c.buf, names); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
