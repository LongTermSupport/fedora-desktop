package consumer

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	allPlay = "---\n- hosts: desktop\n  vars:\n    audience: all   # all | developer\n  tasks: []\n"
	devPlay = "---\n- hosts: desktop\n  vars:\n    audience: developer\n  tasks:\n    - name: Audience guard — end play on a consumer install\n      ansible.builtin.meta: end_play\n      when: install_audience != 'developer'\n"
	// importedPlay is a file whose first entry imports another play: it has no hosts and no
	// vars, and only the play after it says the audience.
	importedPlay = "---\n- import_playbook: ../../play-golang.yml\n\n- hosts: desktop\n  vars:\n    audience: all\n  tasks: []\n"
)

func TestOptionalPlaysListsOnlyThePlaysForEveryone(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, commonDir, "play-qobuz.yml"), allPlay)
	writeFile(t, filepath.Join(root, commonDir, "play-rust.yml"), devPlay)
	writeFile(t, filepath.Join(root, commonDir, "play-hd-audio.yml"), importedPlay)
	writeFile(t, filepath.Join(root, commonDir, "README.md"), "not a play\n")
	writeFile(t, filepath.Join(root, hardwareDir, "play-nvidia.yml"), allPlay)
	plays, err := optionalPlays(root)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(plays))
	for _, p := range plays {
		got = append(got, p.label+"="+p.path)
	}
	want := []string{
		"Hd Audio=" + commonDir + "/play-hd-audio.yml",
		"Qobuz=" + commonDir + "/play-qobuz.yml",
		"Nvidia (hardware)=" + hardwareDir + "/play-nvidia.yml",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("plays %q, want %q", got, want)
	}
}

// The checkout's own optional plays all parse, Qobuz is offered and no developer play is.
func TestOptionalPlaysOfThisCheckout(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	plays, err := optionalPlays(root)
	if err != nil {
		t.Fatal(err)
	}
	labels := make([]string, 0, len(plays))
	for _, p := range plays {
		labels = append(labels, p.label)
		forAll, ferr := forEveryone(filepath.Join(root, p.path))
		if ferr != nil || !forAll {
			t.Errorf("%s offered but not for everyone (%v)", p.path, ferr)
		}
	}
	if !slices.Contains(labels, "Qobuz") || slices.Contains(labels, "Rust Dev") {
		t.Fatalf("offered %q", labels)
	}
}

func TestOptionalPlaysRefusesAPlayThatSaysNoAudience(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, commonDir, "play-x.yml"), "---\n- hosts: desktop\n  tasks: []\n")
	if _, err := optionalPlays(root); err == nil || !strings.Contains(err.Error(), "audience") {
		t.Fatalf("err = %v, want the missing audience named", err)
	}
}

func TestOptionalPlaysRefusesAFileThatIsNotYAML(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, commonDir, "play-x.yml"), "- hosts: [\n")
	if _, err := optionalPlays(root); err == nil || !strings.Contains(err.Error(), "play-x.yml") {
		t.Fatalf("err = %v, want the file named", err)
	}
}

func TestOptionalPlaysRefusesAMissingDirectory(t *testing.T) {
	if _, err := optionalPlays(t.TempDir()); err == nil {
		t.Fatal("a checkout with no optional plays was accepted")
	}
}

func TestOptionalPlaysMixedAudienceIsForDevelopers(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, commonDir, "play-mixed.yml"), allPlay+strings.TrimPrefix(devPlay, "---\n"))
	writeFile(t, filepath.Join(hardwareDirOf(root), "play-y.yml"), allPlay)
	plays, err := optionalPlays(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 1 || plays[0].label != "Y (hardware)" {
		t.Fatalf("plays %+v, want only Y", plays)
	}
}

func hardwareDirOf(root string) string { return filepath.Join(root, hardwareDir) }

func TestParsePicks(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []int
		bad  bool
	}{
		{in: "", want: nil},
		{in: "  ", want: nil},
		{in: "2", want: []int{1}},
		{in: "3 1", want: []int{0, 2}},
		{in: "1,3, 1", want: []int{0, 2}},
		{in: "0", bad: true},
		{in: "4", bad: true},
		{in: "a", bad: true},
		{in: "1 x", bad: true},
	} {
		got, err := parsePicks(tc.in, 3)
		if tc.bad {
			if err == nil {
				t.Errorf("%q accepted as %v", tc.in, got)
			}
			continue
		}
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%q = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}
