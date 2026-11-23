package installplay

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The member tag's kit is a whole kit (Plan 00209 DESIGN-host-cc-v2.md section 6 step 2): the
// play makes the build's digest directory empty, and the member program it copied, checked
// against the controller's hash, assembles the kit into it (`teamchat kit assemble --into`:
// binary, plugin, hooks, skill and settings, kit.Assemble) before the current link moves, so a
// host `run --claude` finds the plugin and the settings. The kit's own program is checked against
// the controller's hash again after the assembly.
func TestTheMemberTagAssemblesTheKitIntoItsDigestDirectory(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef01234567"
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	kit := paths.HostKitDir(d)
	tasks := flatten(loadPlay(t).Tasks)
	at := func(name string) int {
		i := slices.IndexFunc(tasks, func(task map[string]any) bool { return task["name"] == name })
		if i < 0 {
			t.Fatalf("the play has no task %q", name)
		}
		return i
	}
	field := func(i int, module, key string) string {
		args, ok := tasks[i][module].(map[string]any)
		if !ok {
			t.Fatalf("task %q does not call %s", tasks[i]["name"], module)
		}
		return rendered(t, text(args[key]), digest, "v")
	}

	mkdir := at("Make the Kit Directory")
	if got := field(mkdir, "ansible.builtin.file", "path"); got != kit.String() {
		t.Errorf("the kit directory made is %s, want the empty digest directory %s", got, kit)
	}
	staged := at("Copy the Member Program Beside the Kits")
	program := field(staged, "ansible.builtin.copy", "dest")
	check := at("Check the Member Program Against the Controller's Hash")

	assemble := at("Assemble the Member Kit")
	args, ok := tasks[assemble]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatal("Assemble the Member Kit does not call ansible.builtin.command")
	}
	var argv []string
	for _, a := range sequence(args["argv"]) {
		argv = append(argv, rendered(t, text(a), digest, "v"))
	}
	if want := []string{program, "kit", "assemble", "--into", kit.String()}; !slices.Equal(argv, want) {
		t.Errorf("the assembly runs %q, want %q", argv, want)
	}
	if got := rendered(t, text(args["creates"]), digest, "v"); got != kit.Settings() {
		t.Errorf("the assembly is guarded by %s, want the kit's settings %s", got, kit.Settings())
	}
	if !slices.Contains(stringsOf(tasks[assemble]["tags"]), "member") {
		t.Errorf("the assembly is not tagged member: %v", tasks[assemble]["tags"])
	}

	kitCheck := at("Check the Kit's Member Program Against the Controller's Hash")
	read := at("Read the Kit's Member Program")
	if got := field(read, "ansible.builtin.stat", "path"); got != kit.Binary() {
		t.Errorf("the kit's program read is %s, want %s", got, kit.Binary())
	}
	current := at("Point the Kit's Current Link at This Build")
	if order := []int{mkdir, staged, check, assemble, read, kitCheck, current}; !slices.IsSorted(order) {
		t.Errorf("order: make %d, copy %d, check %d, assemble %d, read %d, check kit %d, link %d",
			mkdir, staged, check, assemble, read, kitCheck, current)
	}
	if !strings.HasPrefix(program, "/usr/local/lib/teamchat/") || strings.HasPrefix(program, kit.String()+"/") {
		t.Errorf("the staged program %s is not beside the kits", program)
	}
}
