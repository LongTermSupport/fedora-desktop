package element

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// fixedWall is the one clock reading pki takes, held at testNow.
type fixedWall struct{}

func (fixedWall) WallMS() int64 { return testNow.UnixMilli() }

// placeCA writes what the Element play's check leaves for the https team, web, once a handshake
// from Element's sandbox succeeded: the team's CA certificate at paths.ElementCAPlaced.
func (s *setup) placeCA(data []byte) string {
	s.t.Helper()
	path, err := paths.ElementCAPlaced(s.home, mustTeam(s.t, "web"))
	if err != nil {
		s.t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		s.t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		s.t.Fatal(err)
	}
	return path
}

func teamCAPEM(t *testing.T) []byte {
	t.Helper()
	ca, err := pki.Maker{Clock: fixedWall{}}.NewCA("web")
	if err != nil {
		t.Fatal(err)
	}
	return ca.CertPEM()
}

// X20, X34: a profile for an https team whose CA the Element play has not placed says, on
// stderr, that Element does not trust the team's CA yet, where the CA is, and that the Element
// play places it; a profile for an http team says nothing of the kind. Check and apply both
// say it, and stdout stays the CHANGED lines alone.
func TestAnHTTPSTeamWithoutItsCAPlacedSaysToRunTheElementPlay(t *testing.T) {
	s := newSetup(t)
	for name, run := range map[string]func(string) error{"check": s.check, "apply": s.apply} {
		if err := run(`[{"team":"ops"},{"team":"web"}]`); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		assertToldToRunThePlay(t, name, s.stderr.String())
		if name == "check" && s.stdout.Len() != 0 {
			t.Errorf("check wrote to stdout: %q", s.stdout.String())
		}
		if name == "apply" && len(s.changed()) != 6 {
			t.Errorf("apply's stdout is the six CHANGED lines of two profiles: %q", s.stdout.String())
		}
	}
}

// assertToldToRunThePlay is the one line web, and only web, is told: where its CA is, that the
// Element play places it, and no hand step.
func assertToldToRunThePlay(t *testing.T, verb, got string) {
	t.Helper()
	ca := paths.CACertFile(mustTeam(t, "web"))
	for _, want := range []string{"web: ", "Element does not trust the team's CA", ca, "run the Element play"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s: stderr lacks %q: %q", verb, want, got)
		}
	}
	for _, gone := range []string{"has not been checked", "copy it out as root"} {
		if strings.Contains(got, gone) {
			t.Errorf("%s: stderr still asks for a hand step (%q): %q", verb, gone, got)
		}
	}
	if strings.Contains(got, "ops") || strings.Count(got, "\n") != 1 {
		t.Errorf("%s: only the https team is told, in one line: %q", verb, got)
	}
}

// X34: once the Element play's check has placed the team's CA, check and apply say nothing
// about it, and apply still writes both profiles.
func TestAnHTTPSTeamWhoseCAThePlayPlacedIsNotToldAboutIt(t *testing.T) {
	s := newSetup(t)
	s.placeCA(teamCAPEM(t))
	for name, run := range map[string]func(string) error{"check": s.check, "apply": s.apply} {
		if err := run(`[{"team":"ops"},{"team":"web"}]`); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := s.stderr.String(); got != "" {
			t.Errorf("%s: stderr %q, want nothing", name, got)
		}
		if name == "apply" && len(s.changed()) != 6 {
			t.Errorf("apply's stdout is the six CHANGED lines of two profiles: %q", s.stdout.String())
		}
	}
}

// X34: what stands where the play's record goes and is not that record is refused, naming the
// path, rather than read as placed or as absent: a link, a file others may write, and content
// that is not a CA certificate.
func TestARecordOfAPlacedCAThatIsNotThePlaysIsRefused(t *testing.T) {
	cases := map[string]func(s *setup){
		"a link": func(s *setup) {
			path := s.placeCA(teamCAPEM(s.t))
			target := filepath.Join(s.home, "elsewhere.pem")
			if err := os.Rename(path, target); err != nil {
				s.t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				s.t.Fatal(err)
			}
		},
		"writable by others": func(s *setup) {
			path := s.placeCA(teamCAPEM(s.t))
			if err := os.Chmod(path, 0o666); err != nil {
				s.t.Fatal(err)
			}
		},
		"not a CA certificate": func(s *setup) { s.placeCA([]byte("not a certificate\n")) },
	}
	for name, plant := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSetup(t)
			plant(s)
			path, err := paths.ElementCAPlaced(s.home, mustTeam(t, "web"))
			if err != nil {
				t.Fatal(err)
			}
			for verb, run := range map[string]func(string) error{"check": s.check, "apply": s.apply} {
				err := run(`[{"team":"web"}]`)
				if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), path) {
					t.Errorf("%s: %v, want a refusal naming %s", verb, err, path)
				}
			}
		})
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
