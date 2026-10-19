package membership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

func TestReadReportTakesTheKeepersReport(t *testing.T) {
	checkout := t.TempDir()
	teamtest.MustWrite(t, ReportPath(checkout, "ccy-proj"),
		`{"v":1,"generation":4,"inbound":false,"seats":[{"team":"alpha","seat":"3","handle":"proj.3+local.podman","state":"held"}],`+
			`"refused":[{"team":"beta","seat":"1","why":"held-elsewhere","holder":"seat"}],"told":{"generation":4,"roles":[]}}`)
	r, err := ReadReport(checkout, "ccy-proj")
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 4 || !r.Holds("alpha", "3") || r.Holds("beta", "1") || r.Inbound == nil || *r.Inbound {
		t.Fatalf("report %+v", r)
	}
	if why := r.Why("beta", "1"); why != "held-elsewhere (held by a seat lock)" {
		t.Fatalf("why %q", why)
	}
}

// The report and the conversation record (Plan 00203 SPEC-team-add-and-restart C1) share one
// directory per session, beside the checkout's seats.
func TestSessionDirHoldsTheReport(t *testing.T) {
	if got, want := SessionDir("/c", "s"), "/c/.claude/ccy/pingbus/sessions/s"; got != want {
		t.Fatalf("SessionDir %q, want %q", got, want)
	}
	if ReportPath("/c", "s") != filepath.Join(SessionDir("/c", "s"), "applied.json") {
		t.Fatalf("ReportPath %q is not in SessionDir", ReportPath("/c", "s"))
	}
}

func TestReadReportOfNoReportIsNotExist(t *testing.T) {
	if _, err := ReadReport(t.TempDir(), "ccy-proj"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}

func TestReadReportRefusesAnUnknownKey(t *testing.T) {
	checkout := t.TempDir()
	teamtest.MustWrite(t, ReportPath(checkout, "s"), `{"v":1,"generation":1,"seats":[],"refused":[],"extra":1}`)
	if _, err := ReadReport(checkout, "s"); err == nil {
		t.Fatal("accepted")
	}
	if want := filepath.Join(checkout, ".claude/ccy/pingbus/sessions/s/applied.json"); ReportPath(checkout, "s") != want {
		t.Fatalf("path %q", ReportPath(checkout, "s"))
	}
}

func TestToldRoleIsWhatTheSessionWasToldOfItsRole(t *testing.T) {
	checkout := t.TempDir()
	teamtest.MustWrite(t, ReportPath(checkout, "s"),
		`{"v":1,"generation":2,"seats":[],"refused":[],"told":{"generation":2,"roles":[{"team":"alpha","handle":"proj.3+local.podman","role":"coordinator"}]}}`)
	r, err := ReadReport(checkout, "s")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.ToldRole("alpha", "proj.3+local.podman"); got != "coordinator" {
		t.Fatalf("told %q", got)
	}
	if got := r.ToldRole("beta", "proj.3+local.podman"); got != "" {
		t.Fatalf("a team it was not told of: %q", got)
	}
	teamtest.MustWrite(t, ReportPath(checkout, "s"), `{"v":1,"generation":2,"seats":[],"refused":[]}`)
	if r, err = ReadReport(checkout, "s"); err != nil || r.ToldRole("alpha", "proj.3+local.podman") != "" {
		t.Fatalf("no told: %v", err)
	}
	teamtest.MustWrite(t, ReportPath(checkout, "s"), `{"v":1,"generation":2,"seats":[],"refused":[],"told":{"roles":7}}`)
	if r, err = ReadReport(checkout, "s"); err != nil || r.ToldRole("alpha", "proj.3+local.podman") != "" {
		t.Fatalf("a told it cannot read is nothing told: %v", err)
	}
}
