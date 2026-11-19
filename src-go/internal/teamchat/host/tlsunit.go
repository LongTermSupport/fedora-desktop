package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/netip"
	"os"
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
	team paths.TeamID
	port int
	// perSource is the team's connections_per_source; 0, a team no declaration filled, is
	// paths.MaxConnectionsPerSource, the declaration's default.
	perSource int
	listen    []netip.Addr
	networks  []netip.Prefix
	devices   []string
}

func (d tlsDropInSpec) unit() string {
	if len(d.devices) == 0 {
		return "[Unit]\n"
	}
	devices := strings.Join(d.devices, " ")
	return "[Unit]\nAfter=" + devices + "\nWants=" + devices + "\n"
}

// execStart is the verb `teamchat tls` with the team, the port, the per-source cap and each
// listen address; none of it is secret, the leaf and its key come as credentials.
func (d tlsDropInSpec) execStart() string {
	perSource := d.perSource
	if perSource == 0 {
		perSource = paths.MaxConnectionsPerSource
	}
	args := make([]string, 0, 8+2*len(d.listen))
	args = append(args, paths.MemberBinaryPath, "tls", "--team", d.team.String(), "--port", strconv.Itoa(d.port),
		"--per-source", strconv.Itoa(perSource))
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
// terminator's drop-in written when it differs; systemd reloaded and the unit restarted when what
// it was last started on, which units.json records, is not this render's (its base unit, its
// drop-in, its program: HOST-2, HOST-3) or its leaf was made again (a unit reads its credentials
// only at its start); started when it is not running; enabled; and its bound addresses held to
// the listen set (X18, X4). The record is written after the start, so a write whose reload or
// restart failed is finished by the next apply, whatever the bytes on disk say.
func (a *applying) terminate(ctx context.Context) error {
	if err := a.convergeTLS(ctx); err != nil {
		return a.fail(StepTerminator, err)
	}
	return nil
}

func (a *applying) convergeTLS(ctx context.Context) error {
	changed := string(a.tlsDropIn) != string(a.r.tlsDropIn)
	owed := changed || a.units == nil || !maps.Equal(a.units.digests.Terminator, a.r.digests.Terminator)
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
	}
	if owed {
		if err := a.d.Systemd.DaemonReload(ctx); err != nil {
			return err
		}
	}
	st, err := a.d.Systemd.State(ctx, a.tlsUnit)
	if err != nil {
		return err
	}
	switch {
	case st.Active == active && (owed || a.leafMade):
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

// tlsBase is the terminator's base unit installed under root, which the render needs as it needs
// the homeserver's.
func tlsBase(root string) ([]byte, error) {
	text, found, err := state.ReadFile(root+paths.TLSUnitTemplateFile, fileMax)
	if err == nil && !found {
		err = fmt.Errorf("the base unit %s is not installed", paths.TLSUnitTemplateFile)
	}
	return text, err
}

// programDigest is the SHA-256 of the program the terminator runs, paths.MemberBinaryPath under
// root, its links followed: the play re-points that link at each build, and a running terminator
// keeps the program it was started with (HOST-2).
func programDigest(root string) (string, error) {
	f, err := os.Open(root + paths.MemberBinaryPath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("the terminator's program %s is not installed", paths.MemberBinaryPath)
	}
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	if err = errors.Join(err, f.Close()); err != nil {
		return "", fmt.Errorf("the terminator's program %s: %w", paths.MemberBinaryPath, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
