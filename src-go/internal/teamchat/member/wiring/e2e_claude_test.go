package wiring

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/colour"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/kit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/testfiles"
)

// The member end to end under --claude (SPEC-n3 n3e): the keeper, its link and the client verbs
// as in e2e_test.go, and CMD a fake Claude Code (e2e_claudecode_test.go) that runs the kit's own
// hook and status line commands. The hooks and the status line are the real verbs of the real
// binary; Claude Code is not, so what these rows prove is what the verbs do for the commands a
// kit names, not what Claude Code does with their answers.

// The hook events, as Claude Code names them.
const (
	evStart  = "SessionStart"
	evPrompt = "UserPromptSubmit"
	evStop   = "Stop"
	evEnd    = "SessionEnd"
)

// msgStarting is the verbs' exit 69 text for a link that has not answered while its keeper runs
// (B 2.4.2), which a hook gives as its own recovery line.
const msgStarting = "teamchat is starting or restarting; team messages wait on the server"

// sgrRed is the escape the status line draws a link that does not answer with.
const sgrRed = "\x1b[31m"

// ccSeq numbers the commands sent to a fake Claude Code.
var ccSeq atomic.Int64

// ccAsk sends the session's fake Claude Code a command and decodes its reply.
func ccAsk[T any](s *session, args ...string) T {
	s.w.t.Helper()
	n := strconv.FormatInt(ccSeq.Add(1), 10)
	_, err := io.WriteString(s.stdin, n+" "+strings.Join(args, " ")+"\n")
	check(s.w.t, err)
	prefixes := []string{"cc-reply " + n + " ", "cc-error " + n + " "}
	var line string
	eventually(s.w.t, func() bool {
		for _, p := range prefixes {
			if l, ok := s.out.find(p); ok {
				line = l
				return true
			}
		}
		return s.exited()
	}, s.describe)
	body, ok := strings.CutPrefix(line, prefixes[0])
	if !ok {
		s.w.t.Fatalf("the fake Claude Code, asked %v, said %q: %s", args, line, s.describe())
	}
	var out T
	check(s.w.t, json.Unmarshal([]byte(body), &out))
	return out
}

// hook is the plugin's hook command for event, run by the fake Claude Code.
func (s *session) hook(event string, args ...string) ccRun {
	s.w.t.Helper()
	return ccAsk[ccRun](s, append([]string{"hook", event}, args...)...)
}

// statusLine is the settings' status line command, run by the fake Claude Code.
func (s *session) statusLine() ccRun { return ccAsk[ccRun](s, "statusline") }

// frames are the lines the fake Claude Code's inbox has read.
func (s *session) frames() []ccFrame { return ccAsk[[]ccFrame](s, "frames") }

// claudeKit makes the world's kit the kit a Claude Code launch needs: kit.Assemble over the empty
// directory the bare kit stood in, so it holds the plugin whose hooks.json runs `<kit>/bin/teamchat
// hook <event>` and the settings whose statusLine runs `<kit>/bin/teamchat status-line` (PK1); and
// a link of the test binary that is Claude Code by its name.
func (w *world) claudeKit() {
	w.t.Helper()
	root, err := os.OpenRoot(w.dir)
	check(w.t, err)
	check(w.t, errors.Join(root.RemoveAll("k"), root.MkdirAll("k/"+testDigest, 0o700), root.Close()))
	k, err := paths.KitAt(w.kit)
	check(w.t, err)
	binary := testfiles.Executable(w.t)
	check(w.t, errors.Join(kit.Assemble(w.kit, k, binary), binary.Close()))
	check(w.t, os.Link(w.agent, w.dir+"/"+claudeName))
}

// desiredFlags are the flags of a run that is told its membership by a desired file that gives
// the world's seat and says inbound: a run with no desired file refuses the inbox (KP1).
func (w *world) desiredFlags(inbound string) []string {
	w.t.Helper()
	w.desireInbound(1, []state.DesiredSeat{{Team: alphaID, Seat: mySeat}}, nil, inbound)
	return []string{"--desired", w.desired, "--state", w.state}
}

