package businstall

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/busaddr"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/netprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	busIface = "agentbus0"
	// kitSource is the member kit in a checkout.
	kitSource = "files/opt/claude-yolo/optional/agent-bus"
	// launcher is the kit file that is a program.
	launcher = "agent-bus-claude"
)

var packages = []string{
	"python3", "firewalld", "NetworkManager", "zstd", "curl", "jq",
	"tcpdump",    // acceptance.bash captures the homeserver's traffic with it.
	"util-linux", // setpriv: the agent-bus wrapper writes an --out bundle as the sudo user.
}

var unitFiles = []string{"agent-bus-hs@.service", "agent-bus-backup@.service", "agent-bus-backup@.timer"}

// kitFiles is the member kit beside its pingbus, relative to kitSource.
var kitFiles = []string{
	launcher, "settings.json", "plugin/pingbus/.claude-plugin/plugin.json",
	"plugin/pingbus/hooks/hooks.json", "plugin/pingbus/skills/pingbus/SKILL.md",
	"README.podman", "README.host", "README.lxc", "README.docker", "README.vm",
}

var kitDirs = []string{
	"plugin", "plugin/pingbus", "plugin/pingbus/.claude-plugin", "plugin/pingbus/hooks",
	"plugin/pingbus/skills", "plugin/pingbus/skills/pingbus",
}

// software installs everything a team needs and no team: packages, the service account, the
// pinned Tuwunel, the zipapps, the units. It starts and enables nothing.
func (a *App) software(args []string) error {
	in, err := softwareInputs(args)
	if err != nil {
		return err
	}
	if err = a.requireRoot(); err != nil {
		return err
	}
	if err = a.requireFedora(); err != nil {
		return err
	}
	if err = requireCheckout(in.source); err != nil {
		return err
	}
	local, err := a.fsAt(usrLocalDir)
	if err != nil {
		return failf("%v", err)
	}
	if err = a.requireSystemDirs(local); err != nil {
		return err
	}
	pinned, err := pin.Load(filepath.Join(in.source, "files/usr/local/share/agent-bus/tuwunel.pin"))
	if err != nil {
		return fail(term.ExitConfig, "%v", err)
	}
	steps := []func() error{
		func() error { return a.installBase(local) },
		func() error { return a.installTuwunel(local, pinned) },
		func() error { return a.installProgramsAndKit(local, in.source) },
		func() error { return a.installUnits(in.source) },
	}
	for _, step := range steps {
		if err = step(); err != nil {
			return err
		}
	}
	if in.hasAddr {
		return a.ensureBusInterface(in.addr)
	}
	return nil
}

// softwareFlags are what `software` was asked to install from and for.
type softwareFlags struct {
	source  string
	addr    netip.Addr
	hasAddr bool
}

func softwareInputs(args []string) (softwareFlags, error) {
	opts, err := parseOptions(args, map[string]bool{"source": true, "bus-address": true})
	if err != nil {
		return softwareFlags{}, err
	}
	if err = opts.require("source", "software"); err != nil {
		return softwareFlags{}, err
	}
	addr, hasAddr, err := busAddress(opts)
	if err != nil {
		return softwareFlags{}, err
	}
	if !filepath.IsAbs(opts["source"]) {
		return softwareFlags{}, fail(term.ExitUsage, "--source must be an absolute path")
	}
	return softwareFlags{source: opts["source"], addr: addr, hasAddr: hasAddr}, nil
}

// busAddress is the validated --bus-address, if given.
func busAddress(opts options) (netip.Addr, bool, error) {
	text, given := opts["bus-address"]
	if !given {
		return netip.Addr{}, false, nil
	}
	addr, err := busaddr.Parse(text)
	if err != nil {
		return netip.Addr{}, true, fail(term.ExitUsage, "--bus-address '%s' is not a concrete, canonical IP address", text)
	}
	return addr, true, nil
}

