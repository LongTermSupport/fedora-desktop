package businstall

import (
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/firewalld"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/netprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// unitProperties are the hardening properties of the homeserver unit that check reports.
var unitProperties = []string{
	"User", "IPAddressDeny", "IPAddressAllow", "RestrictAddressFamilies", "IPAccounting",
	"SocketBindDeny", "SocketBindAllow",
}

// facts prints CHECK lines and remembers whether any was a failure.
type facts struct {
	app    *App
	failed bool
}

func (f *facts) fact(name, verdict, value string) {
	f.app.stdout.printf("CHECK\t%s\t%s\t%s\n", name, verdict, value)
	if verdict == "FAIL" {
		f.failed = true
	}
}

func verdict(ok bool) string {
	if ok {
		return "ok"
	}
	return "FAIL"
}

// check prints the construction facts the privacy checks rely on, one CHECK line each, and
// exits 1 if any is a failure, after printing them all. It changes nothing.
func (a *App) check(args []string) error {
	opts, err := parseOptions(args, map[string]bool{optTeam: true})
	if err != nil {
		return err
	}
	team, err := opts.team("check")
	if err != nil {
		return err
	}
	if err = a.requireRoot(); err != nil {
		return err
	}
	teamFS, err := a.requireTeamDir(team)
	if err != nil {
		return err
	}
	doc, err := a.readTeamDoc(teamFS, "team.json")
	if err != nil {
		return err
	}
	f := &facts{app: a}
	if err := a.checkUnit(f, team); err != nil {
		return err
	}
	if err := a.checkConstruction(f, team, doc); err != nil {
		return err
	}
	if f.failed {
		return &failure{code: term.ExitFailure}
	}
	return nil
}

func (a *App) checkUnit(f *facts, team string) error {
	hs := hsUnit(team)
	sd := a.sd()
	state, err := a.unitState(hs)
	if err != nil {
		return err
	}
	f.fact("ActiveState", verdict(state == stateActive), state)
	fileState, err := sd.Show(a.ctx(), "UnitFileState", hs)
	if err != nil {
		return failf("%v", err)
	}
	f.fact("UnitFileState", verdict(fileState == "enabled"), fileState)
	for _, property := range unitProperties {
		// A list property set more than once (SocketBindDeny per family) comes one per line.
		value, err := sd.Show(a.ctx(), property, hs)
		if err != nil {
			return failf("%v", err)
		}
		f.fact(property, "info", strings.ReplaceAll(value, "\n", " "))
	}
	return nil
}

func (a *App) checkConstruction(f *facts, team string, doc teamDoc) error {
	dropin := dropinName(team) + "/network.conf"
	present := false
	if units, found, err := a.tryFS(unitsDir); err != nil {
		return failf("%v", err)
	} else if found {
		info, statErr := units.Stat(dropin)
		present = statErr == nil && info.Mode().IsRegular()
	}
	f.fact("drop-in", verdict(present), unitsDir+"/"+dropin)
	tld := doc.ServerName
	if i := strings.LastIndex(tld, "."); i >= 0 {
		tld = tld[i+1:]
	}
	f.fact("server_name", verdict(tld == "internal"), doc.ServerName)
	out, err := a.net().Listening(a.ctx(), doc.Port)
	if err != nil {
		return failf("%v", err)
	}
	got := netprobe.BoundAddresses(out)
	f.fact("listen", verdict(slices.Equal(got, netprobe.ExpectedAddresses(doc.Listen))),
		"port "+strconv.Itoa(doc.Port)+" bound on: "+strings.Join(got, " "))
	if err := a.checkFirewall(f, team); err != nil {
		return err
	}
	binary := a.path(usrLocalDir + "/" + libRel + "/tuwunel")
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	} else if !errors.Is(err, fs.ErrNotExist) {
		return failf("%v", err)
	}
	f.fact("binary", "info", binary)
	return nil
}

// checkFirewall asks the permanent configuration for each rule in the record.
func (a *App) checkFirewall(f *facts, team string) error {
	record, err := a.fwRecord(team)
	if err != nil {
		return failf("%v", err)
	}
	data, _, err := record.Read()
	if err != nil {
		return failf("%v", err)
	}
	rules, err := firewalld.ParseRecord(data)
	if err != nil {
		return failf("%v", err)
	}
	absent := 0
	for _, rule := range rules {
		present, err := a.fw().Query(a.ctx(), rule.Zone, rule.Text, true)
		if err != nil {
			return failf("%v", err)
		}
		if !present {
			absent++
		}
	}
	f.fact("firewalld", verdict(absent == 0), strconv.Itoa(len(rules))+" rule(s) recorded, "+
		strconv.Itoa(absent)+" missing from the permanent configuration")
	return nil
}
