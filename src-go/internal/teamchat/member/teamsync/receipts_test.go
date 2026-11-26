package teamsync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/leakcheck"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
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
	// owed is the owed receipts; failOwedOnce the next OwedSent's error, once; owedSent each
	// receipt the team said was posted.
	owed         []state.Owed
	failOwedOnce error
	owedSent     chan state.Owed
}

func newFakeReceipts() *fakeReceipts {
	return &fakeReceipts{changed: make(chan struct{}), sent: make(chan string, 16), owedSent: make(chan state.Owed, 16)}
}

func (f *fakeReceipts) PendingOwed(paths.TeamID) ([]state.Owed, <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.owed), f.changed
}

func (f *fakeReceipts) OwedSent(_ context.Context, _ paths.TeamID, o state.Owed) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owedSent <- o
	if f.failOwedOnce != nil {
		err := f.failOwedOnce
		f.failOwedOnce = nil
		return err
	}
	if f.sentErr != nil {
		return f.sentErr
	}
	f.owed = slices.DeleteFunc(f.owed, func(x state.Owed) bool { return x == o })
	f.changeLocked()
	return nil
}

func (f *fakeReceipts) addOwed(o state.Owed) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owed = append(f.owed, o)
	f.changeLocked()
}

func (f *fakeReceipts) leftOwed() []state.Owed {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.owed)
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
	_, waits := w.owedTeam(rc)
	return waits
}

// owedTeam is receiptTeam, with the team.
func (w *world) owedTeam(rc *fakeReceipts) (*Team, chan time.Duration) {
	w.t.Helper()
	tm := w.newTeam(withReceipts(rc))
	waits := make(chan time.Duration, 8)
	tm.onReceiptWait = func(d time.Duration) { waits <- d }
	r := w.startTeam(tm)
	w.batch()
	r.poll(w.t)
	return tm, waits
}

