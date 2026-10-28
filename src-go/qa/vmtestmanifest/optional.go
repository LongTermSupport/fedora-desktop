package vmtestmanifest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// The directories the consumer menu offers optional plays from (src-go/internal/consumer).
var optionalDirs = []string{"playbooks/imports/optional/common", "playbooks/imports/optional/hardware-specific"}

// optionalPlayEntry is the part of one entry of a playbook file V17 reads: an import has no hosts.
type optionalPlayEntry struct {
	Hosts any `yaml:"hosts"`
	Vars  struct {
		Audience string `yaml:"audience"`
	} `yaml:"vars"`
}

// checkOptionalPlays is V17: every play a consumer scenario picks at the optional-app menu is
// one the menu offers, a play file in exactly one optional directory whose every play says
// vars.audience all. A picked play that cannot be read or parsed is an error.
func (g *gate) checkOptionalPlays(m shape) ([]string, error) {
	var findings []string
	for _, sc := range m.scenarios {
		for _, name := range sc.optionalPlays {
			finding, err := g.judgePick(sc.id, name)
			if err != nil {
				return nil, err
			}
			if finding != "" {
				findings = append(findings, finding)
			}
		}
	}
	return findings, nil
}

func (g *gate) judgePick(id, name string) (string, error) {
	var found []string
	for _, dir := range optionalDirs {
		rel := dir + "/" + name
		info, err := os.Stat(filepath.Join(g.root, filepath.FromSlash(rel)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return "", fmt.Errorf("cannot read %s: %w", rel, err)
		case !info.Mode().IsRegular():
			return "", fmt.Errorf("%s is not a regular file", rel)
		}
		found = append(found, rel)
	}
	switch len(found) {
	case 0:
		return fmt.Sprintf("%s: optional_plays names %s, which is not a play in %s or %s", id, name, optionalDirs[0], optionalDirs[1]), nil
	case 2:
		return fmt.Sprintf("%s: optional_plays names %s, which is in both %s and %s", id, name, optionalDirs[0], optionalDirs[1]), nil
	}
	data, err := g.readFile(found[0])
	if err != nil {
		return "", err
	}
	var entries []optionalPlayEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return "", fmt.Errorf("%s is not a playbook: %w", found[0], err)
	}
	plays := 0
	for _, e := range entries {
		if e.Hosts == nil {
			continue
		}
		plays++
		if e.Vars.Audience != "all" {
			plays = 0
			break
		}
	}
	if plays == 0 {
		return fmt.Sprintf("%s: optional_plays names %s, which is not for every audience (vars.audience all in each of its plays), so the consumer menu never offers it", id, found[0]), nil
	}
	return "", nil
}
