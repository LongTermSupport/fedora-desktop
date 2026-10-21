package convert

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseArgsAcceptsTheGrammar(t *testing.T) {
	cases := []struct {
		args  []string
		file  string
		to    []string
		force bool
	}{
		{[]string{"a.md"}, "a.md", nil, false},
		{[]string{"a.md", "--to", "docx"}, "a.md", []string{"docx"}, false},
		{[]string{"--to=DOCX", "a.md"}, "a.md", []string{"docx"}, false},
		{[]string{"a.md", "--to", " docx , DOCX"}, "a.md", []string{"docx"}, false}, // E6
		{[]string{"-f", "a.md", "--to", "docx,odt"}, "a.md", []string{"docx", "odt"}, true},
		{[]string{"--force", "--", "-x.md"}, "-x.md", nil, true}, // E2: -- ends options
	}
	for _, c := range cases {
		o, err := parseArgs(c.args)
		if err != nil || o.file != c.file || !slices.Equal(o.to, c.to) || o.force != c.force {
			t.Errorf("%q: got %+v, %v", c.args, o, err)
		}
	}
}

// E1: help anywhere wins over an error.
func TestParseArgsHelpWins(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--bogus", "--help"}, {"a.md", "b.md", "-h"}} {
		o, err := parseArgs(args)
		if err != nil || !o.help {
			t.Errorf("%q: %+v %v", args, o, err)
		}
	}
}

// E2, E6: each usage error, worded with its reason.
func TestParseArgsUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"unknown option '--bogus'":     {"--bogus", "a.md"},
		"--to needs a list of formats": {"a.md", "--to"},
		"--to is given twice":          {"a.md", "--to", "docx", "--to=odt"},
		"no file given":                {"--to", "docx"},
		"one file at a time, got 2":    {"a.md", "b.md"},
		"empty format in --to 'docx,'": {"a.md", "--to", "docx,"},
		"empty format in --to ''":      {"a.md", "--to="},
	}
	for want, args := range cases {
		_, err := parseArgs(args)
		f, ok := errors.AsType[*failure](err)
		if !ok || f.code != 64 || !strings.Contains(f.msg, want) || !strings.HasSuffix(f.msg, "see 'convert --help'.") {
			t.Errorf("%q: want %q, got %v", args, want, err)
		}
	}
}
