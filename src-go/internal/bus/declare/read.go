package declare

import (
	"errors"
	"io/fs"
	"strings"
	"unicode/utf8"
)

// Read is declare.py's _read, pathlib.Path(path).read_text(encoding="utf-8") (SPEC-view.md
// V1): the path as pathlib writes it (repeated slashes and "." parts dropped, "" being "."),
// read with readFile (os.ReadFile, or a fake); a missing file refused, naming it; text that
// is not UTF-8 or a file that cannot be read the crash class v1's traceback was; "\r\n" and
// "\r" read as "\n" (universal newlines); a byte order mark kept as a character.
func Read(path string, readFile func(string) ([]byte, error)) (string, error) {
	shown := pyPath(path)
	data, err := readFile(shown)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", refusedf("%s does not exist (copy localhost.yml.dist to make it)", shown)
		}
		return "", crashf("%s cannot be read: %v", shown, err)
	}
	if !utf8.Valid(data) {
		return "", crashf("%s is not UTF-8 text", shown)
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n"), nil
}

// pyPath is str(pathlib.PurePosixPath(p)).
func pyPath(p string) string {
	root := ""
	switch {
	case strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///"):
		root = "//"
	case strings.HasPrefix(p, "/"):
		root = "/"
	}
	var parts []string
	for part := range strings.SplitSeq(p, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	if out := root + strings.Join(parts, "/"); out != "" {
		return out
	}
	return "."
}
