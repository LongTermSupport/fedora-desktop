package qatrees

import (
	"strings"
	"testing"
	"testing/fstest"
)

const table = "# a comment\n\nshell\t.claude/hooks\nshell\tCLAUDE/Plan/mkplan.bash\nshell\tboth\npython\tboth\npython\tpyonly\n"

func parsed(t *testing.T) Table {
	t.Helper()
	tab, err := Parse(strings.NewReader(table), "qa-trees.tsv")
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func TestTheTableIsReadByList(t *testing.T) {
	tab := parsed(t)
	if strings.Join(tab.Shell, ",") != ".claude/hooks,CLAUDE/Plan/mkplan.bash,both" || strings.Join(tab.Python, ",") != "both,pyonly" {
		t.Fatalf("%+v", tab)
	}
}

func TestABadTableIsAnError(t *testing.T) {
	for name, text := range map[string]string{
		"one field":     "shell\n",
		"empty dir":     "shell\t\npython\tx\n",
		"unknown list":  "shell\tx\npython\tx\nruby\tx\n",
		"no shell row":  "python\tx\n",
		"no python row": "shell\tx\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(text), "qa-trees.tsv"); err == nil || !strings.Contains(err.Error(), "qa-trees.tsv") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestLoadReadsTheFileAndNamesOneThatIsMissing(t *testing.T) {
	if _, err := Load(fstest.MapFS{}); err == nil || !strings.Contains(err.Error(), "excluded trees") {
		t.Fatalf("a missing table loaded: %v", err)
	}
	if tab, err := Load(fstest.MapFS{File: {Data: []byte(table)}}); err != nil || len(tab.Shell) != 3 {
		t.Fatalf("%+v %v", tab, err)
	}
}

func TestAPathIsExcludedByWholeSegmentsAtAnyDepth(t *testing.T) {
	tab := parsed(t)
	for rel, want := range map[string]bool{
		"CLAUDE/Plan/mkplan.bash":         true,
		"sub/CLAUDE/Plan/mkplan.bash":     true,
		"CLAUDE/Plan/mkplan.bash.orig":    false,
		".claude/hooks/pre-tool-use":      true,
		".claude/hooksx/pre-tool-use":     false,
		"both/x.txt":                      true,
		"notboth/x.txt":                   false,
		"CLAUDE/Plan/00001-x/mkplan.bash": false,
	} {
		if got := excluded(rel, tab.Shell); got != want {
			t.Errorf("%s: %v, want %v", rel, got, want)
		}
	}
}

// A file's language picks its list: Python by its .py name or a python shebang, anything else
// by the shell list (which also serves YAML, JavaScript and Markdown discovery).
func TestAFileIsJudgedByTheListOfItsLanguage(t *testing.T) {
	tab := parsed(t)
	fsys := fstest.MapFS{
		".claude/hooks/pre-tool-use":         {Data: []byte("#!/bin/bash\n")},
		".claude/hooks/handlers/h.py":        {Data: []byte("x = 1\n")},
		".claude/hooks/handlers/tool":        {Data: []byte("#!/usr/bin/env python3\n")},
		".claude/hooks/handlers/README.md":   {Data: []byte("# r\n")},
		"pyonly/tool.bash":                   {Data: []byte("echo\n")},
		"pyonly/tool.py":                     {Data: []byte("x = 1\n")},
		"both/anything":                      {Data: []byte("x\n")},
		"elsewhere/file":                     {Data: []byte("x\n")},
		".claude/hooks/handlers/python3-ish": {Data: []byte("#!/usr/bin/python3.14 -u\n")},
	}
	for rel, want := range map[string]bool{
		".claude/hooks/pre-tool-use":         true,
		".claude/hooks/handlers/h.py":        false,
		".claude/hooks/handlers/tool":        false,
		".claude/hooks/handlers/python3-ish": false,
		".claude/hooks/handlers/README.md":   true,
		"pyonly/tool.bash":                   false,
		"pyonly/tool.py":                     true,
		"both/anything":                      true,
		"elsewhere/file":                     false,
	} {
		got, err := tab.ExcludesFile(fsys, rel)
		if err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", rel, got, err, want)
		}
	}
}

func TestAFileThatCannotBeReadWhenItsLanguageDecidesIsAnError(t *testing.T) {
	if _, err := parsed(t).ExcludesFile(fstest.MapFS{}, ".claude/hooks/gone"); err == nil {
		t.Fatal("an unreadable file was judged")
	}
}
