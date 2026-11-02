package seat

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
)

// A session's seat directory, and sessions/<session>/ above it, are mounted read-write into its
// container, so anything there is the member's to plant. Revocation must never depend on it: a
// remove, the release after the session ends, and a take by another session each retire the
// token, whatever the member left (n2c-D14).

// plant puts something a member can make at path.
type plant func(w *world, path string) error

// lockPlants are what a member can leave at its seat lock.
var lockPlants = map[string]plant{
	"a link to /dev/null":     func(_ *world, p string) error { return os.Symlink("/dev/null", p) },
	"a link to an owner file": func(w *world, p string) error { return os.Symlink(w.pool().Password(), p) },
	"a FIFO":                  func(_ *world, p string) error { return unix.Mkfifo(p, 0o600) },
	"a directory":             func(_ *world, p string) error { return os.Mkdir(p, 0o700) },
}

// dirPlants are what a member can leave in place of its seat directory.
var dirPlants = map[string]plant{
	"a link":   func(w *world, p string) error { return os.Symlink(w.home, p) },
	"a FIFO":   func(_ *world, p string) error { return unix.Mkfifo(p, 0o600) },
	"a file":   func(_ *world, p string) error { return os.WriteFile(p, []byte("x"), 0o600) },
	"a socket": func(_ *world, p string) error { return unix.Mknod(p, unix.S_IFSOCK|0o600, 0) },
}

// takeAndPlant is a world whose session s1 holds the seat, with plant made at at(taken).
func takeAndPlant(t *testing.T, p plant, at func(Taken) string) (*world, Taken, string) {
	t.Helper()
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	token := readFile(t, w.pool().HeldToken())
	path := at(taken)
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := p(w, path); err != nil {
		t.Fatal(err)
	}
	return w, taken, token
}

// revoked fails the test unless the holding taken is released: its token dead, held/ gone and
// released.json naming its claim.
func (w *world) revoked(taken Taken, token string) {
	w.t.Helper()
	if w.alive(token) {
		w.t.Errorf("the released token is still live")
	}
	if exists(w.t, w.pool().Held()) {
		w.t.Errorf("held/ was kept")
	}
	rec, err := decodeReleasedRecord([]byte(readFile(w.t, w.pool().ReleasedRecord())))
	if err != nil || rec.Claim != taken.Claim {
		w.t.Errorf("released.json: %+v, %v", rec, err)
	}
}

func lockAt(taken Taken) string { return taken.Dir + "/seat.lock" }
func dirAt(taken Taken) string  { return taken.Dir }

// SX1, SX2, DC16: a remove of a live session retires the token whatever is at its seat lock,
// and moves what is there to quarantine without following it.
func TestAPlantedSeatLockStopsNoRemove(t *testing.T) {
	for name, p := range lockPlants {
		t.Run(name, func(t *testing.T) {
			w, taken, token := takeAndPlant(t, p, lockAt)
			w.sessions.set("s1", true)
			if err := w.release("s1", true); err != nil {
				t.Fatalf("remove: %v", err)
			}
			w.revoked(taken, token)
			if !exists(t, w.releasedSession(taken)+"/seat.lock") || exists(t, taken.Dir) {
				t.Errorf("the session's copy was not moved to quarantine")
			}
		})
	}
}

// SX1, SD1, SD3: once the session is over, its release and a take by another session each retire
// the token whatever is at its seat lock, and the crashed-session path announces nothing.
func TestAPlantedSeatLockStopsNoReleaseOrTake(t *testing.T) {
	for name, p := range lockPlants {
		t.Run(name+", released", func(t *testing.T) {
			w, taken, token := takeAndPlant(t, p, lockAt)
			if err := w.store.Announce(t.Context(), w.seat, mustSession(t, "s1")); err != nil {
				t.Errorf("announce: %v", err)
			}
			if err := w.release("s1", false); err != nil {
				t.Fatalf("the release of a session that is over: %v", err)
			}
			w.revoked(taken, token)
			if len(w.notices.calls()) != 0 {
				t.Errorf("a refused lock named a run: %v", w.notices.calls())
			}
		})
		t.Run(name+", taken", func(t *testing.T) {
			w, taken, token := takeAndPlant(t, p, lockAt)
			next := w.take("s2")
			if w.alive(token) || next.Claim == taken.Claim || exists(t, taken.Dir) {
				t.Errorf("the ended holding was not released by the take")
			}
		})
	}
}

