package businstall

import (
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/netprobe"
)

const (
	// stateActive is systemd's ActiveState of a running unit.
	stateActive = "active"

	readyTries      = 60
	journalLines    = 40
	backupWaitTries = 60
)

func (a *App) unitState(unit string) (string, error) {
	state, err := a.sd().Show(a.ctx(), "ActiveState", unit)
	if err != nil {
		return "", failf("%v", err)
	}
	return state, nil
}

// requireRunning fails unless every unit is active.
func (a *App) requireRunning(units ...string) error {
	for _, unit := range units {
		state, err := a.unitState(unit)
		if err != nil {
			return err
		}
		if state != stateActive {
			return failf("%s is %s; the homeserver needs it running", unit, state)
		}
	}
	return nil
}

// printJournal puts the unit's last journal lines on stderr, so the run's log says why a
// homeserver exited.
func (a *App) printJournal(unit string) {
	a.sayf("the last %d journal lines of %s:", journalLines, unit)
	if err := a.sd().Journal(a.ctx(), unit, journalLines, a.stderr); err != nil {
		a.sayf("journalctl could not read the journal of %s", unit)
	}
}

// waitReady polls the homeserver until it answers. It fails as soon as the unit is not
// active (an unknown tuwunel.toml key makes Tuwunel exit): the verdict is the unit's state,
// never a scan of the journal, which is printed only to explain a failure.
func (a *App) waitReady(unit string, port int) error {
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/_tuwunel/server_version"
	answer := ""
	for range readyTries {
		out, err := a.net().Probe(a.ctx(), url)
		if err == nil {
			a.sayf("%s answers on 127.0.0.1:%d", unit, port)
			return nil
		}
		answer = strings.TrimRight(out, "\n")
		state, stateErr := a.unitState(unit)
		if stateErr != nil {
			return stateErr
		}
		if state != stateActive {
			sub, subErr := a.sd().Show(a.ctx(), "SubState", unit)
			if subErr != nil {
				return failf("%v", subErr)
			}
			a.printJournal(unit)
			return failf("readiness: %s is %s (%s), not active: it did not start (an unknown or "+
				"malformed tuwunel.toml key is one cause). See: systemctl status --no-pager %s; "+
				"journalctl -u %s --no-pager", unit, state, sub, unit, unit)
		}
		a.cfg.Sleep(time.Second)
	}
	a.printJournal(unit)
	return failf("readiness: %s is active but %s did not answer in %d s: %s", unit, url, readyTries, answer)
}

// assertListening fails unless exactly loopback and the listen addresses are bound on port.
func (a *App) assertListening(port int, listen []string) error {
	out, err := a.net().Listening(a.ctx(), port)
	if err != nil {
		return failf("%v", err)
	}
	if err := netprobe.AssertListening(port, out, listen); err != nil {
		return failf("%v", err)
	}
	return nil
}
