// Package paths is the one source of the names and paths v2 of the agent-team bus (product name
// teamchat) uses: binaries, units, users, host and member directories, the session runtime's
// files, environment variables, the Matrix namespace and the Ansible names (Plan 00209
// DESIGN-v2-D-migration.md section 3, and the five member path variables of area B's header).
// It holds every name that crosses an area or process boundary and every directory v2 owns; the
// task that first reads or writes a file inside one of those directories adds the file's name
// here, so no other v2 package writes a name of its own.
//
// A name derived from a team, run, session, kit or other identifier takes that identifier's
// type, which only its Parse function (for a kit, this package) makes, so a derived name never
// holds an unchecked string. A function that takes a base directory (a home, an XDG directory)
// refuses one that is not absolute and clean, or is the root.
//
// Every v2 name starts with teamchat (paths, binaries, units, users), teamchat_ (host_vars
// keys), TEAMCHAT_ (environment) or teamchat. (Matrix), or is a file name inside a directory
// that does (D 3.1). The package's tests hold every exported name to that, and to the v1 name
// list of the static gate teamchat-keeps-off-v1 (D 4.1 part 3).
package paths

import (
	"fmt"
	"path/filepath"
)

// Product is the product name every v2 name is formed from.
const Product = "teamchat"

// The Matrix namespace for custom event types and keys (A).
const (
	MatrixNamespace = Product
	MatrixPrefix    = Product + "."
)

// Environment variables (B 2.4.1). Every variable the member reads starts with EnvPrefix.
const (
	EnvPrefix          = "TEAMCHAT_"
	EnvRuntime         = EnvPrefix + "RUNTIME"
	EnvKit             = EnvPrefix + "KIT"
	EnvStatusLineInner = EnvPrefix + "STATUS_LINE_INNER"
)

// Binaries (D 3.3). InstallBinary is reserved for area C, should it split the installer out.
const (
	MemberBinary     = Product
	AdminBinary      = Product + "-admin"
	InstallBinary    = Product + "-install"
	MemberBinaryPath = "/usr/local/bin/" + MemberBinary
	AdminBinaryPath  = "/usr/local/sbin/" + AdminBinary
)

// The Claude Code plugin, its skill, and the LXC session's tmux socket and session (D 3.4, 3.6).
const (
	PluginName  = Product
	SkillName   = PluginName + ":" + Product
	TmuxSocket  = Product
	TmuxSession = Product
)

// Users and the network interface (D 3.5).
const (
	CtlUser   = Product + "-ctl"
	Interface = Product + "0"
)

// Host directories and files (D 3.5).
const (
	LibDir                     = "/usr/local/lib/" + Product
	ShareDir                   = "/usr/local/share/" + Product
	ResolverStub               = ShareDir + "/resolv.conf"
	EtcDir                     = "/etc/" + Product
	OwnerGroupFile             = EtcDir + "/owner-group"
	ServerDir                  = LibDir + "/server"
	ServerPinFile              = ServerDir + "/tuwunel.pin"
	HostKitRoot                = LibDir + "/kit"
	HomeserverStateRoot        = "/var/lib/" + Product
	HomeserverPrivateStateRoot = "/var/lib/private/" + Product
	AdminStateRoot             = "/var/lib/" + AdminBinary
	// AdminLXCDir holds C's LXC guest records. It starts with an underscore, which no team id
	// can, so it never meets a team's AdminState (C 3.9).
	AdminLXCDir         = AdminStateRoot + "/_lxc"
	CtlStateRoot        = "/var/lib/" + CtlUser
	StatusRoot          = "/var/lib/" + Product + "-status"
	StatusInstalledFile = StatusRoot + "/installed.json"
	AdminRunDir         = "/run/" + AdminBinary
)

// HostKitCurrent is the link, in the host kit root of digests, to the newest kit (D 3.5).
const HostKitCurrent Kit = HostKitRoot + "/current"

