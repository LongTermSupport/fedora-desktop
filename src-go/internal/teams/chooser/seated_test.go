package chooser

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Plan 00265 R90, Q1: plain cc in a checkout holding a host seat joins it. `choose-teams
// --type=host --seated` is cc's question: the one team this checkout holds a host seat of, as
// the payload, said in words; nothing, quietly, when it holds none.

func TestSeatedNamesTheOneTeamThisCheckoutHoldsAHostSeatOf(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"1:host"}, "other-v2": {"2:podman"}})
	exit(t, r.run("--type=host", "--seated"), term.ExitOK, r.rig)
	if r.payload.String() != v2Team+"\n" || len(r.Script.GumLog) != 0 {
		t.Fatalf("payload %q, questions %v", r.payload.String(), r.Script.GumLog)
	}
	contains(t, "stderr", r.stderr(), "This checkout holds a host seat of "+v2Team+": cc joins it",
		"cc --no-teams starts without it")
}

func TestSeatedIsQuietWhenThisCheckoutHoldsNoHostSeat(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2:podman"}})
	exit(t, r.run("--type=host", "--seated"), term.ExitOK, r.rig)
	r.lacksOutput(t)
	if r.stderr() != "" {
		t.Fatalf("a plain cc with no seat here is told %q", r.stderr())
	}
}

func TestSeatedJoinsNoneOfSeveralTeamsAndSaysHow(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"1:host"}, "other-v2": {"2:host"}})
	exit(t, r.run("--type=host", "--seated"), term.ExitOK, r.rig)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "host seats of "+v2Team+" and other-v2", "cc --teams <team>")
}

func TestSeatedLeavesOutATeamWhoseBusDoesNotAnswer(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"1:host"}})
	r.dead[v2Team] = "its bus is stopped"
	exit(t, r.run("--type=host", "--seated"), term.ExitOK, r.rig)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "Not joined: team "+v2Team+": its bus is stopped.")
}

func TestSeatedOutsideAHostCheckoutIsQuiet(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"1:host"}})
	r.On("git rev-parse --is-inside-work-tree", "false\n", 0)
	exit(t, r.run("--type=host", "--seated"), term.ExitOK, r.rig)
	r.lacksOutput(t)
	if r.stderr() != "" {
		t.Fatalf("outside a checkout plain cc is told %q", r.stderr())
	}
}

func TestSeatedIsCcsQuestionAlone(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"1:host"}})
	exit(t, r.run("--type=podman", "--seated"), term.ExitUsage, r.rig)
	exit(t, r.run("--type=host", "--seated", v2Team), term.ExitUsage, r.rig)
	exit(t, r.run("--type=host", "--seated", "--bus"), term.ExitUsage, r.rig)
	r.lacksOutput(t)
}
