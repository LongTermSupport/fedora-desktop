package teamsync

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// fakeReceipts is the engine's side of the read receipts: a list, a channel closed at each
// change, and the ids the team said were sent.
type fakeReceipts struct {
	mu      sync.Mutex
	rs      []state.Receipt
	changed chan struct{}
	sentErr error
	// failOnce is the next ReceiptSent's error, once.
	failOnce error
	sent     chan string
}

func newFakeReceipts() *fakeReceipts {
	return &fakeReceipts{changed: make(chan struct{}), sent: make(chan string, 16)}
}

func (f *fakeReceipts) PendingReceipts(paths.TeamID) ([]state.Receipt, <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.rs), f.changed
}

func (f *fakeReceipts) ReceiptSent(_ context.Context, _ paths.TeamID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOnce != nil {
		err := f.failOnce
		f.failOnce = nil
		f.sent <- id
		return err
	}
	if f.sentErr != nil {
		f.sent <- id
		return f.sentErr
	}
	f.rs = slices.DeleteFunc(f.rs, func(r state.Receipt) bool { return r.ID == id })
	f.changeLocked()
	f.sent <- id
	return nil
}

func (f *fakeReceipts) add(r state.Receipt) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rs = append(f.rs, r)
	f.changeLocked()
}

func (f *fakeReceipts) changeLocked() {
	close(f.changed)
	f.changed = make(chan struct{})
}

func (f *fakeReceipts) left() []state.Receipt {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.rs)
}

func withReceipts(rc team.Receipts) option { return func(c *Config) { c.Receipts = rc } }

func (w *world) sentReceipt(rc *fakeReceipts) string {
	w.t.Helper()
	select {
	case id := <-rc.sent:
		return id
	case <-w.t.Context().Done():
		w.t.Fatal("no receipt sent")
	}
	return ""
}

// receiptTeam starts a team that sends rc's receipts, past its first answer; the channel gets
// each receipt backoff.
func (w *world) receiptTeam(rc *fakeReceipts) chan time.Duration {
	w.t.Helper()
	tm := w.newTeam(withReceipts(rc))
	waits := make(chan time.Duration, 8)
	tm.onReceiptWait = func(d time.Duration) { waits <- d }
	r := w.startTeam(tm)
	w.batch()
	r.poll(w.t)
	return waits
}

func (w *world) myReceipts() []fakehs.Receipt {
	var out []fakehs.Receipt
	for _, r := range w.srv.Receipts() {
		if r.UserID == user(me) {
			out = append(out, r)
		}
	}
	return out
}

// DV5 (B 2.7.5): a receipt the engine holds is sent once as m.read on main, then reported sent;
// a second receipt is sent without the first again.
func TestAReceiptIsSentOnce(t *testing.T) {
	w := newWorld(t)
	rc := newFakeReceipts()
	w.receiptTeam(rc)
	first, second := w.say(alice, human("one", user(me))), w.say(alice, human("two", user(me)))
	rc.add(state.Receipt{ID: first, Room: proto.PurposeMain})
	if id := w.sentReceipt(rc); id != first {
		t.Fatalf("sent %s", id)
	}
	rc.add(state.Receipt{ID: second, Room: proto.PurposeMain})
	if id := w.sentReceipt(rc); id != second {
		t.Fatalf("sent %s", id)
	}
	got := w.myReceipts()
	if len(got) != 2 || got[0].EventID != first || got[1].EventID != second || got[0].RoomID != w.main || got[0].Type != "m.read" {
		t.Fatalf("receipts %+v", got)
	}
}

