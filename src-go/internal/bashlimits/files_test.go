package bashlimits

import (
	"path"
	"testing"
	"testing/fstest"
)

func TestIsCandidate(t *testing.T) {
	fsys := fstest.MapFS{
		"a/x.bash":              {Data: []byte("echo\n")},
		"a/x.sh":                {Data: []byte("echo\n")},
		"a/bin/tool":            {Data: []byte("#!/usr/bin/bash\necho\n")},
		"a/bin/envtool":         {Data: []byte("#!/usr/bin/env bash\n")},
		"a/bin/envdashs":        {Data: []byte("#!/usr/bin/env -S bash -e\n")},
		"a/bin/shtool":          {Data: []byte("#!/bin/sh\n")},
		"a/bin/spaced":          {Data: []byte("#! /bin/bash\n")},
		"a/bin/pytool":          {Data: []byte("#!/usr/bin/python3\n")},
		"a/bin/shell-ish":       {Data: []byte("#!/usr/bin/shellcheck\n")},
		"a/bin/noshebang":       {Data: []byte("echo\n")},
		"a/bin/empty":           {Data: nil},
		"a/tpl.j2":              {Data: []byte("#!/usr/bin/bash\n")},
		"a/pytpl.j2":            {Data: []byte("#!/usr/bin/python3\n")},
		"a/note.j2":             {Data: []byte("{{ x }}\n")},
		"a/doc.md":              {Data: []byte("#!/bin/bash\n")},
		"a/prog.py":             {Data: []byte("#!/bin/bash\n")},
		"a/with.dot.tool":       {Data: []byte("#!/bin/bash\n")},
		"a/.hidden":             {Data: []byte("#!/bin/bash\n")},
		"a/shebang-only-second": {Data: []byte("\n#!/bin/bash\n")},
	}
	want := map[string]bool{
		"a/x.bash": true, "a/x.sh": true, "a/bin/tool": true, "a/bin/envtool": true,
		"a/bin/envdashs": true, "a/bin/shtool": true, "a/bin/spaced": true,
		"a/bin/pytool": false, "a/bin/shell-ish": false, "a/bin/noshebang": false,
		"a/bin/empty": false, "a/tpl.j2": true, "a/pytpl.j2": false, "a/note.j2": false,
		"a/doc.md": false, "a/prog.py": false, "a/with.dot.tool": false, "a/.hidden": false,
		"a/shebang-only-second": false,
	}
	for p, w := range want {
		got, err := IsCandidate(fsys, p)
		if err != nil || got != w {
			t.Errorf("%s: got %v, %v; want %v", p, got, err, w)
		}
	}
}

func TestIsCandidateMissingFileIsAnError(t *testing.T) {
	if _, err := IsCandidate(fstest.MapFS{}, "a/tool"); err == nil {
		t.Fatal("a missing extensionless file was not an error")
	}
}

func TestExemptionMatching(t *testing.T) {
	cases := []struct {
		pattern, p string
		want       bool
	}{
		{"run.bash", "run.bash", true},
		{"run.bash", "x/run.bash", false},
		{"tests/", "tests/helpers/a.bash", true},
		{"tests/", "tests", false},
		{"tests/", "xtests/a.bash", false},
		{"scripts/test-*.bash", "scripts/test-a.bash", true},
		{"scripts/test-*.bash", "scripts/sub/test-a.bash", false},
		{"scripts/test-*.bash", "scripts/qa-a.bash", false},
		{"CLAUDE/Plan/[0-9]*/", "CLAUDE/Plan/00192-x/deploy.bash", true},
		{"CLAUDE/Plan/[0-9]*/", "CLAUDE/Plan/00192-x/sub/dir/a.bash", true},
		{"CLAUDE/Plan/[0-9]*/", "CLAUDE/Plan/mkplan.bash", false},
		{"CLAUDE/Plan/[0-9]*/", "CLAUDE/Plan/_planlib.inc.bash", false},
		{"CLAUDE/Plan/Completed/", "CLAUDE/Plan/Completed/00001-x/a.bash", true},
	}
	for _, c := range cases {
		got, err := matchPattern(c.pattern, c.p)
		if err != nil || got != c.want {
			t.Errorf("%q vs %q: got %v, %v; want %v", c.pattern, c.p, got, err, c.want)
		}
	}
	if _, err := matchPattern("[", "a"); err == nil {
		t.Fatal("a malformed pattern was not an error")
	}
}

func TestExemptionTableIsSound(t *testing.T) {
	for _, e := range Exemptions {
		if e.Reason == "" || len(e.Reason) < 20 {
			t.Errorf("%q has no real reason", e.Pattern)
		}
		if _, err := path.Match(e.Pattern, ""); err != nil && e.Pattern[len(e.Pattern)-1] != '/' {
			t.Errorf("%q is not a valid pattern: %v", e.Pattern, err)
		}
	}
}

func TestExemptionFor(t *testing.T) {
	cases := map[string]bool{
		"run.bash": true,
		"files/home/bashrc-includes/history-search.bash": true,
		"files/etc/profile.d/zz_lts-fedora-desktop.bash": true,
		"scripts/test-ccy-lifecycle.bash":                true,
		"tests/helpers/x.bash":                           true,
		"CLAUDE/Plan/00192-x/triage.bash":                true,
		"CLAUDE/Plan/Completed/00001-y/a.bash":           true,
		".claude/init.sh":                                true,
		"scripts/qa-all.bash":                            false,
		"CLAUDE/Plan/mkplan.bash":                        false,
		"CLAUDE/Plan/_planlib.inc.bash":                  false,
		"files/home/.local/bin/vmtest":                   false,
		"scripts/lib/qa-helper-summary.bash":             false,
	}
	for p, want := range cases {
		reason, ok, err := ExemptionFor(p)
		if err != nil || ok != want || (ok && reason == "") {
			t.Errorf("%s: got %q, %v, %v; want exempt=%v", p, reason, ok, err, want)
		}
	}
}
