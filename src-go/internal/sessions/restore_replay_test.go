package sessions

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
)

// Plan 00266 Task A.5: a cc record whose words would reach claude on the host is refused by the
// restore and by --check alike, naming the word and the way out; nothing is started.
func TestRestoreRefusesACCRecordWithAWordForTheHostsClaude(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{
		Name: "cc-a", Dir: a, Launcher: "/launch/cc", Prefix: "cc", Restore: true,
		Args: []string{"--bus", "--mcp-config", "/tmp/x.json"},
	})
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	if h.stdout() != "cc-a UNRESTORABLE "+whyArgumentsRefused+"\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "check", h.stderr(), "its recorded arguments hold '--mcp-config'",
		"Launch it again with cc in "+a+" without it", "ccy-sessions forget cc-a")
	h.out.Reset()
	exitCodeIs(t, h.run("restore"), 1, h)
	if len(startedSessions(h)) != 0 {
		t.Fatalf("nothing started: %v", h.ran())
	}
}

// A ccy record holding a flag ccy acts on at launch and no launch records is refused, naming the
// declare that writes it again; a word for the container's claude is replayed as it was.
func TestRestoreRefusesACcyRecordWithAHostActionNoLaunchRecords(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{
		Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true,
		Args: []string{"--token", "work", "--export-token", "work"},
	})
	record(t, h, registry.Record{
		Name: "ccy-b", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true,
		Args: []string{"--model", "opus", "--", "--verbose"},
	})
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	if h.stdout() != "ccy-a UNRESTORABLE "+whyArgumentsRefused+"\nccy-b RESTORABLE\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "check", h.stderr(), "its recorded arguments hold '--export-token'",
		"ccy-sessions declare --replace --name ccy-a "+a+" -- ")
	h.out.Reset()
	exitCodeIs(t, h.run("restore"), 1, h)
	calls := startedSessions(h)
	if len(calls) != 1 {
		t.Fatalf("one session started: %v", h.ran())
	}
	contains(t, "ccy-b", strings.Join(calls[0].Args, " "), "/launch/ccy --model opus --supervise -- --verbose --resume ")
}
