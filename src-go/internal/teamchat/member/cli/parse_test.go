package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
)

const eventID = "$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"

func TestParseAcceptsBsForms(t *testing.T) {
	cases := []struct {
		args  []string
		verb  string
		pos   []string
		flags map[string][]string
	}{
		{[]string{"recv"}, "recv", nil, nil},
		{[]string{"recv", "--team", "alpha"}, "recv", nil, map[string][]string{"team": {"alpha"}}},
		{[]string{"wait", "--timeout", "30"}, "wait", nil, map[string][]string{"timeout": {"30"}}},
		{[]string{"reply", eventID, "--verb", "ack"}, "reply", []string{eventID}, map[string][]string{"verb": {"ack"}}},
		{[]string{"reply", "--verb", "done", "--ref", "pr:o/r#1@x", eventID, "--to-coordinator"}, "reply", []string{eventID}, map[string][]string{"verb": {"done"}, "ref": {"pr:o/r#1@x"}, "to-coordinator": {""}}},
		{[]string{"say", "--to", "alice,repo.2"}, "say", nil, map[string][]string{"to": {"alice,repo.2"}}},
		{[]string{"say", "--to-coordinator", "--team", "beta"}, "say", nil, map[string][]string{"to-coordinator": {""}, "team": {"beta"}}},
		{[]string{"ping", "review", "pr:o/r#12@abc", "--to", "repo.3", "--branch", "feat/x"}, "ping", []string{"review", "pr:o/r#12@abc"}, map[string][]string{"to": {"repo.3"}, "branch": {"feat/x"}}},
		{[]string{"ping", "check", "--to", `"$1"`, "--team", "alpha"}, "ping", []string{"check"}, map[string][]string{"to": {`"$1"`}, "team": {"alpha"}}},
		{[]string{"ask", "--asked-by", "repo.1,repo.2", "--re", eventID, "--remind-after", "600"}, "ask", nil, map[string][]string{"asked-by": {"repo.1,repo.2"}, "re": {eventID}, "remind-after": {"600"}}},
		{[]string{"thread", "Q4", "--ask", "--close"}, "thread", []string{"Q4"}, map[string][]string{"ask": {""}, "close": {""}}},
		{[]string{"questions", "--all"}, "questions", nil, map[string][]string{"all": {""}}},
		{[]string{"history", "--room", "questions", "--limit", "20"}, "history", nil, map[string][]string{"room": {"questions"}, "limit": {"20"}}},
		{[]string{"verify", eventID}, "verify", []string{eventID}, nil},
		{[]string{"version"}, "version", nil, nil},
		{[]string{"help"}, "help", nil, nil},
		{[]string{"help", "seat", "set-aside"}, "help", []string{"seat", "set-aside"}, nil},
		{[]string{"link", "--run", "/run/user/1000/teamchat/run/0123456789abcdef"}, "link", nil, map[string][]string{"run": {"/run/user/1000/teamchat/run/0123456789abcdef"}}},
		{[]string{"hook", "session-start"}, "hook", []string{"session-start"}, nil},
		{[]string{"hook", "user-prompt-submit"}, "hook", []string{"user-prompt-submit"}, nil},
		{[]string{"status-line"}, "status-line", nil, nil},
		{[]string{"reach", "--team", "alpha"}, "reach", nil, map[string][]string{"team": {"alpha"}}},
		{[]string{"seat", "disconnected", "--bundle", "/b", "--since", "1760000000000", "--seed", "abc"}, "seat disconnected", nil, map[string][]string{"bundle": {"/b"}, "since": {"1760000000000"}, "seed": {"abc"}}},
		{[]string{"seat", "set-aside", "--team", "alpha", "--state", "/s"}, "seat set-aside", nil, map[string][]string{"team": {"alpha"}, "state": {"/s"}}},
		{[]string{"guest", "check", "--user", "agent", "--team", "alpha"}, "guest check", nil, map[string][]string{"user": {"agent"}, "team": {"alpha"}}},
		{[]string{"guest", "place-bundle", "--user", "agent"}, "guest place-bundle", nil, map[string][]string{"user": {"agent"}}},
		{[]string{"guest", "membership", "--user", "agent"}, "guest membership", nil, map[string][]string{"user": {"agent"}}},
		{[]string{"guest", "remove", "--user", "agent", "--team", "alpha"}, "guest remove", nil, map[string][]string{"user": {"agent"}, "team": {"alpha"}}},
	}
	for _, c := range cases {
		inv, err := Parse(c.args)
		if err != nil {
			t.Errorf("%q: %v", c.args, err)
			continue
		}
		if inv.Verb.Name != c.verb || !slices.Equal(inv.Args, c.pos) {
			t.Errorf("%q: verb %q args %q", c.args, inv.Verb.Name, inv.Args)
		}
		for name, want := range c.flags {
			if !slices.Equal(inv.Values(name), want) {
				t.Errorf("%q: --%s %q", c.args, name, inv.Values(name))
			}
		}
		if len(inv.flags.values) != len(c.flags) {
			t.Errorf("%q: flags %v", c.args, inv.flags.values)
		}
		if v, given := inv.Flag("team"); given != (inv.Team != nil) || (given && inv.Team.String() != v) {
			t.Errorf("%q: team %v", c.args, inv.Team)
		}
	}
}