// requireCheckout fails unless source holds every file `software` installs from.
func requireCheckout(source string) error {
	need := make([]string, 0, 4+len(kitFiles))
	need = append(need,
		"helpers/pingbus/bundle.py",
		"files/usr/local/bin/agent-bus",
		"files/usr/local/share/agent-bus/tuwunel.pin",
		"files/usr/local/share/agent-bus/resolv.conf",
	)
	for _, f := range kitFiles {
		need = append(need, kitSource+"/"+f)
	}
	for _, rel := range need {
		if info, err := os.Stat(filepath.Join(source, rel)); err != nil || !info.Mode().IsRegular() {
			return fail(term.ExitConfig, "--source %s holds no %s: not a fedora-desktop checkout", source, rel)
		}
	}
	return nil
}

// requireSystemDirs checks the three directories the OS owns: the installer never makes them
// and never changes their mode or owner.
func (a *App) requireSystemDirs(local *fsx.FS) error {
	for _, name := range []string{"bin", "sbin"} {
		if err := local.RequireSystemDir(name); err != nil {
			return failf("%v", err)
		}
	}
	if info, err := os.Stat(a.path(unitsDir)); err != nil || !info.IsDir() {
		return failf("%s is not a directory on this host", unitsDir)
	}
	return nil
}

// installBase installs the packages, makes the service account and the directories.
func (a *App) installBase(local *fsx.FS) error {
	sys := a.sys()
	missing, err := sys.Missing(a.ctx(), packages)
	if err != nil {
		return failf("%v", err)
	}
	if len(missing) > 0 {
		if err = sys.Install(a.ctx(), missing); err != nil {
			return failf("%v", err)
		}
		a.changedf("packages %s", strings.Join(missing, " "))
	}
	if err = a.ensureUser(); err != nil {
		return err
	}
	root, err := a.rootOwner()
	if err != nil {
		return failf("%v", err)
	}
	svc, err := a.svcOwner()
	if err != nil {
		return failf("%v", err)
	}
	varLib, err := a.fsAt(varLibDir)
	if err != nil {
		return failf("%v", err)
	}
	for _, dir := range []struct {
		name string
		own  fsx.Owner
	}{{"agent-bus", svc}, {"agent-bus-install", root}} {
		if _, err := varLib.EnsureDir(dir.name, privateMode, dir.own); err != nil {
			return failf("%v", err)
		}
	}
	return a.ensureLibDirs(local, root)
}

func (a *App) ensureUser() error {
	sys := a.sys()
	exists, err := sys.UserExists(a.ctx(), svcName)
	if err != nil {
		return failf("%v", err)
	}
	if exists {
		return nil
	}
	if err := sys.AddServiceUser(a.ctx(), svcName, stateDir, "Agent team bus homeserver"); err != nil {
		return failf("%v", err)
	}
	a.changedf("system user %s", svcName)
	return nil
}

func (a *App) ensureLibDirs(local *fsx.FS, root fsx.Owner) error {
	for _, parent := range []string{"lib", "share"} {
		if err := a.ensureOSParent(local, parent); err != nil {
			return failf("%v", err)
		}
	}
	for _, dir := range []string{libRel, shareRel, kitRel} {
		if _, err := local.EnsureDir(dir, dirMode, root); err != nil {
			return failf("%v", err)
		}
	}
	return nil
}

// installTuwunel installs the pinned Tuwunel next to the others and points the tuwunel link
// at it; it downloads only when that version is not installed from an asset with the pinned
// hash. The release is unsigned, so the hash is the supply-chain check.
func (a *App) installTuwunel(local *fsx.FS, pinned pin.Pin) error {
	machine, err := a.sys().Machine(a.ctx())
	if err != nil {
		return failf("%v", err)
	}
	build, err := pinned.ForMachine(machine)
	if err != nil {
		return fail(term.ExitConfig, "%v", err)
	}
	binary := libRel + "/" + build.Binary
	record := binary + ".asset-sha256"
	recorded, err := recordedHash(local, binary, record)
	if err != nil {
		return err
	}
	if recorded == build.SHA256 {
		a.sayf("Tuwunel %s is installed from the pinned asset; nothing to download", pinned.Version)
	} else if err = a.downloadTuwunel(local, build, pinned.Bare(), binary, record); err != nil {
		return err
	}
	_, err = local.PutSymlink("tuwunel now "+pinned.Bare(), build.Binary, libRel+"/tuwunel")
	return wrapFS(err)
}

