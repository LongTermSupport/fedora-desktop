package installplay

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/unitfiles"
)

// flatten is the tasks of a play in the order they run, a block's children where the block is.
func flatten(tasks []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task)
		for _, section := range []string{"block", "rescue", "always"} {
			out = append(out, flatten(asTasks(task[section]))...)
		}
	}
	return out
}

// rendered is text with the play's own path variables replaced by their values and the two it
// learns at run time by the stand-ins a test gives them.
func rendered(t *testing.T, text, digest, version string) string {
	t.Helper()
	var pairs []string
	for name, value := range loadPlay(t).Vars {
		if s, ok := value.(string); ok && !strings.Contains(s, "{{") {
			pairs = append(pairs, "{{ "+name+" }}", s)
		}
	}
	pairs = append(pairs, "{{ tc_tree.stdout }}", digest, "{{ tc_version }}", version)
	return strings.NewReplacer(pairs...).Replace(text)
}

// text is v when it is a string, and "" when it is anything else.
func text(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// mapping is v when it is a mapping, and nil when it is anything else.
func mapping(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

// sequence is v when it is a list, and nil when it is anything else.
func sequence(v any) []any {
	l, ok := v.([]any)
	if !ok {
		return nil
	}
	return l
}

// spelled are the paths a task spells: a copy's or a link's dest, a file task's path, and the path
// of each item of its loop.
func spelled(task map[string]any) []string {
	loop := sequence(task["loop"])
	out := make([]string, 0, 4+len(loop))
	for _, module := range []string{"ansible.builtin.copy", "ansible.builtin.file"} {
		for _, key := range []string{"dest", "path"} {
			out = append(out, text(mapping(task[module])[key]))
		}
	}
	for _, item := range loop {
		out = append(out, text(mapping(item)["path"]))
	}
	return slices.DeleteFunc(out, func(p string) bool { return p == "" })
}

// destinations are the paths the play makes, as a host has them. A path built from an item the
// test cannot resolve stays as the play spells it and matches nothing.
func destinations(t *testing.T, digest, version string) []string {
	t.Helper()
	var out []string
	for _, task := range flatten(loadPlay(t).Tasks) {
		for _, p := range spelled(task) {
			out = append(out, rendered(t, p, digest, version))
		}
	}
	return out
}

// notInstalled are the binaries the unit texts run that are no destination.
func notInstalled(texts, dests []string) []string {
	var out []string
	for _, text := range texts {
		for _, bin := range unitfiles.ExecBinaries(text) {
			if !slices.Contains(dests, bin) && !slices.Contains(out, bin) {
				out = append(out, bin)
			}
		}
	}
	return out
}

const (
	stubDigest  = "0123456789abcdef0123456789abcdef01234567"
	stubVersion = "1.9.4"
)

// unitTexts are the texts a unit runs a binary from: every file the play installs, and the two
// drop-ins `team apply` renders for a team, which carry the homeserver's and the terminator's
// ExecStart=.
func unitTexts(t *testing.T) []string {
	t.Helper()
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	version, err := paths.ParseServerVersion(stubVersion)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, f := range unitfiles.Files() {
		texts = append(texts, f.Text)
	}
	server, terminator := host.DropIns(team, 8448, host.Pin{Version: version, MemoryHigh: "512M", CacheCapacityModifier: "0.5"},
		[]netip.Addr{netip.MustParseAddr("192.0.2.10")}, []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, nil)
	return append(texts, string(server), string(terminator))
}

// Every binary a unit runs is a file the play installs: the admin program the health and backup
// units run, the member program the terminator runs and the pinned homeserver binary. A unit
// whose binary no task installs would fail at its first start on a real host, long after the play
// said it was done.
func TestEveryBinaryAUnitRunsIsInstalledByThePlay(t *testing.T) {
	dests := destinations(t, stubDigest, stubVersion)
	if missing := notInstalled(unitTexts(t), dests); len(missing) > 0 {
		t.Errorf("the units run %q, which no task of %s installs; its destinations are %q", missing, paths.PlayFile, dests)
	}
	for _, want := range []string{paths.AdminBinaryPath, paths.MemberBinaryPath} {
		if !slices.Contains(unitfiles.ExecBinaries(strings.Join(unitTexts(t), "\n")), want) {
			t.Errorf("no unit runs %s, so the check above does not cover it", want)
		}
	}
}

// The check above can fail: a unit that runs a binary no task installs is named, one that runs an
// installed binary is not, and a prefix of an installed path is no match.
func TestTheInstalledBinaryCheckDiscriminates(t *testing.T) {
	texts := []string{
		"[Service]\nExecStart=/usr/local/bin/missing --flag\nExecStart=-/usr/local/bin/there\n",
		"[Service]\nExecStart=/usr/local/bin\n",
	}
	got := notInstalled(texts, []string{"/usr/local/bin/there", "/usr/local/bin/missing-too"})
	if want := []string{"/usr/local/bin/missing", "/usr/local/bin"}; !slices.Equal(got, want) {
		t.Errorf("notInstalled = %q, want %q", got, want)
	}
}

// writablePaths are the directories the installed services name in ReadWritePaths=, none of them
// optional (a "-" prefix would hide a directory that is not there, which a unit's namespace setup
// refuses without it) and none a template's: a team's directories are made by `team apply`.
func writablePaths() []string {
	var out []string
	for _, f := range unitfiles.Files() {
		for line := range strings.SplitSeq(f.Text, "\n") {
			value, ok := strings.CutPrefix(line, "ReadWritePaths=")
			if !ok {
				continue
			}
			for p := range strings.FieldsSeq(value) {
				if !strings.HasPrefix(p, "-") && !strings.Contains(p, "%") && !slices.Contains(out, p) {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// tmpfilesPaths are the directories the tmpfiles entry makes.
func tmpfilesPaths(t *testing.T) []string {
	t.Helper()
	f, ok := unitfiles.Lookup("teamchat.conf")
	if !ok {
		t.Fatal("the play installs no tmpfiles entry")
	}
	var out []string
	for line := range strings.SplitSeq(f.Text, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && fields[0] == "d" {
			out = append(out, fields[1])
		}
	}
	return out
}

// Every directory a service may write exists before the service runs: the play makes it, or the
// tmpfiles entry does at every boot (the lock directory under /run is empty after one).
func TestEveryDirectoryAUnitWritesIsMadeBeforeItRuns(t *testing.T) {
	made := slices.Concat(destinations(t, stubDigest, stubVersion), tmpfilesPaths(t))
	writable := writablePaths()
	if len(writable) < 3 {
		t.Fatalf("the units name %d writable paths, which is too few to be all of them: %q", len(writable), writable)
	}
	for _, p := range writable {
		if !slices.Contains(made, p) {
			t.Errorf("a unit may write %s, which neither the play nor the tmpfiles entry makes", p)
		}
	}
}

// The tmpfiles entry is acted on in the play: written with the other files, then created from,
// before the first team is applied and the timers started.
func TestThePlayCreatesTheTmpfilesDirectoriesBeforeTheTeams(t *testing.T) {
	play := loadPlay(t)
	if got := text(play.Vars["tc_tmpfiles_file"]); got != paths.TmpfilesFile {
		t.Errorf("tc_tmpfiles_file = %q, want %q", got, paths.TmpfilesFile)
	}
	index := func(name string) int {
		for i, task := range flatten(play.Tasks) {
			if task["name"] == name {
				return i
			}
		}
		return -1
	}
	written, made, applied, timers := index("Write Each Unit"), index("Make the Tmpfiles Directories"), index("Apply the Present Teams"), index("Run the Health and Backup Timers While a Team Is Present")
	if written < 0 || made < 0 || applied < 0 || timers < 0 || written >= made || made >= applied || applied >= timers {
		t.Fatalf("the order of the tasks is wrong or one is missing: write %d, tmpfiles %d, apply %d, timers %d", written, made, applied, timers)
	}
	task := flatten(play.Tasks)[made]
	argv := sequence(mapping(task["ansible.builtin.command"])["argv"])
	if len(argv) != 3 || argv[0] != "systemd-tmpfiles" || argv[1] != "--create" || argv[2] != "{{ tc_tmpfiles_file }}" {
		t.Errorf("the tmpfiles task runs %v, want systemd-tmpfiles --create {{ tc_tmpfiles_file }}", argv)
	}
}
