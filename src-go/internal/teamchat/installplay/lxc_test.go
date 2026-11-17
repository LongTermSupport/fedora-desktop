package installplay

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The play's tasks that carry LXC members (Plan 00209 Task 3.3, C 3.5.1 steps 10 to 13).
const (
	approveTask    = "Approve the Teams the Operator Names"
	lxcAbsentTask  = "Park and Remove the LXC Members That Leave"
	removeTask     = "Remove the Absent Teams"
	applyTask      = "Apply the Present Teams"
	lxcPresentTask = "Place and Join the Declared LXC Members"
	inputCheckTask = "Check the Play's Inputs"
)

// stdinKey is a key the play writes into a JSON object it hands over: `'<key>':`.
var stdinKey = regexp.MustCompile(`'([a-z_]+)':`)

// stdinKeys are the keys of the object a stdin template builds, in order.
func stdinKeys(t *testing.T, template any, what string) []string {
	t.Helper()
	text, ok := template.(string)
	if !ok {
		t.Fatalf("%s has no stdin template", what)
	}
	found := stdinKey.FindAllStringSubmatch(text, -1)
	keys := make([]string, 0, len(found))
	for _, m := range found {
		keys = append(keys, m[1])
	}
	return keys
}

// sampleTeam is a present team every rule passes, in host_vars' shape.
func sampleTeam(id string) map[string]any {
	return map[string]any{
		"team": id, "state": "present", "port": 8448, "description": "The team that builds the thing.",
		"networks": []any{"192.0.2.0/24"}, "url": "https://192.0.2.1:8448", "humans": []any{"alice"},
		"trust": map[string]any{"kind": "github", "org": "example-org"},
	}
}

