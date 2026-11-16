package login

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Plan 00254 Tasks 6.1 and 6.2: a team opens in the client its host_vars resolve it to, and
// every message names that client.

// alphaLightning is team alpha's Lightning launcher.
func (h rig) alphaLightning() string {
	return filepath.Join(h.l.Home, ".local/share/applications/agent-bus-alpha-lightning.desktop")
}

// onLightning makes the home-level setting Lightning, with these lines of agent_bus_element_teams
// after it.
func (h rig) onLightning(t *testing.T, entries string) {
	t.Helper()
	text := teamtest.HostVarsAlphaOn("lightning")
	if entries != "" {
		text += "agent_bus_element_teams:\n" + entries
	}
	h.WriteHostVars(t, text)
}

// ownerInLightning makes owner's account in alpha's Lightning instance, as Lightning's sign-in does.
func (h rig) ownerInLightning(t *testing.T) {
	t.Helper()
	teamtest.MustWrite(t, filepath.Join(h.l.Home, ".var/app/org.lightning_matrix.Lightning/teams/alpha/data/MatrixClient/matrix-client/owner_alpha.agent-bus.internal/store"), "")
}

func (h rig) launches() []string {
	var lines []string
	for _, c := range h.Fake.Calls() {
		if c.Name == "systemd-run" {
			lines = append(lines, c.Line())
		}
	}
	return lines
}

