package seat

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// closedBase is a member URL nothing listens on: a logout to it gets no answer.
const closedBase = "http://127.0.0.1:1"

// teams is a fake of what the machine knows of a seat's team: why it is gone, or an error.
type teams struct {
	mu    sync.Mutex
	why   string
	err   error
	asked []Bundle
}

func (f *teams) Gone(b Bundle) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, b)
	return f.why, f.err
}

func (f *teams) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

// newTLSWorld is a world whose homeserver serves a leaf of ca, its pool seat pinned to ca.
func newTLSWorld(t *testing.T, ca pki.CA) *world {
	t.Helper()
	clock := newClock()
	w := &world{
		homeserver: startTLSHomeserver(t, mustLeaf(t, ca, loopback), clock), home: t.TempDir(), key: mustKey(t, "c0"),
		sessions: &sessions{}, notices: &announcer{outcome: Outcome{Posted: true}},
		seat: Seat{Team: mustTeam(t), Name: mustSeatName(t, testHandle)},
	}
	w.store = w.open()
	b := w.bundle()
	b.TLSCA = string(ca.CertPEM())
	w.placeBundle(b)
	writeFile(t, w.pool().Password(), testPassword)
	return w
}

// placeBundle writes b as the pool's seat.json, as a placement (or a test) leaves it.
func (w *world) placeBundle(b Bundle) {
	w.t.Helper()
	data, err := b.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	writeFile(w.t, w.pool().Bundle(), string(data))
}

// withTeams reopens the world's store with f as its teams and log as its log.
func (w *world) withTeams(f Teams, log *bytes.Buffer) {
	w.reopen(func(c *Config) { c.Teams, c.Log = f, log })
}

// released is the world's released.json, decoded.
func (w *world) released() ReleasedRecord {
	w.t.Helper()
	rec, err := decodeReleasedRecord([]byte(w.readFile(w.pool().ReleasedRecord())))
	if err != nil {
		w.t.Fatal(err)
	}
	return rec
}

// assertAbandoned holds that the release finished without the logout: held/ in quarantine, its
// token gone, released.json naming the claim and why, and the log saying why.
func (w *world) assertAbandoned(taken Taken, log *bytes.Buffer, why string) {
	w.t.Helper()
	if exists(w.t, w.pool().Held()) || !exists(w.t, w.releasedPool(taken)+"/held.json") || exists(w.t, w.releasedPool(taken)+"/token") {
		w.t.Errorf("held/ was not moved to quarantine with its token unlinked")
	}
	rec := w.released()
	if rec.Claim != taken.Claim || !strings.Contains(rec.Abandoned, why) {
		w.t.Errorf("released.json %+v, want the claim and abandoned naming %q", rec, why)
	}
	if !strings.Contains(log.String(), why) || !strings.Contains(log.String(), w.seat.Name.String()) {
		w.t.Errorf("the log %q does not say why seat %s was abandoned", log.String(), w.seat.Name)
	}
}

// assertRetried holds that the release stopped as before: an error, held/ and its token kept,
// no released.json.
func (w *world) assertRetried(err error) {
	w.t.Helper()
	if err == nil {
		w.t.Fatal("the release completed")
	}
	if !exists(w.t, w.pool().HeldToken()) || exists(w.t, w.pool().ReleasedRecord()) {
		w.t.Errorf("held/ lost, or released.json written, after a release that must be retried")
	}
}

// A server that presents a certificate in date and valid for the seat's host but signed by
// another CA than the bundle's has been recreated (or its CA rotated): no logout with this
// bundle can ever succeed, so the release finishes without it.
func TestAbandonOnAnotherCA(t *testing.T) {
	ca := mustCA(t, "team-old")
	w := newTLSWorld(t, ca)
	taken := w.take("s1")
	b := w.bundle()
	b.TLSCA = string(mustCA(t, "team-new").CertPEM())
	w.placeBundle(b)
	var log bytes.Buffer
	f := &teams{}
	w.withTeams(f, &log)
	if err := w.release("s1", true); err != nil {
		t.Fatalf("release: %v", err)
	}
	w.assertAbandoned(taken, &log, "another CA")
	if f.calls() != 0 {
		t.Errorf("a CA mismatch asked the teams too")
	}
}