// DV5: a 500 keeps the receipt and retries it after the team's backoff, 1 s first.
func TestAReceiptIsRetriedAfterA500(t *testing.T) {
	w := newWorld(t)
	rc := newFakeReceipts()
	waits := w.receiptTeam(rc)
	id := w.say(alice, human("one", user(me)))
	w.srv.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/receipt/", Status: http.StatusInternalServerError})
	rc.add(state.Receipt{ID: id, Room: proto.PurposeMain})
	d := <-waits
	if d != time.Second || len(rc.left()) != 1 {
		t.Fatalf("wait %s, left %+v", d, rc.left())
	}
	w.clk.Advance(d)
	if got := w.sentReceipt(rc); got != id || len(w.myReceipts()) != 1 {
		t.Fatalf("sent %s, receipts %+v", got, w.myReceipts())
	}
}

// DV5: a receipt the homeserver can never take (its event gone, a room not read) is dropped with
// one line, and the next is still sent.
func TestAReceiptThatCanNeverBeSentIsDropped(t *testing.T) {
	w := newWorld(t)
	rc := newFakeReceipts()
	w.receiptTeam(rc)
	rc.add(state.Receipt{ID: "$" + "a0000000000000000000000000000000000000000000", Room: proto.PurposeMain})
	w.sentReceipt(rc)
	rc.add(state.Receipt{ID: "$" + "b0000000000000000000000000000000000000000000", Room: proto.PurposeQuestions})
	w.sentReceipt(rc)
	if len(rc.left()) != 0 || w.log.count("receipt for ") != 2 {
		t.Fatalf("left %+v, log %q", rc.left(), w.log.String())
	}
}

// A refused token ends the session's receipts with the session: the receipt waits for the next.
func TestAReceiptRefusedTokenWaitsForTheNextSession(t *testing.T) {
	w := newWorld(t)
	rc := newFakeReceipts()
	w.receiptTeam(rc)
	id := w.say(alice, human("one", user(me)))
	w.srv.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/receipt/", Status: http.StatusUnauthorized, Body: []byte(`{"errcode":"M_UNKNOWN_TOKEN","error":"x"}`)})
	rc.add(state.Receipt{ID: id, Room: proto.PurposeMain})
	w.waitLog("receipts wait for the next session")
	if len(rc.left()) != 1 {
		t.Fatalf("left %+v", rc.left())
	}
}

// waitLog waits for s in link.log.
func (w *world) waitLog(s string) {
	w.t.Helper()
	for w.log.count(s) == 0 {
		if w.t.Context().Err() != nil {
			w.t.Fatalf("no %q in %q", s, w.log.String())
		}
		runtime.Gosched()
	}
}

// The receipt's removed team ends the sender: ReceiptSent's ErrRemoved is its last call.
func TestARemovedTeamSendsNoMoreReceipts(t *testing.T) {
	w := newWorld(t)
	rc := newFakeReceipts()
	rc.sentErr = team.ErrRemoved
	w.receiptTeam(rc)
	one, two := w.say(alice, human("one", user(me))), w.say(alice, human("two", user(me)))
	rc.add(state.Receipt{ID: one, Room: proto.PurposeMain})
	rc.add(state.Receipt{ID: two, Room: proto.PurposeMain})
	w.sentReceipt(rc)
	w.waitLog("receipts end: the team was removed")
	if n := len(w.myReceipts()); n != 1 {
		t.Fatalf("%d receipts", n)
	}
}

// A sent receipt whose store write failed is sent again after the backoff: a second m.read on
// the same event is harmless, a receipt lost is not.
func TestASentReceiptNotRecordedIsSentAgain(t *testing.T) {
	w := newWorld(t)
	rc := newFakeReceipts()
	rc.failOnce = errors.New("disk full")
	waits := w.receiptTeam(rc)
	id := w.say(alice, human("one", user(me)))
	rc.add(state.Receipt{ID: id, Room: proto.PurposeMain})
	w.sentReceipt(rc)
	w.clk.Advance(<-waits)
	w.sentReceipt(rc)
	if len(rc.left()) != 0 || len(w.myReceipts()) != 2 || w.log.count("was not recorded") != 1 {
		t.Fatalf("left %+v, receipts %+v, log %q", rc.left(), w.myReceipts(), w.log.String())
	}
}
