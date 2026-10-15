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

// playKey matches a mapping key that makes a list item a play (`hosts:`) or an entrypoint that
// only composes other playbooks and has no play of its own (`import_playbook:`).
var playKey = regexp.MustCompile(`^(hosts|import_playbook):`)

// listItem matches a line that opens a list item: its indent and what follows the dash.
var listItem = regexp.MustCompile(`^( *)-(?: +(.*))?$`)

// scan is the position of isPlaybook in the text: the indent of the top-level list items (-1
// until the first content line shows it) and of the keys of the item being read (-1 before its
// first key).
type scan struct{ top, itemKeys int }

// isContent is whether a line says anything: not blank, not a comment, not a document marker.
func isContent(content string) bool {
	return content != "" && !strings.HasPrefix(content, "#") && content != "---" && !strings.HasPrefix(content, "--- ")
}

// isPlaybook is whether the text is a YAML list at the top level one of whose items has `hosts:`
// or `import_playbook:` as a key of its own, whichever key comes first. Anything nested deeper
// (a loop list, a block scalar, a play-level variable) is not a play.
func isPlaybook(body []byte) bool {
	sc := scan{top: -1, itemKeys: -1}
	for line := range strings.SplitSeq(string(body), "\n") {
		content := strings.TrimLeft(line, " ")
		if !isContent(content) {
			continue
		}
		switch sc.step(line, len(line)-len(content), content) {
		case isPlay:
			return true
		case notPlaybook:
			return false
		case undecided:
		}
	}
	return false
}

// what a line settles: nothing yet, that the file is a playbook, or that it is not.
type finding int

const (
	undecided finding = iota
	isPlay
	notPlaybook
)

// step reads one content line.
func (sc *scan) step(line string, indent int, content string) finding {
	item := listItem.FindStringSubmatch(line)
	if sc.top < 0 {
		if item == nil {
			return notPlaybook
		}
		sc.top = indent
	}
	switch {
	case indent < sc.top:
		return notPlaybook
	case indent == sc.top:
		sc.itemKeys = -1
		if item != nil && item[2] != "" {
			sc.itemKeys = len(line) - len(item[2])
			if playKey.MatchString(item[2]) {
				return isPlay
			}
		}
	default:
		if sc.itemKeys < 0 {
			sc.itemKeys = indent
		}
		if indent == sc.itemKeys && playKey.MatchString(content) {
			return isPlay
		}
	}
	return undecided
}

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
		if isPlaybook(body) {
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
