package host

import (
	"context"
	"fmt"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The team's TLS terminator is a second unit, teamchat-tls@<team> (Plan 00246 SPEC-cross-network
// X18): it accepts TLS on the derived listen addresses and relays to the homeserver, which binds
// loopback alone. The terminator's drop-in carries what is a function of the effective networks
// (X4): the listen addresses it is started with, the device units those addresses wait for, the
// unit's IP filter and its socket bind. The homeserver's own drop-in and config do not change when
// a network does.

// tlsDropInSpec is what the terminator's drop-in says.
type tlsDropInSpec struct {
	team     paths.TeamID
	port     int
	listen   []netip.Addr
	networks []netip.Prefix
	devices  []string
}

func (d tlsDropInSpec) unit() string {
	if len(d.devices) == 0 {
		return "[Unit]\n"
	}
	devices := strings.Join(d.devices, " ")
	return "[Unit]\nAfter=" + devices + "\nWants=" + devices + "\n"
}

// execStart is the verb `teamchat tls` with the team, the port and each listen address; none of
// it is secret, the leaf and its key come as credentials.
func (d tlsDropInSpec) execStart() string {
	args := make([]string, 0, 6+2*len(d.listen))
	args = append(args, paths.MemberBinaryPath, "tls", "--team", d.team.String(), "--port", strconv.Itoa(d.port))
	for _, a := range d.listen {
		args = append(args, "--listen", a.String())
	}
	return "ExecStart=" + strings.Join(args, " ") + "\n"
}

// credentials are the leaf and its key, the only secrets the unit holds; the CA's key stays with
// root (X13).
func (d tlsDropInSpec) credentials() string {
	return "LoadCredential=" + paths.LeafCertCredential + ":" + paths.LeafCertFile(d.team) + "\n" +
		"LoadCredential=" + paths.LeafKeyCredential + ":" + paths.LeafKeyFile(d.team) + "\n"
}

func (d tlsDropInSpec) text() []byte {
	return []byte(header(d.team) + d.unit() + "\n[Service]\n" + d.execStart() + d.credentials() +
		ipFilterLines(d.listen, d.networks) + socketBindLines(d.port))
}

// terminate is the step after the homeserver is ready and bound to loopback alone: the
// terminator's drop-in written when it differs and systemd reloaded for it, the unit started, or
// restarted when its drop-in changed or its leaf was made again (a unit reads its credentials only
// at its start), enabled, and its bound addresses held to the listen set (X18, X4).
func (a *applying) terminate(ctx context.Context) error {
	if err := a.convergeTLS(ctx); err != nil {
		return a.fail(StepTerminator, err)
	}
	return nil
}

func (a *applying) convergeTLS(ctx context.Context) error {
	changed := string(a.tlsDropIn) != string(a.r.tlsDropIn)
	if changed {
		file := a.path(paths.TLSDropInFile(a.team))
		if err := MkdirShared(filepath.Dir(file)); err != nil {
			return err
		}
		if err := state.WriteFile(file, a.r.tlsDropIn, 0o644, -1); err != nil {
			return err
		}
		a.tlsDropIn = a.r.tlsDropIn
		a.note("tls drop-in written")
		if err := a.d.Systemd.DaemonReload(ctx); err != nil {
			return err
		}
	}
	st, err := a.d.Systemd.State(ctx, a.tlsUnit)
	if err != nil {
		return err
	}
	switch {
	case st.Active == active && (changed || a.leafMade):
		err = a.d.Systemd.Restart(ctx, a.tlsUnit)
		a.noteIf(err, "tls unit restarted")
	case st.Active != active:
		err = a.d.Systemd.Start(ctx, a.tlsUnit)
		a.noteIf(err, "tls unit started")
	}
	if err == nil && !st.Enabled {
		err = a.d.Systemd.Enable(ctx, a.tlsUnit)
		a.noteIf(err, "tls unit enabled")
	}
	if err != nil {
		return err
	}
	return awaitBound(ctx, a.d, a.tlsUnit, a.in.Team.Port, a.listenText())
}

// awaitBound waits, as readiness does, for the unit to be active with its sockets on port bound
// to exactly want.
func awaitBound(ctx context.Context, d Deps, unit string, port int, want []string) error {
	return waitUnit(ctx, d, unit, func(context.Context) error { return boundAs(d, unit, port, want) })
}

// tlsBaseInstalled is nil when the terminator's base unit is installed under root, which the
// render needs as it needs the homeserver's.
func tlsBaseInstalled(root string) error {
	_, found, err := state.ReadFile(root+paths.TLSUnitTemplateFile, fileMax)
	if err == nil && !found {
		err = fmt.Errorf("the base unit %s is not installed", paths.TLSUnitTemplateFile)
	}
	return err
}
