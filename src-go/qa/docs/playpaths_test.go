package docs

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

const missingPlay = "names a playbook the repository does not have"

func TestPlayPathsAPathToNoTrackedPlaybookIsAFindingInEverySource(t *testing.T) {
	w := newWorld(t, map[string]string{
		"run.bash":                     "echo ./playbooks/imports/optional/common/play-gone.yml\n",
		"scripts/tool.bash":            "#!/usr/bin/bash\n\necho playbooks/imports/play-a.yml playbooks/imports/play-b.yml\n",
		"files/home/.local/bin/helper": "#!/usr/bin/bash\n# ~/Projects/fedora-desktop/playbooks/imports/play-c.yml\n",
		"docs/README.md":               "# Docs\n\nRun `./playbooks/imports/play-a.yml` or `playbooks/imports/play-d.yml`.\n",
	})
	w.wantFindings(
		"docs/README.md:3  playbooks/imports/play-d.yml  — "+missingPlay,
		"files/home/.local/bin/helper:2  playbooks/imports/play-c.yml  — "+missingPlay,
		"run.bash:1  playbooks/imports/optional/common/play-gone.yml  — "+missingPlay,
		"scripts/tool.bash:3  playbooks/imports/play-b.yml  — "+missingPlay,
	)
}

func TestPlayPathsAPatternAnElisionAndAPlaceholderAreNoPath(t *testing.T) {
	w := newWorld(t, map[string]string{
		"run.bash": "ls playbooks/imports/optional/experimental/play-*.yml\n" +
			"run playbooks/imports/$name.yml playbooks/{{ x }}.yml playbooks/imports/play-<name>.yml\n" +
			"usage --run-play <playbooks/...yml>\n",
	})
	w.wantFindings()
}

func TestPlayPathsTestSuitesFixturesAndOtherTreesAreNotRead(t *testing.T) {
	w := newWorld(t, map[string]string{
		"scripts/test-thing.bash":          "playbooks/imports/fixture.yml\n",
		"scripts/test_thing.py":            "playbooks/imports/fixture.yml\n",
		"files/x/testdata/case.txt":        "playbooks/imports/fixture.yml\n",
		"playbooks/imports/play-other.yml": "# playbooks/imports/elsewhere.yml\n",
		"CLAUDE/Plan/00001-old/PLAN.md":    "playbooks/imports/history.yml\n",
		"files/home/.local/share/blob.bin": "\x00playbooks/imports/binary.yml\n",
		"files/home/.local/bin/clean-tool": "#!/usr/bin/bash\n./playbooks/imports/../imports/play-a.yml\n",
	})
	w.wantFindings()
}

func TestPlayPathsAFileOnDiskButNotTrackedIsNoPlaybook(t *testing.T) {
	w := newWorld(t, map[string]string{"run.bash": "playbooks/imports/play-new.yml\n"})
	w.ignored("playbooks/imports/play-new.yml", "- hosts: all\n")
	w.wantFindings("run.bash:1  playbooks/imports/play-new.yml  — " + missingPlay)
}

func TestPlayPathsATrackedSymlinkIsNotReadAndAMissingSourceIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.ignored("vendored/tool.bash", "playbooks/imports/elsewhere.yml\n")
	if err := os.Symlink("../vendored", filepath.Join(w.root, "scripts", "vault")); err != nil {
		t.Fatal(err)
	}
	w.run.tracked = append(w.run.tracked, "scripts/vault")
	w.wantFindings()
	w.run.tracked = append(w.run.tracked, "scripts/gone.bash")
	wantNamed(t, w.check(), "scripts/gone.bash")
}

func TestPlayPathsAnUnreadableSourceIsExitTwoNamingIt(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/tool.bash": "#!/usr/bin/bash\n"})
	w.readFail["tool.bash"] = fs.ErrPermission
	wantNamed(t, w.check(), "scripts/tool.bash")
}
