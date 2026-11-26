package loadgen

import (
	"net/http"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
)

// forbidden is the server refusing every request of method on a path holding path.
func forbidden(method, path string) fakehs.Fault {
	return fakehs.Fault{Method: method, Path: path, Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_FORBIDDEN","error":"refused"}`), Times: 1000}
}

// LG8: a refused request stops the unit with the account and the cause.
func TestAFailedRequestStopsTheUnit(t *testing.T) {
	cases := []struct {
		pattern string
		fault   fakehs.Fault
	}{
		{PatternPings, forbidden(http.MethodPut, "/send/")},
		{PatternReceipts, forbidden(http.MethodPost, "/receipt/")},
		{PatternPresence, forbidden(http.MethodPut, "/presence/")},
		{PatternQuestions, forbidden(http.MethodGet, "/event/")},
		{PatternReceiptEvents, forbidden(http.MethodPut, "/send/")},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			c := newStepClock()
			srv := lab(t, labName, c)
			team := setUp(t, srv, c, 2)
			g := newGen(t, team, c, Options{})
			srv.Inject(tc.fault)
			err := g.Unit(t.Context(), tc.pattern)
			if err == nil || !strings.Contains(err.Error(), "pattern "+tc.pattern) || !strings.Contains(err.Error(), "@") {
				t.Fatalf("Unit: %v, want the pattern and the account named", err)
			}
			if g.Tally().Units[tc.pattern] != 0 {
				t.Fatalf("a failed unit was counted: %+v", g.Tally())
			}
		})
	}
}

func TestNewGeneratorNeedsTheRoster(t *testing.T) {
	c := newStepClock()
	srv := lab(t, labName, c)
	team := setUp(t, srv, c, 2)
	srv.Inject(forbidden(http.MethodGet, "/state/"))
	if _, err := NewGenerator(t.Context(), team, c, Options{}); err == nil || !strings.Contains(err.Error(), "roster") {
		t.Fatalf("NewGenerator: %v", err)
	}
	bad := team
	bad.Agents = nil
	if _, err := NewGenerator(t.Context(), bad, c, Options{}); err == nil {
		t.Fatal("NewGenerator took a team file off the rule")
	}
}

func TestAFailedSetupStepStopsTheSetup(t *testing.T) {
	cases := []struct {
		fault fakehs.Fault
		says  string
	}{
		{forbidden(http.MethodPut, "/v2/users/"), "make the account"},
		{forbidden(http.MethodPost, "/createRoom"), "converge"},
		{forbidden(http.MethodPost, "/join"), "joins"},
		{forbidden(http.MethodPost, "/login"), "a token for"},
	}
	for _, tc := range cases {
		t.Run(tc.says, func(t *testing.T) {
			c := newStepClock()
			srv := lab(t, labName, c)
			srv.Inject(tc.fault)
			_, err := Setup(t.Context(), SetupOptions{Base: srv.URL(), ServerName: labName, RegKey: srv.RegistrationKey(), Agents: 2, Clock: c})
			if err == nil || !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("Setup: %v, want it stopped saying %q", err, tc.says)
			}
		})
	}
}

func TestRecorderRefusesARoomTheTeamDoesNotHave(t *testing.T) {
	if err := (&recorder{}).Record("side", "!x:"+labName); err == nil {
		t.Fatal("the recorder took a fourth room")
	}
}
