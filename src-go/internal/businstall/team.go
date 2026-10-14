package businstall

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/firewalld"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/netprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const secretName = "secrets/registration_shared_secret"

// team installs or updates the team a team file declares, restarting its homeserver only
// when something it runs under changed. The stamp is written last, so it says the team was
// applied in full.
func (a *App) team(args []string) error {
	opts, err := parseOptions(args, map[string]bool{"team-file": true})
	if err != nil {
		return err
	}
	if err = opts.require("team-file", "team"); err != nil {
		return err
	}
	file := opts["team-file"]
	if err = a.requireRoot(); err != nil {
		return err
	}
	if err = a.requireFedora(); err != nil {
		return err
	}
	raw, err := readableFile(file)
	if err != nil {
		return fail(term.ExitUsage, "--team-file %s is not a readable file", file)
	}
	if err = a.requireSoftware(); err != nil {
		return err
	}
	if err = a.requireRunning("firewalld.service", "NetworkManager.service"); err != nil {
		return err
	}
	name, err := teamNameOf(raw, file)
	if err != nil {
		return err
	}
	canonical, doc, err := a.checkTeamFile(raw, file, name)
	if err != nil {
		return err
	}
	if err = a.assertPortFree(name, doc.Port); err != nil {
		return err
	}
	toml, dropin, err := a.renderTeam(canonical, doc)
	if err != nil {
		return err
	}
	return a.applyTeam(name, canonical, toml, dropin, doc)
}

// readableFile is the content of a regular file the caller can read.
func readableFile(file string) ([]byte, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return os.ReadFile(filepath.Clean(file))
}

// requireSoftware fails unless `software` has installed what a team runs on.
func (a *App) requireSoftware() error {
	local, err := a.fsAt(usrLocalDir)
	if err != nil {
		return failf("%v", err)
	}
	for _, need := range []string{libRel + "/tuwunel", libRel + "/agent-bus.pyz", "bin/agent-bus"} {
		if _, err = local.Stat(need); err != nil {
			return a.missingSoftware(usrLocalDir+"/"+need, err)
		}
	}
	units, found, err := a.tryFS(unitsDir)
	if err != nil {
		return failf("%v", err)
	}
	if !found {
		return failf("%s/%s is missing: run %s software first", unitsDir, hsTemplate, prog)
	}
	if _, err := units.Stat(hsTemplate); err != nil {
		return a.missingSoftware(unitsDir+"/"+hsTemplate, err)
	}
	return nil
}

func (a *App) missingSoftware(display string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return failf("%s is missing: run %s software first", display, prog)
	}
	return failf("%v", err)
}

// teamNameOf is the team name in a team file: only to find the installed copy, for the admin
// tool's check validates everything.
func teamNameOf(raw []byte, file string) (string, error) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return "", fail(term.ExitConfig, "team file %s: no team name: %v", file, err)
	}
	name, ok := top["team"].(string)
	if !ok {
		return "", fail(term.ExitConfig, "team file %s: no team name: .team is not a string", file)
	}
	if !teamPattern.MatchString(name) {
		return "", fail(term.ExitConfig, "team file %s: team '%s' is not a team name", file, name)
	}
	return name, nil
}

// checkTeamFile has the admin tool validate the team file against the installed team.json
// and return its canonical form.
func (a *App) checkTeamFile(raw []byte, file, name string) ([]byte, teamDoc, error) {
	previous := ""
	if teamFS, found, err := a.tryFS(stateDir + "/" + name); err != nil {
		return nil, teamDoc{}, failf("%v", err)
	} else if found {
		if info, statErr := teamFS.Stat("team.json"); statErr == nil && info.Mode().IsRegular() {
			previous = a.path(stateDir + "/" + name + "/team.json")
		}
	}
	canonical, err := a.admin().Check(a.ctx(), raw, previous)
	if err != nil {
		return nil, teamDoc{}, adminFailure(err, "team file %s refused (above)", file)
	}
	doc, err := parseTeamDoc(canonical)
	if err != nil || doc.Port == 0 {
		return nil, teamDoc{}, failf("team file %s: the admin tool's check printed no usable team: %v", file, err)
	}
	if doc.State != "present" {
		return nil, teamDoc{}, fail(term.ExitConfig, "team %s is state %s: run %s remove --team %s", name, doc.State, prog, name)
	}
	return canonical, doc, nil
}

