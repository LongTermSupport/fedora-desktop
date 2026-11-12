package wiring

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/testfiles"
)

// The scripted one-shot (Plan 00246, X30's message leg): a member with no session running sends
// and reads history from a script, each verb the CMD of its own `run --plain`, which starts the
// keeper and link, runs the verb, and ends the session. The verb's record is the session's
// stdout and the verb's status is run's.

// oneShot is startOneShot run to its end: its exit code and stdout.
func (w *world) oneShot(tag, stdin string, verb ...string) (int, string) {
	w.t.Helper()
	return w.startOneShot(tag, stdin, verb...).finish()
}

// startOneShot is `run --plain --state S --seat alpha/<seat> -- teamchat <verb...>` with stdin,
// started. It runs as a script beside any session would: the same seat, a runtime directory of
// its own (tag, short for the socket path's 107 bytes).
func (w *world) startOneShot(tag, stdin string, verb ...string) *session {
	w.t.Helper()
	o := *w
	o.rt = w.dir + "/" + tag
	check(w.t, os.Mkdir(o.rt, 0o700))
	s := o.startArgv("--plain", append([]string{w.kit + "/bin/teamchat"}, verb...),
		"--state", w.state, "--seat", teamName+"/"+w.seatName)
	if n, err := s.stdin.WriteString(stdin); err != nil || n != len(stdin) {
		w.t.Fatalf("the one-shot's stdin: wrote %d of %d: %v", n, len(stdin), err)
	}
	check(w.t, s.stdin.Close())
	return s
}

// finish is the one-shot's exit code and stdout, once it has ended.
func (s *session) finish() (int, string) {
	s.w.t.Helper()
	s.wait()
	st := <-s.ended
	s.ended <- st
	eventually(s.w.t, s.outputsRead, s.describe)
	if st.Code != 0 {
		s.w.t.Logf("exit %d: %s", st.Code, s.describe())
	}
	return st.Code, s.out.text()
}

// A first one-shot on a seat never used before: its say is posted (an event id, not queued),
// the coordinator receives it, and a second one-shot's history lists it as sent.
func TestE2EOneShotSayAndHistory(t *testing.T) {
	k := newWorld(t, fakeServer(t))
	cs := k.member(coord, coordSeat, "c").seated()
	const text = "one-shot from a script"
	code, out := k.oneShot("o1", text+"\n", "say", "--to-coordinator", "--wait-contact", "--team", teamName)
	recs := parseRecords(t, []byte(out))
	if code != 0 || len(recs) != 1 || recs[0]["kind"] != "sent" || recs[0]["queued"] != false {
		t.Fatalf("the one-shot say: exit %d, records %v", code, recs)
	}
	id, ok := recs[0]["id"].(string)
	if !ok {
		t.Fatalf("the one-shot say has no event id: %v", recs)
	}
	if got := cs.recvUntil(id); !hasKind(got, id, "peer") {
		t.Fatalf("C's recv: %v", got)
	}
	code, out = k.oneShot("o2", "", "history", "--team", teamName)
	if code != 0 || !strings.Contains(out, id) {
		t.Fatalf("the one-shot history: exit %d, stdout %s", code, out)
	}
}

// waitingLine is the link's log line for a send held until its team's first contact.
const waitingLine = "a send waits for the team's first contact"

// waitingIn is whether the one-shot at tag's link has logged that a send waits for its team's
// first contact.
func (w *world) waitingIn(tag string) bool {
	runs, err := filepath.Glob(w.dir + "/" + tag + "/teamchat/run/*")
	check(w.t, err)
	for _, run := range runs {
		if data, err := testfiles.Try(run, "link.log"); err == nil && strings.Contains(string(data), waitingLine) {
			return true
		}
	}
	return false
}

// A one-shot's say that reaches the link before its team's first contact in the run (the
// homeserver slow to answer the verification) waits for that contact and is posted: on a seat
// never verified (no roster cache, which would be 20 unreachable) and on one with a cache
// (which would be queued, and left in the outbox when the one-shot ends).
func TestE2EOneShotSayWaitsForFirstContact(t *testing.T) {
	k := newWorld(t, fakeServer(t))
	cs := k.member(coord, coordSeat, "c").seated()
	for i, row := range []string{"no roster cache", "a roster cache"} {
		tag := "g" + strconv.Itoa(i)
		g := k.hs.fake.Gate(http.MethodGet, url.PathEscape(k.main)+"/state")
		t.Cleanup(g.Open)
		text := "sent before the first contact, " + row
		s := k.startOneShot(tag, text+"\n", "say", "--to-coordinator", "--wait-contact", "--team", teamName)
		// The homeserver answers the verification only once the say has arrived and waits.
		eventually(t, func() bool { return k.waitingIn(tag) || s.exited() }, s.describe)
		g.Open()
		code, out := s.finish()
		recs := parseRecords(t, []byte(out))
		if code != 0 || len(recs) != 1 || recs[0]["queued"] != false {
			t.Fatalf("%s: the one-shot say: exit %d, records %v", row, code, recs)
		}
		id, ok := recs[0]["id"].(string)
		if !ok {
			t.Fatalf("%s: no event id: %v", row, recs)
		}
		cs.recvUntil(id)
	}
}

// A one-shot on a seat a running session holds is refused at once, exit 75, and posts nothing.
func TestE2EOneShotRefusedWhileHeld(t *testing.T) {
	k := newWorld(t, fakeServer(t))
	k.seated()
	const text = "must not be posted"
	code, out := k.oneShot("o1", text+"\n", "say", "--to-coordinator", "--wait-contact", "--team", teamName)
	if code != 75 || out != "" {
		t.Fatalf("a one-shot on a held seat: exit %d, stdout %q", code, out)
	}
	if ids := k.textsFrom(me, text); len(ids) != 0 {
		t.Fatalf("a refused one-shot posted %v", ids)
	}
}