func TestParseRun(t *testing.T) {
	inv, err := Parse(strings.Fields("run --plain --kind other -- bash"))
	if err != nil || inv.Launch == nil || inv.Launch.Mode != LaunchPlain || inv.Verb.Name != "run" {
		t.Fatalf("%+v %v", inv, err)
	}
	if _, err := Parse(strings.Fields("run --plain -- bash")); !isUsage(err) {
		t.Errorf("a bad launch line: %v", err)
	}
}

func TestInvocationAccessors(t *testing.T) {
	inv, err := Parse([]string{"thread", "Q1", "--ask"})
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Has("ask") || inv.Has("close") {
		t.Error("Has")
	}
	if v, ok := inv.Flag("close"); ok || v != "" {
		t.Error("Flag of an absent flag")
	}
}

func usageOf(t *testing.T, args []string) *exits.Error {
	t.Helper()
	_, err := Parse(args)
	e, ok := errors.AsType[*exits.Error](err)
	if !ok || e.Exit != exits.ExitUsage {
		t.Errorf("%q: %v", args, err)
		return &exits.Error{}
	}
	return e
}

func TestParseRefusals(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--team", "alpha", "recv"},
		{"-v"},
		{"--version", "extra"},
		{"nonsense"},
		{"seat"},
		{"seat", "nonsense"},
		{"guest"},
		{"recv", "extra"},
		{"recv", "--team"},
		{"recv", "--team", "--all"},
		{"recv", "--team", "Bad Team"},
		{"recv", "--team", "alpha", "--team", "beta"},
		{"recv", "--team=alpha"},
		{"recv", "--colour"},
		{"recv", "-t", "alpha"},
		{"recv", "--"},
		{"version", "--team", "alpha"},
		{"wait", "--timeout", "soon"},
		{"wait", "--timeout", "-1"},
		{"wait", "--timeout", "1.5"},
		{"reply"},
		{"reply", "not-an-event-id"},
		{"reply", eventID, "--verb", "review"},
		{"reply", eventID, "--to-coordinator", "--to-coordinator"},
		{"say"},
		{"say", "--to", "a", "--to-coordinator"},
		{"say", "--to", ",a"},
		{"say", "--to", "a,"},
		{"ping", "--to", "a"},
		{"ping", "review", "ref", "extra", "--to", "a"},
		{"ping", "review", "--to", "a", "--branch", "bad branch"},
		{"thread", "4"},
		{"thread"},
		{"history", "--room", "reports"},
		{"history", "--limit", "x"},
		{"hook"},
		{"hook", "post-tool-use"},
		{"hook", "stop", "extra"},
		{"link"},
		{"link", "--run", "rel"},
		{"reach"},
		{"seat", "set-aside"},
		{"seat", "set-aside", "--team", "alpha", "--state", "rel"},
		{"seat", "disconnected", "--bundle", "/b", "--since", "x", "--seed", "s"},
		{"guest", "check", "--user", "agent"},
		{"guest", "check", "--user", "Bad User", "--team", "alpha"},
		{"guest", "remove", "--team", "alpha"},
		{"help", "a", "b", "c"},
	} {
		if e := usageOf(t, args); e.Code != exits.CodeUsage {
			t.Errorf("%q: code %s", args, e.Code)
		}
	}
}

// A bare ping exits 64 listing the verbs and saying that status shows who is live (B 2.2.1).
func TestABarePingListsTheVerbs(t *testing.T) {
	e := usageOf(t, []string{"ping", "--to", "repo.2"})
	for _, verb := range []string{"review", "fetch", "sync", "run-qa", "halt", "check"} {
		if !strings.Contains(e.Message, verb) {
			t.Errorf("%q does not list %s", e.Message, verb)
		}
	}
	if strings.Contains(e.Message, "ack") || !strings.Contains(e.Message, "teamchat status") {
		t.Errorf("%q", e.Message)
	}
}

func TestRefusalsNameWhatWasWrong(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--team", "alpha", "recv"}, "flags follow the verb"},
		{[]string{"nonsense"}, "no verb nonsense"},
		{[]string{"seat"}, "seat takes one of: disconnected, set-aside"},
		{[]string{"recv", "--colour"}, "teamchat recv has no flag --colour"},
		{[]string{"recv", "--team=alpha"}, "--team VALUE, not --team=VALUE"},
		{[]string{"say"}, "exactly one of --to, --to-coordinator"},
		{[]string{"reach"}, "teamchat reach needs --team"},
		{[]string{"reply"}, "teamchat reply needs its <event-id> argument"},
		{[]string{"recv", "x"}, "teamchat recv takes at most 0 arguments"},
		{[]string{"recv", "--team"}, "--team needs a value"},
		{[]string{"recv", "--team", "A"}, "--team takes a team id"},
	} {
		if e := usageOf(t, c.args); !strings.Contains(e.Message, c.want) {
			t.Errorf("%q: %q does not say %q", c.args, e.Message, c.want)
		}
	}
}

// A value that fails its grammar is never quoted in the refusal.
func TestARefusalQuotesNoValue(t *testing.T) {
	e := usageOf(t, []string{"recv", "--team", "secret-looking value"})
	if strings.Contains(e.Message, "secret") {
		t.Errorf("%q", e.Message)
	}
	e = usageOf(t, []string{"no such verb"})
	if strings.Contains(e.Message, "such") {
		t.Errorf("%q", e.Message)
	}
}
