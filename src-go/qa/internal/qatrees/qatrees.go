// Package qatrees reads scripts/qa-trees.tsv, the one table of the trees no QA gate opens: the
// daemon-deployed files (the owner's decision, CLAUDE/QA.md "Daemon-deployed files are never
// gated here"), vendored and generated trees, and the semgrep fixtures. A gate that chooses its
// own files reads the table through this package instead of carrying a second list, which would
// drift from it (issue #217).
package qatrees

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// File is the table, relative to the repository root.
const File = "scripts/qa-trees.tsv"

// headBytes is how much of a file is read to see its shebang.
const headBytes = 256

// Table is the two lists: Shell serves shell, YAML, JavaScript and Markdown, Python serves
// Python.
type Table struct {
	Shell, Python []string
}

// Load reads the table from the repository tree fsys.
func Load(fsys fs.FS) (Table, error) {
	data, err := fs.ReadFile(fsys, File)
	if err != nil {
		return Table{}, fmt.Errorf("cannot read the excluded trees: %w", err)
	}
	return Parse(bytes.NewReader(data), File)
}

// Parse reads `<list><TAB><dir>` rows; comments and blank lines are skipped. An unknown list, a
// malformed row or a list with no row is an error, because an empty list would open trees that
// are not ours.
func Parse(r io.Reader, name string) (Table, error) {
	var t Table
	scanner := bufio.NewScanner(r)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
			return Table{}, fmt.Errorf("%s:%d: expected '<list><TAB><dir>', got %q", name, n, line)
		}
		switch fields[0] {
		case "shell":
			t.Shell = append(t.Shell, fields[1])
		case "python":
			t.Python = append(t.Python, fields[1])
		default:
			return Table{}, fmt.Errorf("%s:%d: unknown list '%s' (want shell or python)", name, n, fields[0])
		}
	}
	if err := scanner.Err(); err != nil {
		return Table{}, fmt.Errorf("%s: %w", name, err)
	}
	if len(t.Shell) == 0 || len(t.Python) == 0 {
		return Table{}, fmt.Errorf("%s needs a 'shell' row and a 'python' row: with no excluded trees a gate would read trees that are not ours", name)
	}
	return t, nil
}

// ExcludesFile is whether the table keeps the file at rel out of every gate. A gate that reads
// files of any language judges each by the list of its language, as the discovery gates do:
// Python (a .py name or a python shebang) by the Python list, anything else by the shell list.
// The file is read only when the two lists disagree about it.
func (t Table) ExcludesFile(fsys fs.FS, rel string) (bool, error) {
	shell, python := excluded(rel, t.Shell), excluded(rel, t.Python)
	if shell == python {
		return shell, nil
	}
	isPy, err := isPython(fsys, rel)
	if err != nil {
		return false, err
	}
	if isPy {
		return python, nil
	}
	return shell, nil
}

// excluded is whether the path lies inside an excluded tree: /dir/ occurs as whole segments
// anywhere in /path/.
func excluded(rel string, dirs []string) bool {
	within := "/" + rel + "/"
	return slices.ContainsFunc(dirs, func(dir string) bool { return strings.Contains(within, "/"+dir+"/") })
}

// isPython is a .py name or a first line that runs python.
func isPython(fsys fs.FS, rel string) (bool, error) {
	if path.Ext(rel) == ".py" {
		return true, nil
	}
	f, err := fsys.Open(rel)
	if err != nil {
		return false, fmt.Errorf("cannot tell the language of %s: %w", rel, err)
	}
	head, err := io.ReadAll(io.LimitReader(f, headBytes))
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return false, fmt.Errorf("cannot tell the language of %s: %w", rel, err)
	}
	first, _, found := strings.Cut(string(head), "\n")
	if !found {
		first = string(head)
	}
	rest, ok := strings.CutPrefix(first, "#!")
	if !ok {
		return false, nil
	}
	fields := strings.Fields(rest)
	if len(fields) > 0 && path.Base(fields[0]) == "env" {
		fields = fields[1:]
	}
	return len(fields) > 0 && strings.HasPrefix(path.Base(fields[0]), "python"), nil
}
