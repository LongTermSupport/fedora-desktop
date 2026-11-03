package seat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Plan 00246 X8, X9: the first bundle a checkout takes for a team records the team's server_name
// and canonical base URL in the checkout record; a later bundle with the same team id and another
// canonical URL or server_name is refused, naming both.

const (
	otherBase = "https://hs-b.example.test:8448"
	// recordFmt is checkout.json for team ops: its server name and a base URL.
	recordFmt = `{"v":1,"teams":{"ops":{"server_name":"ops.teamchat.internal","base_url":%q}}}`
)

// reacher is the creation probe a test sets: it counts the bundles it is asked about and answers
// err.
type reacher struct {
	mu    sync.Mutex
	asked []Bundle
	err   error
}

func (r *reacher) Reach(_ context.Context, b Bundle) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, b)
	return r.err
}

func (r *reacher) calls() []Bundle {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Bundle(nil), r.asked...)
}

// bundleOf is the test handle's pool seat.json on team at base, pinned to ca ("" for loopback).
func bundleOf(t *testing.T, team, base, ca string) Bundle {
	t.Helper()
	id := mustTeamID(t, team)
	server := paths.ServerName(id)
	b := bundleAt(t, base)
	b.Team, b.ServerName, b.UserID, b.AdminID = id, server, "@"+testHandle+":"+server, "@admin:"+server
	b.TLSCA = ca
	return b
}

// fill claims seat and fills the claim with b as member add's answer, under the lock. A refused
// fill gives the names its claim holds, which must then be abandonable: a refusal writes nothing.
func (w *world) fill(seat Seat, b Bundle) ([]string, error) {
	w.t.Helper()
	a := w.lock()
	defer w.unlock(a)
	c, err := w.store.Claim(a, seat)
	if err != nil {
		w.t.Fatal(err)
	}
	err = c.Fill(w.t.Context(), Issued{Seat: b, Kind: KindPassword, Secret: mustSecret(w.t, testPassword)})
	if err == nil {
		return nil, nil
	}
	names, listErr := w.store.root.entries(c.claim.Dir())
	if listErr != nil {
		w.t.Fatal(listErr)
	}
	if abandonErr := c.Abandon(); abandonErr != nil {
		w.t.Fatalf("a refused fill left a claim that cannot be abandoned: %v", abandonErr)
	}
	return names, err
}

// stubbed is a world whose creation probe is a stub that finds every homeserver there.
func stubbed(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.probe = &reacher{}
	w.reopen(func(c *Config) { c.Reach = w.probe })
	return w
}

func (w *world) checkoutRecord() string { return w.store.store.PoolCheckoutRecord(w.key) }

func (w *world) record() string {
	w.t.Helper()
	return w.readFile(w.checkoutRecord())
}

func (w *world) writeRecord(text string) {
	w.t.Helper()
	if err := os.MkdirAll(w.store.store.PoolCheckout(w.key), 0o700); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(w.checkoutRecord(), []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// X8: the first placement of a team's seat records the team's server name and base URL in
// checkout.json, 0600, in the pool's checkout directory.
func TestTheFirstPlacementRecordsTheTeamsHomeserver(t *testing.T) {
	w := newWorld(t)
	if exists(t, w.checkoutRecord()) {
		t.Fatal("a checkout record before any placement")
	}
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	if got, want := w.record(), fmt.Sprintf(recordFmt, w.hs.URL()); got != want {
		t.Errorf("checkout.json = %s, want %s", got, want)
	}
	if m := mode(t, w.checkoutRecord()).Perm(); m != 0o600 {
		t.Errorf("checkout.json mode %o", m)
	}
	if err := w.place(otherPassword); err != nil {
		t.Errorf("the same homeserver again: %v", err)
	}
}

// X8: a bundle for the same team naming another URL (another host, another port, another scheme)
// is refused naming both URLs and the team, before anything is written: the claim stays empty, the
// record and the pool seat are as they were.
func TestAnotherURLForTheTeamIsRefusedNamingBoth(t *testing.T) {
	ca := mustCA(t, "team-a")
	w := stubbed(t)
	if names, err := w.fill(w.seat, bundleOf(t, "ops", testBase, "")); err != nil {
		t.Fatalf("the first placement: %v (%v)", err, names)
	}
	record, pool := w.record(), w.readFile(w.pool().Bundle())
	asked := len(w.probe.calls())
	for name, other := range map[string]Bundle{
		"another host":   bundleOf(t, "ops", otherBase, string(ca.CertPEM())),
		"another port":   bundleOf(t, "ops", "http://127.0.0.1:9", ""),
		"another scheme": bundleOf(t, "ops", "https://127.0.0.1:8448", string(ca.CertPEM())),
	} {
		names, err := w.fill(w.seat, other)
		if !errors.Is(err, ErrHomeserverClash) {
			t.Errorf("%s: %v, want ErrHomeserverClash", name, err)
			continue
		}
		for _, want := range []string{testBase, other.BaseURL, "ops"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: %q does not name %s", name, err, want)
			}
		}
		if len(names) != 0 {
			t.Errorf("%s: the refused claim holds %v", name, names)
		}
		if w.record() != record || w.readFile(w.pool().Bundle()) != pool || len(quarantined(t, w)) != 0 {
			t.Errorf("%s: the refusal changed the record or the pool seat", name)
		}
	}
	if len(w.probe.calls()) != asked {
		t.Errorf("a clash reached out to the homeserver")
	}
}

// X8: the server name is part of the pin: a record that names another server name for the team is
// a clash with a bundle on the same URL, naming both server names and the URL.
func TestAnotherServerNameForTheTeamIsRefusedNamingBoth(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	other := `{"v":1,"teams":{"ops":{"server_name":"ops.other.internal","base_url":"` + w.hs.URL() + `"}}}`
	w.writeRecord(other)
	_, err := w.fill(w.seat, w.bundle())
	if !errors.Is(err, ErrHomeserverClash) {
		t.Fatalf("a bundle on another server name: %v", err)
	}
	for _, want := range []string{"ops.other.internal", paths.ServerName(w.seat.Team), w.hs.URL()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not name %s", err, want)
		}
	}
	if w.record() != other {
		t.Errorf("the refusal changed the record")
	}
}

