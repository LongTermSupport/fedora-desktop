package python

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
)

// sources is what the gate checks: the Python files and the Python templates, as paths relative
// to the root, sorted.
type sources struct {
	files     []string
	templates []string
}

// discover asks the shared discovery (SPEC-qa-discovery.md) for the Python files and the Python
// templates git would publish, outside the excluded trees. Which files those are is defined
// there and nowhere else; an empty list of files is its error (D13).
func (g *gate) discover() (sources, error) {
	var out sources
	var err error
	if out.files, err = g.list("python"); err != nil {
		return out, err
	}
	out.templates, err = g.list("python-templates")
	return out, err
}

// list runs one discovery list and returns its paths relative to the root.
func (g *gate) list(name string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	env := discovery.Env{Stdout: &stdout, Stderr: &stderr, Cwd: g.root, Runner: g.env.Runner}
	args := []string{flagRoot, g.root, flagScripts, g.scripts, name}
	if code := discovery.Main(args, env); code != discovery.ExitOK {
		return nil, fmt.Errorf("discovery of %s failed: %s", name, strings.TrimSpace(strings.TrimPrefix(stderr.String(), "ERROR: ")))
	}
	var rels []string
	for abs := range strings.SplitSeq(stdout.String(), "\x00") {
		if abs != "" {
			rels = append(rels, strings.TrimPrefix(abs, g.root+"/"))
		}
	}
	return rels, nil
}