// claude is `run --claude` over the world's kit, its fake Claude Code begun: its inbox socket and
// transcript made, and no hook yet run.
func (w *world) claude(flags ...string) *session {
	w.t.Helper()
	w.claudeKit()
	s := w.awaitCmd(w.startCmd("--claude", w.dir+"/"+claudeName, flags...), claudeName)
	begun := ccAsk[ccBegun](s, "begin")
	if !strings.HasPrefix(begun.Socket, w.rt+"/cc-socks/") || begun.Session != ccSessionID {
		w.t.Fatalf("begin: %+v", begun)
	}
	s.transcript = begun.Transcript
	return s
}

// claudeSeated is claude, its SessionStart hook run until the link has answered it (startAnswered:
// a SessionStart the link answers late tells it no session, so a run whose link never learns where
// to deliver waits for ever for its first delivery) and the seat held.
func (w *world) claudeSeated(flags ...string) *session {
	w.t.Helper()
	s := w.claude(flags...)
	s.startAnswered()
	s.seatedIn()
	return s
}

// startAnswered is the answer of the session's SessionStart hook once the link has answered it.
// The hook dials the link once and, when the answer is later than its 300 ms deadline (LN2), as
// it is on a loaded host, gives its own recovery line, which says teamchat is starting, and the
// link never learns the session. Claude Code fires SessionStart again on a resume, so the row
// asks again until the answer is not that line. Only SessionStart: Stop blocks once per pending
// set, so a Stop the link answered late would answer differently when asked again.
func (s *session) startAnswered() string {
	s.w.t.Helper()
	starting := withContext(evStart, msgStarting)
	var got string
	eventually(s.w.t, func() bool {
		got = answer(s.w.t, s.hook(evStart))
		return got != starting
	}, s.describe)
	return got
}

// answer is the one line a hook that ran answered: it exits 0, says nothing on stderr and ends
// its answer with one newline.
func answer(t *testing.T, r ccRun) string {
	t.Helper()
	line, ok := strings.CutSuffix(r.Stdout, "\n")
	if r.Exit != 0 || r.Stderr != "" || !ok || strings.Contains(line, "\n") {
		t.Fatalf("%s: exit %d, stdout %q, stderr %q", r.Command, r.Exit, r.Stdout, r.Stderr)
	}
	return line
}

// withContext is the answer a hook gives with text as the context of event.
func withContext(event, text string) string { return string(hooktext.Context(event, text).Line) }

// blocking is the answer Stop gives that blocks, for reason.
func blocking(reason string) string { return string(hooktext.Block(reason).Line) }

// frameOf is the SessionStart frame the world's one team gives a session in mode. The team has
// been verified by the time these runs start a session, so its role and coordinator are current
// (issue 192: a record not verified current names neither).
func (w *world) frameOf(mode string) string {
	return hooktext.Frame([]hooktext.FrameTeam{{Team: w.team.id, Handle: w.self, Role: proto.RoleWorker, Coordinator: coord, Current: true}}, mode)
}

// userFrames are the frames of the fake inbox that carry a delivery: the user lines.
func userFrames(all []ccFrame) []ccFrame {
	var out []ccFrame
	for _, f := range all {
		if !f.Auth {
			out = append(out, f)
		}
	}
	return out
}

