package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/kit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func mustTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustSeat(t *testing.T, s string) paths.SeatName {
	t.Helper()
	n, err := paths.ParseSeatName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// B 2.2.1's verbs, every one, and no other.
var bVerbs = []string{
	"recv", "wait", "inbox", "reply", "say", "ping", "report", "ask", "thread", "questions",
	"history", "verify", "status", "version", "help",
	"run", "link", "hook", "status-line", "reach", "seat disconnected", "seat set-aside",
	"guest check", "guest place-bundle", "guest membership", "guest remove",
}

func TestTheTableIsBsVerbs(t *testing.T) {
	names := make([]string, 0, len(bVerbs))
	for _, v := range Verbs() {
		names = append(names, v.Name)
	}
	if !slices.Equal(names, bVerbs) {
		t.Errorf("verbs %q", names)
	}
}

func TestEveryVerbIsBuiltOrNamesItsSlice(t *testing.T) {
	slices := map[string]bool{"n3a guest": true, "n3c": true, "n3d": true, "n3e": true, "n3f": true}
	for _, v := range Verbs() {
		_, built := runs[v.Name]
		slice, later := unbuilt[v.Name]
		if built == later {
			t.Errorf("%s: built %v, unbuilt %v", v.Name, built, later)
		}
		if later && !slices[slice] {
			t.Errorf("%s lands in unknown slice %q", v.Name, slice)
		}
	}
	if len(runs)+len(unbuilt) != len(Verbs()) {
		t.Errorf("%d built and %d unbuilt for %d verbs", len(runs), len(unbuilt), len(Verbs()))
	}
}

// The unbuilt list as n3a leaves it; each later slice removes its own.
func TestUnbuiltAreTheLaterSlices(t *testing.T) {
	want := map[string]string{
		"run": "n3c", "link": "n3c", "recv": "n3c", "wait": "n3c", "inbox": "n3c", "status": "n3c",
		"verify": "n3c", "history": "n3c", "seat disconnected": "n3c", "reach": "n3c",
		"reply": "n3d", "say": "n3d", "ping": "n3d",
		"hook": "n3e", "status-line": "n3e",
		"report": "n3f", "ask": "n3f", "thread": "n3f", "questions": "n3f",
		"guest check": "n3a guest", "guest place-bundle": "n3a guest", "guest membership": "n3a guest",
		"guest remove": "n3a guest", "seat set-aside": "n3a guest",
	}
	for name, slice := range want {
		if unbuilt[name] != slice {
			t.Errorf("%s: %q, want %q", name, unbuilt[name], slice)
		}
	}
	if len(unbuilt) != len(want) {
		t.Errorf("%d unbuilt, want %d", len(unbuilt), len(want))
	}
}

// --team on every agent-facing verb but version and help (B 2.2.1, B5).
func TestTeamOnEveryAgentVerbButVersionAndHelp(t *testing.T) {
	for _, v := range Verbs() {
		has := slices.ContainsFunc(v.Flags, func(f Flag) bool { return f.Name == "team" })
		want := v.Agent && v.Name != "version" && v.Name != "help"
		if v.Agent && has != want {
			t.Errorf("%s: --team %v", v.Name, has)
		}
		if v.Agent != (v.Team != TeamNone || v.Name == "version" || v.Name == "help") {
			t.Errorf("%s: agent %v, team use %v", v.Name, v.Agent, v.Team)
		}
	}
}

func TestTeamUses(t *testing.T) {
	var one, multi []string
	for _, v := range Verbs() {
		switch v.Team {
		case TeamOne:
			one = append(one, v.Name)
		case TeamMulti:
			multi = append(multi, v.Name)
		case TeamNone:
		}
	}
	if !slices.Equal(one, []string{"reply", "say", "ping", "report", "ask", "thread", "verify"}) {
		t.Errorf("single-team verbs %q", one)
	}
	if !slices.Equal(multi, []string{"recv", "wait", "inbox", "questions", "history", "status"}) {
		t.Errorf("multi-team verbs %q", multi)
	}
}

func TestUsageSpellsBsForms(t *testing.T) {
	for name, want := range map[string]string{
		"reply":             "teamchat reply <event-id> [--verb V] [--ref R] [--to-coordinator] [--team T]",
		"say":               "teamchat say --to N[,N...]|--to-coordinator [--team T]",
		"ping":              "teamchat ping <verb> [<ref>] --to N[,N...]|--to-coordinator [--branch B] [--team T]",
		"ask":               "teamchat ask [--asked-by N,...] [--re ID,...] [--remind-after S] [--team T]",
		"thread":            "teamchat thread Q<n> [--ask] [--close] [--team T]",
		"history":           "teamchat history [--room main|questions] [--limit N] [--team T]",
		"version":           "teamchat version",
		"help":              "teamchat help [<verb>|exit-codes]",
		"run":               "teamchat run --claude|--plain --kind ccy|lxc|host|other [--desired DIR] [--seat T/S...] [--state DIR] [--resume-recorded] [--kit DIR] -- CMD",
		"seat set-aside":    "teamchat seat set-aside --team T [--state DIR]",
		"seat disconnected": "teamchat seat disconnected --bundle DIR --since MS --seed ID",
		"guest check":       "teamchat guest check --user U --team T",
		"hook":              "teamchat hook session-start|user-prompt-submit|stop|session-end",
	} {
		v, ok := lookup(name)
		if !ok {
			t.Fatalf("no verb %s", name)
		}
		if got := Usage(v); got != want {
			t.Errorf("%s:\ngot  %s\nwant %s", name, got, want)
		}
	}
}

func TestEveryVerbSaysWhatItDoes(t *testing.T) {
	for _, v := range Verbs() {
		if v.Does == "" || strings.HasSuffix(v.Does, ".") {
			t.Errorf("%s: %q", v.Name, v.Does)
		}
	}
}

func TestVerbsIsACopy(t *testing.T) {
	v := Verbs()
	v[0].Flags[0].Name = "changed"
	if Verbs()[0].Flags[0].Name == "changed" {
		t.Error("the table changed through its copy")
	}
}

// The hook events the parser takes are those the kit's hooks.json names.
func TestHookEventsAreTheKits(t *testing.T) {
	args := make([]string, 0, len(kit.Hooks))
	for _, h := range kit.Hooks {
		args = append(args, h.Arg)
	}
	if !slices.Equal(args, HookEvents) {
		t.Errorf("kit %q, cli %q", args, HookEvents)
	}
}
