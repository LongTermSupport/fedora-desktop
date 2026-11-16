package ops

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// Plan 00254 Task 6.2: the set-up path checks the launcher of the client each team opens in.

func (r *opsRig) lightningLauncher(team string) string {
	return filepath.Join(r.home, ".local/share/applications", "agent-bus-"+team+"-lightning.desktop")
}

// watch declares the teams in agent_bus_element_teams, after head (the home-level setting, or
// ""); a team written "name:client" carries its own client.
func (r *opsRig) watch(t *testing.T, head string, teams ...string) {
	t.Helper()
	var text strings.Builder
	text.WriteString(head + "agent_bus_element_teams:\n")
	for _, team := range teams {
		name, client, own := strings.Cut(team, ":")
		text.WriteString("  - team: " + name + "\n    base_url: http://192.0.2.10:6167\n")
		if own {
			text.WriteString("    client: " + client + "\n")
		}
	}
	teamtest.MustWrite(t, r.hostVars, text.String())
}

func TestTheLauncherOfEachClient(t *testing.T) {
	r := newOpsRig(t)
	if got := r.ops.Launcher("beta", Element); got != r.ops.ElementLauncher("beta") {
		t.Errorf("Element: %q", got)
	}
	if got := r.ops.Launcher("beta", Lightning); got != r.lightningLauncher("beta") {
		t.Errorf("Lightning: %q", got)
	}
	for key, want := range map[string]Client{declare.ClientElement: Element, declare.ClientLightning: Lightning} {
		if got, err := ClientNamed(key); err != nil || got != want {
			t.Errorf("%s: %+v %v", key, got, err)
		}
	}
	if _, err := ClientNamed("nheko"); err == nil {
		t.Error("a client ccy-teams cannot open is an error")
	}
}

func TestClientOfIsTheTeamsOwnElseTheHomeLevelElseLightning(t *testing.T) {
	r := newOpsRig(t)
	ctx := context.Background()
	for _, c := range []struct {
		head  string
		teams []string
		want  Client
	}{
		{"", []string{"beta"}, Lightning},
		{"agent_bus_matrix_client: element\n", []string{"beta"}, Element},
		{"", []string{"beta:element"}, Element},
		{"agent_bus_matrix_client: element\n", []string{"beta:lightning"}, Lightning},
		{"agent_bus_matrix_client: element\n", []string{"other"}, Element},
		{"", []string{"other"}, Lightning},
	} {
		r.watch(t, c.head, c.teams...)
		if got, err := r.ops.ClientOf(ctx, "beta"); err != nil || got != c.want {
			t.Errorf("%q %v: %+v %v", c.head, c.teams, got, err)
		}
	}
}

func TestApplyTeamsOfALightningTeamChecksItsLightningLauncher(t *testing.T) {
	for _, head := range []string{"agent_bus_matrix_client: lightning\n", ""} {
		r := newOpsRig(t)
		r.on(opsPlay, execx.Result{})
		if head == "" {
			r.watch(t, "", "beta:lightning")
		} else {
			r.watch(t, head, "beta")
		}
		r.on(opsElementPlay, execx.Result{})
		r.files[r.lightningLauncher("beta")] = true
		r.statuses = []map[string]declare.Status{{"beta": st("current", "")}}
		if err := r.ops.ApplyTeams(context.Background(), []string{"beta"}); err != nil {
			t.Fatalf("%q: %v: %s", head, err, r.said())
		}
		if want := "Running play-agent-bus-element.yml: it installs Element Desktop and Lightning (sudo) and writes the Lightning instance for beta."; !strings.Contains(r.said(), want) {
			t.Errorf("%q: missing %q in %q", head, want, r.said())
		}
	}

	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.watch(t, "", "beta:lightning")
	r.on(opsElementPlay, execx.Result{})
	r.files[r.ops.ElementLauncher("beta")] = true // the other client's launcher proves nothing
	r.statuses = []map[string]declare.Status{{"beta": st("current", "")}}
	if err := r.ops.ApplyTeams(context.Background(), []string{"beta"}); err == nil {
		t.Fatal("a Lightning team with no Lightning launcher passed")
	}
	for _, want := range []string{
		"ERROR: play-agent-bus-element.yml finished, but beta's Lightning launcher is not there.",
		"ERROR: the homeserver side is applied, but the Lightning instance is not; once the cause above is fixed, ccy-teams apply beta runs both again.",
	} {
		if !strings.Contains(r.said(), want) {
			t.Errorf("missing %q in %q", want, r.said())
		}
	}
}