// A delivery through the real hooks (DV1, DV3, DV4, DV5, HK1, PK1): SessionStart gives the frame
// and the socket, a human's message reaches the session's inbox as one user line with the
// delivery envelope, the transcript carries it, the human gets a read receipt, and the item is no
// longer owed.
func TestClaudeDelivers(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	s := w.claude(w.desiredFlags(state.InboundAccept)...)
	if got, want := s.startAnswered(), withContext(evStart, w.frameOf(state.DeliverySocket)); got != want {
		t.Fatalf("SessionStart answered\n%s\nwant\n%s", got, want)
	}
	s.seatedIn()
	human := w.say("hello claude")
	var sent []ccFrame
	eventually(t, func() bool {
		sent = userFrames(s.frames())
		return len(sent) > 0
	}, s.describe)
	if len(sent) != 1 || !sent[0].Accepted || sent[0].Reason != "" || !strings.Contains(sent[0].Content, "hello claude") {
		t.Fatalf("the inbox's user lines: %+v", sent)
	}
	envelope := sent[0].Content
	fields := strings.Fields(envelope)
	if len(fields) < 3 || !strings.HasPrefix(envelope, "teamchat delivery ") || !strings.HasSuffix(fields[2], ":") {
		t.Fatalf("the delivery's first line: %q", envelope)
	}
	needle := "teamchat delivery " + fields[2]
	if text := ccAsk[string](s, "transcript"); !strings.Contains(text, needle) || !strings.Contains(text, "hello claude") {
		t.Fatalf("the transcript does not hold %q and the message:\n%s", needle, text)
	}
	want := fakehs.Receipt{UserID: w.user(me), RoomID: w.main, Type: "m.read", EventID: human}
	eventually(t, func() bool { return slices.Contains(w.hs.fake.Receipts(), want) }, func() string {
		return "receipts: " + strconv.Itoa(len(w.hs.fake.Receipts())) + ", none for the human's message"
	})
	if recs := s.records("recv", "--team", teamName); len(recs) != 0 {
		t.Fatalf("recv after a delivery proved: %v", recs)
	}
	var owed []map[string]any
	eventually(t, func() bool {
		owed = s.records("inbox", "--team", teamName)
		return len(owed) == 0
	}, func() string { return "inbox after the delivery: " + strconv.Itoa(len(owed)) + " owed" })
	if again := userFrames(s.frames()); len(again) != 1 {
		t.Fatalf("a second delivery: %+v", again)
	}
}

// procIs is whether pid is in the scheduler state letter ('T' is stopped).
func procIs(pid int, letter byte) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	i := bytes.LastIndexByte(stat, ')')
	return i >= 0 && i+2 < len(stat) && stat[i+2] == letter
}

// freeze sends pid SIGSTOP and waits until it is stopped; the cleanup that continues it is
// registered after the session's, so it runs first: a stopped process cannot see its parent die,
// and the session's own cleanup would wait for it.
func freeze(t *testing.T, pid int) {
	t.Helper()
	kill(t, pid, syscall.SIGSTOP)
	t.Cleanup(func() {
		if err := syscall.Kill(pid, syscall.SIGCONT); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Error(err)
		}
	})
	eventually(t, func() bool { return procIs(pid, 'T') }, func() string { return "process " + strconv.Itoa(pid) + " is not stopped" })
}

// lastLink is the pid of the link the keeper started last.
func (s *session) lastLink() int {
	s.w.t.Helper()
	links := s.links()
	if len(links) == 0 {
		s.w.t.Fatalf("no link started: %s", s.describe())
	}
	return links[len(links)-1]
}

// A hook whose link does not answer (HK1, OF1): the link is stopped, so its socket accepts and
// says nothing, and the hook gives its own line within its deadline, as the verbs' recovery text
// on the two events that take context and {} on the two that do not, each exiting 0. Once the
// link is continued the hook is answered by the link again.
func TestClaudeLinkSilent(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	s := w.claude(w.desiredFlags(state.InboundAccept)...)
	link := s.lastLink()
	freeze(t, link)
	for _, ev := range []string{evPrompt, evStart} {
		if got, want := answer(t, s.hook(ev)), withContext(ev, msgStarting); got != want {
			t.Fatalf("%s with the link stopped answered\n%s\nwant\n%s", ev, got, want)
		}
	}
	for _, ev := range []string{evStop, evEnd} {
		if got := answer(t, s.hook(ev)); got != "{}" {
			t.Fatalf("%s with the link stopped answered %s", ev, got)
		}
	}
	kill(t, link, syscall.SIGCONT)
	eventually(t, func() bool { return !procIs(link, 'T') }, s.describe)
	if got, want := s.startAnswered(), withContext(evStart, w.frameOf(state.DeliverySocket)); got != want {
		t.Fatalf("SessionStart with the link continued answered\n%s\nwant\n%s", got, want)
	}
}

