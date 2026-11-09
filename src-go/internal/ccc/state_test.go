package ccc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readState(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(filepath.Join(dir, claudeStateFile)))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestStateIsMadePrivateAndTrustsTheWorkspace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := prepareState(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("%v %v", info, err)
	}
	got := readState(t, dir)
	if got["hasCompletedOnboarding"] != true || got["bypassPermissionsModeAccepted"] != true {
		t.Fatalf("got %v", got)
	}
	project, ok := got["projects"].(map[string]any)[containerWorkspace].(map[string]any)
	if !ok || project["hasTrustDialogAccepted"] != true {
		t.Fatalf("projects %v", got["projects"])
	}
}

func TestStateKeepsWhatClaudeCodeWrote(t *testing.T) {
	dir := t.TempDir()
	old := `{"theme":"dark","projects":{"/workspace":{"history":[1]},"/other":{}}}`
	if err := os.WriteFile(filepath.Join(dir, claudeStateFile), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareState(dir); err != nil {
		t.Fatal(err)
	}
	got := readState(t, dir)
	projects, ok := got["projects"].(map[string]any)
	if !ok {
		t.Fatalf("projects %v", got["projects"])
	}
	workspace, ok := projects[containerWorkspace].(map[string]any)
	if !ok || got["theme"] != "dark" || projects["/other"] == nil || workspace["history"] == nil {
		t.Fatalf("got %v", got)
	}
}

func TestStateThatIsNotJSONIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, claudeStateFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareState(dir); err == nil {
		t.Fatal("want an error")
	}
	if err := os.WriteFile(filepath.Join(dir, claudeStateFile), []byte(`{"projects":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareState(dir); err == nil {
		t.Fatal("want an error for projects that is not an object")
	}
}