// Units and the names inside them (D 3.5). The health, backup and seats units are one each per
// host; the server and fence units are templates instanced by team id.
const (
	ServerUnitTemplate   = Product + "-server@.service"
	FenceUnitTemplate    = Product + "-fence@.service"
	ServerStateDirectory = Product + "/%i"
	HealthService        = Product + "-health.service"
	HealthTimer          = Product + "-health.timer"
	BackupService        = Product + "-backup.service"
	BackupTimer          = Product + "-backup.timer"
	SeatsService         = Product + "-seats.service"
	SeatsTimer           = Product + "-seats.timer"
	SyslogServer         = Product + "-server"
	SyslogHealth         = Product + "-health"
	SyslogBackup         = Product + "-backup"
)

// Ansible names (D 3.7, 3.8). TeamPortField is the port's key in a HostVarTeams entry.
const (
	HostVarTeams        = "teamchat_teams"
	HostVarAddress      = "teamchat_address"
	HostVarLXCMembers   = "teamchat_lxc_members"
	HostVarElementTeams = "teamchat_element_teams"
	HostVarMigrations   = "teamchat_migrations"
	OnlyTeamsVar        = "teamchat_only_teams"
	PlayFile            = "playbooks/imports/play-teamchat.yml"
	ElementPlayFile     = "playbooks/imports/optional/common/play-teamchat-element.yml"
	AnsibleTag          = Product
	TeamPortField       = "port"
)

// The ccy session's fixed paths (D 3.6): its one kit in the image, the seat and membership
// mounts, and the root of its runtime directories in the container-private /tmp.
const (
	CCYKitDir              Kit = "/opt/claude-yolo/optional/" + Product
	SessionSeatMount           = "/run/" + Product + "/seats"
	SessionMembershipMount     = "/run/" + Product + "/membership"
	CCYRunRoot                 = "/tmp/" + Product
)

// HomeserverUser is the team's homeserver DynamicUser.
func HomeserverUser(t TeamID) string { return Product + "-" + string(t) }

// ServerName is the team's Matrix server name.
func ServerName(t TeamID) string { return string(t) + "." + Product + ".internal" }

// ServerUnit is the team's homeserver unit.
func ServerUnit(t TeamID) string { return Product + "-server@" + string(t) + ".service" }

// ServerUnitDropIn is the drop-in directory of the team's homeserver unit.
func ServerUnitDropIn(t TeamID) string { return ServerUnit(t) + ".d" }

// ServerRuntimeDirectory is the RuntimeDirectory= value of the team's homeserver unit.
func ServerRuntimeDirectory(t TeamID) string { return Product + "/" + string(t) }

// FenceUnit is the team's migration fence unit (D 3.5).
func FenceUnit(t TeamID) string { return Product + "-fence@" + string(t) + ".service" }

// FenceTable is the name of the team's nftables fence table, in family inet.
func FenceTable(t TeamID) string { return Product + "_fence_" + string(t) }

// SessionUnit is an LXC guest's supervised session unit.
func SessionUnit(u GuestUser) string { return Product + "-session-" + string(u) + ".service" }

// GuestRuntimeDir is the XDG_RUNTIME_DIR the guest's session unit gives the session (C 3.9.4).
func GuestRuntimeDir(u GuestUser) string { return "/run/" + Product + "-session-" + string(u) }

// ServerBinary is the pinned homeserver binary of a version.
func ServerBinary(v ServerVersion) string { return ServerDir + "/tuwunel-" + string(v) }

// AdminBinaryCopy is root's immutable copy of one teamchat-admin build, which the units run
// (C 3.5).
func AdminBinaryCopy(d Digest) string { return LibDir + "/" + AdminBinary + "-" + string(d) }

// Kit is a kit directory: one in the host kit root (HostKitDir, HostKitCurrent) or the ccy
// image's (CCYKitDir).
type Kit string

// HostKitDir is one kit digest's directory on a host or LXC guest.
func HostKitDir(d Digest) Kit { return Kit(HostKitRoot + "/" + string(d)) }

// Binary is the member binary inside the kit.
func (k Kit) Binary() string { return string(k) + "/bin/" + MemberBinary }

