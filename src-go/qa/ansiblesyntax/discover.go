package ansiblesyntax

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
)

// playbookMarker is a list item whose first key is `hosts:` (a play) or `import_playbook:` (an
// entrypoint that only composes other playbooks, which has no play of its own).
var playbookMarker = regexp.MustCompile(`(?m)^\s*-\s+(hosts|import_playbook):`)

// discover is the playbooks: the YAML files git would publish (the shared discovery) that carry
// the marker, as paths relative to the root, sorted. None, or a tracked playbook that is not
// among them, is an error: the gate would vouch for files nothing had parsed.
func (g *gate) discover() ([]string, error) {
	yamlFiles, err := g.list("yaml")
	if err != nil {
		return nil, err
	}
	var playbooks []string
	for _, rel := range yamlFiles {
		body, readErr := os.ReadFile(filepath.Join(g.root, rel))
		if readErr != nil {
			return nil, fmt.Errorf("cannot read %s: %w", rel, readErr)
		}
		if playbookMarker.Match(body) {
			playbooks = append(playbooks, rel)
		}
	}
	if len(playbooks) == 0 {
		return nil, fmt.Errorf("playbook discovery found 0 playbooks under %s: this repository is an Ansible project, "+
			"so the discovery is broken, and a pass here would vouch for playbooks nothing had parsed", g.root)
	}
	tracked, err := g.list("tracked-playbooks")
	if err != nil {
		return nil, err
	}
	var missed []string
	for _, rel := range tracked {
		if !slices.Contains(playbooks, rel) {
			missed = append(missed, "    "+rel)
		}
	}
	if len(missed) > 0 {
		return nil, fmt.Errorf("%d tracked YAML file(s) under playbooks/ are not in the population:\n%s\n"+
			"  Every file in that directory is a playbook this gate should parse. Either the playbook marker no longer "+
			"matches a legal playbook shape, or a non-playbook was added there and needs an explicit exemption in "+
			"src-go/qa/discovery (tracked-playbooks) with a reason", len(missed), strings.Join(missed, "\n"))
	}
	return playbooks, nil
}

// list runs one shared discovery list and returns its paths relative to the root.
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
