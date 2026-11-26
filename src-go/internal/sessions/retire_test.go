package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
)

// renamedHost is a registry holding one record, ccy-new, in a folder whose checkout records a
// conversation (with its transcript) under ccy-old, a name no record owns: a rename, or a
// session that ended or was forgotten (fedora-desktop#431).
func renamedHost(t *testing.T, marked bool) (*harness, string) {
	t.Helper()
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	dir := realDir(t, h, "a")
	recordOnly(t, h, registry.Record{Name: "ccy-new", Dir: dir, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, FreshIfNoConversation: marked})
	converse(t, h, registry.Record{Name: "ccy-old", Dir: dir, Prefix: "ccy"})
	return h, dir
}

func oldRecord(dir string) string {
	return filepath.Join(membership.SessionDir(dir, "ccy-old"), conversation.File)
}

// startedFresh is whether the one session the restore started was started with no --resume.
func startedFresh(t *testing.T, h *harness) {
	t.Helper()
	calls := startedSessions(h)
	if len(calls) != 1 {
		t.Fatalf("one session starts: %v", h.ran())
	}
	lacks(t, "fresh", strings.Join(calls[0].Args, " "), "--resume")
}

// fedora-desktop#431 review: a marked record refused beside a conversation recorded under another
// name is started fresh on purpose with no terminal: the refusal names retire-conversation, which
// retires the old record, and the restore then starts the record fresh.
func TestRetireConversationLetsAMarkedRecordStartFresh(t *testing.T) {
	h, dir := renamedHost(t, true)
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	contains(t, "refusal", h.stdout()+h.stderr(), "ccy-new UNRESTORABLE", "ccy-sessions retire-conversation "+dir+" ccy-old",
		"then ccy-sessions restore ccy-new starts it fresh")

	h.out.Reset()
	h.errOut.Reset()
	exitCodeIs(t, h.run("retire-conversation", dir, "ccy-old"), 0, h)
	if h.stdout() != "ccy-old RETIRED\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "says", h.stderr(), oldRecord(dir), conversation.RetiredFile)
	if _, err := os.Lstat(oldRecord(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the old record is still there: %v", err)
	}
	h.errOut.Reset()
	exitCodeIs(t, h.run("restore", "ccy-new"), 0, h)
	startedFresh(t, h)
}

// An unmarked record is started fresh on purpose by the commands its refusal names: the old
// record retired, the record declared again marked, then the restore.
func TestRetireConversationThenDeclareStartsAnUnmarkedRecordFresh(t *testing.T) {
	h, dir := renamedHost(t, false)
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	declare := "ccy-sessions declare --name ccy-new --fresh-if-no-conversation --replace " + dir + " --"
	contains(t, "refusal", h.stdout()+h.stderr(), declare+", then ccy-sessions restore ccy-new starts it fresh")

	lacks(t, "not stale", h.stderr(), "stale record")

	exitCodeIs(t, h.run("retire-conversation", dir, "ccy-old"), 0, h)
	h.out.Reset()
	asUser(h)
	exitCodeIs(t, h.run(strings.Fields(strings.TrimPrefix(declare, "ccy-sessions "))...), 0, h)
	if h.stdout() != "ccy-new REPLACED\n" {
		t.Fatalf("declare stdout %q", h.stdout())
	}
	exitCodeIs(t, h.run("restore", "ccy-new"), 0, h)
	startedFresh(t, h)
}

// A name a session record answers to is that session's: its conversation is retired by
// forgetting it, never under it. A live session's is refused too, as is a live set that cannot be
// read; nothing to retire is a refusal naming the path, and every refusal keeps the record.
func TestRetireConversationRefusals(t *testing.T) {
	h, dir := renamedHost(t, true)
	exitCodeIs(t, h.run("retire-conversation", dir, "ccy-new"), 1, h)
	contains(t, "owned", h.stderr(), "ccy-new has a session record", "ccy-sessions forget ccy-new")
	oldKept(t, dir)

	recordOnly(t, h, registry.Record{Name: "ccy-old", Dir: "/elsewhere", Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	h.errOut.Reset()
	exitCodeIs(t, h.run("retire-conversation", dir, "ccy-old"), 1, h)
	contains(t, "owned elsewhere", h.stderr(), "ccy-old has a session record")
	oldKept(t, dir)

	h, dir = renamedHost(t, true)
	h.sessions("ccy-old 0 " + dir)
	exitCodeIs(t, h.run("retire-conversation", dir, "ccy-old"), 1, h)
	contains(t, "live", h.stderr(), "ccy-old is a live session")
	oldKept(t, dir)

	h, dir = renamedHost(t, true)
	h.tmuxFail("list-sessions", "boom")
	exitCodeIs(t, h.run("retire-conversation", dir, "ccy-old"), 1, h)
	contains(t, "unreadable", h.stderr(), "could not be read")
	oldKept(t, dir)

	h, dir = renamedHost(t, true)
	exitCodeIs(t, h.run("retire-conversation", dir, "ccy-other"), 1, h)
	contains(t, "nothing", h.stderr(), "no conversation record of ccy-other", filepath.Join(membership.SessionDir(dir, "ccy-other"), conversation.File))

	oldKept(t, dir)
}

func oldKept(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Lstat(oldRecord(dir)); err != nil {
		t.Fatalf("the old record is kept: %v", err)
	}
}

func TestRetireConversationUsage(t *testing.T) {
	cases := []struct {
		args []string
		msg  string
	}{
		{[]string{"retire-conversation"}, "retire-conversation takes a checkout and a session name."},
		{[]string{"retire-conversation", "/a"}, "retire-conversation takes a checkout and a session name."},
		{[]string{"retire-conversation", "/a", "b", "c"}, "retire-conversation takes a checkout and a session name."},
		{[]string{"retire-conversation", "rel", "ccy-old"}, "the checkout 'rel' is not an absolute path."},
		{[]string{"retire-conversation", "/a", "a/b"}, "'a/b' is not a session name."},
	}
	for _, c := range cases {
		h := staleHost(t)
		exitCodeIs(t, h.run(c.args...), 64, h)
		if want := "ccy-sessions: " + c.msg + " See ccy-sessions --help.\n"; h.stderr() != want {
			t.Fatalf("%v: %q", c.args, h.stderr())
		}
	}
	for _, flag := range []string{"-h", "--help"} {
		h := staleHost(t)
		exitCodeIs(t, h.run("retire-conversation", flag), 0, h)
		contains(t, flag, h.stdout(), "ccy-sessions retire-conversation [--] CHECKOUT NAME")
	}
}

// Forgetting a session retires its own conversation record too: once the session is gone, the
// record would stop every later fresh start in the checkout. A marked record there then starts
// fresh.
func TestForgetRetiresTheSessionsConversationRecord(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	dir := realDir(t, h, "a")
	gone := registry.Record{Name: "ccy-old", Dir: dir, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true}
	record(t, h, gone)
	exitCodeIs(t, h.run("forget", "ccy-old"), 0, h)
	if h.stdout() != "ccy-old FORGOTTEN\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "says", h.stderr(), "retired its conversation record "+oldRecord(dir))
	if _, err := os.Lstat(oldRecord(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the record is still there: %v", err)
	}
	recordOnly(t, h, registry.Record{Name: "ccy-new", Dir: dir, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, FreshIfNoConversation: true})
	exitCodeIs(t, h.run("restore"), 0, h)
	startedFresh(t, h)
}
