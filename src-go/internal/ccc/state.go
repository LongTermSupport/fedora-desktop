package ccc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

// claudeStateFile is Claude Code's account and project state, which it keeps inside its
// config directory when CLAUDE_CONFIG_DIR is set.
const claudeStateFile = ".claude.json"

// prepareState makes the agent's state directory private and records in Claude Code's state
// what ccy's entrypoint records in its own: onboarding done, the permission-prompt bypass
// accepted and /workspace trusted, so a person meets none of the first-run questions. Every
// other key Claude Code wrote is kept.
func prepareState(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("make %s: %w", dir, err)
	}
	path := filepath.Join(dir, claudeStateFile)
	state := map[string]any{}
	data, err := os.ReadFile(filepath.Clean(path))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read %s: %w", path, err)
	default:
		if jerr := json.Unmarshal(data, &state); jerr != nil {
			return fmt.Errorf("%s is not JSON: %w", path, jerr)
		}
	}
	state["hasCompletedOnboarding"] = true
	state["bypassPermissionsModeAccepted"] = true
	projects, ok := state["projects"].(map[string]any)
	if state["projects"] == nil {
		projects, ok = map[string]any{}, true
	}
	if !ok {
		return fmt.Errorf("%s: projects is not an object", path)
	}
	project, ok := projects[containerWorkspace].(map[string]any)
	if !ok {
		project = map[string]any{}
	}
	project["hasTrustDialogAccepted"] = true
	projects[containerWorkspace] = project
	state["projects"] = projects
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return fsx.WriteAtomic(path, claudeStateFile, append(out, '\n'), 0o600)
}