func TestOpenTeamOpensALightningTeamInLightning(t *testing.T) {
	h := newRig(t)
	h.onLightning(t, "")
	h.ownerInLightning(t)
	teamtest.MustWrite(t, h.alphaLightning(), "[Desktop Entry]\n")
	teamtest.MustWrite(t, h.elementLauncher(), "[Desktop Entry]\n") // the other client's: not opened
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	res, err := h.l.OpenTeam(context.Background(), h.pass(t), "alpha")
	if err != nil || res != flow.Done {
		t.Fatalf("%v %v: %s", res, err, h.ErrOut.String())
	}
	launched := h.launches()
	if len(launched) != 1 || !strings.Contains(launched[0], "--unit ccy-teams-lightning-alpha-") ||
		!strings.HasSuffix(launched[0], "setsid -w gio launch "+h.alphaLightning()) {
		t.Fatalf("launched %q", launched)
	}
	logPath := filepath.Join(h.l.StateDir, "lightning-alpha.log")
	for _, want := range []string{"Opening alpha in Lightning.\n", "Lightning is starting; its own messages go to " + term.AbbrevHome(logPath, h.l.Home) + ".\n"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
}

// A team's own client wins over the home-level setting.
func TestOpenTeamFollowsTheTeamsOwnClient(t *testing.T) {
	h := newRig(t)
	h.onLightning(t, "  - team: alpha\n    base_url: http://192.0.2.10:6167\n    client: element\n")
	teamtest.MustWrite(t, h.elementLauncher(), "[Desktop Entry]\n")
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	if _, err := h.l.OpenTeam(context.Background(), h.pass(t), "alpha"); err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if launched := h.launches(); len(launched) != 1 || !strings.HasSuffix(launched[0], "gio launch "+h.elementLauncher()) {
		t.Fatalf("launched %q", launched)
	}
	if !strings.Contains(h.ErrOut.String(), "Opening alpha in Element.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

// An Element launcher does not stand in for a Lightning team's: its set-up is offered, in
// Lightning's words, and the play then has to write the Lightning launcher.
func TestOpenTeamSetsUpALightningTeamThatHasNoLightningLauncher(t *testing.T) {
	h := newRig(t)
	h.onLightning(t, "")
	teamtest.MustWrite(t, h.elementLauncher(), "[Desktop Entry]\n")
	h.Script.Gum = []string{"NO"}
	res, err := h.l.OpenTeam(context.Background(), h.pass(t), "alpha")
	if err != nil || res != flow.Refused {
		t.Fatalf("%v %v", res, err)
	}
	for _, want := range []string{
		"alpha has no Lightning instance on this desktop yet.\n", "Nothing set up. Opening the team in Lightning offers it again.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if want := "gum confirm --default=true Set one up now? It installs Element Desktop and Lightning if needed (sudo asks for your password)."; h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}

	h = newRig(t)
	h.onLightning(t, "")
	h.OnDeclare("element", "CHANGED\tteam alpha\n")
	h.ownerInLightning(t)
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	h.Script.Gum = []string{"YES"}
	h.Fake.OnFunc(strings.Fields(h.Host.ElementPlay), func(execx.Cmd) (execx.Result, error) {
		teamtest.MustWrite(t, h.alphaLightning(), "")
		return execx.Result{}, nil
	})
	if _, err := h.l.OpenTeam(context.Background(), h.pass(t), "alpha"); err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if h.elementPlays() != 1 || len(h.launches()) != 1 || !strings.HasSuffix(h.launches()[0], "gio launch "+h.alphaLightning()) {
		t.Fatalf("plays %d launched %q: %s", h.elementPlays(), h.launches(), h.ErrOut.String())
	}
}

// Plan 00254 Task 7.1's gap: while the team's Lightning instance holds no human's account, each
// human with sessions on the homeserver from another client (an Element sign-in of a team since
// moved to Lightning) is told so and asked about a new password, default no. Once any human's
// account is there (whoever this desktop is), nobody is asked.
func TestOpenTeamOffersAPasswordToAPersonLightningHasNoAccountFor(t *testing.T) {
	h := newRig(t)
	h.onLightning(t, "")
	teamtest.MustWrite(t, h.alphaLightning(), "[Desktop Entry]\n")
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	h.Script.Fzf = []string{"|EXIT", "|EXIT", "|EXIT"}
	res, err := h.l.OpenTeam(context.Background(), h.pass(t), "alpha")
	if err != nil || res != flow.Done {
		t.Fatalf("%v %v: %s", res, err, h.ErrOut.String())
	}
	for _, want := range []string{
		"owner has 1 session on alpha's homeserver, from another client, and nobody is signed in to alpha's Lightning on this desktop yet.\n",
		"guest has 2 sessions on alpha's homeserver, from another client, and nobody is signed in to alpha's Lightning on this desktop yet.\n",
		"No password was set for owner.\n", "No password was set for guest.\n", "Opening alpha in Lightning.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if len(h.Script.FzfArgs) != 3 {
		t.Fatalf("asked %q, want a question for each of the three humans", h.Script.FzfArgs)
	}
	for _, args := range h.Script.FzfArgs {
		if !slices.Contains(args, "--bind=start:last") {
			t.Fatalf("%q does not default to no", args)
		}
	}
	if len(h.launches()) != 1 {
		t.Fatalf("launched %q", h.launches())
	}

	h = newRig(t)
	h.onLightning(t, "")
	teamtest.MustWrite(t, h.alphaLightning(), "[Desktop Entry]\n")
	teamtest.MustWrite(t, filepath.Join(h.l.Home, ".var/app/org.lightning_matrix.Lightning/teams/alpha/data/MatrixClient/matrix-client/guest_alpha.agent-bus.internal/store"), "")
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	if _, err := h.l.OpenTeam(context.Background(), h.pass(t), "alpha"); err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if len(h.Script.FzfArgs) != 0 || strings.Contains(h.ErrOut.String(), "nobody is signed in") {
		t.Fatalf("asked %q: %s", h.Script.FzfArgs, h.ErrOut.String())
	}
}

// A Lightning instance that cannot say whether it holds an account stops the open: nothing is
// asked and nothing starts.
func TestOpenTeamStopsWhenLightningsAccountsCannotBeRead(t *testing.T) {
	h := newRig(t)
	h.onLightning(t, "")
	teamtest.MustWrite(t, h.alphaLightning(), "[Desktop Entry]\n")
	teamtest.MustWrite(t, filepath.Join(h.l.Home, ".var/app/org.lightning_matrix.Lightning/teams/alpha/data/MatrixClient/matrix-client/owner_alpha.agent-bus.internal"), "")
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	if _, err := h.l.OpenTeam(context.Background(), h.pass(t), "alpha"); err == nil || !strings.Contains(err.Error(), "is not a folder") {
		t.Fatalf("err %v", err)
	}
	if len(h.Script.FzfArgs) != 0 || len(h.launches()) != 0 {
		t.Fatalf("asked %q launched %q", h.Script.FzfArgs, h.launches())
	}
}

// An Element team is never asked about who is signed in to Lightning.
func TestOpenTeamAsksNothingOfASignedInElementPerson(t *testing.T) {
	h := newRig(t)
	h.onLightning(t, "  - team: alpha\n    base_url: http://192.0.2.10:6167\n    client: element\n")
	teamtest.MustWrite(t, h.elementLauncher(), "[Desktop Entry]\n")
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	if _, err := h.l.OpenTeam(context.Background(), h.pass(t), "alpha"); err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if len(h.Script.FzfArgs) != 0 {
		t.Fatalf("asked %q", h.Script.FzfArgs)
	}
}

func TestOpenEveryTeamOpensEachInItsOwnClient(t *testing.T) {
	h := newRig(t)
	h.onLightning(t, "  - team: beta\n    base_url: http://192.0.2.10:6168\n    client: element\n")
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	snap := h.pass(t)
	teamtest.MustWrite(t, h.alphaLightning(), "")
	teamtest.MustWrite(t, filepath.Join(h.l.Home, ".local/share/applications/agent-bus-beta-element.desktop"), "")
	snap.Teams = append(snap.Teams, "beta", "gamma")
	if _, err := h.l.OpenEveryTeam(context.Background(), snap); err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	launched := h.launches()
	if len(launched) != 2 || !strings.Contains(launched[0], "ccy-teams-lightning-alpha-") || !strings.Contains(launched[1], "ccy-teams-element-beta-") {
		t.Fatalf("launched %q", launched)
	}
	for _, want := range []string{
		"Opening alpha in Lightning and beta in Element: one window each (each team is its own Matrix server, and a window holds one account).\n",
		"gamma has no Lightning instance on this desktop yet: Open gamma in Lightning (on its row's menu) sets one up.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
}

// Who has signed in is read from the homeserver (a device per sign-in, whatever the client), so
// the check after an apply is the same for a Lightning team; only the way it says to sign in
// names the team's client.
func TestSignInGapOfALightningTeamSaysToOpenItInLightning(t *testing.T) {
	h := newRig(t)
	h.onLightning(t, "")
	h.Script.Gum = []string{"NO"}
	if err := h.l.SignInGap(t.Context(), []string{"alpha"}, true); err != nil {
		t.Fatal(err)
	}
	want := "To sign in: ccy-teams, Enter on alpha, choose Open alpha in Lightning: it offers each person who has never signed in a password."
	if !strings.Contains(h.ErrOut.String(), want) || !strings.Contains(h.ErrOut.String(), "owner has never signed in to alpha.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if gum := h.Script.GumLog; len(gum) == 0 || !strings.Contains(gum[0], "Sign owner in to Lightning now?") {
		t.Fatalf("gum %q", gum)
	}
}

// The password a person is offered is named for their team's client, and the sessions it signs
// out are counted on the homeserver, whichever client made them.
func TestPasswordOfALightningTeamNamesLightningAndCountsTheHomeserversSessions(t *testing.T) {
	h := newRig(t)
	h.onLightning(t, "")
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "guest")); err != nil {
		t.Fatal(err)
	}
	header := h.HeaderOf(0)
	for _, want := range []string{"New Lightning password for guest?", "This signs out guest's 2 sessions on the homeserver."} {
		if !strings.Contains(header, want) {
			t.Fatalf("header %q lacks %q", header, want)
		}
	}
	h.Script.HeadingAt(0, "Teams › alpha › guest › Lightning password\nguest in team alpha · 2 sessions on the homeserver")

	h = newRig(t)
	h.onLightning(t, "")
	snap = h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "1")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "A new Lightning password is for one of the team's people (KIND human)") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

// ClientName, which the create wizard's offers use, says why the client could not be read.
func TestClientNameReportsWhyTheClientCouldNotBeRead(t *testing.T) {
	h := newRig(t)
	h.onLightning(t, "")
	if got := h.l.ClientName(t.Context(), "alpha"); got != "Lightning" {
		t.Fatalf("got %q", got)
	}
	h.WriteHostVars(t, teamtest.HostVarsAlphaOn("nheko"))
	if got := h.l.ClientName(t.Context(), "alpha"); got != "its Matrix client" {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(h.ErrOut.String(), "agent_bus_matrix_client is 'nheko'; the client must be element or lightning") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}