// PluginDir is the plugin directory inside the kit.
func (k Kit) PluginDir() string { return string(k) + "/plugin/" + PluginName }

// PluginHooks is the plugin's hooks file, which names the kit's binary by absolute path (B 2.12).
func (k Kit) PluginHooks() string { return k.PluginDir() + "/hooks/hooks.json" }

// Settings is the session settings file inside the kit.
func (k Kit) Settings() string { return string(k) + "/" + settingsFile }

// HomeserverState is the team's homeserver state (StateDirectory=, kept by systemd in
// HomeserverPrivateState).
func HomeserverState(t TeamID) string { return HomeserverStateRoot + "/" + string(t) }

// HomeserverPrivateState is where systemd keeps HomeserverState for a DynamicUser.
func HomeserverPrivateState(t TeamID) string {
	return HomeserverPrivateStateRoot + "/" + string(t)
}

// AdminState is the team's root-only records.
func AdminState(t TeamID) string { return AdminStateRoot + "/" + string(t) }

// ServerConfig is the team's rendered homeserver config, which its unit loads as a credential.
func ServerConfig(t TeamID) string { return AdminState(t) + "/config.toml" }

// TeamDeclaration is the team declaration last applied.
func TeamDeclaration(t TeamID) string { return AdminState(t) + "/team.json" }

// RestoreBlock stops every start of the team's homeserver while it exists: a restore is under
// way.
func RestoreBlock(t TeamID) string { return AdminState(t) + "/restore.block" }

// MigrationDir is the team's migration state.
func MigrationDir(t TeamID) string { return AdminState(t) + "/migration" }

// FenceFile is the nftables file the fence unit loads.
func FenceFile(t TeamID) string { return MigrationDir(t) + "/fence.nft" }

// FirewallRecord is the record of the firewalld rules v2 added for the team.
func FirewallRecord(t TeamID) string { return AdminState(t) + "/firewall.json" }

// CtlState is the team's admin token, roster and rooms, owned by CtlUser.
func CtlState(t TeamID) string { return CtlStateRoot + "/" + string(t) }

// TeamLock is root's lock for the team.
func TeamLock(t TeamID) string { return AdminRunDir + "/team-" + string(t) + ".lock" }

// SetLock is the lock all the steps of one migration set run under. Its directory starts with
// an underscore, which no team id can, so it never meets a team's AdminState.
func SetLock(s SetID) string { return AdminStateRoot + "/_sets/" + string(s) + ".lock" }

// StatusTeamDir is the team's status records, readable without sudo.
func StatusTeamDir(t TeamID) string { return StatusRoot + "/teams/" + string(t) }

// StampFile is the team's install stamp.
func StampFile(t TeamID) string { return StatusTeamDir(t) + "/stamp.json" }

// StatusFile is the team's status record (health included).
func StatusFile(t TeamID) string { return StatusTeamDir(t) + "/status.json" }

// ApprovedFile is the team's approved security fields, readable by the owner group (C DC18).
func ApprovedFile(t TeamID) string { return StatusTeamDir(t) + "/approved.json" }

// LXCOnlineFile is the team's record of its LXC members online.
func LXCOnlineFile(t TeamID) string { return StatusTeamDir(t) + "/lxc-online.json" }

// LXCRecord is the team's LXC record.
func LXCRecord(t TeamID) string { return StatusRoot + "/lxc/" + string(t) + ".json" }

// SetRecord is a migration set's record, the one source of which system a migrating team is
// live on (D 5.3).
func SetRecord(s SetID) string { return StatusRoot + "/sets/" + string(s) + ".json" }

// ElementProfile is the --profile of the team's Element.
func ElementProfile(t TeamID) string { return Product + "-" + string(t) }

// ElementLauncherName is the Name= of the team's Element launcher, by its display name.
func ElementLauncherName(d DisplayName) string { return string(d) + " team (" + Product + ")" }

