// Package cli is the command-line grammar the two freeze tools share: verbs, -n, -h, one
// target mode, and the names that make up the "names" mode. Each tool declares its own target
// flags in a Spec; the rules about how tokens combine are written once, here.
package cli

import (
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

// ModeNames is the target mode of container names given on the command line.
const ModeNames = "names"

// Flag is a target flag a tool accepts. Needs is empty for a flag with no value; otherwise it
// is what the missing value is called in the error ("a network name").
type Flag struct {
	Name  string
	Needs string
}

// Spec is one tool's grammar.
type Spec struct {
	Tool  string
	Flags []Flag
}

// Parsed is what the command line asked for.
type Parsed struct {
	Help   bool     // -h/--help was reached; nothing after it was read
	DryRun bool     // -n/--dry-run
	Verb   string   // "", "list", "freeze" or "thaw"
	Mode   string   // "" (the menu), a Flag name, or ModeNames
	Value  string   // the value of a Mode flag that takes one
	Names  []string // container names, in order, repeats kept (selection de-duplicates)
}

// List reports the list verb.
func (p Parsed) List() bool { return p.Verb == "list" }

// Action is the explicit freeze or thaw verb, or VerbDerived when the state decides.
func (p Parsed) Action() freeze.Verb {
	switch p.Verb {
	case "freeze":
		return freeze.VerbFreeze
	case "thaw":
		return freeze.VerbThaw
	}
	return freeze.VerbDerived
}

// HasTarget reports whether the command line named a target; without one the tool runs its
// interactive menu.
func (p Parsed) HasTarget() bool { return p.Mode != "" }

// Parse reads args left to right, one token at a time. The first problem ends it, so the order
// of tokens matters: --help before a bad option prints usage, after it the option is refused.
// A flag's value is the next token verbatim, even when it looks like an option.
func Parse(spec Spec, args []string) (Parsed, error) {
	var p Parsed
	rest := args
	for len(rest) > 0 {
		tok := rest[0]
		rest = rest[1:]
		switch tok {
		case "-h", "--help":
			p.Help = true
			return p, nil
		case "-n", "--dry-run":
			p.DryRun = true
		case "list", "freeze", "thaw":
			if p.Verb != "" {
				return Parsed{}, freeze.Die("only one verb at a time — already have '%s'.", p.Verb)
			}
			p.Verb = tok
		default:
			var err error
			if rest, err = spec.consume(&p, tok, rest); err != nil {
				return Parsed{}, err
			}
		}
	}
	if len(p.Names) > 0 {
		if err := setMode(&p, ModeNames); err != nil {
			return Parsed{}, err
		}
	}
	return p, nil
}

// consume handles a token that is not a verb or a common option: a target flag (taking its value
// from rest), an unknown option, or a container name. It returns what is left of the line.
func (s Spec) consume(p *Parsed, tok string, rest []string) ([]string, error) {
	flag, ok := s.flag(tok)
	switch {
	case ok:
		if err := setMode(p, tok); err != nil {
			return nil, err
		}
		if flag.Needs == "" {
			return rest, nil
		}
		if len(rest) == 0 {
			return nil, freeze.Die("%s needs %s — see '%s --help'.", tok, flag.Needs, s.Tool)
		}
		p.Value = rest[0]
		return rest[1:], nil
	case strings.HasPrefix(tok, "-"):
		return nil, freeze.Die("unknown option '%s' — see '%s --help'.", tok, s.Tool)
	}
	p.Names = append(p.Names, tok)
	return rest, nil
}

func (s Spec) flag(tok string) (Flag, bool) {
	for _, f := range s.Flags {
		if f.Name == tok {
			return f, true
		}
	}
	return Flag{}, false
}

func setMode(p *Parsed, mode string) error {
	if p.Mode != "" {
		return freeze.Die("choose one target only — '%s' and '%s' are mutually exclusive.", p.Mode, mode)
	}
	p.Mode = mode
	return nil
}
