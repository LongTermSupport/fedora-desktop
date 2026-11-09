package placeissued

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/testfiles"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var testStart = time.UnixMilli(1_800_000_000_000)

// fakeClock moves only when the code sleeps, so the allocation lock's 30 second wait is instant.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return nil
}

// reacher answers whether a new seat's homeserver answers, and records the URLs it was asked.
type reacher struct {
	err   error
	asked []string
}

func (r *reacher) Reach(_ context.Context, b seat.Bundle) error {
	r.asked = append(r.asked, b.BaseURL)
	return r.err
}

// tokenPool is a pool credential of the token kind (DESIGN-v2 7.1 (d1)), which no production
// code has yet; place-issued never mints or retires, so both refuse.
type tokenPool struct{}

func (tokenPool) Kind() seat.Kind { return seat.KindToken }

func (tokenPool) Mint(context.Context, seat.Bundle, seat.Secret) (seat.Secret, error) {
	return seat.Secret{}, errors.New("place-issued does not mint")
}

func (tokenPool) Retire(context.Context, seat.Bundle, seat.Secret) error {
	return errors.New("place-issued does not retire")
}

func mustSeat(t *testing.T) paths.SeatName {
	t.Helper()
	name, err := paths.ParseSeatName(testHandle)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func mustCheckout(t *testing.T) paths.CheckoutKey {
	t.Helper()
	key, err := paths.CheckoutKeyOf("/srv/example/checkout")
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// world is one machine under a temporary directory: the state home, the runtime directory, and
// the fakes of the clock and the homeserver's answer.
type world struct {
	t     *testing.T
	dir   string
	deps  Deps
	reach *reacher
	store paths.Store
	key   paths.CheckoutKey
	team  paths.TeamID
	name  paths.SeatName
	// unhold lets go of the allocation lock holdLock took, once; nil when none is held.
	unhold func()
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := t.TempDir()
	w := &world{t: t, dir: dir, reach: &reacher{}, key: mustCheckout(t), team: mustTeam(t), name: mustSeat(t)}
	w.deps = Deps{
		StateHome: dir + "/state", RuntimeDir: dir + "/run", Checkout: w.key,
		Clock: &fakeClock{now: testStart}, Credential: seat.Password{}, Reach: w.reach,
	}
	store, err := paths.SeatStore(w.deps.StateHome)
	if err != nil {
		t.Fatal(err)
	}
	w.store = store
	return w
}

func (w *world) place(doc []byte) (Placed, error) {
	w.t.Helper()
	return Place(w.t.Context(), w.deps, bytes.NewReader(doc))
}

func (w *world) mustPlace(doc []byte) {
	w.t.Helper()
	placed, err := w.place(doc)
	if err != nil {
		w.t.Fatalf("place: %v", err)
	}
	if placed != (Placed{Team: w.team, Seat: w.name}) {
		w.t.Fatalf("placed %+v", placed)
	}
}

func (w *world) pool() paths.PoolSeat { return w.store.PoolSeat(w.key, w.team, w.name) }

// text is the content of the file at path, which must exist.
func (w *world) text(path string) string {
	w.t.Helper()
	return string(testfiles.Read(w.t, w.dir, path))
}

func (w *world) exists(path string) bool {
	w.t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	if !errors.Is(err, fs.ErrNotExist) {
		w.t.Fatal(err)
	}
	return false
}

func (w *world) perm(path string) fs.FileMode {
	w.t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		w.t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// files is every regular file under root: its path below root, and its text.
func (w *world) files(root string) map[string]string {
	w.t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			out[path] = string(testfiles.Read(w.t, w.dir, path))
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		w.t.Fatal(err)
	}
	return out
}

// storeFiles is every file of the seat store.
func (w *world) storeFiles() map[string]string { return w.files(w.store.Root()) }

// holding is the paths under the store whose text holds secret.
func (w *world) holding(secret string) []string {
	var out []string
	for path, text := range w.storeFiles() {
		if strings.Contains(text, secret) {
			out = append(out, path)
		}
	}
	slices.Sort(out)
	return out
}

// leftovers are the `<seat>.new-*` entries of the seat's team directory.
func (w *world) leftovers() []string {
	w.t.Helper()
	entries, err := os.ReadDir(w.store.PoolTeam(w.key, w.team))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		w.t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), w.name.String()+".new-") {
			out = append(out, e.Name())
		}
	}
	return out
}

