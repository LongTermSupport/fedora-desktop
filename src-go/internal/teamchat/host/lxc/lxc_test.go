package lxc

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func TestLineString(t *testing.T) {
	l := Line{Kind: Deferred, Team: mustTeam(t, "t1"), User: mustUser(t, "agent1"), Container: mustBox(t, "c1"), Why: "seat held"}
	if got := l.String(); got != "DEFERRED\tt1\tagent1@c1\tseat held" {
		t.Fatalf("line %q", got)
	}
}

func TestSummary(t *testing.T) {
	r := report([]Line{{Kind: Changed}, {Kind: Changed}, {Kind: Skipped}})
	if r.Summary != "2 changed, 1 skipped, 0 deferred" {
		t.Fatalf("summary %q", r.Summary)
	}
}

func TestOfflineErrorNamesTheContainer(t *testing.T) {
	e := &OfflineError{Container: mustBox(t, "c2"), Why: whyMissing}
	if e.Error() != "lxc: container c2 is offline (offline: container missing); start it with lxc-start -n c2" {
		t.Fatalf("%q", e.Error())
	}
	var zero paths.ContainerName
	if (&OfflineError{Container: zero}).Error() == "" {
		t.Fatal("empty")
	}
}