// sampleMember is a seat of team in host_vars' shape.
func sampleMember(team string) map[string]any {
	return map[string]any{"team": team, "container": "box-1", "user": "agent", "checkout": "/srv/checkouts/thing", "seat": 1}
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The play reads the LXC members from the inventory variable paths names, and still refuses the
// bus address, which the LXC path does not need: a team with a present LXC member is served on
// the LXC bridge's address (the effective networks, C 3.2.5), which its url names.
func TestThePlayReadsTheLXCMembersAndRefusesTheBusAddress(t *testing.T) {
	p := loadPlay(t)
	if want := "{{ " + paths.HostVarLXCMembers + " | default([]) }}"; p.Vars["tc_lxc"] != want {
		t.Errorf("play variable tc_lxc = %v, want %q", p.Vars["tc_lxc"], want)
	}
	that, ok := taskModule(t, p, inputCheckTask, "ansible.builtin.assert")["that"].([]any)
	if !ok {
		t.Fatal("the input check has no list of conditions")
	}
	if want := paths.HostVarAddress + " | default('') == ''"; !slices.Contains(that, any(want)) {
		t.Errorf("the input check no longer refuses %s (%q)", paths.HostVarAddress, want)
	}
	for _, c := range that {
		if s, ok := c.(string); !ok || strings.Contains(s, paths.HostVarLXCMembers) {
			t.Errorf("the input check names %s itself (%q): it judges tc_lxc", paths.HostVarLXCMembers, s)
		}
	}
	for _, want := range []string{
		"tc_lxc is sequence and tc_lxc is not string",
		"tc_lxc | reject('mapping') | list | length == 0",
		"tc_lxc | map(attribute='team') | difference(tc_present | map(attribute='team') | list) | length == 0",
	} {
		if !slices.Contains(that, any(want)) {
			t.Errorf("the input check lacks %q", want)
		}
	}
}

// approve and apply each get the team and that team's own LXC entries (DR7, AG1): the members
// are security fields, and a team with a present member is served on the bridge. The keys the
// play writes are the ones declare.DecodeInput reads.
func TestThePlayHandsEachTeamItsOwnLXCMembers(t *testing.T) {
	p := loadPlay(t)
	const want = "{{ {'team': item, 'lxc_members': tc_lxc | selectattr('team', 'eq', item.team) | list} | to_json }}"
	for _, task := range []string{approveTask, applyTask} {
		args := taskModule(t, p, task, "ansible.builtin.command")
		if args["stdin"] != want {
			t.Errorf("task %q stdin = %v, want %q", task, args["stdin"], want)
		}
		keys := stdinKeys(t, args["stdin"], "task "+task)
		if len(keys) != 2 {
			t.Fatalf("task %q writes keys %v, want the team and its members", task, keys)
		}
		in, err := declare.DecodeInput(marshal(t, map[string]any{keys[0]: sampleTeam("alpha"), keys[1]: []any{sampleMember("alpha")}}))
		if err != nil {
			t.Fatalf("task %q: the input the play writes does not decode: %v", task, err)
		}
		if len(in.LXC) != 1 {
			t.Errorf("task %q: the input holds %d LXC members, want 1", task, len(in.LXC))
		}
	}
}

// The two LXC phases run `lxc apply` with the declaration on stdin in host_vars' own keys, which
// declare.Decode reads, scoped by --only-teams to the present teams the run acts on, and report a
// change only on a CHANGED line.
func TestThePlayRunsBothLXCPhases(t *testing.T) {
	p := loadPlay(t)
	keys := stdinKeys(t, p.Vars["tc_lxc_declaration"], "play variable tc_lxc_declaration")
	if want := []string{paths.HostVarTeams, paths.HostVarLXCMembers, declare.HostVarSite}; !slices.Equal(keys, want) {
		t.Errorf("tc_lxc_declaration writes keys %v, want %v", keys, want)
	}
	d, err := declare.Decode(marshal(t, map[string]any{
		paths.HostVarTeams: []any{sampleTeam("alpha")}, paths.HostVarLXCMembers: []any{sampleMember("alpha")},
		declare.HostVarSite: "desk",
	}))
	if err != nil || len(d.LXC) != 1 || d.LXC[0].Site != "desk" {
		t.Errorf("the declaration in host_vars' keys does not decode: %+v, %v", d, err)
	}
	for task, phase := range map[string]string{lxcAbsentTask: "absent", lxcPresentTask: "present"} {
		args := taskModule(t, p, task, "ansible.builtin.command")
		want := "{{ [tc_admin_link, 'lxc', 'apply', '--phase', '" + phase + "', '--only-teams', tc_lxc_" + phase + "_teams | join(',')] }}"
		if args["argv"] != want {
			t.Errorf("task %q argv = %v, want %q", task, args["argv"], want)
		}
		if args["stdin"] != "{{ tc_lxc_declaration }}" {
			t.Errorf("task %q stdin = %v, want the declaration", task, args["stdin"])
		}
		if got := changedWhen(t, p, task); got != "tc_lxc_"+phase+".stdout_lines | select('match', tc_changed_re) | list | length > 0" {
			t.Errorf("task %q changed_when = %q", task, got)
		}
	}
}

// The order is C 3.5.1's, with the approval first: `lxc apply` holds every acted team against its
// approval in both phases, so a team approved in this run is approved before the absent phase.
// The absent phase parks a leaving member while its team's homeserver still runs, before any
// team is removed; the present phase places members once their team is applied.
func TestThePlayOrdersTheLXCPhasesAroundTheTeams(t *testing.T) {
	var names []string
	for _, task := range loadPlay(t).Tasks {
		if n, ok := task["name"].(string); ok {
			names = append(names, n)
		}
	}
	order := []string{approveTask, lxcAbsentTask, removeTask, applyTask, lxcPresentTask}
	last := -1
	for _, name := range order {
		at := slices.Index(names, name)
		if at < 0 {
			t.Fatalf("the play has no task %q", name)
		}
		if at < last {
			t.Errorf("task %q runs before the task it must follow (want the order %q)", name, order)
		}
		last = at
	}
}

// changedWhen is the changed_when of the play's task of that name.
func changedWhen(t *testing.T, p play, name string) string {
	t.Helper()
	for _, task := range p.Tasks {
		if task["name"] == name {
			s, ok := task["changed_when"].(string)
			if !ok {
				t.Fatalf("task %q has no changed_when expression", name)
			}
			return s
		}
	}
	t.Fatalf("the play has no task %q", name)
	return ""
}
