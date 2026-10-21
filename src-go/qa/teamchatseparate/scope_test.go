package teamchatseparate

import (
	"slices"
	"strings"
	"testing"
)

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
		// Most plays live in a category under optional/, as v1's Element play does.
		"playbooks/imports/optional/common/play-teamchat-element.yml",
		"playbooks/imports/sub/play-teamchat.yml",
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
		"playbooks/imports/optional/common/play-agent-bus-element.yml",
		"playbooks/imports/play-teamchat.yaml",
		"playbooks/imports/optional/play-teamchat/x.yml",
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

// A play or task file names the files it includes or installs by a path from the repository
// root, often after ../ or a {{ }} directory; a path inside a longer one, or prose, is no
// reference.
func TestReferencesAreTheTasksAndFilesPathsALineNames(t *testing.T) {
	for _, c := range []struct {
		line string
		want []string
	}{
		{`- ansible.builtin.import_tasks: "{{ playbook_dir }}/../../tasks/build-go-command.yml"`, []string{"tasks/build-go-command.yml"}},
		{`    src: "{{ root_dir }}/files/etc/systemd/system/teamchat-server@.service"`, []string{"files/etc/systemd/system/teamchat-server@.service"}},
		{`  include_tasks: tasks/render-units.yml`, []string{"tasks/render-units.yml"}},
		{`  include_tasks: ./tasks/a.yml # then tasks/b.yml.`, []string{"tasks/a.yml", "tasks/b.yml"}},
		{`  include_tasks: "tasks/{{ step }}.yml"`, []string{"tasks/{{"}},
		{`  with_items: [files/a, 'files/b']`, []string{"files/a", "files/b"}},
		{`    dest: /usr/local/share/files/x`, nil},
		{`# the tasks/ directory and mytasks/x.yml`, nil},
		{`- name: Build`, nil},
	} {
		if got := References(c.line); !slices.Equal(got, c.want) {
			t.Errorf("References(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// A file outside the scope that a v2 play may name is listed with its reason, and is out of
// scope (else it would not need listing).
func TestSharedFilesAreOutOfScopeWithAReason(t *testing.T) {
	if len(SharedFiles) == 0 {
		t.Fatal("no shared file is listed")
	}
	for p, reason := range SharedFiles {
		if InScope(p) || strings.TrimSpace(reason) == "" {
			t.Errorf("%s: in scope %v, reason %q", p, InScope(p), reason)
		}
	}
}

// Every scoped path lies under a path the git listing is asked for.
func TestScanPathsCoverTheScope(t *testing.T) {
	for _, p := range []string{
		"src-go/internal/teamchat/a.go", "src-go/cmd/teamchat/main.go", "files/teamchat/x",
		"playbooks/imports/play-teamchat.yml", "playbooks/imports/optional/common/play-teamchat-element.yml",
		"tasks/teamchat.yml",
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
