package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// The health check's record (helpers/agent_bus/health.py) is read beside the applied stamps.
func writeHealth(t *testing.T, h rig, team, body string) {
	t.Helper()
	teamtest.MustWrite(t, filepath.Join(h.c.AppliedDir, "health", team+".json"), body)
}

func teamWhere(t *testing.T, h rig) string {
	t.Helper()
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	return s.Rows[0].Where
}

func TestAHealthyTeamShowsNoHealthWords(t *testing.T) {
	h := populated(t)
	if got := teamWhere(t, h); got != "port 6167" {
		t.Fatalf("where %q", got)
	}
}

func TestATeamsHealthProblemsAreOnItsRowWithWhatToDo(t *testing.T) {
	h := populated(t)
	h.c.Now = func() time.Time { return time.Unix(1_791_000_000, 0) }
	writeHealth(t, h, "alpha", `{"checked": 1791000000, "expires": 1791000300, "problems": [
		{"what": "the newest backup is 1 d 13 h old (#4, 2026-10-01 00:10:00 UTC)",
		 "fix": "sudo systemctl start agent-bus-backup@alpha.service"}]}`)
	got := teamWhere(t, h)
	want := "port 6167, health: the newest backup is 1 d 13 h old (#4, 2026-10-01 00:10:00 UTC): " +
		"sudo systemctl start agent-bus-backup@alpha.service"
	if got != want {
		t.Fatalf("where\n%q\nwant\n%q", got, want)
	}
}

func TestAStaleHealthRecordSaysTheCheckHasStopped(t *testing.T) {
	h := populated(t)
	h.c.Now = func() time.Time { return time.Unix(1_791_000_000+900, 0) }
	writeHealth(t, h, "alpha", `{"checked": 1791000000, "expires": 1791000300, "problems": []}`)
	got := teamWhere(t, h)
	if !strings.Contains(got, "health: the check last ran 15 min ago") ||
		!strings.Contains(got, "systemctl status agent-bus-hs-health@alpha.timer") {
		t.Fatalf("where %q", got)
	}
}

func TestAnInstalledTeamWithNoHealthRecordSaysSo(t *testing.T) {
	h := populated(t)
	if err := os.Remove(filepath.Join(h.c.AppliedDir, "health", "alpha.json")); err != nil {
		t.Fatal(err)
	}
	got := teamWhere(t, h)
	if !strings.Contains(got, "health: never checked: ccy-teams apply alpha starts agent-bus-hs-health@alpha.timer") {
		t.Fatalf("where %q", got)
	}
}

func TestAnUnreadableHealthRecordIsSaidNotSkipped(t *testing.T) {
	h := populated(t)
	writeHealth(t, h, "alpha", "{")
	if got := teamWhere(t, h); !strings.Contains(got, "health: record unreadable") {
		t.Fatalf("where %q", got)
	}
}
