package element

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// X20: a profile for an https team says, on stderr, that Element must trust the team's CA and
// where the CA is, and that whether the Flatpak build honours an added CA has not been checked;
// a profile for an http team says nothing of the kind. Check and apply both say it, and stdout
// stays the CHANGED lines alone.
func TestAnHTTPSTeamSaysItsCAMustBeTrusted(t *testing.T) {
	s := newSetup(t)
	ca := paths.CACertFile(mustTeam(t, "web"))
	for name, run := range map[string]func(string) error{"check": s.check, "apply": s.apply} {
		if err := run(`[{"team":"ops"},{"team":"web"}]`); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := s.stderr.String()
		for _, want := range []string{"web: ", "must trust the team's CA", ca, "has not been checked"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: stderr lacks %q: %q", name, want, got)
			}
		}
		if strings.Contains(got, "ops") || strings.Count(got, "\n") != 1 {
			t.Errorf("%s: only the https team is told, in one line: %q", name, got)
		}
		if name == "check" && s.stdout.Len() != 0 {
			t.Errorf("check wrote to stdout: %q", s.stdout.String())
		}
		if name == "apply" && len(s.changed()) != 6 {
			t.Errorf("apply's stdout is the six CHANGED lines of two profiles: %q", s.stdout.String())
		}
	}
}

// X20: a team that is not approved has no profile, so no note about a CA either.
func TestAnUnapprovedHTTPSTeamIsNotToldAboutACA(t *testing.T) {
	s := newSetup(t)
	s.env.Approvals = approvals{urls: map[string]string{}}
	if err := s.check(`[{"team":"web"}]`); err != nil {
		t.Fatal(err)
	}
	if got := s.stderr.String(); got != "web: not approved yet, no profile\n" {
		t.Errorf("stderr %q", got)
	}
}
