package cli

import (
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Invocation is one parsed command line: the verb, its positional arguments, its flags, the
// --team it names, and for run its launch line.
type Invocation struct {
	Verb   Verb
	Args   []string
	Team   *paths.TeamID
	Launch *Launch
	flags  parsed
}

// Flag is a flag's value and whether it was given; a flag that takes no value reads "".
func (i Invocation) Flag(name string) (string, bool) {
	return i.flags.value(name), i.flags.has(name)
}

// Has is whether a flag was given.
func (i Invocation) Has(name string) bool { return i.flags.has(name) }

// Values is every value a repeatable flag was given, in order.
func (i Invocation) Values(name string) []string { return slices.Clone(i.flags.values[name]) }

// parsed is a verb's arguments after the verb: flags by name, and the positionals.
type parsed struct {
	values map[string][]string
	args   []string
}

func (p parsed) has(name string) bool {
	_, ok := p.values[name]
	return ok
}

func (p parsed) value(name string) string {
	if v := p.values[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// The messages of a refused command line.
const (
	msgNoVerb       exits.Template = "teamchat needs a verb; teamchat help lists them"
	msgFlagFirst    exits.Template = "flags follow the verb: teamchat <verb> [arguments] [flags]; teamchat help lists the verbs"
	msgUnknownVerb  exits.Template = "teamchat has no verb {}; teamchat help lists the verbs"
	msgSubVerb      exits.Template = "teamchat {} takes one of: {}"
	msgFlagTwice    exits.Template = "--{} is given twice"
	msgFlagEquals   exits.Template = "flags take their value as the next argument: --{} VALUE, not --{}=VALUE"
	msgFlagUnknown  exits.Template = "teamchat {} has no flag {}; teamchat help {} lists its flags"
	msgFlagNoValue  exits.Template = "--{} needs a value"
	msgFlagRequired exits.Template = "teamchat {} needs --{}"
	msgGroup        exits.Template = "teamchat {} takes exactly one of {}"
	msgArgMissing   exits.Template = "teamchat {} needs its <{}> argument"
	msgArgsTooMany  exits.Template = "teamchat {} takes at most {} arguments; teamchat help {} shows its form"
)

// versionFlag is the one flag before a verb: `teamchat --version` alone, the kit check of B
// 2.4.1 step 2.
const versionFlag = "--version"

// Parse parses a command line after the program name (CL2): the verb first, then its
// arguments and flags. A refusal is a usage *exits.Error that quotes no value it was given.
func Parse(args []string) (Invocation, error) {
	if len(args) == 0 {
		return Invocation{}, usage(msgNoVerb)
	}
	if strings.HasPrefix(args[0], "-") {
		return Invocation{}, usage(msgFlagFirst)
	}
	v, rest, err := findVerb(args)
	if err != nil {
		return Invocation{}, err
	}
	inv := Invocation{Verb: v}
	if v.Name == verbRun {
		l, lerr := ParseLaunch(rest)
		if lerr != nil {
			return Invocation{}, lerr
		}
		inv.Launch = &l
		return inv, nil
	}
	if inv.flags, err = parseFlags(v.Name, v.Flags, rest); err != nil {
		return Invocation{}, err
	}
	if inv.Args, err = checkArgs(v, inv.flags.args); err != nil {
		return Invocation{}, err
	}
	if t, given := inv.Flag(flagTeam); given {
		id, err := paths.ParseTeamID(t)
		if err != nil {
			return Invocation{}, usage(checkTeam.msg, exits.Name(flagTeam))
		}
		inv.Team = &id
	}
	return inv, nil
}

// findVerb is the verb args start with, one word or two (`seat set-aside`), and the rest.
func findVerb(args []string) (Verb, []string, error) {
	if len(args) > 1 {
		if v, ok := lookup(args[0] + " " + args[1]); ok {
			return v, args[2:], nil
		}
	}
	if v, ok := lookup(args[0]); ok {
		return v, args[1:], nil
	}
	var subs []exits.Fill
	for _, v := range table {
		if group, sub, ok := strings.Cut(v.Name, " "); ok && group == args[0] {
			subs = append(subs, exits.Words(sub))
		}
	}
	if len(subs) > 0 {
		return Verb{}, nil, usage(msgSubVerb, exits.Words(args[0]), exits.Join(", ", subs...))
	}
	return Verb{}, nil, usage(msgUnknownVerb, exits.Name(args[0]))
}

// parseFlags reads a verb's flags and positionals: `--flag value` or `--flag` only, each given
// once unless it repeats, each value checked; then every required flag and every group.
func parseFlags(verb string, flags []Flag, args []string) (parsed, error) {
	p := parsed{values: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			p.args = append(p.args, a)
			continue
		}
		f, err := findFlag(verb, flags, a)
		if err != nil {
			return parsed{}, err
		}
		if p.has(f.Name) && !f.Repeat {
			return parsed{}, usage(msgFlagTwice, exits.Name(f.Name))
		}
		val := ""
		if f.Value != "" {
			if i+1 == len(args) || strings.HasPrefix(args[i+1], "--") {
				return parsed{}, usage(msgFlagNoValue, exits.Name(f.Name))
			}
			i++
			val = args[i]
			if f.check != nil && !f.check.ok(val) {
				return parsed{}, usage(f.check.msg, exits.Name(f.Name))
			}
		}
		p.values[f.Name] = append(p.values[f.Name], val)
	}
	return p, checkPresence(verb, flags, p)
}

func findFlag(verb string, flags []Flag, a string) (Flag, error) {
	name, isLong := strings.CutPrefix(a, "--")
	if isLong {
		if before, _, eq := strings.Cut(name, "="); eq {
			return Flag{}, usage(msgFlagEquals, exits.Name(before), exits.Name(before))
		}
		if i := slices.IndexFunc(flags, func(f Flag) bool { return f.Name == name }); i >= 0 {
			return flags[i], nil
		}
	}
	return Flag{}, usage(msgFlagUnknown, exits.Words(verb), exits.Name(a), exits.Words(verb))
}

func checkPresence(verb string, flags []Flag, p parsed) error {
	groups := map[string][]exits.Fill{}
	given := map[string]int{}
	var order []string
	for _, f := range flags {
		if f.Required && !p.has(f.Name) {
			return usage(msgFlagRequired, exits.Words(verb), exits.Name(f.Name))
		}
		if f.Group == "" {
			continue
		}
		if _, seen := groups[f.Group]; !seen {
			order = append(order, f.Group)
		}
		groups[f.Group] = append(groups[f.Group], exits.Name("--"+f.Name))
		if p.has(f.Name) {
			given[f.Group]++
		}
	}
	for _, g := range order {
		if given[g] != 1 {
			return usage(msgGroup, exits.Words(verb), exits.Join(", ", groups[g]...))
		}
	}
	return nil
}

func checkArgs(v Verb, args []string) ([]string, error) {
	if len(args) > len(v.Args) {
		return nil, usage(msgArgsTooMany, exits.Words(v.Name), exits.Count(int64(len(v.Args))), exits.Words(v.Name))
	}
	for i, a := range v.Args {
		if i >= len(args) {
			if a.Optional {
				break
			}
			if a.missing != nil {
				return nil, a.missing()
			}
			return nil, usage(msgArgMissing, exits.Words(v.Name), exits.Words(a.Name))
		}
		if a.check != nil && !a.check.ok(args[i]) {
			return nil, usage(a.check.msg, exits.Words(a.Name))
		}
	}
	return args, nil
}