// The status line through the command the kit's settings name (SL1, PK1): the team's segment in
// its colour, one row; and, with the link gone and its keeper unable to restart it, the one red
// form that says teamchat is starting. Both exit 0.
func TestClaudeStatusLine(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	s := w.claudeSeated(w.desiredFlags(state.InboundAccept)...)
	segment := colour.SGR(colour.Auto(teamName)) + "👥 " + teamName + ": " + kShort + " (w)" + colour.Reset + "\n"
	var got ccRun
	eventually(t, func() bool {
		got = s.statusLine()
		return got.Stdout == segment
	}, func() string {
		return "the status line: " + strconv.Quote(got.Stdout) + " stderr " + strconv.Quote(got.Stderr)
	})
	if got.Exit != 0 || got.Stderr != "" || got.Command != w.kit+"/bin/teamchat status-line" {
		t.Fatalf("the status line: %+v", got)
	}
	// The keeper stopped cannot restart the link or say it is restarting; the link killed has
	// let go of link.lock: what is left to read is keeper.json's running link and no answer.
	freeze(t, s.keeper.Pid())
	link := s.lastLink()
	kill(t, link, syscall.SIGKILL)
	eventually(t, func() bool { return gone(link) }, s.describe)
	down := sgrRed + "👥 teamchat starting" + colour.Reset + "\n"
	got = s.statusLine()
	if got.Exit != 0 || got.Stderr != "" || got.Stdout != down {
		t.Fatalf("the status line with the link gone: %+v, want %q", got, down)
	}
}

// Stop blocks (HK2), on a session launched to refuse the inbox, so that its deliveries are not
// pushed and every pending item is one it must read: the first Stop with pending items blocks,
// once for each distinct set of them, so the second Stop with the same set is {}, a Stop with a
// stop_hook_active true is {} and changes nothing, and a new item blocks again. Nothing goes to the
// inbox socket (DV1).
func TestClaudeStopBlocks(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	s := w.claude(w.desiredFlags(state.InboundRefuse)...)
	if got, want := s.startAnswered(), withContext(evStart, w.frameOf(state.DeliveryWait)); got != want {
		t.Fatalf("SessionStart of a run that refuses the inbox answered\n%s\nwant\n%s", got, want)
	}
	s.seatedIn()
	owe := func(n int) {
		t.Helper()
		var owed []map[string]any
		eventually(t, func() bool {
			owed = s.records("inbox", "--team", teamName)
			return len(owed) == n
		}, func() string { return "inbox: " + strconv.Itoa(len(owed)) + " owed, want " + strconv.Itoa(n) })
	}
	pending := func(n int64) string { return hooktext.StopPending(hooktext.Counts{Total: n, Humans: n}) }
	w.say("first")
	owe(1)
	// The first block also says no wait is running, which a run that refuses the inbox is told.
	if got, want := answer(t, s.hook(evStop)), blocking(pending(1)+" "+hooktext.StopNoWaker()); got != want {
		t.Fatalf("the first Stop answered\n%s\nwant\n%s", got, want)
	}
	if got := answer(t, s.hook(evStop)); got != "{}" {
		t.Fatalf("the second Stop of the same pending set answered %s", got)
	}
	w.say("second")
	owe(2)
	if got := answer(t, s.hook(evStop, "active")); got != "{}" {
		t.Fatalf("a Stop with stop_hook_active true answered %s", got)
	}
	if got, want := answer(t, s.hook(evStop)), blocking(pending(2)); got != want {
		t.Fatalf("the Stop of a new pending set answered\n%s\nwant\n%s", got, want)
	}
	if sent := s.frames(); len(sent) != 0 {
		t.Fatalf("the inbox of a run that refuses it was written to: %+v", sent)
	}
}
