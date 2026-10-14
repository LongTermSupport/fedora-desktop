package cli

// The shared command-line parser of the freeze tools (spec 2.2). Neither bash suite ever ran
// argument parsing (spec 9.3), so every case here is new. Each die message is asserted
// verbatim because users and scripts read them.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

var podSpec = Spec{
	Tool: "podfreeze",
	Flags: []Flag{
		{Name: "--ccy"},
		{Name: "--all"},
		{Name: "--network", Needs: "a network name"},
		{Name: "--github", Needs: "a value"},
		{Name: "--token", Needs: "a value"},
		{Name: "--ssh-key", Needs: "a value"},
	},
}

var lxcSpec = Spec{
	Tool: "lxcfreeze",
	Flags: []Flag{
		{Name: "--all"},
		{Name: "--bridge", Needs: "a bridge name"},
	},
}

func dieMessage(t *testing.T, err error) string {
	t.Helper()
	die, ok := errors.AsType[*freeze.DieError](err)
	if !ok {
		t.Fatalf("want a *freeze.DieError, got %T: %v", err, err)
	}
	return die.Msg
}

func TestParseAcceptsEveryTokenKind(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		args []string
		want Parsed
	}{
		{"no arguments is the menu", podSpec, nil, Parsed{}},
		{"help short", podSpec, []string{"-h"}, Parsed{Help: true}},
		{"help long", podSpec, []string{"--help"}, Parsed{Help: true}},
		{"dry run short", podSpec, []string{"-n"}, Parsed{DryRun: true}},
		{"dry run long", podSpec, []string{"--dry-run"}, Parsed{DryRun: true}},
		{"ccy", podSpec, []string{"--ccy"}, Parsed{Mode: "--ccy"}},
		{"all", podSpec, []string{"--all"}, Parsed{Mode: "--all"}},
		{"network with its value", podSpec, []string{"--network", "appnet"}, Parsed{Mode: "--network", Value: "appnet"}},
		{"github", podSpec, []string{"--github", "octo"}, Parsed{Mode: "--github", Value: "octo"}},
		{"token", podSpec, []string{"--token", "work"}, Parsed{Mode: "--token", Value: "work"}},
		{"ssh-key", podSpec, []string{"--ssh-key", "key_a"}, Parsed{Mode: "--ssh-key", Value: "key_a"}},
		{"verb list", podSpec, []string{"list"}, Parsed{Verb: "list"}},
		{"verb freeze", podSpec, []string{"freeze"}, Parsed{Verb: "freeze"}},
		{"verb thaw", podSpec, []string{"thaw"}, Parsed{Verb: "thaw"}},
		{"one name", podSpec, []string{"mike"}, Parsed{Mode: ModeNames, Names: []string{"mike"}}},
		{"names keep their order and repeats", podSpec, []string{"b", "a", "b"}, Parsed{Mode: ModeNames, Names: []string{"b", "a", "b"}}},
		{"the empty string is a name", podSpec, []string{""}, Parsed{Mode: ModeNames, Names: []string{""}}},
		{"words and flags in any order", podSpec, []string{"-n", "--ccy", "freeze"}, Parsed{Verb: "freeze", Mode: "--ccy", DryRun: true}},
		{"verb after the target", podSpec, []string{"--all", "thaw"}, Parsed{Verb: "thaw", Mode: "--all"}},
		{"names around a verb", podSpec, []string{"a", "freeze", "b"}, Parsed{Verb: "freeze", Mode: ModeNames, Names: []string{"a", "b"}}},
		{"lxc bridge", lxcSpec, []string{"--bridge", "lxcbr0"}, Parsed{Mode: "--bridge", Value: "lxcbr0"}},
		{"lxc all", lxcSpec, []string{"thaw", "--all", "-n"}, Parsed{Verb: "thaw", Mode: "--all", DryRun: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.spec, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseRefusesWithTheVerbatimMessage(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		args []string
		want string
	}{
		{"unknown long option", podSpec, []string{"--bogus"}, "unknown option '--bogus' — see 'podfreeze --help'."},
		{"unknown short option", podSpec, []string{"-x"}, "unknown option '-x' — see 'podfreeze --help'."},
		{"combined short flags are unknown", podSpec, []string{"-nh"}, "unknown option '-nh' — see 'podfreeze --help'."},
		{"a lone dash is an option", podSpec, []string{"-"}, "unknown option '-' — see 'podfreeze --help'."},
		{"a double dash is an option", podSpec, []string{"--"}, "unknown option '--' — see 'podfreeze --help'."},
		{"bridge is not a podfreeze flag", podSpec, []string{"--bridge", "br"}, "unknown option '--bridge' — see 'podfreeze --help'."},
		{"ccy is not an lxcfreeze flag", lxcSpec, []string{"--ccy"}, "unknown option '--ccy' — see 'lxcfreeze --help'."},
		{"network needs a name", podSpec, []string{"--network"}, "--network needs a network name — see 'podfreeze --help'."},
		{"bridge needs a name", lxcSpec, []string{"--bridge"}, "--bridge needs a bridge name — see 'lxcfreeze --help'."},
		{"github needs a value", podSpec, []string{"--github"}, "--github needs a value — see 'podfreeze --help'."},
		{"token needs a value", podSpec, []string{"--token"}, "--token needs a value — see 'podfreeze --help'."},
		{"ssh-key needs a value", podSpec, []string{"--ssh-key"}, "--ssh-key needs a value — see 'podfreeze --help'."},
		{"two verbs", podSpec, []string{"list", "freeze"}, "only one verb at a time — already have 'list'."},
		{"the same verb twice", podSpec, []string{"thaw", "thaw"}, "only one verb at a time — already have 'thaw'."},
		{"two targets", podSpec, []string{"--ccy", "--all"}, "choose one target only — '--ccy' and '--all' are mutually exclusive."},
		{"one flag repeated", podSpec, []string{"--ccy", "--ccy"}, "choose one target only — '--ccy' and '--ccy' are mutually exclusive."},
		{"a flag then a value flag", podSpec, []string{"--all", "--github", "octo"}, "choose one target only — '--all' and '--github' are mutually exclusive."},
		{"a flag then names", podSpec, []string{"--ccy", "foo"}, "choose one target only — '--ccy' and 'names' are mutually exclusive."},
		{"names then a flag", podSpec, []string{"foo", "--ccy"}, "choose one target only — '--ccy' and 'names' are mutually exclusive."},
		{"two bridges", lxcSpec, []string{"--bridge", "a", "--all"}, "choose one target only — '--bridge' and '--all' are mutually exclusive."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.spec, tc.args)
			if got := dieMessage(t, err); got != tc.want {
				t.Fatalf("got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// Parsing is left to right and the first problem ends it (bug 16, kept), so the order of two
// tokens decides whether the person is helped or refused.
func TestParseIsOrderDependent(t *testing.T) {
	if _, err := Parse(podSpec, []string{"--bogus", "--help"}); err == nil {
		t.Fatal("an unknown option before --help must die")
	}
	got, err := Parse(podSpec, []string{"--help", "--bogus"})
	if err != nil || !got.Help {
		t.Fatalf("--help before a bad option prints usage: %+v, %v", got, err)
	}
	if _, err = Parse(podSpec, []string{"--ccy", "--all", "-h"}); err == nil {
		t.Fatal("a second target dies before a later -h is reached")
	}
	got, err = Parse(podSpec, []string{"-n", "--ccy", "-h", "--all"})
	if err != nil || !got.Help {
		t.Fatalf("-h mid-line stops parsing: %+v, %v", got, err)
	}
}

// A flag's value is the next token verbatim (bug 15, kept): the error that follows is a clean
// "no such network" / "unknown value" from the engine, not a parse puzzle.
func TestParseTakesAFlagsValueVerbatim(t *testing.T) {
	got, err := Parse(podSpec, []string{"--network", "-n"})
	if err != nil || got.Mode != "--network" || got.Value != "-n" || got.DryRun {
		t.Fatalf("got %+v, %v", got, err)
	}
	got, err = Parse(podSpec, []string{"--github", "--ccy"})
	if err != nil || got.Mode != "--github" || got.Value != "--ccy" {
		t.Fatalf("got %+v, %v", got, err)
	}
	got, err = Parse(podSpec, []string{"--token", "list"})
	if err != nil || got.Value != "list" || got.Verb != "" {
		t.Fatalf("a verb word after a value flag is the value: %+v, %v", got, err)
	}
	got, err = Parse(podSpec, []string{"--network", ""})
	if err != nil || got.Mode != "--network" || got.Value != "" {
		t.Fatalf("an empty value is still a value: %+v, %v", got, err)
	}
}

func TestParsedAccessors(t *testing.T) {
	cases := []struct {
		verb   string
		list   bool
		action freeze.Verb
	}{
		{"", false, freeze.VerbDerived},
		{"list", true, freeze.VerbDerived},
		{"freeze", false, freeze.VerbFreeze},
		{"thaw", false, freeze.VerbThaw},
	}
	for _, tc := range cases {
		p := Parsed{Verb: tc.verb}
		if p.List() != tc.list || p.Action() != tc.action {
			t.Fatalf("verb %q: List %v Action %q", tc.verb, p.List(), p.Action())
		}
	}
}

func TestParsedTargetSet(t *testing.T) {
	if (Parsed{}).HasTarget() {
		t.Fatal("no mode is no target")
	}
	if !(Parsed{Mode: "--all"}).HasTarget() || !(Parsed{Mode: ModeNames}).HasTarget() {
		t.Fatal("any mode is a target")
	}
}