// X9: the pin compares canonical URLs: the host lower-cased as matrix.memberBase has it, the port
// kept as given; a bundle spelling the same homeserver another way is the same homeserver.
func TestTheSameHomeserverSpelledAnotherWayIsTheSame(t *testing.T) {
	ca := string(mustCA(t, "team-a").CertPEM())
	w := stubbed(t)
	if names, err := w.fill(w.seat, bundleOf(t, "ops", "https://HS-B.Example.test:8448", ca)); err != nil {
		t.Fatalf("the first placement: %v (%v)", err, names)
	}
	if got, want := w.record(), fmt.Sprintf(recordFmt, otherBase); got != want {
		t.Errorf("the record holds %s, want the canonical %s", got, want)
	}
	for _, spelled := range []string{otherBase, "HTTPS://hs-b.example.test:8448", "https://Hs-B.EXAMPLE.test:8448"} {
		if names, err := w.fill(w.seat, bundleOf(t, "ops", spelled, ca)); err != nil {
			t.Errorf("%s is refused: %v (%v)", spelled, err, names)
		}
	}
	// A port left out is not folded into 443: it is another URL (the declaration always carries
	// the port).
	if _, err := w.fill(w.seat, bundleOf(t, "ops", "https://hs-b.example.test", ca)); !errors.Is(err, ErrHomeserverClash) {
		t.Errorf("a URL with no port: %v, want ErrHomeserverClash", err)
	}
}

