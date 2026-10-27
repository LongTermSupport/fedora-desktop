package changed

import (
	"os"
	"reflect"
	"slices"
	"testing"
)

const (
	pyGlob   = "helpers/self_update/" + "*.py"
	jsonGlob = "helpers/self_update/" + "*.json"
	cmdGlob  = "src-go/cmd/" + "*"
)

// The case that shipped: the self-update suite copies helpers/self_update/*.py and *.json into
// its fixture and names neither affected_plays.py nor its JSON, so a change to them chose no
// suite and qa-all found 125 failures qa-changed never ran.
func TestAChangeAGlobOfTheSuiteMatchesSelectsTheSelfUpdateSuite(t *testing.T) {
	w := suiteWorld(t)
	writeFile(t, w.root, "scripts/test-alpha.bash", readReal(t, "test-self-update-cycle.bash"))
	w.touch("helpers/self_update/affected_plays.py", "# x")
	w.touch("helpers/self_update/not-play-inputs.json", "{}")
	p, err := w.selectSuites([]string{"helpers/self_update/affected_plays.py", "helpers/self_update/not-play-inputs.json"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"reaches helpers/self_update/affected_plays.py through " + pyGlob,
		"reaches helpers/self_update/not-play-inputs.json through " + jsonGlob,
	}
	if len(p.suites) != 1 || p.suites[0].name != "scripts/test-alpha.bash" || !reflect.DeepEqual(p.suites[0].why, want) {
		t.Fatalf("got %+v", p.suites)
	}
	if !p.covered["helpers/self_update/affected_plays.py"] || !p.covered["helpers/self_update/not-play-inputs.json"] {
		t.Fatalf("a reached file is covered: %v", p.covered)
	}
}

// A file the glob does not match (another directory, a deeper one, another extension) is not
// reached: a glob segment does not cross a slash.
func TestAGlobSelectsOnlyWhatItMatches(t *testing.T) {
	w := suiteWorld(t)
	writeFile(t, w.root, "scripts/test-alpha.bash", "#!/usr/bin/bash\ncp \"$REPO_ROOT\"/"+pyGlob+" \"$WORK/\"\n")
	for _, rel := range []string{"helpers/play_lock/lock.py", "helpers/self_update/sub/deep.py", "helpers/self_update/data.yml"} {
		w.touch(rel, "# x")
		p, err := w.selectSuites([]string{rel})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.suites) != 0 {
			t.Errorf("%s is not matched by %s: %+v", rel, pyGlob, p.suites)
		}
	}
}

// The fixture holds every way a suite roots a variable in the repository, a reach through each,
// and the near misses: a variable rooted in a scratch directory, an indented reassignment, a
// comment, an escaped \$NAME the suite greps for, a path made only of a variable, a path that
// leaves the repository, and a glob under src-go/ (its changes go by build closure).
func TestSuiteReachesResolvesTheVariablesASuiteRootsInTheRepository(t *testing.T) {
	text, err := os.ReadFile("testdata/reach/suite.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		pyGlob,
		"files/var/local/claude-yolo/lib/" + "*.bash",
		"files/var/local/claude-yolo/lib/" + "*.sh",
		"playbooks/imports/play-" + "*.yml",
		"files/etc/skel",
		"scripts/git-hooks",
	}
	if got := suiteReaches(string(text), "scripts/test-x.bash"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// The suites the Task 3.9 sweep found reaching repository files only by glob or by directory,
// each with what it reaches: every one must still be read, or a change under it chooses no suite.
func TestTheRealSuitesThatReachFilesByGlobOrDirectoryAreRead(t *testing.T) {
	lib := "files/var/local/claude-yolo/lib"
	for suite, want := range map[string][]string{
		"test-self-update-cycle.bash":          {pyGlob, jsonGlob},
		"test-agent-bus-install.bash":          {"helpers/pingbus", "helpers/agent_bus"},
		"test-cc-teams.bash":                   {"files/opt/claude-yolo/optional/agent-bus/plugin/pingbus"},
		"test-ccy-compose-restore.bash":        {lib + "/" + "*.bash"},
		"test-ccy-lifecycle.bash":              {lib + "/" + "*.bash"},
		"test-ccy-session-registry.bash":       {lib + "/" + "*.bash"},
		"test-ccy-info-flags.bash":             {lib},
		"test-ccy-token-handoff.bash":          {lib},
		"test-ccy-container-version-hook.bash": {"scripts/git-hooks"},
		"test-fedora-desktop-health.bash":      {"helpers"},
		"test-run-bash-gh-scopes.bash":         {"helpers", "helpers/github_scopes"},
	} {
		got := suiteReaches(readReal(t, suite), "scripts/"+suite)
		for _, p := range want {
			if !slices.Contains(got, p) {
				t.Errorf("%s reaches %s and it was not read: %q", suite, p, got)
			}
		}
	}
}

// A reach is a glob that matches the file, or a directory (or a glob of directories) that holds
// it: a suite that copies a tree reaches every file in it.
func TestReachedMatchesTheFileOrADirectoryThatHoldsIt(t *testing.T) {
	for _, c := range []struct {
		pattern, rel string
		want         bool
	}{
		{pyGlob, "helpers/self_update/affected_plays.py", true},
		{pyGlob, "helpers/self_update/x/affected_plays.py", false},
		{pyGlob, "helpers/self_update/data.json", false},
		{cmdGlob, "src-go/cmd/ccy-teams/main.go", true},
		{cmdGlob, "src-go/internal/teams/x.go", false},
		{"files/etc/skel", "files/etc/skel/.bashrc", true},
		{"files/etc/skel", "files/etc/skeleton/.bashrc", false},
		{"files/etc/skel", "files/etc/skel", true},
	} {
		if got := reached(c.pattern, c.rel); got != c.want {
			t.Errorf("reached(%q, %q) = %v, want %v", c.pattern, c.rel, got, c.want)
		}
	}
}
