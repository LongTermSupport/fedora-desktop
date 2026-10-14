package discovery

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// trees are the directory names the gates must not gate on, one list per language.
type trees struct {
	shell, python []string
}

// loadTrees reads scripts/qa-trees.tsv. Every row is `<list><TAB><dir>`; both lists must have a
// row, because an empty list would gate trees that are not ours.
func loadTrees(file string) (trees, error) {
	f, err := os.Open(file)
	if err != nil {
		return trees{}, fmt.Errorf("cannot read the excluded trees: %w", err)
	}
	t, err := parseTrees(f, file)
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("%s: %w", file, closeErr)
	}
	return t, err
}

func parseTrees(r io.Reader, name string) (trees, error) {
	var t trees
	scanner := bufio.NewScanner(r)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
			return trees{}, fmt.Errorf("%s:%d: expected '<list><TAB><dir>', got %q", name, n, line)
		}
		switch fields[0] {
		case "shell":
			t.shell = append(t.shell, fields[1])
		case "python":
			t.python = append(t.python, fields[1])
		default:
			return trees{}, fmt.Errorf("%s:%d: unknown list '%s' (want shell or python)", name, n, fields[0])
		}
	}
	if err := scanner.Err(); err != nil {
		return trees{}, fmt.Errorf("%s: %w", name, err)
	}
	if len(t.shell) == 0 || len(t.python) == 0 {
		return trees{}, fmt.Errorf("%s needs a 'shell' row and a 'python' row: with no excluded trees a gate would read trees that are not ours", name)
	}
	return t, nil
}

// excluded is whether the path lies inside an excluded tree: /dir/ occurs as whole segments
// anywhere in /path/.
func excluded(rel string, dirs []string) bool {
	within := "/" + rel + "/"
	return slices.ContainsFunc(dirs, func(dir string) bool { return strings.Contains(within, "/"+dir+"/") })
}