func wrapFS(err error) error {
	if err == nil {
		return nil
	}
	return failf("%v", err)
}

// recordedHash is the pinned hash the installed binary was installed from, or "" when the
// binary or its record is not there.
func recordedHash(local *fsx.FS, binary, record string) (string, error) {
	for _, name := range []string{binary, record} {
		info, err := local.Stat(name)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", nil
			}
			return "", failf("%v", err)
		}
		if !info.Mode().IsRegular() {
			return "", nil
		}
	}
	data, err := local.ReadFile(record)
	if err != nil {
		return "", failf("%v", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

func (a *App) downloadTuwunel(local *fsx.FS, build pin.Build, bare, binary, record string) error {
	work, err := a.newWorkDir()
	if err != nil {
		return failf("%v", err)
	}
	asset := filepath.Join(work, build.Asset)
	a.sayf("downloading %s", build.Asset)
	if err = a.net().Download(a.ctx(), build.URL, asset); err != nil {
		return failf("%v", err)
	}
	got, err := sha256File(asset)
	if err != nil {
		return failf("%v", err)
	}
	if got != build.SHA256 {
		return failf("%s: sha256 %s is not the pinned %s; nothing installed", build.Asset, got, build.SHA256)
	}
	unpacked := filepath.Join(work, "tuwunel")
	if err = a.sys().Decompress(a.ctx(), asset, unpacked); err != nil {
		return failf("%v", err)
	}
	recordFile := filepath.Join(work, "asset-sha256")
	if err = os.WriteFile(recordFile, []byte(build.SHA256+"\n"), privateFile); err != nil {
		return failf("%v", err)
	}
	root, err := a.rootOwner()
	if err != nil {
		return failf("%v", err)
	}
	if _, err = local.PutFile("tuwunel "+bare, unpacked, binary, programMode, root); err != nil {
		return failf("%v", err)
	}
	_, err = local.PutFile("tuwunel "+bare+" asset record", recordFile, record, fileMode, root)
	return wrapFS(err)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	sum := sha256.New()
	_, copyErr := io.Copy(sum, f)
	if closeErr := f.Close(); copyErr != nil || closeErr != nil {
		return "", fmt.Errorf("hash %s: %w", path, errors.Join(copyErr, closeErr))
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// installProgramsAndKit builds the zipapps, installs the commands (the admin wrapper, this
// installer, pingbus) and the member kit.
func (a *App) installProgramsAndKit(local *fsx.FS, source string) error {
	root, err := a.rootOwner()
	if err != nil {
		return failf("%v", err)
	}
	files := filepath.Join(source, "files")
	share := files + "/usr/local/share/agent-bus/"
	if _, err = local.PutFile("tuwunel.pin", share+"tuwunel.pin", shareRel+"/tuwunel.pin", fileMode, root); err != nil {
		return failf("%v", err)
	}
	if _, err = local.PutFile("resolver stub", share+"resolv.conf", shareRel+"/resolv.conf", fileMode, root); err != nil {
		return failf("%v", err)
	}
	if err = a.buildZipapps(source); err != nil {
		return err
	}
	self, err := a.cfg.Executable()
	if err != nil {
		return failf("find this installer to install it: %v", err)
	}
	steps := []func() (bool, error){
		func() (bool, error) {
			return local.PutSymlink("pingbus command", "../lib/agent-bus/pingbus.pyz", "bin/pingbus")
		},
		func() (bool, error) {
			return local.PutFile("agent-bus admin wrapper", files+"/usr/local/bin/agent-bus", "bin/agent-bus", programMode, root)
		},
		func() (bool, error) {
			return local.PutFile(prog, self, "sbin/"+prog, programMode, root)
		},
		func() (bool, error) {
			return local.PutFile("member kit pingbus", a.path(usrLocalDir+"/"+libRel+"/pingbus.pyz"), kitRel+"/pingbus", programMode, root)
		},
	}
	for _, step := range steps {
		if _, err := step(); err != nil {
			return failf("%v", err)
		}
	}
	return a.installKit(local, filepath.Join(source, kitSource), root)
}

func (a *App) buildZipapps(source string) error {
	for _, app := range []string{"agent-bus", "pingbus"} {
		out := a.path(usrLocalDir + "/" + libRel + "/" + app + ".pyz")
		marker, err := a.bundler().Build(a.ctx(), source, out, app)
		if err != nil {
			return failf("%v", err)
		}
		switch {
		case strings.HasPrefix(marker, "BUNDLE-CHANGED "):
			a.changedf("zipapp %s", app)
		case strings.HasPrefix(marker, "BUNDLE-UNCHANGED "):
		default:
			return failf("the zipapp builder printed no marker for %s: %s", app, marker)
		}
	}
	return nil
}

// installKit installs the rest of the member kit beside its pingbus: the launcher for
// members outside ccy, its settings and the Claude Code plugin.
func (a *App) installKit(local *fsx.FS, src string, root fsx.Owner) error {
	for _, dir := range kitDirs {
		if _, err := local.EnsureDir(kitRel+"/"+dir, dirMode, root); err != nil {
			return failf("%v", err)
		}
	}
	for _, file := range kitFiles {
		mode := fileMode
		if file == launcher {
			mode = programMode
		}
		if _, err := local.PutFile("member kit "+file, src+"/"+file, kitRel+"/"+file, mode, root); err != nil {
			return failf("%v", err)
		}
	}
	_, err := local.PutSymlink(launcher+" command", "../share/agent-bus/kit/"+launcher, "bin/"+launcher)
	return wrapFS(err)
}

// installUnits installs the unit files and reloads systemd once if any changed. Nothing is
// enabled, started or restarted.
func (a *App) installUnits(source string) error {
	units, err := a.fsAt(unitsDir)
	if err != nil {
		return failf("%v", err)
	}
	root, err := a.rootOwner()
	if err != nil {
		return failf("%v", err)
	}
	reload := false
	for _, unit := range unitFiles {
		changed, err := units.PutFile("unit "+unit, filepath.Join(source, "files/etc/systemd/system", unit), unit, fileMode, root)
		if err != nil {
			return failf("%v", err)
		}
		reload = reload || changed
	}
	if reload {
		if err := a.sd().DaemonReload(a.ctx()); err != nil {
			return failf("%v", err)
		}
	}
	return nil
}

// ensureBusInterface gives the NetworkManager dummy interface exactly the one address.
func (a *App) ensureBusInterface(addr netip.Addr) error {
	n := a.net()
	exists, err := n.ConnectionExists(a.ctx(), busIface)
	if err != nil {
		return failf("%v", err)
	}
	if !exists {
		if err = n.AddDummy(a.ctx(), busIface, netprobe.Settings(addr)); err != nil {
			return failf("%v", err)
		}
		a.changedf("%s dummy connection with %s", busIface, addr)
	} else if realigned, realignErr := a.realignBusInterface(addr); realignErr != nil || realigned {
		return realignErr
	}
	state, err := n.State(a.ctx(), busIface)
	if err != nil {
		return failf("%v", err)
	}
	if state != "activated" {
		if err = n.Up(a.ctx(), busIface); err != nil {
			return failf("%v", err)
		}
		a.changedf("%s up", busIface)
	}
	return nil
}

// realignBusInterface moves an existing connection to addr when it holds another, and says
// whether it did (the connection is then already up).
func (a *App) realignBusInterface(addr netip.Addr) (bool, error) {
	n := a.net()
	current, err := n.Addresses(a.ctx(), busIface)
	if err != nil {
		return false, failf("%v", err)
	}
	match, err := netprobe.AddressesMatch(current, addr)
	if err != nil {
		return false, failf("%v", err)
	}
	if match {
		return false, nil
	}
	if err = n.Modify(a.ctx(), busIface, netprobe.Settings(addr)); err != nil {
		return false, failf("%v", err)
	}
	if err = n.Up(a.ctx(), busIface); err != nil {
		return false, failf("%v", err)
	}
	a.changedf("%s address now %s", busIface, addr)
	return true, nil
}
