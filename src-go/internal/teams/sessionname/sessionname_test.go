package sessionname

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestASeatSessionNamesLauncherTeamAndSeat(t *testing.T) {
	for _, tc := range []struct{ launcher, seats, want string }{
		{"ccy", "fedora-desktop.4@team-a", "ccy-team-a-fedora-desktop-4"},
		{"cc", "fedora-desktop.2@team-b", "cc-team-b-fedora-desktop-2"},
		{"cc", "a+b.1@x", "cc-x-a-b-1"},
		{"ccy", "app.1@alpha,app.3@beta", "ccy-alpha-app-1_beta-app-3"},
	} {
		got, note, err := Name(tc.launcher, "proj", tc.seats)
		if err != nil || got != tc.want || note != "" {
			t.Fatalf("%s %q: got %q note %q err %v, want %q", tc.launcher, tc.seats, got, note, err, tc.want)
		}
		if _, err := paths.ParseSessionName(got); err != nil {
			t.Fatalf("%q is not a session name the seat store takes: %v", got, err)
		}
	}
}

func TestNoSeatKeepsTheProjectName(t *testing.T) {
	for _, seats := range []string{"", "team-a", "app.1@alpha,beta", "@alpha", "app.1@"} {
		got, note, err := Name("cc", "parent-proj", seats)
		if err != nil || got != "cc-parent-proj" || note != "" {
			t.Fatalf("%q: got %q note %q err %v", seats, got, note, err)
		}
	}
}

func TestATooLongNameKeepsTheProjectNameAndSaysSo(t *testing.T) {
	seats := strings.Repeat("s", 60) + ".1@alpha," + strings.Repeat("t", 60) + ".2@beta"
	got, note, err := Name("ccy", "proj", seats)
	if err != nil || got != "ccy-proj" || !strings.Contains(note, "longer than") {
		t.Fatalf("got %q note %q err %v", got, note, err)
	}
}

func TestWrongLauncherOrProjectIsRefused(t *testing.T) {
	for _, tc := range []struct{ launcher, project string }{
		{"podman", "proj"}, {"", "proj"}, {"ccy", ""}, {"ccy", "a.b"}, {"ccy", "-x"},
	} {
		if _, _, err := Name(tc.launcher, tc.project, ""); err == nil {
			t.Fatalf("%q %q was taken", tc.launcher, tc.project)
		}
	}
}

func TestRunPrintsTheName(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"ccy", "proj", "app.1@alpha"}, &out, &errOut); code != term.ExitOK || out.String() != "ccy-alpha-app-1\n" {
		t.Fatalf("exit %d out %q err %q", code, out.String(), errOut.String())
	}
	out.Reset()
	long := strings.Repeat("s", 130) + "@alpha"
	if code := Run([]string{"cc", "proj", long}, &out, &errOut); code != term.ExitOK || out.String() != "cc-proj\n" ||
		!strings.Contains(errOut.String(), "longer than") {
		t.Fatalf("long: exit %d out %q err %q", code, out.String(), errOut.String())
	}
}

func TestRunRefusesWrongArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"ccy", "proj"}, {"ccy", "proj", "a@b", "x"}, {"nope", "proj", ""}} {
		var out, errOut bytes.Buffer
		if code := Run(args, &out, &errOut); code != term.ExitUsage || out.Len() != 0 || errOut.Len() == 0 {
			t.Fatalf("%q: exit %d out %q err %q", args, code, out.String(), errOut.String())
		}
	}
}