// ElementConfigDir is the team's Element profile directory under a home.
func ElementConfigDir(home string, t TeamID) (string, error) {
	if err := checkBase("home", home); err != nil {
		return "", err
	}
	return home + "/.var/app/im.riot.Riot/config/Element-teamchat-" + string(t), nil
}

// ElementLauncher is the team's Element desktop launcher under a home.
func ElementLauncher(home string, t TeamID) (string, error) {
	if err := checkBase("home", home); err != nil {
		return "", err
	}
	return home + "/.local/share/applications/" + Product + "-" + string(t) + "-element.desktop", nil
}

// ElementConsentFile is the owner's consent to an Element profile beside host members.
func ElementConsentFile(home string) (string, error) {
	if err := checkBase("home", home); err != nil {
		return "", err
	}
	return home + "/.config/" + Product + "/element-consent", nil
}

// checkBase refuses a base directory that is not absolute and clean, or is the root, under
// which a joined name would begin with two slashes.
func checkBase(kind, dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" {
		return fmt.Errorf("%w %s directory: must be absolute, clean and not the root", ErrInvalid, kind)
	}
	return nil
}

// File names inside a member's directories (B 2.4, 2.6).
const (
	settingsFile     = "settings.json"
	desiredFile      = "desired.json"
	conversationFile = "conversation.json"
)

// Layout is a member's five path variables of area B's header, whose values differ by member
// kind: the kit root and the kit in use, the persistent state root, the read-only desired
// membership directory, and the root of the per-session runtime directories.
type Layout struct {
	KitRoot    string
	CurrentKit Kit
	State      string
	Desired    string
	RunRoot    string
}

// CCYLayout is a ccy session's layout: one kit in the image, the session's seat and membership
// mounts, and runtime directories in the container-private /tmp.
func CCYLayout() Layout {
	return Layout{
		KitRoot:    string(CCYKitDir),
		CurrentKit: CCYKitDir,
		State:      SessionSeatMount,
		Desired:    SessionMembershipMount,
		RunRoot:    CCYRunRoot,
	}
}

// HomeLayout is an LXC guest's or host member's layout, under the home and $XDG_RUNTIME_DIR the
// process received.
func HomeLayout(home, runtimeDir string) (Layout, error) {
	if err := checkBase("home", home); err != nil {
		return Layout{}, err
	}
	if err := checkBase("runtime", runtimeDir); err != nil {
		return Layout{}, err
	}
	config := home + "/.config/" + Product
	return Layout{
		KitRoot:    HostKitRoot,
		CurrentKit: HostKitCurrent,
		State:      config + "/seats",
		Desired:    config + "/membership",
		RunRoot:    runtimeDir + "/" + Product + "/run",
	}, nil
}

// DesiredFile is the session's desired membership.
func (l Layout) DesiredFile() string { return l.Desired + "/" + desiredFile }

// ConversationFile is the member's persistent conversation record (B43).
func (l Layout) ConversationFile() string { return l.State + "/" + conversationFile }

// Seat is the member's directory for one team: its state, seat lock and bundle together.
func (l Layout) Seat(t TeamID) Seat { return Seat{Dir: l.State + "/" + string(t)} }

// Run is one session's runtime directory.
func (l Layout) Run(r RunID) Run { return Run{Dir: l.RunRoot + "/" + string(r)} }

// Seat is a member's directory for one team.
type Seat struct{ Dir string }

// Bundle is the seat's bundle record, placed by area C.
func (s Seat) Bundle() string { return s.Dir + "/seat.json" }

// Token is the seat's login token, placed by area C.
func (s Seat) Token() string { return s.Dir + "/token" }

// Lock is the seat lock.
func (s Seat) Lock() string { return s.Dir + "/seat.lock" }

// Store is the member's state for the team.
func (s Seat) Store() string { return s.Dir + "/store.json" }

// Report is the seat's report, which carries the run id of the session that wrote it.
func (s Seat) Report() string { return s.Dir + "/report.json" }

// LastLog is the last session's log, kept with the seat.
func (s Seat) LastLog() string { return s.Dir + "/last.log" }

