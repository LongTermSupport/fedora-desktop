package restore

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
)

func TestVerdictGoldenCases(t *testing.T) {
	ended := "ccy exited with status 1. " + screen.EndedText
	none := GoingOutcome{}
	started := GoingOutcome{Kind: OutcomeStarted}
	setting := GoingOutcome{Kind: OutcomeSettingGoing}
	cases := []struct {
		name      string
		prefix    string
		live      bool
		screen    string
		container ContainerState
		going     GoingOutcome
		want      string
	}{
		{"not live", "ccy", false, "", ContainerUp, none, "DEAD session-not-running"},
		{"trampoline hold line", "ccy", true, "...\n" + ended + "\n\n", ContainerUp, none, "DEAD launcher-exited"},
		{"quick launch", "ccy", true, "Found previous launch configuration\nUse same configuration? [Y/n] \n\n\n", ContainerNone, none, "WAITING-AT-PROMPT quick-launch"},
		{"token chooser with a count after", "cc", true, "Select token [0-2]: ", ContainerNone, none, "WAITING-AT-PROMPT token-select"},
		{"ssh passphrase with key after", "ccy", true, "Enter passphrase for /k/id_ed25519: ", ContainerNone, none, "WAITING-AT-PROMPT ssh-passphrase"},
		{"ssh key menu", "ccy", true, "SSH Key Selection for Claude YOLO\n  1) id_one\nSelect SSH key [0-1, a] (default: 1): ", ContainerNone, none, "WAITING-AT-PROMPT ssh-key"},
		{"running container menu, indented", "ccy", true, "  Choice [c/s/m/q]: ", ContainerStarting, none, "WAITING-AT-PROMPT existing-containers"},
		{"a prompt higher up does not count", "ccy", true, "Select token [0-2]: 1\nclaude is running\n", ContainerUp, none, "OK"},
		{"ccy with no container is starting", "ccy", true, "Building image...", ContainerNone, none, "STARTING"},
		{"ccy with client but no listed container", "ccy", true, "", ContainerStarting, none, "STARTING"},
		{"ccy with container up", "ccy", true, "claude", ContainerUp, none, "OK"},
		{"cc past its prompts", "cc", true, "claude", ContainerNone, none, "OK"},
		{"unknown launcher", "zz", true, "", ContainerUp, none, "DEAD unknown-launcher-zz"},
		{"set going and taken", "ccy", true, "claude", ContainerUp, started, "OK"},
		{"not yet set going", "cc", true, "claude", ContainerNone, setting, "SETTING-GOING"},
		{"left alone", "ccy", true, "claude", ContainerUp, GoingOutcome{OutcomeFailed, "busy-before-set-going"}, "NOT-SET-GOING busy-before-set-going"},
		{"starting outranks setting-going", "ccy", true, "", ContainerStarting, setting, "STARTING"},
		{"prompt outranks not-set-going", "cc", true, "Select token [0-2]: ", ContainerNone, GoingOutcome{OutcomeFailed, "waiting-at-prompt-token-select-after-1200s"}, "WAITING-AT-PROMPT token-select"},
		{"blank trailing lines with whitespace", "ccy", true, "Select token [0-2]: \n   \n\t\n", ContainerNone, none, "WAITING-AT-PROMPT token-select"},
		{"empty screen is ok for cc", "cc", true, "", ContainerNone, none, "OK"},
	}
	for _, c := range cases {
		got := Judge(c.prefix, c.live, c.screen, c.container, c.going)
		if got.String() != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got.String(), c.want)
		}
	}
}

func TestVerdictStringRendersStateAndOptionalDetail(t *testing.T) {
	if got := (Verdict{State: StateOK}).String(); got != "OK" {
		t.Fatalf("got %q", got)
	}
	if got := (Verdict{State: StateDead, Detail: "x"}).String(); got != "DEAD x" {
		t.Fatalf("got %q", got)
	}
}

func TestGoingStateGoldenCases(t *testing.T) {
	failed := func(detail string) GoingOutcome { return GoingOutcome{OutcomeFailed, detail} }
	cases := []struct {
		name   string
		going  Going
		at     int64
		detail string
		now    int64
		window int64
		took   Took
		want   GoingOutcome
	}{
		{"found running", GoingNone, 0, "", 100, 120, TookNA, GoingOutcome{Kind: OutcomeNone}},
		{"pending", GoingPending, 0, "", 100, 120, TookNA, GoingOutcome{Kind: OutcomeSettingGoing}},
		{"left alone with reason", GoingUntouched, 0, "no-transcript", 100, 120, TookNA, failed("no-transcript")},
		{"left alone with no reason", GoingUntouched, 0, "", 100, 120, TookNA, failed("no-reason-recorded")},
		{"compact taken", GoingCompact, 100, "300000", 105, 120, TookYes, GoingOutcome{Kind: OutcomeStarted}},
		{"compact inside window", GoingCompact, 100, "300000", 219, 120, TookNo, GoingOutcome{Kind: OutcomeSettingGoing}},
		{"compact past window", GoingCompact, 100, "300000", 220, 120, TookNo, failed("compact-not-started")},
		{"continue past window", GoingContinue, 100, "5000", 400, 120, TookNo, failed("continue-not-started")},
		{"compacting, taken", GoingCompacting, 100, "300000", 9999, 120, TookYes, GoingOutcome{Kind: OutcomeSettingGoing}},
		{"compacting, never taken", GoingCompacting, 100, "300000", 220, 120, TookNo, failed("compact-not-started")},
		{"compacting, inside window", GoingCompacting, 100, "300000", 219, 120, TookNo, GoingOutcome{Kind: OutcomeSettingGoing}},
		{"unknown going", Going("keep"), 0, "", 1, 1, TookNA, failed("unknown-going-keep")},
	}
	for _, c := range cases {
		if got := GoingState(c.going, c.at, c.detail, c.now, c.window, c.took); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// The outcome renders as the going word the bash restore library speaks, the form the
// parity suites know.
func TestGoingOutcomeStringIsTheBashWord(t *testing.T) {
	cases := map[GoingOutcome]string{
		{Kind: OutcomeNone}:                    "-",
		{Kind: OutcomeSettingGoing}:            "setting-going",
		{Kind: OutcomeStarted}:                 "started",
		{OutcomeFailed, "compact-not-started"}: "failed:compact-not-started",
	}
	for outcome, want := range cases {
		if got := outcome.String(); got != want {
			t.Errorf("%+v: got %q, want %q", outcome, got, want)
		}
	}
}
