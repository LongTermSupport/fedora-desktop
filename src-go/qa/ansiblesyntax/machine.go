package ansiblesyntax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// passwordFileSuffix ends the name of every setting that points the engine at a password file.
const passwordFileSuffix = "_PASSWORD_FILE"

// requireMachine checks what the machine must supply before any playbook is judged, so a
// missing prerequisite is one clear exit 2 and never a report of every playbook failing alike.
func (g *gate) requireMachine(ctx context.Context) error {
	res, err := g.runTool(ctx, probeTimeout, "ansible-playbook", "--version")
	if err != nil || res.Code != 0 {
		return errors.New("ansible-playbook not installed or not working (run.bash installs it with pipx)")
	}
	config := filepath.Join(g.root, "ansible.cfg")
	if info, err := os.Stat(config); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s is missing: it is pinned as the engine's config, and without it the playbooks' root_dir lookup fails at parse time", config)
	}
	return g.requirePasswordFiles(ctx)
}

// configSetting is one entry of `ansible-config dump`; the trailing entry that names no setting
// decodes to the zero value.
type configSetting struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// requirePasswordFiles asks the engine which password files it will look for (its own
// resolution of the environment and the config, relative paths included) and requires each to
// exist as a file. Existence only: the file is never opened.
func (g *gate) requirePasswordFiles(ctx context.Context) error {
	res, err := g.runTool(ctx, probeTimeout, "ansible-config", "dump", "--only-changed", "--format", "json")
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("ansible-config dump exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	var settings []configSetting
	if err := json.Unmarshal([]byte(res.Stdout), &settings); err != nil {
		return fmt.Errorf("ansible-config dump did not print JSON: %w", err)
	}
	for _, s := range settings {
		path, isText := s.Value.(string)
		if !strings.HasSuffix(s.Name, passwordFileSuffix) || !isText || path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("the password file %s (%s) does not exist, is not a file, or is empty: "+
				"supply it (a clean checkout and a bare git worktree add lack it). "+
				"Only its existence and a non-empty body are needed: this gate never reads it and --syntax-check does not decrypt; "+
				"without it every playbook would fail with the same message", path, s.Name)
		}
	}
	return nil
}
