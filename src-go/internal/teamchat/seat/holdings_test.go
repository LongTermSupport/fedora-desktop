package seat

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// SW1: Holdings lists every pool seat of the checkout with a held/, complete or not, and
// nothing else: a seat with no held/, a placement's claim directory and checkout.json are not
// holdings.
func TestHoldingsOfNone(t *testing.T) {
	w := newWorld(t)
	if got, err := w.store.Holdings(); err != nil || len(got) != 0 {
		t.Fatalf("an empty store: %v, %v", got, err)
	}
	w.seed()
	writeFile(t, w.store.store.PoolCheckoutRecord(w.key), `{}`)
	if got, err := w.store.Holdings(); err != nil || len(got) != 0 {
		t.Fatalf("a seat with no held/: %v, %v", got, err)
	}
}

func TestHoldings(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.writeRecord(fmt.Sprintf(recordFmt, w.hs.URL()))
	taken := w.take("s1")
	if err := os.Mkdir(w.pool().Dir()+".new-0123456789abcdef", 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := w.store.Holdings()
	if err != nil || len(got) != 1 {
		t.Fatalf("one holding: %v, %v", got, err)
	}
	h := got[0]
	if h.Seat != w.seat || !h.Complete || h.Record.Session.String() != "s1" || h.Record.Claim != taken.Claim {
		t.Errorf("the holding: %+v", h)
	}

	other := w.seat
	other.Name = mustSeatName(t, "repo.2+site.podman")
	otherPool := w.store.store.PoolSeat(w.key, other.Team, other.Name)
	if err = os.MkdirAll(otherPool.Held(), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err = w.store.Holdings()
	if err != nil || len(got) != 2 {
		t.Fatalf("two holdings: %v, %v", got, err)
	}
	i := slices.IndexFunc(got, func(h Holding) bool { return h.Seat == other })
	if i < 0 || got[i].Complete {
		t.Errorf("the incomplete holding: %+v", got)
	}
}

// SW1: the pool is the owner's, so a name in it that the store never makes is an error, not
// skipped.
func TestHoldingsRefusesAStrangeName(t *testing.T) {
	for _, path := range []string{"/Not A Team", "/ops/not a seat"} {
		t.Run(path, func(t *testing.T) {
			w := newWorld(t)
			w.seed()
			if err := os.MkdirAll(w.store.store.PoolCheckout(w.key)+path, 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := w.store.Holdings(); !errors.Is(err, ErrInvalid) {
				t.Errorf("holdings: %v", err)
			}
		})
	}
}

// SW1: ReadHoldings is Holdings read without opening a store for allocation: the same holdings,
// none where the seat store or the checkout's pool does not exist, and nothing made.
func TestReadHoldingsIsHoldingsReadOnly(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.writeRecord(fmt.Sprintf(recordFmt, w.hs.URL()))
	w.take("s1")
	want, err := w.store.Holdings()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadHoldings(w.store.store, w.key)
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("read holdings %+v, %v; want %+v", got, err, want)
	}
	if got, err = ReadHoldings(w.store.store, mustKey(t, "e7")); err != nil || len(got) != 0 {
		t.Errorf("a checkout with no pool: %v, %v", got, err)
	}
	missing, err := paths.SeatStore(t.TempDir() + "/state")
	if err != nil {
		t.Fatal(err)
	}
	if got, err = ReadHoldings(missing, w.key); err != nil || len(got) != 0 {
		t.Errorf("no seat store: %v, %v", got, err)
	}
	if _, err = os.Stat(missing.Root()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("reading made the seat store: %v", err)
	}
}

// HeldBy is the complete holdings whose record names the session: what Verbs.end acts on and
// ccy-teams shutdown closes. An incomplete holding names no session, so it is no one's.
func TestHeldByIsTheSessionsCompleteHoldings(t *testing.T) {
	s1, s2 := mustSession(t, "s1"), mustSession(t, "s2")
	a := Holding{Seat: Seat{Name: mustSeatName(t, "repo.1+site.podman")}, Record: HeldRecord{Session: s1}, Complete: true}
	b := Holding{Seat: Seat{Name: mustSeatName(t, "repo.2+site.podman")}, Record: HeldRecord{Session: s2}, Complete: true}
	c := Holding{Seat: Seat{Name: mustSeatName(t, "repo.3+site.podman")}, Record: HeldRecord{Session: s1}}
	if got := HeldBy([]Holding{a, b, c}, s1); !slices.Equal(got, []Holding{a}) {
		t.Errorf("held by s1: %+v", got)
	}
	if got := HeldBy([]Holding{b, c}, s1); len(got) != 0 {
		t.Errorf("held by s1 with none: %+v", got)
	}
}
