package sttrules

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
)

// headBytes is how much of a named file is read to see its shebang.
const headBytes = 1024

// fileSets is the files to judge, by language, as repository-relative paths.
type fileSets map[language][]string

// discoveryList is the list of qa/discovery that holds each language's files.
var discoveryList = map[language]string{langJS: "js", langPy: "python"}

// excluded is a file the default scope leaves out: the rules' own fixtures, vendored trees and
// anything under a node_modules directory.
func excluded(rel string) bool {
	for _, tree := range []string{".semgrep/", ".claude/hooks-daemon/", ".claude/skills/"} {
		if strings.HasPrefix(rel, tree) {
			return true
		}
	}
	return slices.Contains(strings.Split(rel, "/"), "node_modules")
}

// files is the files to judge: those named, else every JavaScript and Python file git would
// publish.
func (g *gate) files(paths []string) (fileSets, error) {
	if len(paths) > 0 {
		return g.named(paths)
	}
	sets := fileSets{}
	for _, lang := range languages {
		all, err := g.discover(discoveryList[lang])
		if err != nil {
			return nil, err
		}
		sets[lang] = slices.DeleteFunc(all, excluded)
		if len(sets[lang]) == 0 {
			return nil, fmt.Errorf("no %s files were found; nothing was checked", nounOf[lang])
		}
	}
	return sets, nil
}

// discover asks the shared discovery for one list and returns it as repository-relative paths.
func (g *gate) discover(list string) ([]string, error) {
	var out, errs bytes.Buffer
	env := discovery.Env{Stdout: &out, Stderr: &errs, Cwd: g.root, Runner: g.env.Runner}
	args := []string{flagRoot, g.root, flagScripts, g.scripts, list}
	if code := discovery.Main(args, env); code != discovery.ExitOK {
		return nil, errors.New(strings.TrimPrefix(strings.TrimSpace(errs.String()), "ERROR: "))
	}
	var files []string
	for abs := range strings.SplitSeq(out.String(), "\x00") {
		if abs == "" {
			continue
		}
		rel, err := filepath.Rel(g.root, abs)
		if err != nil {
			return nil, fmt.Errorf("discovery named %s outside the repository root", abs)
		}
		files = append(files, filepath.ToSlash(rel))
	}
	return files, nil
}

// named sorts the named files by language. A named file is judged wherever it lives.
func (g *gate) named(paths []string) (fileSets, error) {
	sets := fileSets{}
	seen := map[string]bool{}
	for _, p := range paths {
		rel, err := g.relative(p)
		if err != nil {
			return nil, err
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		lang, err := g.languageOf(rel)
		if err != nil {
			return nil, err
		}
		sets[lang] = append(sets[lang], rel)
	}
	return sets, nil
}

// relative is a named file as the repository names it: relative to the root. An absolute path is
// resolved through the symlinks of its directory first (the root is symlink-free, the argument
// may reach it through a link); one that is outside the root, or climbs out of it, is refused
// rather than judged under another name.
func (g *gate) relative(p string) (string, error) {
	rel := p
	if filepath.IsAbs(p) {
		dir, err := filepath.EvalSymlinks(filepath.Dir(p))
		if err != nil {
			return "", fmt.Errorf("cannot resolve %s: %s", p, reason(err))
		}
		if rel, err = filepath.Rel(g.root, filepath.Join(dir, filepath.Base(p))); err != nil {
			return "", fmt.Errorf("%s is outside the repository root", p)
		}
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s is outside the repository root", p)
	}
	return rel, nil
}

// languageOf is the language of a named file: by its name, else by a shebang naming python.
func (g *gate) languageOf(rel string) (language, error) {
	info, err := os.Stat(g.path(rel))
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %s", rel, reason(err))
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", rel)
	}
	switch filepath.Ext(rel) {
	case ".js", ".mjs":
		return langJS, nil
	case ".py":
		return langPy, nil
	}
	first, err := g.firstLine(rel)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(first, "#!") && strings.Contains(first, "python") {
		return langPy, nil
	}
	return "", fmt.Errorf("%s is neither JavaScript nor Python", rel)
}

// firstLine is the first line of a file, read from its first bytes.
func (g *gate) firstLine(rel string) (string, error) {
	file, err := os.Open(g.path(rel))
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %s", rel, reason(err))
	}
	buf := make([]byte, headBytes)
	n, err := file.Read(buf)
	closeErr := file.Close()
	switch {
	case err != nil && !errors.Is(err, io.EOF):
		return "", fmt.Errorf("cannot read %s: %s", rel, reason(err))
	case closeErr != nil:
		return "", fmt.Errorf("cannot read %s: %s", rel, reason(closeErr))
	}
	line := buf[:n]
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return string(line), nil
}
