package wiring

import (
	"maps"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The reports room end to end (SPEC-n3f RP1, RP3, RP5, n3f-D1, OF1-n3f): the real binary's
// `report`, its stdin rule, the room it posts to, and the eager join's refusal in `status`.

const (
	// reportCap is RP3's cap on a report's text, in bytes (n3f-D1's C).
	reportCap = 1500
	// reportsJoinDeadline is RP1's deadline on the reports join, which runs once the team's first
	// long-poll is sent.
	reportsJoinDeadline = 30 * time.Second
	// refusedBound is how soon a refused reports join shows in status: a real process's work to
	// reach its first long-poll (settle), then the join inside its own deadline.
	refusedBound = settle + reportsJoinDeadline
	// peerSeat is peer's handle's <seat>.
	peerSeat = "3"
)

// report runs `report --team alpha` in the session with text on its stdin; it must be posted.
func (s *session) report(text string) map[string]any {
	s.w.t.Helper()
	r := s.sent(new(text), "report", "--team", teamName)
	if r["room"] != proto.PurposeReports || r["queued"] == true {
		s.w.t.Fatalf("the report was not posted to the reports room: %v", r)
	}
	return r
}

// reportsFrom are the bodies of local's reports in the reports room, newest first, read as
// local (whose link joined it).
func (w *world) reportsFrom(local string) []string {
	w.t.Helper()
	page, err := w.client(local).Messages(w.t.Context(), w.reports, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 100})
	check(w.t, err)
	var bodies []string
	for _, raw := range page.Chunk {
		v, err := proto.Parse(raw)
		check(w.t, err)
		content, ok := v.Get("content")
		if !ok || str(v, "sender") != w.user(local) || str(v, "type") != proto.MessageType {
			continue
		}
		if _, isReport := content.Get(proto.KeyReport); isReport {
			bodies = append(bodies, str(content, "body"))
		}
	}
	return bodies
}

// withoutID is a sent record without its event id, which two posts never share.
func withoutID(r map[string]any) map[string]any {
	out := maps.Clone(r)
	delete(out, "id")
	return out
}

// localpart is a room id's opaque part, as it appears in a request's percent-encoded path.
func localpart(roomID string) string {
	return strings.SplitN(strings.TrimPrefix(roomID, "!"), ":", 2)[0]
}

// OF1-n3f's `report` row: with one worker stopped, the other's report exits 0 with the record it
// answers when every member runs, and each report is in the reports room once.
func TestE2EAReportWithAWorkerStopped(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	s := w.seated()
	p := w.member(peer, peerSeat, "p").seated()
	const allRunning, oneStopped = "All running\nboth workers are up.", "One stopped\nmyrepo.3 is not running."
	running := s.report(allRunning)
	kill(t, p.keeper.Pid(), syscall.SIGTERM)
	p.wait()
	stopped := s.report(oneStopped)
	if !maps.Equal(withoutID(stopped), withoutID(running)) {
		t.Errorf("with a worker stopped the report answered %v, with all running %v", stopped, running)
	}
	if got := w.reportsFrom(me); len(got) != 2 || got[0] != oneStopped || got[1] != allRunning {
		t.Errorf("the reports room holds %q from me, want each report once", got)
	}
}

// n3f-D1's three cases through the binary, with RP3's cap: C bytes, a newline and one more byte
// are refused 10 body; C + 1 bytes with no newline are refused 10 body; C bytes and a newline are
// accepted, the body C bytes. Only the accepted one reaches the reports room.
func TestE2EReportStdinRule(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	s := w.seated()
	headline := "Exactly at the cap\n"
	text := headline + strings.Repeat("x", reportCap-len(headline))
	for name, stdin := range map[string]string{
		"C bytes, a newline and x": text + "\nx",
		"C + 1 bytes":              text + "x",
	} {
		res := s.send(new(stdin), "report", "--team", teamName)
		if res.ExitCode != 10 || !strings.Contains(string(res.Stderr), `"code":"body"`) {
			t.Errorf("%s: exit %d, stdout %s, stderr %s, want 10 body", name, res.ExitCode, res.Stdout, res.Stderr)
		}
	}
	s.report(text + "\n")
	if got := w.reportsFrom(me); len(got) != 1 || got[0] != text {
		t.Fatalf("the reports room holds %d reports from me (want one, of %d bytes): %q", len(got), reportCap, got)
	}
}

// RP5: a report wakes no one. K's report, then K's say to C: C's recv gives the say alone, so C's
// sync has passed the report and found nothing in it; C's inbox is empty.
func TestE2EAReportGivesNoMemberAnItem(t *testing.T) {
	k, _, ks, cs := pair(t, fakeServer(t))
	ks.report("Done\nthe fix is in.")
	said := ks.posted(new("after the report"), "say", "--to", "myrepo.1", "--team", teamName)
	if got := cs.recvUntil(said); len(got) != 1 {
		t.Errorf("C's recv after K's report: %v, want K's say alone", got)
	}
	if owed := cs.records("inbox", "--team", teamName); len(owed) != 0 {
		t.Errorf("C's inbox after the report: %v", owed)
	}
	if got := k.reportsFrom(me); len(got) != 1 {
		t.Errorf("the reports room holds %d reports from K, want 1", len(got))
	}
}

// RP1: a reports join the homeserver refuses shows as the room refused, with its errcode, in
// status; main's items are delivered as before.
func TestE2EARefusedReportsJoinIsRefusedInStatus(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	w.hs.fake.Inject(fakehs.Fault{
		Method: http.MethodPost, Path: localpart(w.reports), Status: http.StatusForbidden,
		Body: []byte(`{"errcode":"M_FORBIDDEN","error":"not allowed"}`),
	})
	s := w.seated()
	var room map[string]any
	within(t, refusedBound, func() bool {
		for _, r := range s.status("room") {
			if r["purpose"] == proto.PurposeReports {
				room = r
			}
		}
		return room != nil && room["state"] == "refused"
	}, func() string { return "the reports room record: " + s.describe() })
	if room["check"] != "M_FORBIDDEN" {
		t.Errorf("the refused room's check = %v, want its errcode", room["check"])
	}
	s.recvUntil(w.say("main still delivers"))
}
