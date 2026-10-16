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
