package installplay

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/kit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The member tag's kit is a whole kit (Plan 00209 DESIGN-host-cc-v2.md section 6 step 2): the
// member program the play copied, checked against the controller's hash, places the kit at the
// build's digest directory (`teamchat kit assemble --into`: kit.Place assembles it in a sibling
// and renames it into place, so the directory holds no kit or a whole one). The play makes no
// digest directory itself and guards nothing on one file: the program judges the kit whole on
// every run, and its stdout says whether it assembled. The play then reads every part of the
// kit, fails unless each is there as its kind, and checks the kit's program against the
// controller's hash, all before the current link moves.

const kitDigest = "0123456789abcdef0123456789abcdef01234567"

// kitTasks is the play's tasks, flattened, with the kit of kitDigest.
type kitTasks struct {
	t     *testing.T
	tasks []map[string]any
	kit   paths.Kit
}

func loadKitTasks(t *testing.T) kitTasks {
	t.Helper()
	d, err := paths.ParseDigest(kitDigest)
	if err != nil {
		t.Fatal(err)
	}
	return kitTasks{t, flatten(loadPlay(t).Tasks), paths.HostKitDir(d)}
}

func (k kitTasks) at(name string) int {
	k.t.Helper()
	i := slices.IndexFunc(k.tasks, func(task map[string]any) bool { return task["name"] == name })
	if i < 0 {
		k.t.Fatalf("the play has no task %q", name)
	}
	return i
}

func (k kitTasks) args(i int, module string) map[string]any {
	k.t.Helper()
	args, ok := k.tasks[i][module].(map[string]any)
	if !ok {
		k.t.Fatalf("task %q does not call %s", k.tasks[i]["name"], module)
	}
	return args
}

func (k kitTasks) field(i int, module, key string) string {
	k.t.Helper()
	return rendered(k.t, text(k.args(i, module)[key]), kitDigest, "v")
}

func TestTheMemberTagAssemblesTheKitIntoItsDigestDirectory(t *testing.T) {
	k := loadKitTasks(t)
	for _, task := range k.tasks {
		args, ok := task["ansible.builtin.file"].(map[string]any)
		if ok && rendered(t, text(args["path"]), kitDigest, "v") == k.kit.String() {
			t.Errorf("task %q makes the digest directory, which only the rename into place may make", task["name"])
		}
	}
	program := k.field(k.at("Copy the Member Program Beside the Kits"), "ansible.builtin.copy", "dest")
	assemble := k.at("Assemble the Member Kit")
	args := k.args(assemble, "ansible.builtin.command")
	var argv []string
	for _, a := range sequence(args["argv"]) {
		argv = append(argv, rendered(t, text(a), kitDigest, "v"))
	}
	if want := []string{program, "kit", "assemble", "--into", k.kit.String()}; !slices.Equal(argv, want) {
		t.Errorf("the assembly runs %q, want %q", argv, want)
	}
	if guard, ok := args["creates"]; ok {
		t.Errorf("the assembly is skipped when %v exists, so a kit that is not whole would be taken as done", guard)
	}
	register := text(k.tasks[assemble]["register"])
	if changed := text(k.tasks[assemble]["changed_when"]); register == "" ||
		changed != register+".stdout.startswith('assembled ')" {
		t.Errorf("the assembly's change is %q (register %q), want its stdout read", changed, register)
	}
	if !slices.Contains(stringsOf(k.tasks[assemble]["tags"]), "member") {
		t.Errorf("the assembly is not tagged member: %v", k.tasks[assemble]["tags"])
	}
	if !strings.HasPrefix(program, "/usr/local/lib/teamchat/") || strings.HasPrefix(program, k.kit.String()+"/") {
		t.Errorf("the staged program %s is not beside the kits", program)
	}
}

// Every part of the kit (kit.Parts) is read, each named with its kind, and one assertion fails
// unless each is there as that kind.
func TestThePlayChecksEveryPartOfTheKit(t *testing.T) {
	k := loadKitTasks(t)
	readParts := k.at("Read Every Part of the Kit")
	if got := k.field(readParts, "ansible.builtin.stat", "path"); got != k.kit.String()+"/{{ item.part }}" {
		t.Errorf("the parts read are at %s, want below %s", got, k.kit)
	}
	dirs := []string{
		"bin", "plugin", "plugin/teamchat", "plugin/teamchat/.claude-plugin",
		"plugin/teamchat/hooks", "plugin/teamchat/skills", "plugin/teamchat/skills/teamchat",
	}
	var parts []string
	for _, item := range sequence(k.tasks[readParts]["loop"]) {
		part, kind := text(mapping(item)["part"]), text(mapping(item)["kind"])
		parts = append(parts, part)
		if want := map[bool]string{true: "dir", false: "file"}[slices.Contains(dirs, part)]; kind != want {
			t.Errorf("part %s is read as %q, want %q", part, kind, want)
		}
	}
	if want := kit.Parts(k.kit); !slices.Equal(parts, want) {
		t.Errorf("the play reads the parts %q, want every part of the kit %q", parts, want)
	}
	var conditions []string
	for _, c := range sequence(k.args(k.at("Check the Kit Is Whole"), "ansible.builtin.assert")["that"]) {
		conditions = append(conditions, text(c))
	}
	that := strings.Join(conditions, "\n")
	partsVar := text(k.tasks[readParts]["register"])
	for _, want := range []string{"rejectattr('stat.exists')", "rejectattr('stat.isdir')", "rejectattr('stat.isreg')"} {
		if partsVar == "" || !strings.Contains(that, partsVar+".results") || !strings.Contains(that, want) {
			t.Errorf("the whole-kit check %q does not hold %s over %q", that, want, partsVar)
		}
	}
}

// The kit is assembled from the checked program, then judged whole and its program checked,
// before the current link moves.
func TestTheKitIsCheckedBeforeTheCurrentLinkMoves(t *testing.T) {
	k := loadKitTasks(t)
	read := k.at("Read the Kit's Member Program")
	if got := k.field(read, "ansible.builtin.stat", "path"); got != k.kit.Binary() {
		t.Errorf("the kit's program read is %s, want %s", got, k.kit.Binary())
	}
	order := []int{
		k.at("Copy the Member Program Beside the Kits"),
		k.at("Check the Member Program Against the Controller's Hash"),
		k.at("Assemble the Member Kit"),
		k.at("Read Every Part of the Kit"),
		k.at("Check the Kit Is Whole"),
		read,
		k.at("Check the Kit's Member Program Against the Controller's Hash"),
		k.at("Point the Kit's Current Link at This Build"),
	}
	if !slices.IsSorted(order) {
		t.Errorf("the kit's tasks are out of order: %v", order)
	}
}
