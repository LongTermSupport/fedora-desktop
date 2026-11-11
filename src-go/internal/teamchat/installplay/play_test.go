package installplay

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// repo is the checkout's root, from this package's directory.
const repo = "../../../.."

const (
	archTableFile = "vars/teamchat-arch.yml"
	pinFile       = "files/usr/local/share/teamchat/tuwunel.pin"
	buildTasks    = "tasks/teamchat-build-go-commands.yml"
	goQAConfig    = "src-go/qa/go-qa.toml"
)

func readRepo(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(filepath.Join(repo, rel)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// play is the part of the install play these tests read.
type play struct {
	Vars  map[string]any   `yaml:"vars"`
	Tasks []map[string]any `yaml:"tasks"`
}

func loadPlay(t *testing.T) play {
	t.Helper()
	var plays []play
	if err := yaml.Unmarshal(readRepo(t, paths.PlayFile), &plays); err != nil {
		t.Fatalf("%s: %v", paths.PlayFile, err)
	}
	if len(plays) != 1 {
		t.Fatalf("%s holds %d plays, want 1", paths.PlayFile, len(plays))
	}
	return plays[0]
}

// The play holds the host's paths as variables, and each is the one `paths` names: the play and
// the program that reads these places cannot drift apart.
func TestThePlaysPathVariablesArePaths(t *testing.T) {
	want := map[string]string{
		"tc_lib_dir":          paths.LibDir,
		"tc_server_dir":       paths.ServerDir,
		"tc_kit_root":         paths.HostKitRoot,
		"tc_share_dir":        paths.ShareDir,
		"tc_resolver_dir":     paths.ResolverDir,
		"tc_etc_dir":          paths.EtcDir,
		"tc_admin_state_root": paths.AdminStateRoot,
		"tc_ctl_state_root":   paths.CtlStateRoot,
		"tc_status_root":      paths.StatusRoot,
		"tc_admin_link":       paths.AdminBinaryPath,
		"tc_member_link":      paths.MemberBinaryPath,
		"tc_ctl_user":         paths.CtlUser,
	}
	vars := loadPlay(t).Vars
	for name, path := range want {
		if got, ok := vars[name].(string); !ok || got != path {
			t.Errorf("play variable %s = %v, want %q", name, vars[name], path)
		}
	}
}

// What the play builds from those variables is what `paths` builds from the same parts: the
// admin program's digest-named home, the kit's binary and its current link, the server binary
// and pin, the owner group file, and the two timers it runs.
func TestThePlaysDerivedPathsAreThePathsPackagesDerivedPaths(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef01234567"
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	v, err := paths.ParseServerVersion("1.9.4")
	if err != nil {
		t.Fatal(err)
	}
	rendered := strings.NewReplacer(
		"{{ tc_lib_dir }}", paths.LibDir, "{{ tc_server_dir }}", paths.ServerDir,
		"{{ tc_kit_root }}", paths.HostKitRoot, "{{ tc_share_dir }}", paths.ShareDir,
		"{{ tc_etc_dir }}", paths.EtcDir, "{{ tc_tree.stdout }}", digest, "{{ tc_version }}", "1.9.4",
	).Replace(string(readRepo(t, paths.PlayFile)))
	for what, want := range map[string]string{
		"the admin program's home":   paths.AdminBinaryCopy(d),
		"the kit's binary":           paths.HostKitDir(d).Binary(),
		"the kit's current binary":   paths.HostKitCurrent().Binary(),
		"the kit's current link":     paths.HostKitCurrent().String(),
		"the server binary":          paths.ServerBinary(v),
		"the server pin":             paths.ServerPinFile,
		"the owner group file":       paths.OwnerGroupFile,
		"the health timer":           paths.HealthTimer,
		"the backup timer":           paths.BackupTimer,
		"the admin program's link":   "{{ tc_admin_link }}",
		"the member program's link":  "{{ tc_member_link }}",
		"the units the verb lists":   "units",
		"the verb that approves":     "approve",
		"the pin file of the server": "tuwunel.pin",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the play does not name %s (%q)", what, want)
		}
	}
}

// loadArchTable is the architecture table: each architecture's row, keyed by the row's field.
func loadArchTable(t *testing.T) map[string]map[string]string {
	t.Helper()
	var doc map[string]map[string]map[string]string
	if err := yaml.Unmarshal(readRepo(t, archTableFile), &doc); err != nil {
		t.Fatalf("%s: %v", archTableFile, err)
	}
	return doc["teamchat_arch"]
}

var sha256Line = regexp.MustCompile(`^[0-9a-f]{64}$`)

// An architecture is handled in every place that names one or in none: the table has a Go
// architecture and a Tuwunel asset for each row, the pin file has a checksum for each row and for
// nothing else, and the QA stage that cross-builds the commands builds for each row's Go
// architecture. A row missing from one place is an architecture lock by omission.
func TestEveryArchitectureIsHandledEverywhere(t *testing.T) {
	table := loadArchTable(t)
	if len(table) < 2 {
		t.Fatalf("the table holds %d architectures, want the two the pin has", len(table))
	}
	var pin map[string]string
	if err := yaml.Unmarshal(readRepo(t, pinFile), &pin); err != nil {
		t.Fatalf("%s: %v", pinFile, err)
	}
	goarches := make([]string, 0, len(table))
	for arch, row := range table {
		if row["goarch"] == "" || row["tuwunel_asset"] == "" {
			t.Errorf("%s: row %s is incomplete: %v", archTableFile, arch, row)
		}
		if sum := pin["tuwunel_sha256_"+arch]; !sha256Line.MatchString(sum) {
			t.Errorf("%s has no checksum for %s (tuwunel_sha256_%s = %q)", pinFile, arch, arch, sum)
		}
		goarches = append(goarches, row["goarch"])
	}
	for key := range pin {
		if arch, ok := strings.CutPrefix(key, "tuwunel_sha256_"); ok {
			if _, found := table[arch]; !found {
				t.Errorf("%s pins %s, which %s has no row for", pinFile, arch, archTableFile)
			}
		}
	}
	sort.Strings(goarches)
	stage := regexp.MustCompile(`for arch in ([a-z0-9 ]+); do`).FindSubmatch(readRepo(t, goQAConfig))
	if stage == nil {
		t.Fatalf("%s has no cross-build stage (for arch in ...; do)", goQAConfig)
	}
	built := strings.Fields(string(stage[1]))
	sort.Strings(built)
	if !slices.Equal(built, goarches) {
		t.Errorf("%s cross-builds for %v, the table's Go architectures are %v", goQAConfig, built, goarches)
	}
}

// offenders are the tasks that run on the target and read what only the controller's checkout or
// a desktop's login user has: the login user, a home directory, the environment, the checkout's
// root as a path. A task delegated to localhost runs on the controller, where the checkout is,
// and an include is read there too, so neither is judged. A block is judged by its children.
func offenders(tasks []map[string]any) []string {
	var out []string
	for _, task := range tasks {
		if task["delegate_to"] == "localhost" {
			continue
		}
		rest := map[string]any{}
		for key, value := range task {
			switch key {
			case "block", "rescue", "always", "include_tasks", "import_tasks", "ansible.builtin.include_tasks", "ansible.builtin.import_tasks":
				// the sections are judged below; an include is read on the controller
			default:
				rest[key] = value
			}
		}
		for _, section := range []string{"block", "rescue", "always"} {
			out = append(out, offenders(asTasks(task[section]))...)
		}
		raw, err := yaml.Marshal(rest)
		if err != nil {
			out = append(out, "unreadable task: "+err.Error())
			continue
		}
		for _, bad := range []string{"user_login", "/home/", "ansible_env", "lookup('env'", `lookup("env"`, "root_dir", "playbook_dir"} {
			if strings.Contains(string(raw), bad) {
				out = append(out, taskName(task)+" reads "+bad)
			}
		}
	}
	return out
}

func asTasks(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	tasks := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			tasks = append(tasks, m)
		}
	}
	return tasks
}

