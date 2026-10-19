package lxcadd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The declared members whose containers are stopped (SPEC-lxc-stopped-members.md L11):
// agent@container-c in alpha (stopped), a member of another team in the same stopped container,
// alpha's member in the running container-b, and one alpha has let go of.
const declaredAroundStopped = "LXCMEMBER\talpha\tcontainer-c\tagent\t-\tworker\tpresent\n" +
	"LXCMEMBER\tbeta\tcontainer-c\tother\t-\tworker\tpresent\n" +
	"LXCMEMBER\talpha\tcontainer-b\tagent\t-\tworker\tpresent\n" +
	"LXCMEMBER\talpha\tcontainer-c\tgone\t-\tworker\tabsent\n"

const stoppedNotice = "Declared in alpha, container not running: agent@container-c. The play skips a member " +
	"already joined and defers one not yet joined; neither blocks this add. Nothing is started."

func TestTheTeamsDeclaredMembersInStoppedContainersAreSaidBeforeTheSummary(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-members", declaredAroundStopped)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	said := r.ErrOut.String()
	at := strings.Index(said, stoppedNotice)
	if at < 0 {
		t.Fatalf("stderr lacks %q:\n%s", stoppedNotice, said)
	}
	if summary := strings.Index(said, "The expected handle is derived"); summary < at {
		t.Fatalf("the notice is not before the summary:\n%s", said)
	}
	if strings.Contains(said, "other@container-c") || strings.Contains(said, "gone@container-c") {
		t.Fatalf("a member of another team, or one not declared present, is named:\n%s", said)
	}
	for _, c := range r.Fake.Calls() {
		if strings.Contains(c.Line(), "lxc-start") {
			t.Fatalf("a container was started: %s", c.Line())
		}
	}
}

func TestNoStoppedNoticeWhenEveryDeclaredMemberIsRunning(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-members", "LXCMEMBER\talpha\tcontainer-b\tagent\t-\tworker\tpresent\n")
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if strings.Contains(r.ErrOut.String(), "container not running") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

// writeRecord is alpha's LXC member record, as the play leaves it.
func (r *rig) writeRecord(t *testing.T, body string) {
	t.Helper()
	teamtest.MustWrite(t, filepath.Join(r.Host.AppliedDir, "lxc", "alpha.json"), body)
}

func TestANewMemberTheRecordShowsDeferredIsSaidDeferredNotJoined(t *testing.T) {
	r := newRig(t)
	r.writeRecord(t, `{"version": 1, "team": "alpha", "members": [
		{"container": "container-a", "user": "agent", "state": "present", "status": "deferred", "handle": null,
		 "checkout": "/srv/agent/one", "why": "LXC container container-a is STOPPED: start it", "since": 1792000000}]}`)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	said := r.ErrOut.String()
	want := "agent@container-a is declared in alpha but deferred, not joined: LXC container container-a is STOPPED: start it"
	if !strings.Contains(said, want) {
		t.Fatalf("stderr lacks %q:\n%s", want, said)
	}
	if strings.Contains(said, "is joined to alpha") || strings.Contains(said, "is a member of alpha") {
		t.Fatalf("a deferred member is said to be joined:\n%s", said)
	}
}

func TestAJoinedNewMemberInTheRecordIsSaidJoined(t *testing.T) {
	r := newRig(t)
	r.writeRecord(t, `{"version": 1, "team": "alpha", "members": [
		{"container": "container-a", "user": "agent", "state": "present", "status": "joined", "handle": "proj.1+worker.lxc",
		 "checkout": "/srv/agent/one", "why": "", "since": 1792000000}]}`)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if strings.Contains(r.ErrOut.String(), "deferred") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestAnUnreadableRecordAfterTheApplyFailsNamingIt(t *testing.T) {
	r := newRig(t)
	r.writeRecord(t, "{")
	r.happy()
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "the LXC member record "+filepath.Join(r.Host.AppliedDir, "lxc", "alpha.json")) {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}
