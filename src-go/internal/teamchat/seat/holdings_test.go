package seat

import (
	"errors"
	"os"
	"slices"
	"testing"
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
	writeFile(t, w.store.store.PoolCheckoutRecord(w.key), `{}`)
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
