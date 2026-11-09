package vmtestmanifest

import (
	"maps"
	"strings"
	"testing"
)

// consumerPicks is goodManifest with the desktop scenario a consumer install that picks plays.
func consumerPicks(plays ...string) string {
	var picks strings.Builder
	picks.WriteString("    reboot_before_checks: true\n    audience: consumer\n    optional_plays:\n")
	for _, p := range plays {
		picks.WriteString("      - " + p + "\n")
	}
	return strings.Replace(goodManifest, "    reboot_before_checks: true\n", picks.String(), 1)
}

const (
	forAll       = "---\n- hosts: desktop\n  vars:\n    audience: all\n  tasks: []\n"
	forDeveloper = "---\n- hosts: desktop\n  vars:\n    audience: developer\n  tasks: []\n"
	commonPlays  = "playbooks/imports/optional/common/"
	hwPlays      = "playbooks/imports/optional/hardware-specific/"
)

// V17: a pick of an optional play for every audience, in either optional directory, passes and
// changes nothing on stdout.
func TestV17APickOfAPlayForEveryAudiencePasses(t *testing.T) {
	w := newWorld(t, map[string]string{
		manifestFile:                            consumerPicks("play-speech-to-text.yml", "play-hd-audio.yml"),
		commonPlays + "play-speech-to-text.yml": "---\n- name: an import\n  ansible.builtin.import_playbook: x.yml\n" + strings.TrimPrefix(forAll, "---\n"),
		hwPlays + "play-hd-audio.yml":           forAll,
	})
	w.wantExit(ExitOK, w.run())
	if got := w.stdout.String(); got != line1+line2+line3 {
		t.Fatalf("stdout:\n%s", got)
	}
}

// V17: a pick the menu would never offer is a finding naming the scenario and the play.
func TestV17APickTheConsumerMenuNeverOffersIsAFinding(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"missing": {
			files: map[string]string{},
			want:  "ERROR: desktop-fresh-install: optional_plays names play-speech-to-text.yml, which is not a play in playbooks/imports/optional/common or playbooks/imports/optional/hardware-specific",
		},
		"developer": {
			files: map[string]string{commonPlays + "play-speech-to-text.yml": forDeveloper},
			want:  "ERROR: desktop-fresh-install: optional_plays names playbooks/imports/optional/common/play-speech-to-text.yml, which is not for every audience (vars.audience all in each of its plays), so the consumer menu never offers it",
		},
		"no audience": {
			files: map[string]string{commonPlays + "play-speech-to-text.yml": "---\n- hosts: desktop\n  tasks: []\n"},
			want:  "which is not for every audience",
		},
		"both directories": {
			files: map[string]string{commonPlays + "play-speech-to-text.yml": forAll, hwPlays + "play-speech-to-text.yml": forAll},
			want:  "ERROR: desktop-fresh-install: optional_plays names play-speech-to-text.yml, which is in both playbooks/imports/optional/common and playbooks/imports/optional/hardware-specific",
		},
	} {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{manifestFile: consumerPicks("play-speech-to-text.yml")}
			maps.Copy(files, tc.files)
			w := newWorld(t, files)
			w.wantExit(ExitFailed, w.run())
			w.wantNoStdout()
			w.wantStderr(tc.want)
		})
	}
}

// V17: a picked play that is not YAML is the gate unable to judge it.
func TestV17AnUnparseablePickedPlayIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{
		manifestFile:                            consumerPicks("play-speech-to-text.yml"),
		commonPlays + "play-speech-to-text.yml": "- hosts: [\n",
	})
	w.wantExit(ExitCannotRun, w.run())
	w.wantNoStdout()
	w.wantStderr("ERROR: playbooks/imports/optional/common/play-speech-to-text.yml is not a playbook")
}

// V17: a picked name that is a directory is the gate unable to judge it.
func TestV17APickThatIsNotAFileIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{
		manifestFile: consumerPicks("play-speech-to-text.yml"),
		hwPlays + "play-speech-to-text.yml/keep.yml": forAll,
	})
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: playbooks/imports/optional/hardware-specific/play-speech-to-text.yml is not a regular file")
}