// A certificate that does not name the seat's host is another server at the address, not proof
// the team was recreated: retried.
func TestNameMismatchIsRetried(t *testing.T) {
	w := newTLSWorld(t, mustCA(t, "team-old"))
	w.take("s1")
	b := w.bundle()
	b.BaseURL = strings.Replace(b.BaseURL, "127.0.0.1", "localhost", 1)
	b.TLSCA = string(mustCA(t, "team-new").CertPEM())
	w.placeBundle(b)
	var log bytes.Buffer
	w.withTeams(&teams{}, &log)
	err := w.release("s1", true)
	w.assertRetried(err)
	if !strings.Contains(err.Error(), "name mismatch") {
		t.Errorf("err %v, want the name mismatch", err)
	}
}

// No answer from the seat's server, and the machine knows the team is gone: the release
// finishes without the logout, the reason in released.json and the log.
func TestAbandonAGoneTeam(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	w.placeBundle(bundleAt(t, closedBase))
	var log bytes.Buffer
	f := &teams{why: "team ops is gone"}
	w.withTeams(f, &log)
	if err := w.release("s1", false); err != nil {
		t.Fatalf("release: %v", err)
	}
	w.assertAbandoned(taken, &log, "team ops is gone")
	if f.calls() != 1 || f.asked[0].BaseURL != closedBase {
		t.Errorf("the teams were asked %d times, of %+v", f.calls(), f.asked)
	}
}

// No answer, and the team is not known to be gone (a transient failure of a live team), or
// the machine cannot judge, or nothing judges at all: retried, as before.
func TestNoAnswerIsRetried(t *testing.T) {
	for name, f := range map[string]*teams{
		"known":         {},
		"cannot judge":  {why: "gone", err: errors.New("status record unreadable")},
		"nobody judges": nil,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.seed()
			w.take("s1")
			w.placeBundle(bundleAt(t, closedBase))
			var log bytes.Buffer
			if f == nil {
				w.withTeams(nil, &log)
			} else {
				w.withTeams(f, &log)
			}
			w.assertRetried(w.release("s1", true))
		})
	}
}

// A server that answers, even with a failure, is there: the logout is retried and the teams
// are never asked, so a live token is never left valid.
func TestAnAnswerIsRetried(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.take("s1")
	token := w.readFile(w.pool().HeldToken())
	f := &teams{why: "gone"}
	var log bytes.Buffer
	w.withTeams(f, &log)
	w.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/logout", Status: http.StatusBadGateway})
	w.assertRetried(w.release("s1", true))
	if f.calls() != 0 || !w.alive(token) {
		t.Errorf("an answering server asked the teams (%d) or lost the token", f.calls())
	}
}

// released.json carries abandoned only for an abandoned release, one line, and reads back.
func TestReleasedRecordAbandoned(t *testing.T) {
	for doc, ok := range map[string]bool{
		`{"v":1,"claim":"00112233aabbccdd","released_ms":1}`:                             true,
		`{"v":1,"claim":"00112233aabbccdd","released_ms":1,"abandoned":"team gone"}`:     true,
		`{"v":1,"claim":"00112233aabbccdd","released_ms":1,"abandoned":"two\nlines"}`:    false,
		`{"v":1,"claim":"00112233aabbccdd","released_ms":1,"abandoned":""}`:              false,
		`{"v":1,"claim":"00112233aabbccdd","released_ms":1,"abandoned":"a","other":"b"}`: false,
	} {
		if _, err := decodeReleasedRecord([]byte(doc)); (err == nil) != ok {
			t.Errorf("%s: %v, want ok %v", doc, err, ok)
		}
	}
	data, err := encodeRecord(ReleasedRecord{V: recordVersion, Claim: mustClaim(t, "00112233aabbccdd"), ReleasedMS: 1})
	if err != nil || strings.Contains(string(data), "abandoned") {
		t.Errorf("a plain release writes %s, %v", data, err)
	}
}
