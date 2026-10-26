package consumer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The optional plays' directories, from the checkout's root. A hardware play is labelled so.
const (
	commonDir   = "playbooks/imports/optional/common"
	hardwareDir = "playbooks/imports/optional/hardware-specific"
)

// play is one optional play offered on the menu.
type play struct {
	label, path string
}

// playEntry is the part of one entry of a playbook file this reads: an import has no hosts.
type playEntry struct {
	Hosts any `yaml:"hosts"`
	Vars  struct {
		Audience string `yaml:"audience"`
	} `yaml:"vars"`
}

// optionalPlays is every optional play for everyone (vars.audience all in each of its plays,
// the key the QA gate requires, CLAUDE/AnsibleStyle.md "Install Audience"), in directory
// order then file order. A play that says no audience, or a file that is not YAML, is refused.
func optionalPlays(root string) ([]play, error) {
	var plays []play
	for _, dir := range []string{commonDir, hardwareDir} {
		files, err := filepath.Glob(filepath.Join(root, dir, "*.yml"))
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", dir, err)
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("no optional plays in %s", dir)
		}
		slices.Sort(files)
		for _, file := range files {
			forAll, err := forEveryone(file)
			if err != nil {
				return nil, err
			}
			if forAll {
				plays = append(plays, play{label: label(file, dir == hardwareDir), path: filepath.Join(dir, filepath.Base(file))})
			}
		}
	}
	return plays, nil
}

// forEveryone says whether every play in the file is for every audience.
func forEveryone(file string) (bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", file, err)
	}
	var entries []playEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return false, fmt.Errorf("%s is not a playbook: %w", file, err)
	}
	all := false
	for _, e := range entries {
		if e.Hosts == nil {
			continue
		}
		switch e.Vars.Audience {
		case "all":
			all = true
		case "developer":
			return false, nil
		default:
			return false, fmt.Errorf("%s: a play declares no vars.audience (all or developer)", file)
		}
	}
	return all, nil
}

// label is a play's file name as words: play-hd-audio.yml is "Hd Audio".
func label(file string, hardware bool) string {
	name := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(file), ".yml"), "play-")
	words := strings.Fields(strings.ReplaceAll(name, "-", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	out := strings.Join(words, " ")
	if hardware {
		out += " (hardware)"
	}
	return out
}

// errPick is an answer that is not a list of the menu's numbers.
var errPick = errors.New("not a menu number")

// parsePicks reads a line of menu numbers, 1 to n, separated by spaces or commas, into
// indexes in menu order with no repeats; an empty line is no pick.
func parsePicks(line string, n int) ([]int, error) {
	var picks []int
	for field := range strings.FieldsFuncSeq(line, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' }) {
		k, err := strconv.Atoi(field)
		if err != nil || k < 1 || k > n {
			return nil, fmt.Errorf("%w: %q. Choose numbers from 1 to %d, or press Enter for none", errPick, field, n)
		}
		if !slices.Contains(picks, k-1) {
			picks = append(picks, k-1)
		}
	}
	slices.Sort(picks)
	return picks, nil
}