// assertPortFree fails when another installed team holds the port, or when something else
// listens on it while this team's homeserver is not running.
func (a *App) assertPortFree(team string, port int) error {
	state, found, err := a.tryFS(stateDir)
	if err != nil {
		return failf("%v", err)
	}
	if found {
		if err = otherTeamsLeavePort(state, team, port); err != nil {
			return err
		}
	}
	active, err := a.unitState(hsUnit(team))
	if err != nil {
		return err
	}
	if active == stateActive {
		return nil
	}
	listing, err := a.net().Listening(a.ctx(), port)
	if err != nil {
		return failf("%v", err)
	}
	if strings.TrimSpace(listing) != "" {
		return fail(term.ExitConfig, "port %d is already in use on this host", port)
	}
	return nil
}

// otherTeamsLeavePort fails when another installed team holds the port.
func otherTeamsLeavePort(state *fsx.FS, team string, port int) error {
	entries, err := state.ReadDir(".")
	if err != nil {
		return failf("%v", err)
	}
	for _, entry := range entries {
		file := entry.Name() + "/team.json"
		if info, statErr := state.Stat(file); statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		other, decodeErr := decodeTeamDoc(state, file)
		if decodeErr != nil {
			return decodeErr
		}
		if other.Team != team && other.Port == port {
			return fail(term.ExitConfig, "port %d is team %s's", port, other.Team)
		}
	}
	return nil
}

// renderTeam renders tuwunel.toml and the unit drop-in; the drop-in names the interface that
// carries each listen address.
func (a *App) renderTeam(canonical []byte, doc teamDoc) ([]byte, []byte, error) {
	addrJSON, err := a.net().AddrJSON(a.ctx())
	if err != nil {
		return nil, nil, failf("%v", err)
	}
	pairs := make([]string, 0, len(doc.Listen))
	for _, address := range doc.Listen {
		iface, ifaceErr := netprobe.InterfaceOf(addrJSON, address)
		if errors.Is(ifaceErr, netprobe.ErrNoInterface) {
			return nil, nil, fail(term.ExitConfig, "listen address %s is %v", address, ifaceErr)
		}
		if multiple, ok := errors.AsType[*netprobe.MultipleInterfacesError](ifaceErr); ok {
			return nil, nil, fail(term.ExitConfig, "listen address %s is %v", address, multiple)
		}
		if ifaceErr != nil {
			return nil, nil, failf("%v", ifaceErr)
		}
		pairs = append(pairs, address+"="+iface)
	}
	toml, err := a.admin().Toml(a.ctx(), canonical)
	if err != nil {
		return nil, nil, failf("%v", err)
	}
	dropin, err := a.admin().Dropin(a.ctx(), canonical, pairs)
	if err != nil {
		return nil, nil, failf("%v", err)
	}
	return toml, dropin, nil
}

// applyTeam writes the team's files, converges its units and waits for the homeserver.
func (a *App) applyTeam(name string, canonical, toml, dropin []byte, doc teamDoc) error {
	restart, reload, err := a.writeTeamFiles(name, canonical, toml, dropin, doc)
	if err != nil {
		return err
	}
	if reload {
		if err := a.sd().DaemonReload(a.ctx()); err != nil {
			return failf("%v", err)
		}
	}
	if err := a.convergeUnits(name, restart); err != nil {
		return err
	}
	if err := a.waitReady(hsUnit(name), doc.Port); err != nil {
		return err
	}
	if err := a.assertListening(doc.Port, doc.Listen); err != nil {
		return err
	}
	if err := a.admin().Bootstrap(a.ctx(), name); err != nil {
		return adminFailure(err, "bootstrap of team %s failed", name)
	}
	return a.stampTeam(name, canonical)
}

// writeTeamFiles makes the team's directories and files and reports whether the homeserver
// must be restarted and systemd reloaded.
func (a *App) writeTeamFiles(name string, canonical, toml, dropin []byte, doc teamDoc) (bool, bool, error) {
	root, err := a.rootOwner()
	if err != nil {
		return false, false, failf("%v", err)
	}
	svc, err := a.svcOwner()
	if err != nil {
		return false, false, failf("%v", err)
	}
	group := fsx.Owner{UID: root.UID, GID: svc.GID}
	teamFS, installFS, err := a.makeTeamDirs(name, root, svc)
	if err != nil {
		return false, false, err
	}
	restart, err := putBytes(teamFS, "team "+name+" tuwunel.toml", toml, "tuwunel.toml", groupReadMode, group)
	if err != nil {
		return false, false, err
	}
	if _, err = putBytes(teamFS, "team "+name+" team.json", canonical, "team.json", groupReadMode, group); err != nil {
		return false, false, err
	}
	if err = a.ensureSharedSecret(teamFS, svc); err != nil {
		return false, false, err
	}
	if err = a.syncFirewall(name, doc); err != nil {
		return false, false, err
	}
	units, dropinChanged, err := a.writeDropin(name, dropin, root)
	if err != nil {
		return false, false, err
	}
	recorded, err := a.recordSoftware(name, installFS, units, root)
	if err != nil {
		return false, false, err
	}
	return restart || dropinChanged || recorded, dropinChanged, nil
}