// quarantined are the directories in quarantine for the seat's team in this checkout.
func (w *world) quarantined() []string {
	w.t.Helper()
	dir := w.store.Released() + "/pool/" + w.key.String() + "/" + w.team.String()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		w.t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// lockPath is the allocation lock file.
func (w *world) lockPath() string {
	w.t.Helper()
	path, err := paths.AllocLock(w.deps.RuntimeDir, w.key)
	if err != nil {
		w.t.Fatal(err)
	}
	return path
}

// holdLock holds the allocation lock on an open file description of its own, as another run
// would, until the test ends.
func (w *world) holdLock() {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(w.lockPath()), 0o700); err != nil {
		w.t.Fatal(err)
	}
	f := testfiles.OpenFile(w.t, w.dir, w.lockPath(), os.O_RDWR|os.O_CREATE, 0o600)
	w.unhold = sync.OnceFunc(func() {
		if err := f.Close(); err != nil {
			w.t.Error(err)
		}
	})
	w.t.Cleanup(w.unhold)
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		w.t.Fatal(err)
	}
}

// lockIsFree fails the test unless the allocation lock can be taken now; a lock file that was
// never made is free.
func (w *world) lockIsFree() {
	w.t.Helper()
	if !w.exists(w.lockPath()) {
		return
	}
	f := testfiles.OpenFile(w.t, w.dir, w.lockPath(), os.O_RDWR, 0)
	defer func() {
		if err := f.Close(); err != nil {
			w.t.Error(err)
		}
	}()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		w.t.Errorf("the allocation lock is still held: %v", err)
	}
}

// writeFile makes a file at path, and the directories above it, all 0700 and 0600.
func (w *world) writeFile(path, text string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// run places doc and answers what Report writes for the refusal, failing if it was placed.
func (w *world) run(doc []byte) (term.ExitCode, string, exits.Error) {
	w.t.Helper()
	_, err := w.place(doc)
	if err == nil {
		w.t.Fatal("placed, want a refusal")
	}
	return reported(w.t, err)
}

// refusalWant is what a Place refusal is: its exit, its error object's code and message, and a
// text its stderr holds.
type refusalWant struct {
	exit  term.ExitCode
	code  string
	msg   string
	cause string
}

// refuses holds that placing doc is refused as want says, changes no file of the store, leaves no
// claimed directory, and shows no credential. It answers the stderr of the refusal.
func (w *world) refuses(doc []byte, want refusalWant) string {
	w.t.Helper()
	before := w.storeFiles()
	code, stderr, obj := w.run(doc)
	if code != want.exit || obj.Exit != want.exit || obj.Code != want.code || obj.Message != want.msg {
		w.t.Errorf("exit %d, object %+v\nwant exit %d %s %q", code, obj, want.exit, want.code, want.msg)
	}
	if !strings.Contains(stderr, want.cause) {
		w.t.Errorf("stderr %q does not hold %q", stderr, want.cause)
	}
	for _, secret := range []string{secretOne, secretTwo} {
		if strings.Contains(stderr, secret) {
			w.t.Errorf("stderr shows a credential: %q", stderr)
		}
	}
	if after := w.storeFiles(); !maps.Equal(before, after) {
		w.t.Errorf("the refusal changed the store's files:\nbefore %v\nafter  %v", before, after)
	}
	if left := w.leftovers(); len(left) != 0 {
		w.t.Errorf("the refusal left %v", left)
	}
	if w.unhold != nil {
		w.unhold() // the other run's hold, which this test made, ends before the lock is judged
	}
	w.lockIsFree()
	return stderr
}
