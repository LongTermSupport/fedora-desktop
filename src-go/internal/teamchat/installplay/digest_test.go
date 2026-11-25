package installplay

import (
	"slices"
	"strings"
	"testing"
)

// The member program's source digest (what `teamchat version` prints, and the name of its kit
// directory) is the image-source digest (Plan 00265 R57): the one script both builders run, over
// the paths files/var/local/claude-yolo/image-source-paths lists, at the commit built. The ccy
// image's builder stage runs it at the commit it clones, the play at the checkout's HEAD, so two
// builds of the same teamchat source print the same digest whatever else src-go holds. The admin
// program is no image source and keeps the git tree id of src-go, which covers all it builds from.
const (
	digestScript = "files/var/local/claude-yolo/image-source-digest"
	memberDigest = "{{ tc_member_digest.stdout }}"
	adminDigest  = "{{ tc_tree.stdout }}"
)

// builds is the play's imports of the cross-build tasks: each one's commands and ldflags.
func builds(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, task := range flatten(loadPlay(t).Tasks) {
		if !strings.HasSuffix(text(task["ansible.builtin.import_tasks"]), "/"+buildTasks) {
			continue
		}
		vars := mapping(task["vars"])
		for _, command := range sequence(vars["go_cross_commands"]) {
			if _, twice := out[text(command)]; twice {
				t.Errorf("the play builds %v twice", command)
			}
			out[text(command)] = text(vars["go_cross_ldflags"])
		}
	}
	return out
}

func TestR57EachProgramCarriesTheDigestOfItsOwnSource(t *testing.T) {
	got := builds(t)
	want := map[string]string{
		"teamchat":       "-X main.SourceDigest=" + memberDigest,
		"teamchat-admin": "-X main.SourceDigest=" + adminDigest,
	}
	for command, ldflags := range want {
		if got[command] != ldflags {
			t.Errorf("the play builds %s with ldflags %q, want %q", command, got[command], ldflags)
		}
	}
	if len(got) != len(want) {
		t.Errorf("the play builds %v, want exactly %v", got, want)
	}
}

func TestR57TheMemberDigestIsTheImageSourceDigestAtHEAD(t *testing.T) {
	tasks := flatten(loadPlay(t).Tasks)
	i := slices.IndexFunc(tasks, func(task map[string]any) bool { return task["register"] == "tc_member_digest" })
	if i < 0 {
		t.Fatal("no task registers tc_member_digest")
	}
	task := tasks[i]
	var argv []string
	for _, a := range sequence(mapping(task["ansible.builtin.command"])["argv"]) {
		argv = append(argv, text(a))
	}
	want := []string{
		"bash", "{{ root_dir }}/" + digestScript, "{{ root_dir }}", "HEAD",
		"{{ root_dir }}/files/var/local/claude-yolo/image-source-paths",
	}
	if !slices.Equal(argv, want) {
		t.Errorf("the member digest is read by %q, want %q", argv, want)
	}
	if task["delegate_to"] != "localhost" || task["run_once"] != true || task["changed_when"] != false {
		t.Errorf("the member digest's task must run once on the controller and change nothing: %v", task)
	}
}

// Every place the play names the member program's build (its staged copy, the kit directory, the
// kit's parts and the current link) names it by the member digest; the admin program alone by
// the tree id. A kit directory named by another digest than the binary inside it prints would
// fail the keeper's kit check (its --version against the directory's name).
func TestR57TheKitIsNamedByTheMemberDigest(t *testing.T) {
	play := string(readRepo(t, "playbooks/imports/play-teamchat.yml"))
	for _, wrong := range []string{
		"teamchat-member-" + adminDigest, "{{ tc_kit_root }}/" + adminDigest, "src: \"" + adminDigest + "\"",
	} {
		if strings.Contains(play, wrong) {
			t.Errorf("the play names the member build by the src-go tree id: %q", wrong)
		}
	}
	for _, right := range []string{
		"teamchat-member-" + memberDigest, "{{ tc_kit_root }}/" + memberDigest, "src: \"" + memberDigest + "\"",
		"teamchat-admin-" + adminDigest,
	} {
		if !strings.Contains(play, right) {
			t.Errorf("the play does not name %q", right)
		}
	}
}