// writeDropin writes the team's unit drop-in and returns the units directory.
func (a *App) writeDropin(name string, dropin []byte, root fsx.Owner) (*fsx.FS, bool, error) {
	units, err := a.fsAt(unitsDir)
	if err != nil {
		return nil, false, failf("%v", err)
	}
	if _, err = units.EnsureDir(dropinName(name), dirMode, root); err != nil {
		return nil, false, failf("%v", err)
	}
	changed, err := putBytes(units, "team "+name+" unit drop-in", dropin, dropinName(name)+"/network.conf", fileMode, root)
	return units, changed, err
}

// recordSoftware records the binary and the base unit the team was applied with, and says
// whether either differs from the record.
func (a *App) recordSoftware(name string, installFS, units *fsx.FS, root fsx.Owner) (bool, error) {
	binaryChanged, err := a.recordBinary(name, installFS, root)
	if err != nil {
		return false, err
	}
	unitChanged, err := a.recordBaseUnit(name, installFS, units, root)
	if err != nil {
		return false, err
	}
	return binaryChanged || unitChanged, nil
}

func putBytes(f *fsx.FS, label string, data []byte, name string, mode fs.FileMode, own fsx.Owner) (bool, error) {
	changed, err := f.PutBytes(label, data, name, mode, own)
	if err != nil {
		return false, failf("%v", err)
	}
	return changed, nil
}

// makeTeamDirs makes the team's directories under the service account's state and under
// root's own install state, and opens them.
func (a *App) makeTeamDirs(name string, root, svc fsx.Owner) (*fsx.FS, *fsx.FS, error) {
	varLib, err := a.fsAt(varLibDir)
	if err != nil {
		return nil, nil, failf("%v", err)
	}
	if _, err = varLib.EnsureDir("agent-bus", privateMode, svc); err != nil {
		return nil, nil, failf("%v", err)
	}
	state, err := a.fsAt(stateDir)
	if err != nil {
		return nil, nil, failf("%v", err)
	}
	if _, err = state.EnsureDir(name, privateMode, svc); err != nil {
		return nil, nil, failf("%v", err)
	}
	teamFS, err := a.fsAt(stateDir + "/" + name)
	if err != nil {
		return nil, nil, failf("%v", err)
	}
	for _, sub := range []string{"db", "backups", "secrets"} {
		if _, err = teamFS.EnsureDir(sub, privateMode, svc); err != nil {
			return nil, nil, failf("%v", err)
		}
	}
	if _, err = varLib.EnsureDir("agent-bus-install", privateMode, root); err != nil {
		return nil, nil, failf("%v", err)
	}
	install, err := a.fsAt(installDir)
	if err != nil {
		return nil, nil, failf("%v", err)
	}
	if _, err = install.EnsureDir(name, privateMode, root); err != nil {
		return nil, nil, failf("%v", err)
	}
	installFS, err := a.fsAt(installDir + "/" + name)
	if err != nil {
		return nil, nil, failf("%v", err)
	}
	return teamFS, installFS, nil
}

// ensureSharedSecret creates the registration secret once: 64 random bytes as hex, with no
// trailing newline, readable by the service account only. An existing one keeps its content;
// a wrong mode or owner is put right.
func (a *App) ensureSharedSecret(teamFS *fsx.FS, svc fsx.Owner) error {
	_, err := teamFS.Lstat(secretName)
	if err == nil {
		_, fixErr := teamFS.EnsureFileMode("registration shared secret mode and owner", secretName, privateFile, svc)
		return wrapFS(fixErr)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return failf("%v", err)
	}
	random := make([]byte, 64)
	if _, err = io.ReadFull(a.cfg.Rand, random); err != nil {
		return failf("could not read 64 random bytes: %v", err)
	}
	_, err = teamFS.PutBytes("registration shared secret created", []byte(hex.EncodeToString(random)), secretName, privateFile, svc)
	return wrapFS(err)
}