// SX1, SX2: a link, FIFO, file or socket in place of the seat directory is moved to quarantine
// unfollowed, and the token retired.
func TestAPlantedSeatDirectoryStopsNoRemove(t *testing.T) {
	for name, p := range dirPlants {
		t.Run(name, func(t *testing.T) {
			w, taken, token := takeAndPlant(t, p, dirAt)
			w.sessions.set("s1", true)
			if err := w.release("s1", true); err != nil {
				t.Fatalf("remove: %v", err)
			}
			w.revoked(taken, token)
			if !exists(t, w.releasedSession(taken)) || !exists(t, w.home) {
				t.Errorf("the plant was not moved to quarantine, or was followed")
			}
		})
	}
}

// SX1, SD3, SD4: no probe opens a FIFO for reading, whether at the session's seat lock, in the
// take's check of an ended holding, or in quarantine on a resumed release. inotify records every
// open but an O_PATH one, which reads nothing and cannot block.
func TestAFIFOIsNeverOpened(t *testing.T) {
	w, taken, token := takeAndPlant(t, lockPlants["a FIFO"], lockAt)
	watch, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Close(watch); err != nil {
			t.Error(err)
		}
	}()
	if _, err := unix.InotifyAddWatch(watch, lockAt(taken), unix.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	if err := w.store.Announce(t.Context(), w.seat, mustSession(t, "s1")); err != nil {
		t.Errorf("announce: %v", err)
	}
	if held, err := w.store.sessionHolds(mustSession(t, "s1"), w.seat.Team); err != nil || held {
		t.Errorf("an ended session with a FIFO lock: held %v, %v", held, err)
	}
	w.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/logout", Status: http.StatusBadGateway})
	if err := w.release("s1", false); err == nil {
		t.Fatal("the release did not stop at step 4")
	}
	if err := w.release("s1", false); err != nil {
		t.Fatalf("the resumed release: %v", err)
	}
	w.revoked(taken, token)
	var events [unix.SizeofInotifyEvent + unix.PathMax]byte
	if n, err := unix.Read(watch, events[:]); !errors.Is(err, unix.EAGAIN) {
		t.Errorf("the FIFO was opened: %d bytes of events, %v", n, err)
	}
}

// SX1, n2c-D14: only what a container can cause is refused; a fault of the owner's own (here a
// store root fd that is not open) is an error that stops the take, never a free lock.
func TestAnOwnerFaultIsNotARefusal(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.take("s1")
	root := w.store.root
	w.store.root = &dir{fd: -1, path: root.path}
	defer func() { w.store.root = root }()
	if got, err := w.store.probe(w.sessionSeat("s1")); !errors.Is(err, unix.EBADF) {
		t.Errorf("probe: %+v, %v", got, err)
	}
	if held, err := w.store.sessionHolds(mustSession(t, "s1"), w.seat.Team); !errors.Is(err, unix.EBADF) {
		t.Errorf("sessionHolds: %v, %v", held, err)
	}
}

// SX1: a seat lock swapped by its container between the probe's O_PATH look and its open for
// reading is refused, not judged as the file it was swapped for.
func TestALockSwappedBetweenOpensIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/was", lockRecord)
	writeFile(t, dir+"/is", lockRecord)
	var was unix.Stat_t
	if err := unix.Stat(dir+"/was", &was); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dir + "/is")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	if got, err := probeOpen(&was, f); !errors.Is(err, ErrInvalid) {
		t.Errorf("a swapped lock: %+v, %v", got, err)
	}
}

// envUnprivileged marks a test binary that asUnprivileged re-ran without capabilities.
const envUnprivileged = "SEAT_TEST_UNPRIVILEGED"

// asUnprivileged is true when the calling test may run its body here, where the kernel enforces
// file modes and owners on this process: not root, or already re-run. As root, which bypasses
// them, it re-runs the test in a child with every capability dropped but keep (setpriv,
// util-linux), the child's result being this test's, and is false.
func asUnprivileged(t *testing.T, keep string) bool {
	t.Helper()
	if os.Getenv(envUnprivileged) == "1" || os.Geteuid() != 0 {
		return true
	}
	caps := "-all"
	if keep != "" {
		caps += ",+" + keep
	}
	args := []string{"--bounding-set=" + caps, "--inh-caps=-all", "--", os.Args[0], "-test.run=^" + t.Name() + "$", "-test.count=1", "-test.v"}
	for _, arg := range os.Args[1:] {
		// The child's coverage joins this run's (go test -cover reads the directory).
		if strings.HasPrefix(arg, "-test.gocoverdir=") {
			args = append(args, arg)
		}
	}
	res, err := execx.OS{}.Run(t.Context(), execx.Cmd{Name: "setpriv", Args: args, Env: []string{envUnprivileged + "=1"}})
	if err != nil || !strings.Contains(string(res.Stdout), "--- PASS: "+t.Name()) {
		t.Fatalf("the run without capabilities (setpriv, util-linux): %v\n%s%s", err, res.Stdout, res.Stderr)
	}
	return false
}

