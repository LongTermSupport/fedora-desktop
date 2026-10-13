package restore

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
)

func TestVerdictGoldenCases(t *testing.T) {
	ended := "ccy exited with status 1. " + screen.EndedText
	cases := []struct {
		name      string
		prefix    string
		live      bool
		screen    string
		container string
		going     string
		want      string
	}{
		{"not live", "ccy", false, "", "up", "", "DEAD session-not-running"},
		{"trampoline hold line", "ccy", true, "...\n" + ended + "\n\n", "up", "", "DEAD launcher-exited"},
		{"quick launch", "ccy", true, "Found previous launch configuration\nUse same configuration? [Y/n] \n\n\n", "-", "", "WAITING-AT-PROMPT quick-launch"},
		{"token chooser with a count after", "cc", true, "Select token [0-2]: ", "-", "", "WAITING-AT-PROMPT token-select"},
		{"ssh passphrase with key after", "ccy", true, "Enter passphrase for /k/id_ed25519: ", "-", "", "WAITING-AT-PROMPT ssh-passphrase"},
		{"ssh key menu", "ccy", true, "SSH Key Selection for Claude YOLO\n  1) id_one\nSelect SSH key [0-1, a] (default: 1): ", "-", "", "WAITING-AT-PROMPT ssh-key"},
		{"running container menu, indented", "ccy", true, "  Choice [c/s/m/q]: ", "starting", "", "WAITING-AT-PROMPT existing-containers"},
		{"a prompt higher up does not count", "ccy", true, "Select token [0-2]: 1\nclaude is running\n", "up", "", "OK"},
		{"ccy with no container is starting", "ccy", true, "Building image...", "-", "", "STARTING"},
		{"ccy with client but no listed container", "ccy", true, "", "starting", "", "STARTING"},
		{"ccy with container up", "ccy", true, "claude", "up", "", "OK"},
		{"cc past its prompts", "cc", true, "claude", "-", "", "OK"},
		{"unknown launcher", "zz", true, "", "up", "", "DEAD unknown-launcher-zz"},
		{"set going and taken", "ccy", true, "claude", "up", "started", "OK"},
		{"not yet set going", "cc", true, "claude", "-", "setting-going", "SETTING-GOING"},
		{"left alone", "ccy", true, "claude", "up", "failed:busy-before-set-going", "NOT-SET-GOING busy-before-set-going"},
		{"starting outranks setting-going", "ccy", true, "", "starting", "setting-going", "STARTING"},
		{"prompt outranks not-set-going", "cc", true, "Select token [0-2]: ", "-", "failed:waiting-at-prompt-token-select-after-1200s", "WAITING-AT-PROMPT token-select"},
		{"unknown going word", "cc", true, "claude", "-", "maybe", "NOT-SET-GOING unknown-state-maybe"},
		{"dash going", "cc", true, "claude", "-", "-", "OK"},
		{"blank trailing lines with whitespace", "ccy", true, "Select token [0-2]: \n   \n\t\n", "-", "", "WAITING-AT-PROMPT token-select"},
		{"empty screen is ok for cc", "cc", true, "", "-", "", "OK"},
	}
	for _, c := range cases {
		going := c.going
		if going == "" {
			going = "-"
		}
		got := Judge(c.prefix, c.live, c.screen, c.container, going)
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
	cases := []struct {
		name   string
		going  Going
		at     int64
		detail string
		now    int64
		window int64
		took   Took
		want   string
	}{
		{"found running", GoingNone, 0, "", 100, 120, TookNA, "-"},
		{"pending", GoingPending, 0, "", 100, 120, TookNA, "setting-going"},
		{"left alone with reason", GoingUntouched, 0, "no-transcript", 100, 120, TookNA, "failed:no-transcript"},
		{"left alone with no reason", GoingUntouched, 0, "", 100, 120, TookNA, "failed:no-reason-recorded"},
		{"compact taken", GoingCompact, 100, "300000", 105, 120, TookYes, "started"},
		{"compact inside window", GoingCompact, 100, "300000", 219, 120, TookNo, "setting-going"},
		{"compact past window", GoingCompact, 100, "300000", 220, 120, TookNo, "failed:compact-not-started"},
		{"continue past window", GoingContinue, 100, "5000", 400, 120, TookNo, "failed:continue-not-started"},
		{"compacting, taken", GoingCompacting, 100, "300000", 9999, 120, TookYes, "setting-going"},
		{"compacting, never taken", GoingCompacting, 100, "300000", 220, 120, TookNo, "failed:compact-not-started"},
		{"compacting, inside window", GoingCompacting, 100, "300000", 219, 120, TookNo, "setting-going"},
		{"unknown going", Going("keep"), 0, "", 1, 1, TookNA, "failed:unknown-going-keep"},
	}
	for _, c := range cases {
		if got := GoingState(c.going, c.at, c.detail, c.now, c.window, c.took); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