// syncFirewall opens the port to exactly the allow_from networks, in the zone of the
// interface the kernel routes each through.
func (a *App) syncFirewall(name string, doc teamDoc) error {
	var wanted []firewalld.Rule
	for _, cidr := range doc.AllowFrom {
		rule, err := firewalld.RichRule(cidr, doc.Port)
		if err != nil {
			return failf("%v", err)
		}
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return failf("%v", err)
		}
		dev, err := a.net().RouteDev(a.ctx(), prefix.Addr().String())
		if errors.Is(err, netprobe.ErrNoRoute) {
			return failf("no route to allow_from %s", cidr)
		}
		if err != nil {
			return failf("%v", err)
		}
		zone, err := a.fw().ZoneOfInterface(a.ctx(), dev)
		if err != nil {
			return failf("%v", err)
		}
		wanted = append(wanted, firewalld.Rule{Zone: zone, Text: rule})
	}
	record, err := a.fwRecord(name)
	if err != nil {
		return failf("%v", err)
	}
	return wrapFS(a.fw().Sync(a.ctx(), record, wanted))
}

// recordBinary records the Tuwunel the team's homeserver last ran, so a new one restarts it.
func (a *App) recordBinary(name string, installFS *fsx.FS, root fsx.Owner) (bool, error) {
	resolved, err := filepath.EvalSymlinks(a.path(usrLocalDir + "/" + libRel + "/tuwunel"))
	if err != nil {
		return false, failf("%v", err)
	}
	return putBytes(installFS, "team "+name+" binary "+resolved, []byte(resolved+"\n"), "binary", privateFile, root)
}

// recordBaseUnit records the hash of the base unit the homeserver last started under:
// `software` reloads systemd for a changed unit, but the running homeserver takes its new
// sandbox only on a restart.
func (a *App) recordBaseUnit(name string, installFS, units *fsx.FS, root fsx.Owner) (bool, error) {
	data, err := units.ReadFile(hsTemplate)
	if err != nil {
		return false, failf("%v", err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	return putBytes(installFS, "team "+name+" base unit "+hash, []byte(hash+"\n"), "unit-sha256", privateFile, root)
}

// convergeUnits enables the homeserver and its backup timer and starts them, restarting the
// homeserver instead when restart is set and it is already running.
func (a *App) convergeUnits(team string, restart bool) error {
	sd := a.sd()
	hs, timer := hsUnit(team), timerUnit(team)
	for _, unit := range []string{hs, timer} {
		enabled, err := sd.IsEnabled(a.ctx(), unit)
		if err != nil {
			return failf("%v", err)
		}
		if !enabled {
			if err := sd.Enable(a.ctx(), unit); err != nil {
				return failf("%v", err)
			}
			a.changedf("enabled %s", unit)
		}
	}
	state, err := a.unitState(hs)
	if err != nil {
		return err
	}
	switch {
	case state != stateActive:
		if err = sd.Start(a.ctx(), hs); err != nil {
			return failf("%v", err)
		}
		a.changedf("started %s", hs)
	case restart:
		if err = sd.Restart(a.ctx(), hs); err != nil {
			return failf("%v", err)
		}
		a.changedf("restarted %s", hs)
	}
	state, err = a.unitState(timer)
	if err != nil {
		return err
	}
	if state != stateActive {
		if err := sd.Start(a.ctx(), timer); err != nil {
			return failf("%v", err)
		}
		a.changedf("started %s", timer)
	}
	return nil
}

// stampTeam writes what was applied: the digests of the team file and of the software that
// applied it (the admin zipapp, the Tuwunel it started and this installer's source). The
// stamp is world-readable on purpose, so ccy-teams can tell a team is out of date without
// sudo; it holds no secret.
func (a *App) stampTeam(name string, canonical []byte) error {
	if a.cfg.SourceDigest == "" {
		return failf("this installer was built without a source digest to stamp (build it with -ldflags \"-X main.SourceDigest=<digest>\")")
	}
	running, err := os.Readlink(a.path(usrLocalDir + "/" + libRel + "/tuwunel"))
	if err != nil {
		return failf("%v", err)
	}
	stamp, err := a.admin().Stamp(a.ctx(), canonical, a.path(usrLocalDir+"/"+libRel+"/agent-bus.pyz"),
		strings.TrimPrefix(running, "tuwunel-"), a.cfg.SourceDigest)
	if err != nil {
		return failf("%v", err)
	}
	root, err := a.rootOwner()
	if err != nil {
		return failf("%v", err)
	}
	varLib, err := a.fsAt(varLibDir)
	if err != nil {
		return failf("%v", err)
	}
	if _, err = varLib.EnsureDir("agent-bus-applied", dirMode, root); err != nil {
		return failf("%v", err)
	}
	applied, err := a.fsAt(appliedDir)
	if err != nil {
		return failf("%v", err)
	}
	_, err = putBytes(applied, "team "+name+" applied stamp", stamp, name+".json", fileMode, root)
	return err
}
