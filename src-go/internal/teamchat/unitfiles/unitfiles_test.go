package unitfiles

import (
	"bytes"
	"flag"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/testfiles"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/tlsterm"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/v1names"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/unitsandbox"
)

var update = flag.Bool("update", false, "rewrite the golden files from the texts")

func lines(text string) []string { return strings.Split(strings.TrimSuffix(text, "\n"), "\n") }

func file(t *testing.T, name string) File {
	t.Helper()
	f, ok := Lookup(name)
	if !ok {
		t.Fatalf("no file %s", name)
	}
	return f
}

// The play installs these, in this order, each where paths says.
func TestFilesAreWhatThePlayInstalls(t *testing.T) {
	want := [][2]string{
		{paths.ServerUnitTemplate, paths.ServerUnitTemplateFile},
		{paths.TLSUnitTemplate, paths.TLSUnitTemplateFile},
		{paths.HealthService, paths.HealthServiceFile},
		{paths.HealthTimer, paths.HealthTimerFile},
		{paths.BackupService, paths.BackupServiceFile},
		{paths.BackupTimer, paths.BackupTimerFile},
		{filepath.Base(paths.ResolverStubFile), paths.ResolverStubFile},
		{filepath.Base(paths.ResolverFile), paths.ResolverFile},
		{filepath.Base(paths.TmpfilesFile), paths.TmpfilesFile},
	}
	got := make([][2]string, 0, len(want))
	for _, f := range Files() {
		got = append(got, [2]string{f.Name, f.Path})
		if f.Text == "" || !strings.HasSuffix(f.Text, "\n") || strings.HasSuffix(f.Text, "\n\n") {
			t.Errorf("%s: the text is empty or does not end in exactly one newline: %q", f.Name, f.Text)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("files %q, want %q", got, want)
	}
}

// sixPathsLine stands in a golden file for D 4.2's line, as it does in C 3.2.3's block: v1's
// names live in package v1names alone (the teamchat-keeps-off-v1 gate).
const sixPathsLine = "InaccessiblePaths=<D 4.2's six v1 paths, each with its \"-\">"

// Each text is its golden file, which changes only when its source does (go test -update
// rewrites them; the server's golden is C 3.2.3's block as the design writes it).
func TestEachTextIsItsGolden(t *testing.T) {
	for _, f := range Files() {
		name := f.Name + ".golden"
		golden := strings.ReplaceAll(f.Text, v1names.InaccessiblePaths+"\n", sixPathsLine+"\n")
		if *update {
			if err := os.WriteFile(filepath.Join("testdata", name), []byte(golden), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if want := testfiles.Read(t, "testdata", name); !bytes.Equal([]byte(golden), want) {
			t.Errorf("%s differs from testdata/%s:\n got:\n%s\nwant:\n%s", f.Name, name, f.Text, want)
		}
	}
}

// The backup units are package backup's, not a copy.
func TestTheBackupUnitsAreBackups(t *testing.T) {
	if file(t, paths.BackupService).Text != backup.ServiceUnit || file(t, paths.BackupTimer).Text != backup.TimerUnit {
		t.Fatal("the backup units are not backup.ServiceUnit and backup.TimerUnit")
	}
}

// The server unit holds host.BaseSandbox's lines in their order, so the restore's staging unit,
// made from the same lines, cannot drift from it; its %i paths are the team's paths.
func TestTheServerUnitIsTheBaseSandbox(t *testing.T) {
	text := file(t, paths.ServerUnitTemplate).Text
	got := lines(text)
	at := 0
	for _, want := range host.BaseSandbox() {
		i := slices.Index(got[at:], want)
		if i < 0 {
			t.Fatalf("the server unit lacks the sandbox line %q after line %d:\n%s", want, at, text)
		}
		at += i + 1
	}
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	team1 := strings.ReplaceAll(text, "%i", team.String())
	for _, want := range []string{
		"AssertPathExists=!" + paths.RestoreBlock(team) + "\n",
		"LoadCredential=config.toml:" + paths.ServerConfig(team) + "\n",
		"User=" + paths.HomeserverUser(team) + "\n",
	} {
		if !strings.Contains(team1, want) {
			t.Errorf("the server unit for team %s lacks %q:\n%s", team, want, team1)
		}
	}
}

// X18a: the terminator's base unit.
func TestTheTerminatorUnitIsX18a(t *testing.T) {
	text := file(t, paths.TLSUnitTemplate).Text
	got := lines(text)
	server := paths.ServerUnitTemplate[:strings.Index(paths.ServerUnitTemplate, "@")+1] + "%i.service"
	for _, want := range []string{
		"DynamicUser=yes", "After=" + server, "Wants=" + server, "Restart=always",
		"RestrictAddressFamilies=AF_INET AF_INET6", "IPAddressDeny=any", "SocketBindDeny=any",
		"LimitNOFILE=" + strconv.Itoa(terminatorFileLimit), v1names.InaccessiblePaths,
	} {
		if !slices.Contains(got, want) {
			t.Errorf("the terminator unit lacks %q:\n%s", want, text)
		}
	}
	for _, key := range []string{"ExecStart=", "User=", "BindsTo=", "StateDirectory", "TemporaryFileSystem=", "BindReadOnlyPaths=", "LoadCredential="} {
		if slices.ContainsFunc(got, func(l string) bool { return strings.HasPrefix(l, key) }) {
			t.Errorf("the terminator unit has a %s line:\n%s", key, text)
		}
	}
	for _, line := range host.BaseSandbox() {
		if !terminatorDrops(line) && !slices.Contains(got, line) {
			t.Errorf("the terminator unit lacks the homeserver's hardening line %q", line)
		}
	}
}

// Both team units end in an [Install] section that multi-user.target wants, so an enabled
// instance is started at boot, and enabling one is a change systemd records rather than the no-op
// a static unit makes of it (issue 247).
func TestTheTeamUnitsAreWantedAtBoot(t *testing.T) {
	for _, name := range []string{paths.ServerUnitTemplate, paths.TLSUnitTemplate} {
		text := file(t, name).Text
		if !strings.HasSuffix(text, "\n[Install]\nWantedBy=multi-user.target\n") {
			t.Errorf("%s does not end in [Install] WantedBy=multi-user.target:\n%s", name, text)
		}
		if strings.Count(text, "[Install]") != 1 {
			t.Errorf("%s has more than one [Install]", name)
		}
	}
}

// The descriptor limit is above twice the connections the terminator holds (each a client's and
// the homeserver's), so tlsterm's DefaultMaxConns growing past it fails here.
func TestTheFileLimitHoldsTheTerminatorsConnections(t *testing.T) {
	if terminatorFileLimit <= 2*tlsterm.DefaultMaxConns+16 {
		t.Fatalf("LimitNOFILE=%d leaves no room above twice tlsterm.DefaultMaxConns (%d)", terminatorFileLimit, tlsterm.DefaultMaxConns)
	}
}

// writable is whether a ReadWritePaths= line of lines covers dir.
func writable(lines []string, dir string) bool {
	for _, line := range lines {
		value, ok := strings.CutPrefix(line, "ReadWritePaths=")
		if !ok {
			continue
		}
		for p := range strings.FieldsSeq(strings.TrimPrefix(value, "-")) {
			if dir == p || strings.HasPrefix(dir, p+"/") {
				return true
			}
		}
	}
	return false
}

// C 3.11.1's health unit, with what S12 and S13 need: the teams' leaves writable and nothing else
// under /var/lib, so the tick that renews a leaf cannot change the CA key, the approvals or the
// team's records; and the IP filter a drop-in can widen.
func TestTheHealthUnit(t *testing.T) {
	text := file(t, paths.HealthService).Text
	got := lines(text)
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{paths.StatusRoot, paths.AdminRunDir, paths.TLSDir(team), paths.LeafKeyFile(team), paths.LeafCertFile(team)} {
		if !writable(got, dir) {
			t.Errorf("the health unit cannot write %s:\n%s", dir, text)
		}
	}
	for _, dir := range []string{
		paths.AdminStateRoot, paths.AdminState(team), paths.CADir(team), paths.CAKeyFile(team), paths.CACertFile(team),
		paths.TeamDeclaration(team), paths.ServerConfig(team),
	} {
		if writable(got, dir) {
			t.Errorf("the health unit can write %s, which it may only read:\n%s", dir, text)
		}
	}
	for _, want := range []string{
		"ExecStart=" + paths.AdminBinaryPath + " health --all", "Type=oneshot",
		"CapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_CHOWN CAP_SETUID CAP_SETGID CAP_SYS_PTRACE",
		"IPAddressDeny=any", "IPAddressAllow=localhost", v1names.InaccessiblePaths,
	} {
		if !slices.Contains(got, want) {
			t.Errorf("the health unit lacks %q:\n%s", want, text)
		}
	}
	timer := lines(file(t, paths.HealthTimer).Text)
	for _, want := range []string{"OnActiveSec=30s", "OnUnitActiveSec=60s", "AccuracySec=5s", "WantedBy=timers.target"} {
		if !slices.Contains(timer, want) {
			t.Errorf("the health timer lacks %q", want)
		}
	}
}

func TestWritableDiscriminates(t *testing.T) {
	cases := []struct {
		lines []string
		dir   string
		want  bool
	}{
		{[]string{"ReadWritePaths=/var/lib/a /run/b"}, "/var/lib/a/x/tls", true},
		{[]string{"ReadWritePaths=/var/lib/a"}, "/var/lib/ab", false},
		{[]string{"ReadOnlyPaths=/var/lib/a"}, "/var/lib/a", false},
		{[]string{"ReadWritePaths=/run/b"}, "/run/b", true},
	}
	for _, c := range cases {
		if got := writable(c.lines, c.dir); got != c.want {
			t.Errorf("writable(%q, %s) = %v", c.lines, c.dir, got)
		}
	}
}

// The directories the units write that no unit and no state keeper makes (the lock directory is
// under /run, which a reboot empties; the leaves' root is the health unit's alone to write) are
// made by systemd-tmpfiles before any unit runs, and by nothing else: a ReadWritePaths= entry that
// does not exist fails the unit's namespace setup, and hiding that with a "-" would hide the fault.
func TestTheTmpfilesEntryMakesTheDirectoriesTheUnitsNeed(t *testing.T) {
	var entries []string
	for _, l := range lines(file(t, filepath.Base(paths.TmpfilesFile)).Text) {
		if l != "" && !strings.HasPrefix(l, "#") {
			entries = append(entries, l)
		}
	}
	want := []string{
		"d " + paths.AdminRunDir + " 0700 root root -",
		"d " + paths.TLSStateRoot + " 0700 root root -",
	}
	if !slices.Equal(entries, want) {
		t.Errorf("the tmpfiles entries are %q, want %q", entries, want)
	}
}

// The terminator stops after the LXC members, and its unit says why: the homeserver listens on
// loopback alone, so a guest's member reaches it only through the terminator.
func TestTheTerminatorSaysWhyItStopsAfterTheGuests(t *testing.T) {
	text := file(t, paths.TLSUnitTemplate).Text
	for _, want := range []string{"Before=lxc.service", "loopback"} {
		if !strings.Contains(text, want) {
			t.Errorf("the terminator unit lacks %q:\n%s", want, text)
		}
	}
}

// Both resolver files send every lookup to an address nothing listens on, and they are the
// directory the server unit binds over systemd's resolver directory.
func TestTheResolverStub(t *testing.T) {
	for _, path := range []string{paths.ResolverStubFile, paths.ResolverFile} {
		if filepath.Dir(path) != paths.ResolverDir {
			t.Errorf("%s is not in %s", path, paths.ResolverDir)
		}
		got := lines(file(t, filepath.Base(path)).Text)
		if !slices.Contains(got, "nameserver 127.0.0.1") || slices.ContainsFunc(got, func(l string) bool { return l != "nameserver 127.0.0.1" && !strings.HasPrefix(l, "#") }) {
			t.Fatalf("%s is not one loopback nameserver line: %q", path, got)
		}
	}
	if server := file(t, paths.ServerUnitTemplate).Text; !strings.Contains(server, "\nBindReadOnlyPaths="+paths.ResolverDir+":/run/systemd/resolve\n") {
		t.Errorf("the server unit does not bind %s over /run/systemd/resolve", paths.ResolverDir)
	}
}

// sourceKind is what a bind source in a teamchat unit is on the host: a file the play installs,
// a directory it makes, or neither, which the rule refuses.
func sourceKind(src string) unitsandbox.SourceKind {
	if slices.ContainsFunc(Files(), func(f File) bool { return f.Path == src }) {
		return unitsandbox.File
	}
	if slices.Contains(installedDirs, src) {
		return unitsandbox.Directory
	}
	return unitsandbox.Unknown
}

// installedDirs are the directories the play makes that a unit binds.
var installedDirs = []string{paths.ResolverDir}

// pidns-idmapped-state: no unit the play installs, and no team's transient sandbox, sets
// PrivatePIDs=yes with DynamicUser=yes and a StateDirectory= (systemd issue 41014: 226/NAMESPACE).
func TestNoUnitSetsPrivatePIDsWithAnIdmappedState(t *testing.T) {
	for name, text := range renderedUnits(t) {
		for _, f := range unitsandbox.CheckPIDNamespace(text) {
			t.Errorf("%s: %s", name, f)
		}
	}
}

// renderedUnits is every unit text the play installs, and team ops's transient sandbox, by name.
func renderedUnits(t *testing.T) map[string]string {
	t.Helper()
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	texts := map[string]string{"the sandbox of a transient unit for team ops": strings.Join(host.TeamSandbox(team), "\n")}
	for _, f := range Files() {
		texts[f.Name] = f.Text
	}
	return texts
}

// tmpfs-file-bind: no unit the play installs, and no team's transient sandbox, binds a file into
// a TemporaryFileSystem= (systemd cannot make the file mount point in a read-only tmpfs: 226/NAMESPACE).
func TestNoUnitBindsAFileInsideATmpfs(t *testing.T) {
	for name, text := range renderedUnits(t) {
		for _, f := range unitsandbox.Check(text, sourceKind) {
			t.Errorf("%s: %s", name, f)
		}
	}
}

func TestLookupKnowsOnlyTheFiles(t *testing.T) {
	for _, f := range Files() {
		if got, ok := Lookup(f.Name); !ok || got != f {
			t.Errorf("Lookup(%s) = %+v, %v", f.Name, got, ok)
		}
	}
	for _, name := range []string{"", "teamchat-server@ops.service", "resolv", paths.ResolverFile, paths.SeatsService} {
		if _, ok := Lookup(name); ok {
			t.Errorf("Lookup(%q) found a file", name)
		}
	}
	if got := strings.Join(Names(), " "); got != "teamchat-server@.service teamchat-tls@.service teamchat-health.service teamchat-health.timer teamchat-backup.service teamchat-backup.timer stub-resolv.conf resolv.conf teamchat.conf" {
		t.Errorf("Names() = %s", got)
	}
}

// ExecBinaries is what a test of the play will read: the binary each ExecStart= line of a unit
// text names, whatever its prefix; an empty ExecStart= (a reset) names none.
func TestExecBinaries(t *testing.T) {
	want := map[string][]string{
		paths.HealthService: {paths.AdminBinaryPath},
		paths.BackupService: {paths.AdminBinaryPath},
	}
	for _, f := range Files() {
		if got := ExecBinaries(f.Text); !slices.Equal(got, want[f.Name]) {
			t.Errorf("%s names %q, want %q", f.Name, got, want[f.Name])
		}
	}
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	version, err := paths.ParseServerVersion("1.9.4")
	if err != nil {
		t.Fatal(err)
	}
	server, terminator := host.DropIns(team, 8448, host.Pin{Version: version, MemoryHigh: "512M", CacheCapacityModifier: "0.5"},
		[]netip.Addr{netip.MustParseAddr("192.0.2.10")}, []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, nil)
	if got := ExecBinaries(string(terminator)); !slices.Equal(got, []string{paths.MemberBinaryPath}) {
		t.Errorf("the terminator's drop-in names %q", got)
	}
	if got := ExecBinaries(string(server)); !slices.Equal(got, []string{paths.ServerBinary(version)}) {
		t.Errorf("the homeserver's drop-in names %q", got)
	}
	odd := "[Service]\nExecStart=\nExecStart=-@:+!/usr/bin/a x y\nExecStartPre=/usr/bin/b\n  ExecStart = /usr/bin/c\nExecStart=/usr/bin/d\n"
	if got := ExecBinaries(odd); !slices.Equal(got, []string{"/usr/bin/a", "/usr/bin/c", "/usr/bin/d"}) {
		t.Errorf("ExecBinaries(%q) = %q", odd, got)
	}
}
