package bashlimits

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// jinjaExt marks a template.
const jinjaExt = ".j2"

// headBytes is how much of a file is read to see its shebang; jinjaHeaderLines is how many
// lines of a template may precede it.
const (
	headBytes        = 1024
	jinjaHeaderLines = 8
)

// IsCandidate reports whether p is a bash file the gate looks at, decided by what the file
// is and not by what it is called: a *.bash or *.sh file whatever it holds, a *.bash.j2 or
// *.sh.j2 template, any file whose first line is a bash or sh shebang (a dot in the name, or
// none, changes nothing), or a .j2 template with such a shebang after only blank lines and
// Jinja comments, tags and expressions. A shebang that runs bash with -c is an inline
// one-liner in another kind of file (the playbooks'), not a bash file.
func IsCandidate(fsys fs.FS, p string) (bool, error) {
	base := path.Base(p)
	ext := path.Ext(base)
	if ext == ".bash" || ext == ".sh" {
		return true, nil
	}
	if ext == jinjaExt {
		if inner := path.Ext(strings.TrimSuffix(base, jinjaExt)); inner == ".bash" || inner == ".sh" {
			return true, nil
		}
	}
	head, err := readHead(fsys, p)
	if err != nil {
		return false, err
	}
	lines := strings.Split(head, "\n")
	if ext != ".j2" {
		return bashShebang(lines[0]), nil
	}
	for _, line := range lines[:min(len(lines), jinjaHeaderLines)] {
		switch trimmed := strings.TrimSpace(line); {
		case strings.HasPrefix(trimmed, "#!"):
			return bashShebang(trimmed), nil
		case trimmed == "" || isJinjaOnly(trimmed):
		default:
			return false, nil
		}
	}
	return false, nil
}

// isJinjaOnly is a line made of nothing but one Jinja comment, tag or expression.
func isJinjaOnly(line string) bool {
	for _, pair := range [][2]string{{"{#", "#}"}, {"{%", "%}"}, {"{{", "}}"}} {
		if strings.HasPrefix(line, pair[0]) && strings.HasSuffix(line, pair[1]) {
			return true
		}
	}
	return false
}

// bashShebang is a first line that runs bash or sh as a script interpreter, directly or
// through env.
func bashShebang(line string) bool {
	rest, ok := strings.CutPrefix(line, "#!")
	if !ok {
		return false
	}
	fields := strings.Fields(rest)
	if len(fields) > 0 && path.Base(fields[0]) == "env" {
		fields = fields[1:]
		for len(fields) > 0 && (strings.HasPrefix(fields[0], "-") || strings.Contains(fields[0], "=")) {
			fields = fields[1:]
		}
	}
	if len(fields) == 0 {
		return false
	}
	if interp := path.Base(fields[0]); interp != "bash" && interp != "sh" {
		return false
	}
	return !slices.Contains(fields[1:], "-c")
}

func readHead(fsys fs.FS, p string) (string, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", p, err)
	}
	head, err := io.ReadAll(io.LimitReader(f, headBytes))
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p, err)
	}
	return string(head), nil
}
