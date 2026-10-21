package paths

import (
	"errors"
	"strings"
	"testing"
)

// The expected values are D 3.2-3.8 and B's header, written out in full: this table is the spec
// the package is held to.
func TestTeamDerivedNames(t *testing.T) {
	team := mustTeam(t, "ops")
	for _, c := range []struct{ got, want string }{
		{HomeserverUser(team), "teamchat-ops"},
		{ServerName(team), "ops.teamchat.internal"},
		{ServerUnit(team), "teamchat-server@ops.service"},
		{ServerUnitDropIn(team), "teamchat-server@ops.service.d"},
		{ServerRuntimeDirectory(team), "teamchat/ops"},
		{FenceUnit(team), "teamchat-fence@ops.service"},
		{FenceTable(team), "teamchat_fence_ops"},
		{HomeserverState(team), "/var/lib/teamchat/ops"},
		{HomeserverPrivateState(team), "/var/lib/private/teamchat/ops"},
		{AdminState(team), "/var/lib/teamchat-admin/ops"},
		{ServerConfig(team), "/var/lib/teamchat-admin/ops/config.toml"},
		{TeamDeclaration(team), "/var/lib/teamchat-admin/ops/team.json"},
		{RestoreBlock(team), "/var/lib/teamchat-admin/ops/restore.block"},
		{MigrationDir(team), "/var/lib/teamchat-admin/ops/migration"},
		{FenceFile(team), "/var/lib/teamchat-admin/ops/migration/fence.nft"},
		{FirewallRecord(team), "/var/lib/teamchat-admin/ops/firewall.json"},
		{CtlState(team), "/var/lib/teamchat-ctl/ops"},
		{TeamLock(team), "/run/teamchat-admin/team-ops.lock"},
		{StatusTeamDir(team), "/var/lib/teamchat-status/teams/ops"},
		{StampFile(team), "/var/lib/teamchat-status/teams/ops/stamp.json"},
		{StatusFile(team), "/var/lib/teamchat-status/teams/ops/status.json"},
		{ApprovedFile(team), "/var/lib/teamchat-status/teams/ops/approved.json"},
		{LXCOnlineFile(team), "/var/lib/teamchat-status/teams/ops/lxc-online.json"},
		{LXCRecord(team), "/var/lib/teamchat-status/lxc/ops.json"},
		{ElementProfile(team), "teamchat-ops"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestOtherIDDerivedNames(t *testing.T) {
	set, err := ParseSetID("s1")
	if err != nil {
		t.Fatal(err)
	}
	user, err := ParseGuestUser("dev")
	if err != nil {
		t.Fatal(err)
	}
	version, err := ParseServerVersion("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ParseDigest("abc123")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ got, want string }{
		{SetRecord(set), "/var/lib/teamchat-status/sets/s1.json"},
		{SetLock(set), "/var/lib/teamchat-admin/_sets/s1.lock"},
		{SessionUnit(user), "teamchat-session-dev.service"},
		{GuestRuntimeDir(user), "/run/teamchat-session-dev"},
		{ServerBinary(version), "/usr/local/lib/teamchat/server/tuwunel-1.2.3"},
		{AdminBinaryCopy(digest), "/usr/local/lib/teamchat/teamchat-admin-abc123"},
		{string(HostKitDir(digest)), "/usr/local/lib/teamchat/kit/abc123"},
		{HostKitCurrent.Binary(), "/usr/local/lib/teamchat/kit/current/bin/teamchat"},
		{CCYKitDir.PluginDir(), "/opt/claude-yolo/optional/teamchat/plugin/teamchat"},
		{CCYKitDir.PluginHooks(), "/opt/claude-yolo/optional/teamchat/plugin/teamchat/hooks/hooks.json"},
		{CCYKitDir.Settings(), "/opt/claude-yolo/optional/teamchat/settings.json"},
		{ElementLauncherName(mustDisplay(t, "Ops")), "Ops team (teamchat)"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestFixedNames(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{MemberBinaryPath, "/usr/local/bin/teamchat"},
		{AdminBinaryPath, "/usr/local/sbin/teamchat-admin"},
		{InstallBinary, "teamchat-install"},
		{SkillName, "teamchat:teamchat"},
		{CtlUser, "teamchat-ctl"},
		{Interface, "teamchat0"},
		{ServerUnitTemplate, "teamchat-server@.service"},
		{FenceUnitTemplate, "teamchat-fence@.service"},
		{ServerStateDirectory, "teamchat/%i"},
		{HealthService, "teamchat-health.service"},
		{HealthTimer, "teamchat-health.timer"},
		{BackupService, "teamchat-backup.service"},
		{BackupTimer, "teamchat-backup.timer"},
		{SeatsService, "teamchat-seats.service"},
		{SeatsTimer, "teamchat-seats.timer"},
		{SyslogServer, "teamchat-server"},
		{SyslogHealth, "teamchat-health"},
		{SyslogBackup, "teamchat-backup"},
		{ServerPinFile, "/usr/local/lib/teamchat/server/tuwunel.pin"},
		{ShareDir, "/usr/local/share/teamchat"},
		{ResolverStub, "/usr/local/share/teamchat/resolv.conf"},
		{AdminLXCDir, "/var/lib/teamchat-admin/_lxc"},
		{OwnerGroupFile, "/etc/teamchat/owner-group"},
		{StatusInstalledFile, "/var/lib/teamchat-status/installed.json"},
		{AdminRunDir, "/run/teamchat-admin"},
		{string(HostKitCurrent), "/usr/local/lib/teamchat/kit/current"},
		{SessionSeatMount, "/run/teamchat/seats"},
		{SessionMembershipMount, "/run/teamchat/membership"},
		{MatrixPrefix, "teamchat."},
		{EnvPrefix, "TEAMCHAT_"},
		{EnvRuntime, "TEAMCHAT_RUNTIME"},
		{EnvKit, "TEAMCHAT_KIT"},
		{EnvStatusLineInner, "TEAMCHAT_STATUS_LINE_INNER"},
		{HostVarTeams, "teamchat_teams"},
		{HostVarAddress, "teamchat_address"},
		{HostVarLXCMembers, "teamchat_lxc_members"},
		{HostVarElementTeams, "teamchat_element_teams"},
		{HostVarMigrations, "teamchat_migrations"},
		{OnlyTeamsVar, "teamchat_only_teams"},
		{PlayFile, "playbooks/imports/play-teamchat.yml"},
		{ElementPlayFile, "playbooks/imports/play-teamchat-element.yml"},
		{AnsibleTag, "teamchat"},
		{TeamPortField, "port"},
		{TmuxSocket, "teamchat"},
		{TmuxSession, "teamchat"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestCCYLayout(t *testing.T) {
	l := CCYLayout()
	team := mustTeam(t, "ops")
	run := mustRun(t)
	for _, c := range []struct{ got, want string }{
		{l.KitRoot, "/opt/claude-yolo/optional/teamchat"},
		{string(l.CurrentKit), "/opt/claude-yolo/optional/teamchat"},
		{l.State, "/run/teamchat/seats"},
		{l.Desired, "/run/teamchat/membership"},
		{l.RunRoot, "/tmp/teamchat"},
		{l.DesiredFile(), "/run/teamchat/membership/desired.json"},
		{l.ConversationFile(), "/run/teamchat/seats/conversation.json"},
		{l.Seat(team).Dir, "/run/teamchat/seats/ops"},
		{l.Run(run).Dir, "/tmp/teamchat/0123456789abcdef"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestHomeLayout(t *testing.T) {
	l, err := HomeLayout("/home/u", "/run/user/1000")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ got, want string }{
		{l.KitRoot, "/usr/local/lib/teamchat/kit"},
		{string(l.CurrentKit), "/usr/local/lib/teamchat/kit/current"},
		{l.State, "/home/u/.config/teamchat/seats"},
		{l.Desired, "/home/u/.config/teamchat/membership"},
		{l.RunRoot, "/run/user/1000/teamchat/run"},
		{l.Run(mustRun(t)).Dir, "/run/user/1000/teamchat/run/0123456789abcdef"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestSessionRuntimeFiles(t *testing.T) {
	r := CCYLayout().Run(mustRun(t))
	for _, c := range []struct{ got, want string }{
		{r.KeeperLock(), "keeper.lock"},
		{r.KeeperJSON(), "keeper.json"},
		{r.KeeperLog(), "keeper.log"},
		{r.LinkSock(), "link.sock"},
		{r.LinkLock(), "link.lock"},
		{r.LinkLog(), "link.log"},
		{r.Settings(), "settings.json"},
		{r.Session(), "session.json"},
		{r.PID(), "session.pid"},
		{r.Status(), "status.json"},
	} {
		if want := r.Dir + "/" + c.want; c.got != want {
			t.Errorf("got %q, want %q", c.got, want)
		}
	}
}

func TestSeatFiles(t *testing.T) {
	s := CCYLayout().Seat(mustTeam(t, "ops"))
	for _, c := range []struct{ got, want string }{
		{s.Bundle(), "seat.json"},
		{s.Token(), "token"},
		{s.Lock(), "seat.lock"},
		{s.Store(), "store.json"},
		{s.Report(), "report.json"},
		{s.LastLog(), "last.log"},
	} {
		if want := "/run/teamchat/seats/ops/" + c.want; c.got != want {
			t.Errorf("got %q, want %q", c.got, want)
		}
	}
}

func TestOwnerSeatStore(t *testing.T) {
	stateHome, err := StateHome("/home/u", "")
	if err != nil {
		t.Fatal(err)
	}
	store, err := SeatStore(stateHome)
	if err != nil {
		t.Fatal(err)
	}
	session, err := ParseSessionName("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	key, err := ParseCheckoutKey(strings.Repeat("ab", 16))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := AllocLock("/run/user/1000", key)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := ProbeState(stateHome, mustTeam(t, "ops"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ got, want string }{
		{stateHome, "/home/u/.local/state"},
		{probe, "/home/u/.local/state/teamchat/probe/ops"},
		{store.PoolCheckoutRecord(key), "/home/u/.local/state/teamchat/seats/pool/" + strings.Repeat("ab", 16) + "/checkout.json"},
		{store.Root, "/home/u/.local/state/teamchat/seats"},
		{store.Pool(), "/home/u/.local/state/teamchat/seats/pool"},
		{store.PoolCheckout(key), "/home/u/.local/state/teamchat/seats/pool/" + strings.Repeat("ab", 16)},
		{store.Session(session), "/home/u/.local/state/teamchat/seats/sessions/sess-1"},
		{store.Control(session), "/home/u/.local/state/teamchat/seats/control/sess-1"},
		{store.DesiredFile(session), "/home/u/.local/state/teamchat/seats/control/sess-1/desired.json"},
		{store.Released(), "/home/u/.local/state/teamchat/seats/released"},
		{lock, "/run/user/1000/teamchat/alloc/" + strings.Repeat("ab", 16) + ".lock"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// $XDG_STATE_HOME is honoured when it is an absolute path, as the XDG spec says; a relative one
// is ignored.
func TestStateHomeHonoursAnAbsoluteXDGStateHome(t *testing.T) {
	for _, c := range []struct{ xdg, want string }{
		{"/srv/state", "/srv/state"},
		{"relative/state", "/home/u/.local/state"},
	} {
		got, err := StateHome("/home/u", c.xdg)
		if err != nil || got != c.want {
			t.Errorf("StateHome(%q) = %q, %v; want %q", c.xdg, got, err, c.want)
		}
	}
	for _, root := range []string{"/", "//", "/.."} {
		if got, err := StateHome("/home/u", root); !errors.Is(err, ErrInvalid) {
			t.Errorf("StateHome(%q) = %q, %v; want ErrInvalid", root, got, err)
		}
	}
}

func TestElementPaths(t *testing.T) {
	team := mustTeam(t, "ops")
	dir, err := ElementConfigDir("/home/u", team)
	if err != nil {
		t.Fatal(err)
	}
	launcher, err := ElementLauncher("/home/u", team)
	if err != nil {
		t.Fatal(err)
	}
	consent, err := ElementConsentFile("/home/u")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ got, want string }{
		{dir, "/home/u/.var/app/im.riot.Riot/config/Element-teamchat-ops"},
		{launcher, "/home/u/.local/share/applications/teamchat-ops-element.desktop"},
		{consent, "/home/u/.config/teamchat/element-consent"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// Every function that takes a base directory refuses one that is relative or not clean, so a
// path built here never lands somewhere its caller did not mean.
func TestBaseDirectoriesMustBeAbsoluteAndClean(t *testing.T) {
	team := mustTeam(t, "ops")
	key, err := ParseCheckoutKey(strings.Repeat("ab", 16))
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]func(string) error{
		"HomeLayout home":    func(d string) error { _, err := HomeLayout(d, "/run/user/1"); return err },
		"HomeLayout runtime": func(d string) error { _, err := HomeLayout("/home/u", d); return err },
		"StateHome":          func(d string) error { _, err := StateHome(d, ""); return err },
		"SeatStore":          func(d string) error { _, err := SeatStore(d); return err },
		"AllocLock":          func(d string) error { _, err := AllocLock(d, key); return err },
		"ElementConfigDir":   func(d string) error { _, err := ElementConfigDir(d, team); return err },
		"ElementLauncher":    func(d string) error { _, err := ElementLauncher(d, team); return err },
		"ElementConsentFile": func(d string) error { _, err := ElementConsentFile(d); return err },
		"ProbeState":         func(d string) error { _, err := ProbeState(d, team); return err },
	}
	for name, call := range calls {
		// The root is refused too: joined to it, a name would begin with two slashes.
		for _, bad := range []string{"", "home/u", "/home/u/", "/home/../u", "/home//u", "/"} {
			if err := call(bad); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s(%q) = %v, want ErrInvalid", name, bad, err)
			}
		}
	}
}

func mustRun(t *testing.T) RunID {
	t.Helper()
	r, err := ParseRunID("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
