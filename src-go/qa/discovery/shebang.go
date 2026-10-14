package discovery

import (
	"path"
	"slices"
	"strings"
)

// shebang is the program a first line names as the script's interpreter, and the arguments after
// it. A line that is not a shebang names none.
type shebang struct {
	name string
	args []string
}

// parseShebang reads a first line; `env` and its options (-S, -i, NAME=value) are looked through.
func parseShebang(line string) shebang {
	rest, ok := strings.CutPrefix(line, "#!")
	if !ok {
		return shebang{}
	}
	fields := strings.Fields(rest)
	if len(fields) > 0 && path.Base(fields[0]) == "env" {
		fields = fields[1:]
		for len(fields) > 0 && (strings.HasPrefix(fields[0], "-") || strings.Contains(fields[0], "=")) {
			fields = fields[1:]
		}
	}
	if len(fields) == 0 {
		return shebang{}
	}
	return shebang{name: path.Base(fields[0]), args: fields[1:]}
}

// shellShebang is a first line that runs bash or sh as a script interpreter, without -c (an
// inline one-liner in another kind of file, such as a playbook).
func shellShebang(line string) bool {
	s := parseShebang(line)
	return (s.name == "bash" || s.name == "sh") && !slices.Contains(s.args, "-c")
}

// pythonShebang is a first line whose interpreter is python, python3, python3.12 and the like.
func pythonShebang(line string) bool {
	return strings.HasPrefix(parseShebang(line).name, "python")
}