// X8: each team of a checkout is pinned to its own homeserver; the pin is the checkout's own, so
// another checkout may pair the same team id with another homeserver (its pool and its session
// seat directories are apart).
func TestEachTeamAndEachCheckoutKeepsItsOwnHomeserver(t *testing.T) {
	ca := string(mustCA(t, "team-a").CertPEM())
	w := stubbed(t)
	dev := Seat{Team: mustTeamID(t, "dev"), Name: w.seat.Name}
	if _, err := w.fill(w.seat, bundleOf(t, "ops", testBase, "")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.fill(dev, bundleOf(t, "dev", otherBase, ca)); err != nil {
		t.Fatalf("a second team on another homeserver: %v", err)
	}
	want := `{"v":1,"teams":{"dev":{"server_name":"dev.teamchat.internal","base_url":"` + otherBase +
		`"},"ops":{"server_name":"ops.teamchat.internal","base_url":"` + testBase + `"}}}`
	if got := w.record(); got != want {
		t.Errorf("checkout.json = %s, want %s", got, want)
	}
	if _, err := w.fill(dev, bundleOf(t, "dev", testBase, "")); !errors.Is(err, ErrHomeserverClash) ||
		!strings.Contains(err.Error(), otherBase) || !strings.Contains(err.Error(), testBase) {
		t.Errorf("team dev on team ops's homeserver: %v", err)
	}
	w.key = mustKey(t, "d1")
	w.reopen(func(c *Config) { c.Reach = w.probe })
	if _, err := w.fill(w.seat, bundleOf(t, "ops", otherBase, ca)); err != nil {
		t.Errorf("another checkout's pin: %v", err)
	}
}

// X8: a pool seat that has no record (it came by another route) is pinned by the first take.
func TestTheFirstTakeRecordsAPoolSeatThatHasNoRecord(t *testing.T) {
	w := newWorld(t)
	w.seed()
	if exists(t, w.checkoutRecord()) {
		t.Fatal("a checkout record before any take")
	}
	w.take("s1")
	if got, want := w.record(), fmt.Sprintf(recordFmt, w.hs.URL()); got != want {
		t.Errorf("checkout.json = %s, want %s", got, want)
	}
	w.reopen(func(c *Config) { c.Reach = &reacher{} })
	if _, err := w.fill(w.seat, bundleOf(t, "ops", otherBase, string(mustCA(t, "team-a").CertPEM()))); !errors.Is(err, ErrHomeserverClash) {
		t.Errorf("a placement on another homeserver after the take: %v", err)
	}
}

// X8: a pool seat.json that disagrees with the record (edited, or put there by another route) is
// refused by the take, naming both URLs, before held/ or a session directory is made.
func TestATakeRefusesASeatJSONThatDisagreesWithTheRecord(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	other := bundleOf(t, "ops", otherBase, string(mustCA(t, "team-a").CertPEM()))
	data, err := other.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(w.pool().Bundle(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = w.tryTake("s1", StateActive)
	if !errors.Is(err, ErrHomeserverClash) || !strings.Contains(err.Error(), otherBase) || !strings.Contains(err.Error(), w.hs.URL()) {
		t.Fatalf("a take of a seat.json on another homeserver: %v", err)
	}
	if exists(t, w.pool().Held()) || exists(t, w.sessionSeat("s1").Dir()) {
		t.Errorf("a refused take made held/ or a session directory")
	}
}

// X8: a checkout record that is not exactly what the store writes is refused by the placement and
// by the take, as ErrInvalid, and left as it was.
func TestACorruptCheckoutRecordIsRefused(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	url := w.hs.URL()
	good := `"server_name":"ops.teamchat.internal","base_url":"` + url + `"`
	named := func(entry string) string { return `{"v":1,"teams":{"ops":{` + entry + `}}}` }
	for name, text := range map[string]string{
		"not json":                   "{",
		"an array":                   "[]",
		"no v":                       `{"teams":{}}`,
		"another v":                  `{"v":2,"teams":{}}`,
		"no teams":                   `{"v":1}`,
		"teams an array":             `{"v":1,"teams":[]}`,
		"another key":                `{"v":1,"teams":{},"path":"/x"}`,
		"a team that is not an id":   `{"v":1,"teams":{"Ops":{` + good + `}}}`,
		"an entry with no URL":       named(`"server_name":"ops.teamchat.internal"`),
		"an entry with no name":      named(`"base_url":"` + url + `"`),
		"an entry with another key":  named(good + `,"x":1`),
		"an entry key in capitals":   named(`"SERVER_NAME":"ops.teamchat.internal","base_url":"` + url + `"`),
		"an empty server name":       named(`"server_name":"","base_url":"` + url + `"`),
		"a server name with a space": named(`"server_name":"ops teamchat","base_url":"` + url + `"`),
		"a URL not canonical":        named(`"server_name":"ops.teamchat.internal","base_url":"HTTP://127.0.0.1:1"`),
		"a URL with a path":          named(`"server_name":"ops.teamchat.internal","base_url":"` + url + `/x"`),
		"plain http to a name":       named(`"server_name":"ops.teamchat.internal","base_url":"http://hs.example.test:1"`),
		"a URL that is a count":      named(`"server_name":"ops.teamchat.internal","base_url":1`),
		"junk after the value":       fmt.Sprintf(recordFmt, url) + "x",
	} {
		w.writeRecord(text)
		if names, err := w.fill(w.seat, w.bundle()); !errors.Is(err, ErrInvalid) || len(names) != 0 {
			t.Errorf("%s: a placement: %v (%v), want ErrInvalid", name, err, names)
		}
		if _, err := w.tryTake("s1", StateActive); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: a take: %v, want ErrInvalid", name, err)
		}
		if w.record() != text {
			t.Errorf("%s: the refusal changed the record", name)
		}
	}
	if err := os.Remove(w.checkoutRecord()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(w.checkoutRecord(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := w.fill(w.seat, w.bundle()); err == nil {
		t.Errorf("a checkout.json that is a directory was taken as none")
	}
}

// X8: checkout.json and the fresh copy its write leaves after a crash are not pool seats: the
// listing of the pool's holdings goes past them, and the next write goes over the leftover.
func TestTheRecordAndItsFreshCopyAreNotPoolSeats(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.take("s1")
	fresh := w.checkoutRecord() + replaceSuffix
	writeFile(t, fresh, "{")
	holdings, err := w.store.Holdings()
	if err != nil || len(holdings) != 1 {
		t.Fatalf("Holdings with a record and a leftover fresh copy: %v, %v", holdings, err)
	}
	w.reopen(func(c *Config) { c.Reach = &reacher{} })
	dev := Seat{Team: mustTeamID(t, "dev"), Name: w.seat.Name}
	if _, err := w.fill(dev, bundleOf(t, "dev", otherBase, string(mustCA(t, "team-a").CertPEM()))); err != nil {
		t.Fatalf("a record write over a leftover fresh copy: %v", err)
	}
	if exists(t, fresh) || !strings.Contains(w.record(), `"dev"`) {
		t.Errorf("the leftover is still there or the new team is not recorded: %s", w.record())
	}
}