func taskName(task map[string]any) string {
	return fmt.Sprintf("task %v", task["name"])
}

// The check above can fail: a task on the target that reads the login user, a block whose child
// reads a home directory, and a task that reads the checkout's root are caught; the same reads in
// a task delegated to localhost, and the root in an include, are not.
func TestTheCheckoutIndependenceCheckDiscriminates(t *testing.T) {
	fixture := `
- name: reads the login user
  ansible.builtin.file: {path: "/x/{{ user_login }}"}
- name: block
  block:
    - name: reads a home
      ansible.builtin.file: {path: "/home/<user>"}
- name: reads the checkout
  ansible.builtin.copy: {src: "{{ root_dir }}/f", dest: /x}
- name: controller side
  ansible.builtin.command: {argv: ["{{ root_dir }}/f", "/home/<user>"]}
  delegate_to: localhost
- name: include
  ansible.builtin.include_tasks: "{{ root_dir }}/tasks/x.yml"
`
	var tasks []map[string]any
	if err := yaml.Unmarshal([]byte(fixture), &tasks); err != nil {
		t.Fatal(err)
	}
	got := offenders(tasks)
	want := []string{"task reads a home reads /home/", "task reads the checkout reads root_dir", "task reads the login user reads user_login"}
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Errorf("offenders = %q, want %q", got, want)
	}
}

// The play works against a host that holds no checkout, no Go toolchain and maybe no login user:
// every task of the play and of the build tasks it includes either runs on the controller or
// reads none of what is only there.
func TestThePlayReadsNothingTheTargetLacks(t *testing.T) {
	if bad := offenders(loadPlay(t).Tasks); len(bad) > 0 {
		t.Errorf("%s: %q", paths.PlayFile, bad)
	}
	var tasks []map[string]any
	if err := yaml.Unmarshal(readRepo(t, buildTasks), &tasks); err != nil {
		t.Fatalf("%s: %v", buildTasks, err)
	}
	if len(tasks) == 0 {
		t.Fatalf("%s holds no tasks", buildTasks)
	}
	for _, task := range tasks {
		if task["delegate_to"] != "localhost" {
			t.Errorf("%s: %s is not delegated to localhost, so it would build on the target", buildTasks, taskName(task))
		}
	}
}
