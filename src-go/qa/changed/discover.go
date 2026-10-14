// Package changed is qa-changed: the diff-scoped QA run while iterating. It collects the
// changed files, classifies each by the routing table (scripts/qa-routing.tsv), chooses the
// checks and the test suites those files need, runs them and prints one verdict. The contract
// callers read (the plan lines, the check lines, the ending, the exit codes) is in
// CLAUDE/Plan/00192-.../SPEC-qa-changed.md section 7 and 8.4.
package changed

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// star is the glob wildcard, spelled apart from a slash in the few Go strings that combine them.
const star = "*"

// globMatch is bash `case` pattern matching restricted to the star: a star matches any run of
// characters including a slash (so "*.bash" matches at any depth and "src-go/" plus a star
// matches everything below), everything else is literal. path.Match differs, so it is not used.
func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, mid := range parts[1 : len(parts)-1] {
		i := strings.Index(s, mid)
		if i < 0 {
			return false
		}
		s = s[i+len(mid):]
	}
	return strings.HasSuffix(s, last)
}

// treeList is a list of excluded directory names.
type treeList []string

// excludes is whether the repo-relative path lies inside one of the directories, matched as a
// whole-name substring of the slash-delimited path so the name matches at any depth.
func (l treeList) excludes(rel string) bool {
	padded := "/" + rel + "/"
	for _, dir := range l {
		if strings.Contains(padded, "/"+dir+"/") {
			return true
		}
	}
	return false
}

var (
	shellInlineScript = regexp.MustCompile(`^#!/.*bash\s+-c\s`)
	shellShebang      = regexp.MustCompile(`^#!/.*bash`)
	pythonShebang     = regexp.MustCompile(`^#!.*python`)
)

// isShellShebang is the one shell-shebang test: a bash shebang, or exactly /bin/sh or
// /usr/bin/sh. `bash -c '<script>'` hands the file to something else, so it is not shell.
func isShellShebang(line string) bool {
	if shellInlineScript.MatchString(line) {
		return false
	}
	return shellShebang.MatchString(line) || line == "#!/bin/sh" || line == "#!/usr/bin/sh"
}

// isPythonShebang is the one Python-shebang test.
func isPythonShebang(line string) bool {
	return pythonShebang.MatchString(line)
}

// maxFirstLine bounds the read of a first line: a shebang is short, and a binary file has no
// line breaks.
const maxFirstLine = 4096

// firstLine is the first line of a file and whether it has one. An unreadable file is an
// error: it must not be quietly classified as "not a script".
func firstLine(path string) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("cannot read %s: %w", path, err)
	}
	raw, readErr := bufio.NewReaderSize(f, maxFirstLine).ReadSlice('\n')
	line := string(raw)
	closeErr := f.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, bufio.ErrBufferFull) {
		return "", false, fmt.Errorf("cannot read %s: %w", path, readErr)
	}
	if closeErr != nil {
		return "", false, fmt.Errorf("cannot close %s: %w", path, closeErr)
	}
	line = strings.TrimSuffix(line, "\n")
	return line, line != "", nil
}

// fileProbe answers what kind of file a repo-relative path is, reading the first line only
// when the name does not already say.
type fileProbe struct {
	root string
}

func (p fileProbe) shebang(rel string, test func(string) bool) (bool, error) {
	line, ok, err := firstLine(filepath.Join(p.root, rel))
	if err != nil {
		return false, err
	}
	return ok && test(line), nil
}

func (p fileProbe) isPython(rel string) (bool, error) {
	if strings.HasSuffix(rel, ".py") {
		return true, nil
	}
	return p.shebang(rel, isPythonShebang)
}

// isPythonTemplate is a .j2 whose shebang is Python's: the python gate compiles it rendered.
func (p fileProbe) isPythonTemplate(rel string) (bool, error) {
	if !strings.HasSuffix(rel, ".j2") {
		return false, nil
	}
	return p.shebang(rel, isPythonShebang)
}

func (p fileProbe) isShell(rel string) (bool, error) {
	if strings.HasSuffix(rel, ".sh") || strings.HasSuffix(rel, ".bash") {
		return true, nil
	}
	return p.shebang(rel, isShellShebang)
}