func TestRunElementPlayNamesEachTeamsClient(t *testing.T) {
	r := newOpsRig(t)
	r.watch(t, "agent_bus_matrix_client: element\n", "a", "b:lightning", "c")
	r.on(opsElementPlay, execx.Result{})
	r.files[r.ops.ElementLauncher("a")] = true
	r.files[r.lightningLauncher("b")] = true
	r.files[r.ops.ElementLauncher("c")] = true
	if err := r.ops.RunElementPlay(context.Background(), []string{"a", "b", "c"}); err != nil {
		t.Fatalf("%v: %s", err, r.said())
	}
	if want := "it installs Element Desktop and Lightning (sudo) and writes the Element profile for a c and the Lightning instance for b."; !strings.Contains(r.said(), want) {
		t.Errorf("missing %q in %q", want, r.said())
	}

	f := newOpsRig(t)
	f.watch(t, "", "a:element", "b")
	f.onExit(opsElementPlay, 2, "")
	if err := f.ops.RunElementPlay(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("want an error")
	}
	if want := "ERROR: play-agent-bus-element.yml failed (above), so a b is not set up in its Matrix client yet."; !strings.Contains(f.said(), want) {
		t.Errorf("missing %q in %q", want, f.said())
	}
}

// A client the play refuses is refused here first, as declare's refusal, and the play is not run.
func TestRunElementPlayRefusesAClientThePlayWould(t *testing.T) {
	r := newOpsRig(t)
	r.watch(t, "agent_bus_matrix_client: nheko\n", "beta")
	if err := r.ops.RunElementPlay(context.Background(), []string{"beta"}); err == nil {
		t.Fatal("want an error")
	}
	if r.called(opsElementPlay) || !strings.Contains(r.said(), "agent_bus_matrix_client is 'nheko'; the client must be element or lightning") {
		t.Fatalf("calls %q said %q", r.lines(), r.said())
	}
}

func TestRemoveOfALightningTeamChecksItsLightningLauncherIsGone(t *testing.T) {
	r := removeRig(t)
	r.watch(t, "", "alpha:lightning")
	launcher := r.lightningLauncher("alpha")
	r.files[launcher] = true
	r.files[r.ops.ElementLauncher("alpha")] = true // not this team's client: not looked at
	r.fake.OnFunc(strings.Fields(opsElementPlay), func(execx.Cmd) (execx.Result, error) {
		delete(r.files, launcher)
		return execx.Result{}, nil
	})
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if want := "Running play-agent-bus-element.yml: it removes alpha's Lightning launcher; its Lightning instance is kept " +
		"(it may hold your signed-in session), for you to delete."; !strings.Contains(r.said(), want) {
		t.Errorf("missing %q in %q", want, r.said())
	}

	s := removeRig(t)
	s.watch(t, "", "alpha:lightning")
	s.files[s.lightningLauncher("alpha")] = true
	s.on(opsElementPlay, execx.Result{})
	if code := s.ops.Remove(context.Background(), []string{"alpha"}); code == 0 {
		t.Fatalf("a launcher left behind passed: %s", s.said())
	}
	for _, want := range []string{"alpha's Lightning launcher is still there", "alpha is down, but its Lightning launcher is not removed"} {
		if !strings.Contains(s.said(), want) {
			t.Errorf("missing %q in %q", want, s.said())
		}
	}
}

// InstallWords is the one wording of what the Element play installs, which the set-up offer
// (login) and the play's own line (RunElementPlay) both say: Element Desktop on every run, and
// Lightning too whenever a team opens in it.
func TestInstallWordsNamesWhatThePlayInstalls(t *testing.T) {
	cases := []struct {
		clients []Client
		want    string
	}{
		{[]Client{Element}, "Element Desktop"},
		{[]Client{Lightning}, "Element Desktop and Lightning"},
		{[]Client{Element, Lightning}, "Element Desktop and Lightning"},
	}
	for _, c := range cases {
		if got := InstallWords(c.clients...); got != c.want {
			t.Errorf("%v: %q, want %q", c.clients, got, c.want)
		}
	}
}
