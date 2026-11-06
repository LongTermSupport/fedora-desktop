package v1names_test

import (
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/v1names"
)

// unit is one unit text the Go code makes, as the directive lines systemd would read from it.
type unit struct {
	name  string
	lines []string
}

// missing is the paths of D 4.2 that no InaccessiblePaths= line of the unit lists, each with its -.
func (u unit) missing() []string {
	var listed []string
	for _, line := range u.lines {
		if value, ok := strings.CutPrefix(line, directiveKey); ok {
			listed = append(listed, strings.Fields(value)...)
		}
	}
	var absent []string
	for _, want := range d42 {
		if !slices.Contains(listed, want) {
			absent = append(absent, want)
		}
	}
	return absent
}

// properties are an argv's --property= values: the directive lines of a transient unit.
func properties(argv []string) []string {
	var lines []string
	for _, arg := range argv {
		if line, ok := strings.CutPrefix(arg, "--property="); ok {
			lines = append(lines, line)
		}
	}
	return lines
}

// madeUnits is every unit text the Go code makes today that D 4.2 names or that inherits from
// one: the base unit's sandbox (bare and for a team), the restore's staging unit made from a
// team's sandbox the way the wiring makes it, its secure unit, and the backup service.
func madeUnits(t *testing.T) []unit {
	t.Helper()
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	version, err := paths.ParseServerVersion("1.9.4")
	if err != nil {
		t.Fatal(err)
	}
	staging, err := restore.StagingArgv(restore.Staging{Team: team, Backup: 7, Version: version, Port: 8448, Sandbox: host.TeamSandbox(team)}, true)
	if err != nil {
		t.Fatal(err)
	}
	secure, err := restore.SecureArgv(team, "/usr/local/lib/teamchat/teamchat-admin-abc")
	if err != nil {
		t.Fatal(err)
	}
	return []unit{
		{"the base unit's sandbox", host.BaseSandbox()},
		{"a team's sandbox", host.TeamSandbox(team)},
		{"the restore's staging unit", properties(staging)},
		{"the restore's secure unit", properties(secure)},
		{"the backup service", strings.Split(backup.ServiceUnit, "\n")},
	}
}

// D 4.2, seam 49: every unit text v2's Go makes lists all six v1 paths, each with its -.
func TestEveryUnitTextCarriesTheSixV1Paths(t *testing.T) {
	for _, u := range madeUnits(t) {
		if absent := u.missing(); len(absent) > 0 {
			t.Errorf("%s does not list %q as inaccessible:\n%s", u.name, absent, strings.Join(u.lines, "\n"))
		}
	}
}

// The check above can fail: a unit whose only such line is another path's lacks all six, one with
// a path spelt without its - lacks that path, and the six on one line or on two pass.
func TestTheUnitCheckDiscriminates(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  []string
	}{
		{"another path's line", []string{"InaccessiblePaths=-/run/dbus"}, d42},
		{"a path without its dash", []string{strings.Replace(v1names.InaccessiblePaths, d42[3], d42[3][1:], 1)}, []string{d42[3]}},
		{"two lines", []string{directiveKey + strings.Join(d42[:3], " "), directiveKey + strings.Join(d42[3:], " ")}, nil},
		{"all six", []string{v1names.InaccessiblePaths}, nil},
	}
	for _, c := range cases {
		if got := (unit{c.name, c.lines}).missing(); !slices.Equal(got, c.want) {
			t.Errorf("%s: missing %q, want %q", c.name, got, c.want)
		}
	}
}

// unitSources are the Go files of internal/teamchat that make unit text (they hold a [Service]
// section or a --property= argument), and why each is, or is not, one of madeUnits.
var unitSources = []struct{ file, why string }{
	{"backup/units.go", "the backup service, in madeUnits"},
	{"host/lxc/unit/unit.go", "a seat's session unit, which runs inside an LXC guest (C 3.9.4): not a host unit D 4.2 names"},
	{"host/render.go", "a team's drop-in, which extends the base unit that carries the line"},
	{"restore/argv.go", "the restore's staging and secure units, in madeUnits"},
}

// A file that makes unit text and is in no row of unitSources is a unit nobody has judged: a new
// unit's text must join madeUnits (or say here why D 4.2 does not apply) before it can pass.
func TestEveryUnitTextSourceIsJudged(t *testing.T) {
	root := os.DirFS("..")
	var found []string
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && d.Name() == "testdata":
			return fs.SkipDir
		case d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go"):
			return nil
		}
		data, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		if text := string(data); strings.Contains(text, "[Service]") || strings.Contains(text, "--property=") {
			found = append(found, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	judged := make([]string, 0, len(unitSources))
	for _, s := range unitSources {
		judged = append(judged, s.file)
	}
	if !slices.Equal(found, judged) {
		t.Fatalf("Go files making unit text: %q\njudged in unitSources: %q", found, judged)
	}
}