// joinedOwedTeam is receiptTeam once the reports room is joined. The join runs beside the
// session, and an owed receipt added before it is kept for a backoff on the fake clock that
// nothing advances, so a test that waits for its post hangs until go test's timeout.
func (w *world) joinedOwedTeam(rc *fakeReceipts) chan time.Duration {
	w.t.Helper()
	tm, waits := w.owedTeam(rc)
	waitReportsJoined(w.t, tm)
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
	leakcheck.Poll(w.t, func() bool { return w.log.count(s) > 0 })
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

// sentOwed is the next owed receipt the team said it posted.
func (w *world) sentOwed(rc *fakeReceipts) state.Owed {
	w.t.Helper()
	select {
	case o := <-rc.owedSent:
		return o
	case <-w.t.Context().Done():
		w.t.Fatal("no owed receipt posted")
	}
	return state.Owed{}
}

// bounceOf is a bounced receipt owed to alice for her item id.
func bounceOf(id string) state.Owed {
	return state.Owed{Of: id, To: user(alice), State: proto.ReceiptBounced, Reason: proto.ReasonRate, AtMS: startMS}
}

// postedReceipts are the teamchat.receipt events this member posted in the reports room, as
// the homeserver holds them.
func (w *world) postedReceipts() []map[string]any {
	w.t.Helper()
	page, err := w.client(me).Messages(w.t.Context(), w.ids.Reports, matrix.MessagesRequest{Dir: matrix.Forward, Limit: 1 << 10, Types: []string{proto.MessageType}})
	check(w.t, err)
	var out []map[string]any
	for _, raw := range page.Chunk {
		var ev struct {
			Sender  string         `json:"sender"`
			Content map[string]any `json:"content"`
		}
		check(w.t, json.Unmarshal(raw, &ev))
		if r, ok := ev.Content[proto.KeyReceipt].(map[string]any); ok && ev.Sender == user(me) {
			out = append(out, r)
		}
	}
	return out
}

// owedSendFault answers the next post in the reports room with status and errcode.
func (w *world) owedSendFault(status int, errcode string) {
	w.srv.Inject(fakehs.Fault{
		Method: http.MethodPut, Path: url.PathEscape(w.ids.Reports) + "/send/", Status: status,
		Body: []byte(`{"errcode":"` + errcode + `","error":"x"}`),
	})
}

// Plan 00267 Task 2.3 (D1): an owed receipt is posted once in the reports room, as A 7.8's
// teamchat.receipt to the item's sender, then reported posted, so the engine owes it no more.
func TestAnOwedReceiptIsPostedInTheReportsRoom(t *testing.T) {
	w := newWorld(t)
	rc := newFakeReceipts()
	w.joinedOwedTeam(rc)
	id := w.say(alice, human("one", user(me)))
	rc.addOwed(bounceOf(id))
	if got := w.sentOwed(rc); got != bounceOf(id) {
		t.Fatalf("posted %+v", got)
	}
	got := w.postedReceipts()
	if len(got) != 1 || got[0]["of"] != id || got[0]["state"] != proto.ReceiptBounced || got[0]["reason"] != proto.ReasonRate {
		t.Fatalf("the reports room holds %+v", got)
	}
	if to, ok := got[0]["to"].([]any); !ok || len(to) != 1 || to[0] != user(alice) {
		t.Fatalf("the receipt is to %v", got[0]["to"])
	}
	if len(rc.leftOwed()) != 0 {
		t.Fatalf("still owed %+v", rc.leftOwed())
	}
}

// Plan 00267 Task 2.3: an owed receipt is never dropped. A refused post (403, 404) keeps it with
// one line, and the backoff posts it again.
func TestARefusedOwedPostIsKeptAndPostedOnTheBackoff(t *testing.T) {
	for _, tc := range []struct {
		status  int
		errcode string
	}{{http.StatusForbidden, "M_FORBIDDEN"}, {http.StatusNotFound, "M_NOT_FOUND"}} {
		t.Run(tc.errcode, func(t *testing.T) {
			w := newWorld(t)
			rc := newFakeReceipts()
			tm, waits := w.owedTeam(rc)
			id := w.say(alice, human("one", user(me)))
			waitReportsJoined(t, tm)
			w.owedSendFault(tc.status, tc.errcode)
			rc.addOwed(bounceOf(id))
			d := <-waits
			if d != time.Second || len(rc.leftOwed()) != 1 || w.log.count("receipt for "+id+" kept") != 1 {
				t.Fatalf("wait %s, owed %+v, log %q", d, rc.leftOwed(), w.log.String())
			}
			w.clk.Advance(d)
			w.sentOwed(rc)
			if n := len(w.postedReceipts()); n != 1 || len(rc.leftOwed()) != 0 {
				t.Fatalf("%d posts, owed %+v", n, rc.leftOwed())
			}
		})
	}
}

// Plan 00267 Task 2.3: a reports room not yet joined, or none in the roster, keeps the receipt
// with one line, for the backoff.
func TestAnOwedReceiptWaitsForTheReportsRoom(t *testing.T) {
	w := newWorld(t)
	w.forbidJoin(1 << 10)
	rc := newFakeReceipts()
	waits := w.receiptTeam(rc)
	id := w.say(alice, human("one", user(me)))
	rc.addOwed(bounceOf(id))
	<-waits
	if len(rc.leftOwed()) != 1 || w.log.count("receipt for "+id+" kept") != 1 || w.reportsPosts() != 0 {
		t.Fatalf("owed %+v, log %q", rc.leftOwed(), w.log.String())
	}
}

// Plan 00267 Task 2.3: a post whose store write failed is posted again under the same
// transaction ID, so the homeserver holds one receipt (the failpoint of Task 6.3).
func TestAnOwedReceiptPostedTwiceIsOneEvent(t *testing.T) {
	w := newWorld(t)
	rc := newFakeReceipts()
	rc.failOwedOnce = errors.New("disk full")
	waits := w.joinedOwedTeam(rc)
	id := w.say(alice, human("one", user(me)))
	rc.addOwed(bounceOf(id))
	w.sentOwed(rc)
	w.clk.Advance(<-waits)
	w.sentOwed(rc)
	if n := w.reportsPosts(); n != 2 {
		t.Fatalf("%d posts, want 2", n)
	}
	if n := len(w.postedReceipts()); n != 1 || len(rc.leftOwed()) != 0 || w.log.count("was not recorded") != 1 {
		t.Fatalf("%d receipts, owed %+v, log %q", n, rc.leftOwed(), w.log.String())
	}
}

// Plan 00267 Task 2.3: a removed team ends the owed sender: OwedSent's ErrRemoved is its last
// call.
func TestARemovedTeamPostsNoMoreOwedReceipts(t *testing.T) {
	w := newWorld(t)
	rc := newFakeReceipts()
	rc.sentErr = team.ErrRemoved
	w.joinedOwedTeam(rc)
	one, two := w.say(alice, human("one", user(me))), w.say(alice, human("two", user(me)))
	rc.addOwed(bounceOf(one))
	rc.addOwed(bounceOf(two))
	w.sentOwed(rc)
	w.waitLog("owed receipts end: the team was removed")
	if n := len(w.postedReceipts()); n != 1 {
		t.Fatalf("%d receipts", n)
	}
}

// waitReportsJoined waits for tm to have joined the reports room.
func waitReportsJoined(t *testing.T, tm *Team) {
	t.Helper()
	leakcheck.Poll(t, func() bool { return tm.ReportsRoom().State == team.ReportsJoined })
}

// reportsPosts are the posts in the reports room fakehs received.
func (w *world) reportsPosts() int {
	n := 0
	for _, r := range w.srv.Requests() {
		if r.Method == http.MethodPut && (strings.Contains(r.Path, url.PathEscape(w.ids.Reports)+"/send/") || strings.Contains(r.Path, w.ids.Reports+"/send/")) {
			n++
		}
	}
	return n
}