// Run is one session's runtime directory: the keeper (teamchat run) and the link
// (teamchat link) and what each holds (B 2.4).
type Run struct{ Dir string }

// KeeperLock is held by the keeper for the session's life.
func (r Run) KeeperLock() string { return r.Dir + "/keeper.lock" }

// KeeperJSON is the keeper's record.
func (r Run) KeeperJSON() string { return r.Dir + "/keeper.json" }

// KeeperLog is the keeper's log.
func (r Run) KeeperLog() string { return r.Dir + "/keeper.log" }

// LinkSock is the link's socket.
func (r Run) LinkSock() string { return r.Dir + "/link.sock" }

// LinkLock is held by the link while it runs.
func (r Run) LinkLock() string { return r.Dir + "/link.lock" }

// LinkLog is the link's log.
func (r Run) LinkLog() string { return r.Dir + "/link.log" }

// Settings is the per-launch copy of the kit's session settings.
func (r Run) Settings() string { return r.Dir + "/" + settingsFile }

// Session is the session's record.
func (r Run) Session() string { return r.Dir + "/session.json" }

// PID is the keeper's and the session's pids with their start times (B 2.4.1), which the
// leftover sweep and the host's kit pruning read.
func (r Run) PID() string { return r.Dir + "/session.pid" }

// Status is the status line's record for the session.
func (r Run) Status() string { return r.Dir + "/status.json" }

// StateHome is the XDG state home: $XDG_STATE_HOME when it is absolute (the XDG spec ignores a
// relative one), else <home>/.local/state.
func StateHome(home, xdgStateHome string) (string, error) {
	if err := checkBase("home", home); err != nil {
		return "", err
	}
	if !filepath.IsAbs(xdgStateHome) {
		return home + "/.local/state", nil
	}
	dir := filepath.Clean(xdgStateHome)
	if err := checkBase("XDG state", dir); err != nil {
		return "", err
	}
	return dir, nil
}

// ProbeState is the fresh state directory of the owner's probe of a team (C 3.3).
func ProbeState(stateHome string, t TeamID) (string, error) {
	if err := checkBase("state", stateHome); err != nil {
		return "", err
	}
	return stateHome + "/" + Product + "/probe/" + string(t), nil
}

// SeatStore is the owner's seat store under an XDG state home (C 3.7), outside every checkout
// and on no ccy mount.
func SeatStore(stateHome string) (Store, error) {
	if err := checkBase("state", stateHome); err != nil {
		return Store{}, err
	}
	return Store{Root: stateHome + "/" + Product + "/seats"}, nil
}

// Store is the owner's seat store.
type Store struct{ Root string }

// Pool is the seats' authoritative records, never mounted into a container.
func (s Store) Pool() string { return s.Root + "/pool" }

// PoolCheckout is one checkout's seats in the pool.
func (s Store) PoolCheckout(k CheckoutKey) string { return s.Pool() + "/" + string(k) }

// PoolCheckoutRecord is the checkout's real path, so the pool can be listed per checkout.
func (s Store) PoolCheckoutRecord(k CheckoutKey) string {
	return s.PoolCheckout(k) + "/checkout.json"
}

// Session is the seats a ccy session holds, mounted read-write at SessionSeatMount.
func (s Store) Session(n SessionName) string { return s.Root + "/sessions/" + string(n) }

// Control is the session's control directory, mounted read-only at SessionMembershipMount.
func (s Store) Control(n SessionName) string { return s.Root + "/control/" + string(n) }

// DesiredFile is the session's desired membership in its control directory.
func (s Store) DesiredFile(n SessionName) string { return s.Control(n) + "/" + desiredFile }

// Released is the quarantine of released seat directories.
func (s Store) Released() string { return s.Root + "/released" }

// AllocLock is the owner's allocation lock for one checkout, under $XDG_RUNTIME_DIR.
func AllocLock(runtimeDir string, k CheckoutKey) (string, error) {
	if err := checkBase("runtime", runtimeDir); err != nil {
		return "", err
	}
	return runtimeDir + "/" + Product + "/alloc/" + string(k) + ".lock", nil
}
