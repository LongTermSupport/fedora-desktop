package businstall

import (
	"errors"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tarstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// restoredTuwunel is the binary the one-off run starts. It is the path on the host even in a
// test prefix: the sandbox the unit describes is the host's.
const restoredTuwunel = "/usr/local/lib/agent-bus/tuwunel"

// restore replaces a team's database and state with a backup. The state tar is read and
// validated first, in root's own directory, before the homeserver is stopped; from the stop
// on, nothing is rolled back.
func (a *App) restore(args []string) error {
	opts, err := parseOptions(args, map[string]bool{optTeam: true, "backup": true})
	if err != nil {
		return err
	}
	team, err := opts.team("restore")
	if err != nil {
		return err
	}
	if err = opts.require("backup", "restore"); err != nil {
		return err
	}
	id := opts["backup"]
	if !backupIDPattern.MatchString(id) {
		return fail(term.ExitUsage, "--backup '%s' is not a backup number", id)
	}
	if err = a.requireRoot(); err != nil {
		return err
	}
	teamFS, err := a.requireTeamDir(team)
	if err != nil {
		return err
	}
	if err = a.requireSoftware(); err != nil {
		return err
	}
	state, err := a.loadStateTar(team, id, teamFS)
	if err != nil {
		return err
	}
	doc, err := a.readTeamDoc(teamFS, "team.json")
	if err != nil {
		return err
	}
	properties, err := a.sandboxFor(team, doc.Port)
	if err != nil {
		return err
	}
	if err := a.sd().Stop(a.ctx(), hsUnit(team)); err != nil {
		return failf("%v", err)
	}
	return a.afterStop(team, a.replaceAndRestart(team, id, doc.Port, teamFS, state, properties))
}

// afterStop says what state a failed restore leaves, once the homeserver has been stopped.
func (a *App) afterStop(team string, err error) error {
	if err != nil {
		a.sayf("the homeserver of %s is stopped and its state is replaced: run restore again, or team to rebuild", team)
	}
	return err
}

// loadStateTar reads the backup's state tar; a tar that is not exactly the team's state is a
// configuration error with every problem listed, and nothing has changed.
func (a *App) loadStateTar(team, id string, teamFS *fsx.FS) (*tarstate.State, error) {
	if _, err := teamFS.Stat(metaRel + "/" + id); err != nil {
		return nil, fail(term.ExitConfig, "team %s has no database backup %s", team, id)
	}
	tars, found, err := a.tryFS(installDir + "/" + team + "/backups")
	if err != nil {
		return nil, failf("%v", err)
	}
	noTar := fail(term.ExitConfig, "team %s has no state tar for backup %s", team, id)
	if !found {
		return nil, noTar
	}
	name := stateTarFile(id)
	if info, statErr := tars.Stat(name); statErr != nil || !info.Mode().IsRegular() {
		return nil, noTar
	}
	f, err := tars.Open(name)
	if err != nil {
		return nil, failf("%v", err)
	}
	state, readErr := tarstate.Read(f, tarstate.Limits{})
	if closeErr := f.Close(); closeErr != nil {
		return nil, failf("%v", closeErr)
	}
	if refused, ok := errors.AsType[*tarstate.RefusedError](readErr); ok {
		for _, problem := range refused.Problems {
			a.stderr.printf("%s\n", problem)
		}
		return nil, fail(term.ExitConfig, "state tar %s refused (above); nothing changed", tars.Display(name))
	}
	if readErr != nil {
		return nil, failf("%v", readErr)
	}
	return state, nil
}

// sandboxFor is the transient unit's properties: the homeserver unit's whole [Service]
// sandbox bar its ExecStart, read from the installed unit so the two cannot drift apart,
// and the address and port allowances of a restore.
func (a *App) sandboxFor(team string, port int) ([]string, error) {
	units, err := a.fsAt(unitsDir)
	if err != nil {
		return nil, failf("%v", err)
	}
	text, err := units.ReadFile(hsTemplate)
	if err != nil {
		return nil, failf("%v", err)
	}
	properties, err := sandboxProperties(string(text), team)
	if err != nil {
		return nil, err
	}
	return append(properties, "IPAddressAllow=127.0.0.1/32 ::1/128", "SocketBindAllow=tcp:"+strconv.Itoa(port)), nil
}

// sandboxProperties are the [Service] lines of the homeserver unit text, with %i replaced by
// the team, except ExecStart. Any other specifier is refused: it would mean something else
// in a transient unit.
func sandboxProperties(unitText, team string) ([]string, error) {
	var properties []string
	section := ""
	for raw := range strings.Lines(unitText) {
		line := strings.TrimRight(raw, "\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		key := line
		if i := strings.IndexByte(line, '='); i >= 0 {
			key = line[:i]
		}
		if section != "[Service]" || key == "ExecStart" {
			continue
		}
		line = strings.ReplaceAll(line, "%i", team)
		if strings.Contains(line, "%") {
			return nil, failf("%s: unsupported specifier in '%s'", hsTemplate, line)
		}
		properties = append(properties, line)
	}
	return properties, nil
}

// replaceAndRestart is everything from the stop on: the state as it was at the backup (files
// the backup did not hold are removed), the one-off restore of the database, the homeserver
// started again and bootstrapped.
func (a *App) replaceAndRestart(team, id string, port int, teamFS *fsx.FS, state *tarstate.State, properties []string) error {
	hs, once := hsUnit(team), "agent-bus-restore-"+team+".service"
	if err := a.replaceState(teamFS, state); err != nil {
		return err
	}
	if err := a.unstamp(team); err != nil {
		return err
	}
	sd := a.sd()
	if err := sd.RunTransient(a.ctx(), once, properties, restoredTuwunel, "--restore-backup", id); err != nil {
		return failf("%v", err)
	}
	if err := a.waitReady(once, port); err != nil {
		return err
	}
	if err := sd.Stop(a.ctx(), once); err != nil {
		return failf("%v", err)
	}
	if err := sd.Start(a.ctx(), hs); err != nil {
		return failf("%v", err)
	}
	a.changedf("restored team %s from backup %s", team, id)
	if err := a.waitReady(hs, port); err != nil {
		return err
	}
	restored, err := a.readTeamDoc(teamFS, "team.json")
	if err != nil {
		return err
	}
	if err := a.assertListening(port, restored.Listen); err != nil {
		return err
	}
	if err := a.admin().Bootstrap(a.ctx(), team); err != nil {
		return adminFailure(err, "bootstrap of team %s failed", team)
	}
	return nil
}

// replaceState swaps the team's state files for the validated state: directories 0700,
// files 0600 and the service account's, team.json 0640 for root and the service group.
func (a *App) replaceState(teamFS *fsx.FS, state *tarstate.State) error {
	svc, err := a.svcOwner()
	if err != nil {
		return failf("%v", err)
	}
	group, err := a.groupOwner()
	if err != nil {
		return failf("%v", err)
	}
	for _, name := range []string{"team.json", "registry.json", "room_id"} {
		if _, err = teamFS.RemoveIfPresent(name); err != nil {
			return failf("%v", err)
		}
	}
	if err = teamFS.RemoveAll("secrets"); err != nil {
		return failf("%v", err)
	}
	quiet := teamFS.Quiet()
	if err = writeStateMembers(quiet, state, svc); err != nil {
		return err
	}
	data, ok := state.File("team.json")
	if !ok {
		return failf("the state tar holds no team.json")
	}
	_, err = quiet.PutBytes("team.json", data, "team.json", groupReadMode, group)
	return wrapFS(err)
}

// writeStateMembers writes every member of the state but team.json, shallowest first.
func writeStateMembers(quiet *fsx.FS, state *tarstate.State, svc fsx.Owner) error {
	names := state.Names()
	slices.SortFunc(names, func(x, y string) int {
		if dx, dy := strings.Count(x, "/"), strings.Count(y, "/"); dx != dy {
			return dx - dy
		}
		return strings.Compare(x, y)
	})
	for _, name := range names {
		if name == "team.json" {
			continue
		}
		if err := ensureParents(quiet, name, svc); err != nil {
			return err
		}
		data, isFile := state.File(name)
		var err error
		if isFile {
			_, err = quiet.PutBytes("state file", data, name, privateFile, svc)
		} else {
			_, err = quiet.EnsureDir(name, privateMode, svc)
		}
		if err != nil {
			return failf("%v", err)
		}
	}
	return nil
}

// ensureParents makes the directories above name, shortest first.
func ensureParents(f *fsx.FS, name string, own fsx.Owner) error {
	dir := path.Dir(name)
	if dir == "." {
		return nil
	}
	if err := ensureParents(f, dir, own); err != nil {
		return err
	}
	if _, err := f.EnsureDir(dir, privateMode, own); err != nil {
		return failf("%v", err)
	}
	return nil
}
