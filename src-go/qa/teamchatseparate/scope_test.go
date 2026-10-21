package teamchatseparate

import "testing"

func TestScopeIsWhatV2Owns(t *testing.T) {
	in := []string{
		"src-go/internal/teamchat/paths/paths.go",
		"src-go/internal/teamchat/proto/testdata/corpus.json",
		"src-go/internal/teamchat/migrate/fence.go",
		"src-go/cmd/teamchat/main.go",
		"src-go/cmd/teamchat-admin/main.go",
		"files/opt/claude-yolo/optional/teamchat/settings.json",
		"files/opt/claude-yolo/optional/teamchat/plugin/teamchat/skills/teamchat/SKILL.md",
		"files/etc/systemd/system/teamchat-server@.service",
		"files/usr/local/share/teamchat/resolver.conf",
		"playbooks/imports/play-teamchat.yml",
		"playbooks/imports/play-teamchat-element.yml",
		"tasks/teamchat-install.yml",
		"tasks/teamchat/units.yml",
	}
	for _, p := range in {
		if !InScope(p) {
			t.Errorf("%s is v2's but out of scope", p)
		}
	}
	out := []string{
		// The named homes of deliberate v1 references (D 4.1).
		"src-go/internal/teamchat/v1names/v1names.go",
		"src-go/internal/teamchat/migrate/v1/adapter.go",
		"src-go/internal/teamchat/migrate/v1/adapter_test.go",
		// Shared code and v1's own.
		"src-go/internal/teams/board/board.go",
		"src-go/internal/teamchatx/x.go",
		"src-go/cmd/ccy-teams/main.go",
		"src-go/qa/teamchatseparate/v1-names.txt",
		"files/var/local/claude-yolo/claude-yolo",
		"files/opt/claude-yolo/optional/agent-bus/settings.json",
		"playbooks/imports/play-agent-bus.yml",
		"playbooks/imports/play-teamchat.yaml",
		"playbooks/imports/sub/play-teamchat.yml",
		"playbooks/play-teamchat.yml",
		"tasks/build-go-command.yml",
		"docs/teamchat.md",
		"scripts/test-teamchat-install.bash",
		"untracked/files/teamchat/x",
	}
	for _, p := range out {
		if InScope(p) {
			t.Errorf("%s is in scope; it must not be", p)
		}
	}
}

// Every scoped path lies under a path the git listing is asked for.
func TestScanPathsCoverTheScope(t *testing.T) {
	for _, p := range []string{
		"src-go/internal/teamchat/a.go", "src-go/cmd/teamchat/main.go", "files/teamchat/x",
		"playbooks/imports/play-teamchat.yml", "tasks/teamchat.yml",
	} {
		covered := false
		for _, root := range ScanPaths() {
			covered = covered || len(p) > len(root) && p[:len(root)+1] == root+"/"
		}
		if !covered {
			t.Errorf("%s is under no scan path of %q", p, ScanPaths())
		}
	}
}