// restoreMode puts path back to 0700 when the test ends, so its directory can be removed.
func restoreMode(t *testing.T, path string) {
	t.Helper()
	t.Cleanup(func() {
		if err := os.Chmod(path, 0o700); err != nil {
			t.Error(err)
		}
	})
}

// The ways a holding ends: a remove of a live session, the release after it is over, and a
// take by another session once it is over.
const (
	byRemove = "remove"
	byEnd    = "ended"
	byTake   = "taken"
)

var endings = []string{byRemove, byEnd, byTake}

// removeKeepsTheCopy fails the test unless the holding of s1 ended by ending retires the token:
// a release is ErrCopyKept naming the copy the owner could not move, a release after it is done,
// and another session can take the seat.
func (w *world) removeKeepsTheCopy(taken Taken, token, ending string) {
	w.t.Helper()
	w.sessions.set("s1", ending == byRemove)
	if ending == byTake {
		next := w.take("s2")
		rec, err := decodeReleasedRecord([]byte(readFile(w.t, w.pool().ReleasedRecord())))
		if w.alive(token) || next.Claim == taken.Claim || err != nil || rec.Claim != taken.Claim {
			w.t.Errorf("the take did not release the ended holding: released.json %+v, %v", rec, err)
		}
		return
	}
	if err := w.release("s1", ending == byRemove); !errors.Is(err, ErrCopyKept) {
		w.t.Errorf("release: %v (want ErrCopyKept)", err)
	}
	w.revoked(taken, token)
	if err := w.release("s1", ending == byRemove); err != nil {
		w.t.Errorf("a release of a seat already released: %v", err)
	}
	w.take("s2")
}

// SX1, SX2, DC16: a mode the member set on its seat directory or on sessions/<session>/ stops
// neither a remove, nor the release after the session, nor a take by another session.
func TestAModeTheMemberSetStopsNoRevocation(t *testing.T) {
	if !asUnprivileged(t, "") {
		return
	}
	for _, where := range []string{"seat", "session"} {
		for _, m := range []os.FileMode{0o000, 0o100, 0o500} {
			for _, ending := range endings {
				t.Run(where+" "+m.String()+" "+ending, func(t *testing.T) {
					w := newWorld(t)
					w.seed()
					taken := w.take("s1")
					token := readFile(t, w.pool().HeldToken())
					dir := taken.Dir
					if where == "session" {
						dir = w.store.store.Session(mustSession(t, "s1"))
					}
					if err := os.Chmod(dir, m); err != nil {
						t.Fatal(err)
					}
					restoreMode(t, dir)
					w.removeKeepsTheCopy(taken, token, ending)
				})
			}
		}
	}
}

// SX1, SX2, DC16: the seat directory or sessions/<session>/ given by the member to another uid
// it maps, which the owner then cannot enter or change, stops no revocation.
func TestAnOwnerTheMemberSetStopsNoRevocation(t *testing.T) {
	if os.Geteuid() != 0 && os.Getenv(envUnprivileged) != "1" {
		t.Skip("a chown to another uid needs CAP_CHOWN, which only root has here")
	}
	if !asUnprivileged(t, "chown") {
		return
	}
	for _, where := range []string{"seat", "session"} {
		for _, ending := range endings {
			t.Run(where+" "+ending, func(t *testing.T) {
				w := newWorld(t)
				w.seed()
				taken := w.take("s1")
				token := readFile(t, w.pool().HeldToken())
				dir := taken.Dir
				if where == "session" {
					dir = w.store.store.Session(mustSession(t, "s1"))
				}
				if err := os.Chown(dir, 1, 1); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Chown(dir, 0, 0); err != nil {
						t.Error(err)
					}
				})
				w.removeKeepsTheCopy(taken, token, ending)
			})
		}
	}
}
