package ansible

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestYamlFilesWalksInNameOrder(t *testing.T) {
	root := writeTree(t, map[string]string{
		"d/b.yml":     "",
		"d/a.yaml":    "",
		"d/c.txt":     "",
		"d/.hid.yml":  "",
		"d/.hd/x.yml": "",
		"d/x.yml.bak": "",
		"d/z/y.yml":   "",
	})
	if err := os.Mkdir(root+"/d/dir.yml", 0o750); err != nil {
		t.Fatal(err)
	}
	got, err := yamlFiles(os.DirFS(root), "d", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"d/.hd/x.yml", "d/.hid.yml", "d/a.yaml", "d/b.yml", "d/z/y.yml"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestYamlFilesExcludesOnlyRolesVendor(t *testing.T) {
	root := writeTree(t, map[string]string{
		"roles/vendor/v.yml":   "",
		"roles/r/vendor/w.yml": "",
		"roles/x.yml":          "",
		"tasks/vendor/t.yml":   "",
	})
	fsys := os.DirFS(root)
	got, err := yamlFiles(fsys, "roles", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"roles/r/vendor/w.yml", "roles/x.yml"}; !slices.Equal(got, want) {
		t.Fatalf("roles: got %q want %q", got, want)
	}
	got, err = yamlFiles(fsys, "tasks", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tasks/vendor/t.yml"}; !slices.Equal(got, want) {
		t.Fatalf("tasks: got %q want %q", got, want)
	}
	got, err = yamlFiles(fsys, "roles", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("unfiltered: got %q", got)
	}
}

func TestYamlFilesReadsASymlinkedFileAndRefusesTheRest(t *testing.T) {
	root := writeTree(t, map[string]string{"real/r.yml": "", "real/n.txt": "", "d/x.yml": ""})
	symlink(t, root, "d/link.yml", "../real/r.yml")
	symlink(t, root, "d/text.txt", "../real/n.txt")
	got, err := yamlFiles(os.DirFS(root), "d", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"d/link.yml", "d/x.yml"}; !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}

	symlink(t, root, "d/dangling.yml", "../nowhere.yml")
	if _, err := yamlFiles(os.DirFS(root), "d", true); err == nil {
		t.Fatal("a dangling symlink was skipped")
	}

	other := writeTree(t, map[string]string{"real/r.yml": ""})
	symlink(t, other, "d/dir.yml", "../real")
	if _, err := yamlFiles(os.DirFS(other), "d", true); err == nil {
		t.Fatal("a symlink to a directory was skipped")
	}
}

func TestYamlFilesRefusesASymlinkToADirectoryWhateverItIsCalled(t *testing.T) {
	root := writeTree(t, map[string]string{"elsewhere/bad.yml": "x", "d/x.yml": ""})
	symlink(t, root, "d/shared", "../elsewhere")
	_, err := yamlFiles(os.DirFS(root), "d", true)
	if err == nil || !strings.Contains(err.Error(), "d/shared") {
		t.Fatalf("a symlink to a directory was skipped or unnamed: %v", err)
	}
}

func TestYamlFilesRefusesADanglingSymlinkWhateverItIsCalled(t *testing.T) {
	root := writeTree(t, map[string]string{"d/x.yml": ""})
	symlink(t, root, "d/broken", "nowhere")
	_, err := yamlFiles(os.DirFS(root), "d", true)
	if err == nil || !strings.Contains(err.Error(), "d/broken") {
		t.Fatalf("a dangling symlink was skipped or unnamed: %v", err)
	}
}

func TestYamlFilesIgnoresASymlinkToANonYAMLFile(t *testing.T) {
	root := writeTree(t, map[string]string{"real/n.txt": "", "d/x.yml": ""})
	symlink(t, root, "d/text", "../real/n.txt")
	got, err := yamlFiles(os.DirFS(root), "d", true)
	if err != nil || !slices.Equal(got, []string{"d/x.yml"}) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestPublishedLeavesOutTheIgnoredEntriesAndNothingElse(t *testing.T) {
	root := writeTree(t, map[string]string{"d/keep.yml": "", "d/secret.yml": "", "d/sub/in.yml": "", "d/sub/out.yml": ""})
	symlink(t, root, "d/dangling.yml", "nowhere.yml")
	fsys := published(os.DirFS(root), []string{"d/secret.yml", "d/dangling.yml", "d/sub/out.yml"})
	got, err := yamlFiles(fsys, "d", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"d/keep.yml", "d/sub/in.yml"}; !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestYamlFilesFollowsASymlinkedSearchDirectory(t *testing.T) {
	root := writeTree(t, map[string]string{"real/x.yml": ""})
	symlink(t, root, "roles", "real")
	got, err := yamlFiles(os.DirFS(root), "roles", true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"roles/x.yml"}) {
		t.Fatalf("got %q", got)
	}
}

func TestYamlFilesOfAMissingDirectoryIsAnError(t *testing.T) {
	if _, err := yamlFiles(os.DirFS(t.TempDir()), "playbooks", false); err == nil {
		t.Fatal("a missing directory was read as empty")
	}
}

func TestSplitLines(t *testing.T) {
	cases := map[string][]string{
		"":         nil,
		"\n":       {""},
		"a":        {"a"},
		"a\n":      {"a"},
		"a\nb":     {"a", "b"},
		"a\r\nb\n": {"a\r", "b"},
		"a\n\nb\n": {"a", "", "b"},
		"x\x00y\n": {"x\x00y"},
	}
	for text, want := range cases {
		if got := splitLines([]byte(text)); !slices.Equal(got, want) {
			t.Errorf("splitLines(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestGrepLinesNumbersTheMatchingLines(t *testing.T) {
	root := writeTree(t, map[string]string{
		"tasks/a.yml": "one\ntwo hit\nthree\nfour hit",
		"vars/b.yml":  "hit\n",
	})
	hits, err := grepLines(os.DirFS(root), []string{"tasks", "vars"}, func(text string) bool {
		return len(text) >= 3 && text[len(text)-3:] == "hit"
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(hits))
	for _, h := range hits {
		got = append(got, h.String())
	}
	want := []string{"tasks/a.yml:2:two hit", "tasks/a.yml:4:four hit", "vars/b.yml:1:hit"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDirExistsFollowsSymlinksAndRefusesFiles(t *testing.T) {
	root := writeTree(t, map[string]string{"real/x": "", "file": ""})
	symlink(t, root, "link", "real")
	fsys := os.DirFS(root)
	for dir, want := range map[string]bool{"real": true, "link": true, "file": false, "nope": false} {
		if got := dirExists(fsys, dir); got != want {
			t.Errorf("dirExists(%q) = %v", dir, got)
		}
	}
}
