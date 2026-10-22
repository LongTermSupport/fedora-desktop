package seat

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
)

// ST2, ST4, ST5: a take writes held/ and the session's copy; the pool record is untouched.
func TestTakeWritesHeldAndTheSessionCopy(t *testing.T) {
	w := newWorld(t)
	w.seed()
	poolBefore := readFile(t, w.pool().Bundle())
	got := w.take("s1")
	held, err := decodeHeldRecord([]byte(readFile(t, w.pool().HeldRecord())))
	if err != nil || held.Session.String() != "s1" || held.Claim != got.Claim || held.SinceMS != testStart.UnixMilli() {
		t.Errorf("held.json %+v, %v (claim %s)", held, err, got.Claim)
	}
	token := readFile(t, w.pool().HeldToken())
	if !w.alive(token) {
		t.Errorf("the held token is dead")
	}
	bundle, err := DecodeHeldBundle([]byte(readFile(t, w.pool().HeldBundle())))
	if err != nil || bundle.Claim != got.Claim || bundle.ReleasedMS != nil || encode(t, bundle.Bundle) != encode(t, w.bundle()) {
		t.Errorf("held seat.json %+v, %v", bundle, err)
	}
	seat := w.sessionSeat("s1")
	if got.Dir != seat.Dir() || readFile(t, seat.Token()) != token || readFile(t, seat.Bundle()) != readFile(t, w.pool().HeldBundle()) {
		t.Errorf("the session copy differs from held/")
	}
	if readFile(t, w.pool().Bundle()) != poolBefore {
		t.Errorf("the take changed the pool's seat.json")
	}
}

// SW1: every directory a take makes is 0700, every file 0600.
func TestTakeModes(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.take("s1")
	seat := w.sessionSeat("s1")
	for _, path := range []string{seat.Dir(), w.pool().Held(), w.store.store.Session(mustSession(t, "s1"))} {
		if mode(t, path).Perm() != 0o700 {
			t.Errorf("%s is %v", path, mode(t, path))
		}
	}
	for _, path := range []string{seat.Token(), seat.Bundle(), w.pool().HeldRecord(), w.pool().HeldToken(), w.pool().HeldBundle()} {
		if mode(t, path).Perm() != 0o600 {
			t.Errorf("%s is %v", path, mode(t, path))
		}
	}
}

// ST6: after a take every token the seat's device had is dead, its release run or not.
func TestTakeEndsEveryEarlierToken(t *testing.T) {
	w := newWorld(t)
	w.seed()
	stale := w.stale()
	first := w.take("s1")
	firstToken := readFile(t, first.Dir+"/token")
	if err := os.RemoveAll(w.pool().Held()); err != nil {
		t.Fatal(err)
	}
	second := w.take("s2")
	if w.alive(stale) || w.alive(firstToken) {
		t.Errorf("a token minted before the take is live")
	}
	if !w.alive(readFile(t, second.Dir+"/token")) {
		t.Errorf("the take's own token is dead")
	}
}

