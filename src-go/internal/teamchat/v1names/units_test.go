package v1names_test

import (
	"io/fs"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/unitfiles"
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
// team's sandbox the way the wiring makes it, its secure unit, the backup service, and every
// service the play installs (the homeserver's and the terminator's base units, the health and
// backup services).
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
	return slices.Concat([]unit{
		{"the base unit's sandbox", host.BaseSandbox()},
		{"a team's sandbox", host.TeamSandbox(team)},
		{"the restore's staging unit", properties(staging)},
		{"the restore's secure unit", properties(secure)},
		{"the backup service", strings.Split(backup.ServiceUnit, "\n")},
	}, installedServices())
}

// installedServices is every file the play installs that runs a process (it has a [Service]
// section), as the play installs it: the text `teamchat-admin units print` writes. The other
// files are timers, which start a service and run nothing themselves, and the resolver stub.
func installedServices() []unit {
	var out []unit
	for _, f := range unitfiles.Files() {
		if strings.Contains(f.Text, "[Service]") {
			out = append(out, unit{"the installed " + f.Name, strings.Split(f.Text, "\n")})
		}
	}
	return out
}

// Every installed file that is a unit is a service or a timer, so installedServices holds every
// installed unit that runs a process: a new kind of unit file must say here how it is judged.
func TestEveryInstalledFileIsJudgedOrRunsNothing(t *testing.T) {
	var services []string
	for _, f := range unitfiles.Files() {
		switch {
		case strings.Contains(f.Text, "[Service]"):
			services = append(services, f.Name)
		case strings.HasSuffix(f.Name, ".timer") && strings.Contains(f.Text, "[Timer]"):
		case f.Path == paths.ResolverStub:
		default:
			t.Errorf("installed file %s is neither a service, a timer nor the resolver stub", f.Name)
		}
	}
	want := []string{paths.ServerUnitTemplate, paths.TLSUnitTemplate, paths.HealthService, paths.BackupService}
	if !slices.Equal(services, want) {
		t.Errorf("installed services %q, want %q", services, want)
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

// filterPairs are the allow directives of a unit's IP filter and of its socket bind, each with the
// deny that makes the allow restrict anything: systemd grants what no rule denies, so an
// allow-list alone restricts nothing (systemd.resource-control(5)).
var filterPairs = [][2]string{{"IPAddressAllow=", "IPAddressDeny=any"}, {"SocketBindAllow=", "SocketBindDeny=any"}}

// unpaired is the allow directives of lines that the same lines do not pair with their deny of
// any: each is a filter that lets everything in.
func unpaired(lines []string) []string {
	var out []string
	for _, pair := range filterPairs {
		allows := slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, pair[0]) })
		if allows && !slices.Contains(lines, pair[1]) {
			out = append(out, pair[0])
		}
	}
	return out
}

// filteredUnits is every unit text the Go code makes, judged for its filter: madeUnits and a
// team's two drop-ins, the homeserver's and the terminator's. A drop-in is judged alone: its base
// unit is installed by a play (or, for the terminator's, by nothing yet), so a deny there is not
// the drop-in's code's to promise, and the drop-in that allows must deny.
func filteredUnits(t *testing.T) []unit {
	t.Helper()
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	version, err := paths.ParseServerVersion("1.9.4")
	if err != nil {
		t.Fatal(err)
	}
	pin := host.Pin{Version: version, MemoryHigh: "512M", CacheCapacityModifier: "0.5"}
	listen := []netip.Addr{netip.MustParseAddr("192.0.2.10")}
	networks := []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
	server, terminator := host.DropIns(team, 8448, pin, listen, networks, []string{"sys-subsystem-net-devices-eth0.device"})
	return append(madeUnits(t),
		unit{"the homeserver's drop-in", strings.Split(string(server), "\n")},
		unit{"the terminator's drop-in", strings.Split(string(terminator), "\n")})
}

// B3 of PR #128's review, as a class: every unit text v2's Go makes that allows addresses or
// binds denies all others in the same text.
func TestEveryUnitTextDeniesWhatItDoesNotAllow(t *testing.T) {
	for _, u := range filteredUnits(t) {
		if open := unpaired(u.lines); len(open) > 0 {
			t.Errorf("%s has %q with no deny of any, so it restricts nothing:\n%s", u.name, open, strings.Join(u.lines, "\n"))
		}
	}
}

// The filter check can fail: an allow alone, an allow beside a narrower deny, and a socket bind
// allow alone are open; an allow with its deny of any, and no allow at all, are not.
func TestTheFilterCheckDiscriminates(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  []string
	}{
		{"an IP allow alone", []string{"IPAddressAllow=127.0.0.1/32"}, []string{"IPAddressAllow="}},
		{"a narrower deny", []string{"IPAddressDeny=192.0.2.0/24", "IPAddressAllow=127.0.0.1/32"}, []string{"IPAddressAllow="}},
		{"a bind allow alone", []string{"IPAddressDeny=any", "IPAddressAllow=::1/128", "SocketBindAllow=tcp:8448"}, []string{"SocketBindAllow="}},
		{"both paired", []string{"IPAddressDeny=any", "IPAddressAllow=::1/128", "SocketBindDeny=any", "SocketBindAllow=tcp:8448"}, nil},
		{"no allow", []string{"IPAddressDeny=any"}, nil},
	}
	for _, c := range cases {
		if got := unpaired(c.lines); !slices.Equal(got, c.want) {
			t.Errorf("%s: open %q, want %q", c.name, got, c.want)
		}
	}
}

// unitSources are the Go files of internal/teamchat that make unit text (they hold a [Service]
// section or a --property= argument), and why each is, or is not, one of madeUnits.
var unitSources = []struct{ file, why string }{
	{"backup/units.go", "the backup service, in madeUnits"},
	{"host/lxc/unit/unit.go", "a seat's session unit, which runs inside an LXC guest (C 3.9.4): not a host unit D 4.2 names"},
	{"host/render.go", "a team's drop-in, which extends the base unit that carries the six paths; its filter is judged alone, in filteredUnits"},
	{"host/tlsunit.go", "the terminator's per-team drop-in, which extends the base unit teamchat-tls@ that unitfiles makes, in madeUnits; the drop-in's filter is judged alone, in filteredUnits"},
	{"restore/argv.go", "the restore's staging and secure units, in madeUnits"},
	{"unitfiles/unitfiles.go", "every file the play installs: each service (the homeserver's and the terminator's base units, the health and backup services) in madeUnits as installedServices; the timers run nothing and the resolver stub is no unit (TestEveryInstalledFileIsJudgedOrRunsNothing)"},
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