// ST4: the session copy carries the seat's last release time.
func TestTakeCarriesReleasedMS(t *testing.T) {
	w := newWorld(t)
	w.seed()
	writeFile(t, w.pool().ReleasedRecord(), `{"v":1,"claim":"00112233aabbccdd","released_ms":77}`)
	got := w.take("s1")
	bundle, err := DecodeHeldBundle([]byte(readFile(t, got.Dir+"/seat.json")))
	if err != nil || bundle.ReleasedMS == nil || *bundle.ReleasedMS != 77 {
		t.Errorf("released_ms %+v, %v", bundle, err)
	}
	if err := os.WriteFile(w.pool().ReleasedRecord(), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(w.pool().Held()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.tryTake("s2", StateActive); !errors.Is(err, ErrInvalid) {
		t.Errorf("an undecodable released.json: %v", err)
	}
	if err := os.Remove(w.pool().ReleasedRecord()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(w.pool().ReleasedRecord(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(w.pool().Held()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.tryTake("s2", StateActive); err == nil {
		t.Errorf("a released.json that is a directory was taken as none")
	}
}

// ST1: the handle's state decides; a retired seat's pool directory goes to quarantine.
func TestTakeByHandleState(t *testing.T) {
	w := newWorld(t)
	w.seed()
	for state, want := range map[HandleState]error{
		StateParked: ErrReturn, StateLost: ErrReturn, StateNeedsReissue: ErrReissue,
	} {
		if _, err := w.tryTake("s1", state); !errors.Is(err, want) {
			t.Errorf("state %d: %v, want %v", state, err, want)
		}
	}
	if exists(t, w.pool().Held()) {
		t.Errorf("a refused take made held/")
	}
	if _, err := w.tryTake("s1", HandleState(99)); err == nil {
		t.Errorf("an unknown state was taken")
	}
	if _, err := w.tryTake("s1", StateRetired); !errors.Is(err, ErrRetired) {
		t.Fatalf("retired: %v", err)
	}
	if exists(t, w.pool().Dir()) {
		t.Errorf("the retired seat's pool directory is still in the pool")
	}
	moved := quarantined(t, w)
	if len(moved) != 1 || !exists(t, moved[0]+"/seat.json") || exists(t, moved[0]+"/password") {
		t.Errorf("quarantine %v", moved)
	}
}

// quarantined are the directories in the world's pool quarantine for its seat's team.
func quarantined(t *testing.T, w *world) []string {
	t.Helper()
	base := w.store.store.Released() + "/pool/" + w.key.String() + "/" + w.seat.Team.String()
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), w.seat.Name.String()+"-") {
			t.Errorf("quarantine entry %s", e.Name())
		}
		out = append(out, base+"/"+e.Name())
	}
	return out
}

// ST1: a record without its credential or seat.json, or with a leftover claim, is refused.
func TestTakeRefusesAnIncompleteRecordOrALeftover(t *testing.T) {
	w := newWorld(t)
	if _, err := w.tryTake("s1", StateActive); !errors.Is(err, ErrIncomplete) {
		t.Errorf("no pool record: %v", err)
	}
	w.seed()
	if err := os.Remove(w.pool().Password()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.tryTake("s1", StateActive); !errors.Is(err, ErrIncomplete) {
		t.Errorf("no password: %v", err)
	}
	w.seed()
	other := mustSeatName(t, "repo.2+site.podman")
	other1 := w.store.store.PoolSeat(w.key, w.seat.Team, other)
	writeFile(t, other1.Bundle(), readFile(t, w.pool().Bundle()))
	writeFile(t, other1.Password(), testPassword)
	a := w.lock()
	if _, err := w.store.Take(t.Context(), a, Take{Seat: Seat{Team: w.seat.Team, Name: other}, Session: mustSession(t, "s1"), State: StateActive}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a record naming another handle: %v", err)
	}
	w.unlock(a)
	leftover := w.pool().Dir() + ".new-00112233aabbccdd"
	if err := os.Mkdir(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := w.tryTake("s2", StateActive)
	if !errors.Is(err, ErrLeftover) || !strings.Contains(err.Error(), leftover) || !strings.Contains(err.Error(), "member reissue") {
		t.Errorf("a leftover claim: %v", err)
	}
}

// ST1: a held/ whose held.json is missing or broken is a take that died; it goes to quarantine.
func TestTakeQuarantinesABrokenHeld(t *testing.T) {
	w := newWorld(t)
	w.seed()
	writeFile(t, w.pool().HeldToken(), "dead")
	w.take("s1")
	moved := quarantined(t, w)
	if len(moved) != 1 || readFile(t, moved[0]+"/token") != "dead" {
		t.Errorf("quarantine %v", moved)
	}
}

// ST1: a seat held by a live or restorable session is ErrHeld.
func TestTakeRefusesAHeldSeat(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.take("s1")
	end := w.keep("s1", `{"v":1,"run":"0123456789abcdef","started_ms":1}`)
	if _, err := w.tryTake("s2", StateActive); !errors.Is(err, ErrHeld) {
		t.Errorf("a seat with its lock held: %v", err)
	}
	end()
	w.sessions.set("s1", true)
	if _, err := w.tryTake("s2", StateActive); !errors.Is(err, ErrHeld) {
		t.Errorf("a seat held by a restorable session: %v", err)
	}
	w.sessions.fail(errors.New("unreadable record"))
	if _, err := w.tryTake("s2", StateActive); err == nil || errors.Is(err, ErrHeld) {
		t.Errorf("an unreadable session record: %v", err)
	}
}

// ST3: a refused first login removes held/; any other failure leaves it.
func TestTakeMintFailures(t *testing.T) {
	w := newWorld(t)
	w.seed()
	if err := os.WriteFile(w.pool().Password(), []byte("reset-elsewhere"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.tryTake("s1", StateActive); !errors.Is(err, ErrReissue) {
		t.Fatalf("a reset password: %v", err)
	}
	if exists(t, w.pool().Held()) {
		t.Errorf("held/ kept after a refused login")
	}
	if err := os.WriteFile(w.pool().Password(), []byte(testPassword), 0o600); err != nil {
		t.Fatal(err)
	}
	w.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/logout", Status: http.StatusBadGateway})
	if _, err := w.tryTake("s1", StateActive); err == nil || errors.Is(err, ErrReissue) {
		t.Fatalf("a failed logout: %v", err)
	}
	if !exists(t, w.pool().HeldRecord()) || exists(t, w.pool().HeldToken()) {
		t.Errorf("held/ after a failed mint")
	}
	if err := os.WriteFile(w.pool().Password(), []byte("two words"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(w.pool().Held()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.tryTake("s1", StateActive); !errors.Is(err, ErrInvalid) {
		t.Errorf("a password that is not a credential: %v", err)
	}
}

// ST5: anything already at the session's seat directory is refused, naming the path.
func TestTakeRefusesAPlantedSessionSeat(t *testing.T) {
	for name, plant := range map[string]func(path string) error{
		"directory": func(p string) error { return os.Mkdir(p, 0o700) },
		"link":      func(p string) error { return os.Symlink(os.TempDir(), p) },
		"fifo":      func(p string) error { return syscall.Mkfifo(p, 0o600) },
	} {
		w := newWorld(t)
		w.seed()
		path := w.sessionSeat("s1").Dir()
		writeFile(t, path+".x", "")
		if err := plant(path); err != nil {
			t.Fatal(err)
		}
		_, err := w.tryTake("s1", StateActive)
		if !errors.Is(err, os.ErrExist) || !strings.Contains(err.Error(), path) {
			t.Errorf("%s: %v", name, err)
		}
		if !exists(t, w.pool().HeldToken()) {
			t.Errorf("%s: held/ is not complete after a failure at step 6", name)
		}
	}
}

// SL1: a take needs its own store's allocation lock, held.
func TestATakeNeedsItsOwnStoresLock(t *testing.T) {
	w := newWorld(t)
	w.seed()
	other := w.open()
	a, err := other.Lock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer w.unlock(a)
	take := Take{Seat: w.seat, Session: mustSession(t, "s1"), State: StateActive}
	if _, err := w.store.Take(t.Context(), a, take); !errors.Is(err, ErrNotLocked) {
		t.Errorf("a take under another store's lock: %v", err)
	}
	if _, err := w.store.Take(t.Context(), nil, take); !errors.Is(err, ErrNotLocked) {
		t.Errorf("a take with no lock: %v", err)
	}
}
