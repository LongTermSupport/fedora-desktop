package nord

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	progNmcli     = "nmcli"
	argConnection = "connection"
	// prefix starts the name of every NetworkManager profile nord owns (E11).
	prefix = "nordvpn-"
	// stateActivated is nmcli's state of a connection that is up.
	stateActivated = "activated"
)

// nmcli's passwd-file line for a VPN's password is <setting>.<property>.<key>:<value> (nmcli(1),
// `connection up ... passwd-file`); nord gives it on nmcli's stdin (D4).
const (
	vpnSetting         = "vpn"
	vpnSecretsProperty = "secrets"
	vpnPasswordKey     = "password"
	passwdKey          = vpnSetting + "." + vpnSecretsProperty + "." + vpnPasswordKey + ":"
)

// importedUUID is the UUID in nmcli's "Connection '<id>' (<uuid>) successfully added."
var importedUUID = regexp.MustCompile(`\(([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\)`)

// nmProfile is one of nord's NetworkManager profiles: its name less the prefix, its UUID, and,
// when it is active, its device and state.
type nmProfile struct {
	name, uuid, device, state string
}

func (p nmProfile) activated() bool { return p.state == stateActivated }

// splitTerse is one line of nmcli -t: its fields split at unescaped colons and unescaped.
func splitTerse(line string) []string {
	var fields []string
	var b strings.Builder
	escaped := false
	for _, ch := range line {
		switch {
		case escaped:
			b.WriteRune(ch)
			escaped = false
		case ch == '\\':
			escaped = true
		case ch == ':':
			fields = append(fields, b.String())
			b.Reset()
		default:
			b.WriteRune(ch)
		}
	}
	return append(fields, b.String())
}

// listed is nord's profiles in an nmcli -t listing whose first three fields are NAME, UUID and
// TYPE, with DEVICE and STATE after them when the listing is of the active ones (E11).
func listed(out []byte, active bool) []nmProfile {
	var ps []nmProfile
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := splitTerse(line)
		if len(f) < 3 || f[2] != "vpn" || !strings.HasPrefix(f[0], prefix) {
			continue
		}
		p := nmProfile{name: strings.TrimPrefix(f[0], prefix), uuid: f[1]}
		if active && len(f) >= 5 {
			p.device, p.state = f[3], f[4]
		}
		ps = append(ps, p)
	}
	return ps
}

// profiles are nord's profiles (E11); false after saying the listing failed.
func (r *run) profiles(ctx context.Context) ([]nmProfile, bool) {
	return r.listing(ctx, false, "-t", "-f", "NAME,UUID,TYPE", "connection", "show")
}

// active are nord's active profiles (E11).
func (r *run) active(ctx context.Context) ([]nmProfile, bool) {
	return r.listing(ctx, true, "-t", "-f", "NAME,UUID,TYPE,DEVICE,STATE", "connection", "show", "--active")
}

func (r *run) listing(ctx context.Context, active bool, args ...string) ([]nmProfile, bool) {
	res, err := r.runner.Run(ctx, execx.Cmd{Name: progNmcli, Args: args})
	if ctx.Err() != nil {
		return nil, false
	}
	if err != nil {
		r.failf("nmcli could not list the connections: %s", reason(res, err))
		return nil, false
	}
	return listed(res.Stdout, active), true
}

// nmChange runs an nmcli command that changes something, waited for even when the run is
// cancelled (E24); the error says what nmcli said.
func (r *run) nmChange(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	res, err := r.runner.Run(changing(ctx), c)
	if err != nil {
		return res, errors.New(reason(res, err))
	}
	return res, nil
}

// nmByUUID is `nmcli connection <verb> uuid <uuid> <more>...`: every command after a listing
// names its profile by UUID (E11, B8).
func nmByUUID(verb, uuid string, more ...string) execx.Cmd {
	return execx.Cmd{Name: progNmcli, Args: append([]string{argConnection, verb, "uuid", uuid}, more...)}
}

// reason is what a failed command said: its stderr, else its stdout, else the error.
func reason(res execx.Result, err error) string {
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok && exitErr != nil {
		for _, text := range []string{string(res.Stderr), string(res.Stdout)} {
			if s := strings.TrimSpace(text); s != "" {
				return s
			}
		}
	}
	return err.Error()
}

// connect is `nord connect` and `nord switch` (E13-E16).
func (r *run) connect(ctx context.Context, name string) term.ExitCode {
	creds, ok := r.check(ctx, needConnect)
	if !ok {
		return term.ExitFailure
	}
	cfg, ok := r.resolve(name)
	if !ok {
		return term.ExitFailure
	}
	return r.connectTo(ctx, cfg, creds)
}

// connectTo is E12-E15 for a config found: its profile imported when there is none, every other
// nord connection brought down, then this one brought up.
func (r *run) connectTo(ctx context.Context, cfg config, creds credentials) term.ExitCode {
	all, ok := r.profiles(ctx)
	if !ok {
		return term.ExitFailure
	}
	up, ok := r.active(ctx)
	if !ok {
		return term.ExitFailure
	}
	uuid, ok := r.profileFor(ctx, cfg, all, creds)
	if !ok {
		return term.ExitFailure
	}
	if isActivated(up, uuid) {
		r.say("Already connected to " + cfg.name + ".")
		return term.ExitOK
	}
	for _, p := range up {
		if p.uuid == uuid {
			continue
		}
		r.say("Disconnecting " + p.name + "...")
		if _, err := r.nmChange(ctx, nmByUUID("down", p.uuid)); err != nil {
			r.failf("could not disconnect %s: %v; not connecting %s beside it.", p.name, err, cfg.name)
			return term.ExitFailure
		}
		if ctx.Err() != nil {
			return term.ExitCancelled
		}
	}
	return r.bringUp(ctx, cfg, uuid, creds)
}

// profileFor is the UUID of cfg's profile, imported when there is none (E12, E13); false after
// saying why there is not one.
func (r *run) profileFor(ctx context.Context, cfg config, all []nmProfile, creds credentials) (string, bool) {
	var uuids []string
	for _, p := range all {
		if p.name == cfg.name {
			uuids = append(uuids, p.uuid)
		}
	}
	switch len(uuids) {
	case 0:
		return r.importConfig(ctx, cfg, creds)
	case 1:
		return uuids[0], true
	}
	r.failf("two NetworkManager connections are named %s%s (%s); run 'nord cleanup' and connect again.",
		prefix, cfg.name, strings.Join(uuids, ", "))
	return "", false
}

// bringUp is E14's connect, the password on nmcli's stdin.
func (r *run) bringUp(ctx context.Context, cfg config, uuid string, creds credentials) term.ExitCode {
	r.say("Connecting to " + cfg.name + "...")
	up := nmByUUID("up", uuid, "passwd-file", "/dev/stdin")
	up.Feed = &execx.Feed{Secret: creds.password, Before: passwdKey, After: "\n"}
	_, err := r.nmChange(ctx, up)
	if err != nil {
		r.failf("could not connect to %s: %v", cfg.name, err)
		r.say("Troubleshooting:")
		r.say("  1. NetworkManager's log: journalctl -u NetworkManager --no-pager --since '5 minutes ago'")
		r.say("  2. Import it again: nord cleanup && nord connect " + cfg.name)
		r.say(fmt.Sprintf("  3. The credentials are %s; %s writes them.", r.abbrev(r.path(accountFileRel)), play))
		return term.ExitFailure
	}
	r.say("Connected to " + cfg.name + ".")
	return term.ExitOK
}

// importConfig is E13: the config imported, renamed and given the username; its UUID, or false
// after saying why, with nothing half-made left.
func (r *run) importConfig(ctx context.Context, cfg config, creds credentials) (string, bool) {
	r.say("Importing " + cfg.name + "...")
	res, err := r.nmChange(ctx, execx.Cmd{Name: progNmcli, Args: []string{argConnection, "import", "type", "openvpn", "file", cfg.path}})
	if err != nil {
		r.failf("could not import %s: %v", cfg.name, err)
		return "", false
	}
	m := importedUUID.FindStringSubmatch(string(res.Stdout))
	if m == nil {
		r.failf("nmcli imported %s but named no connection UUID: %s", cfg.name, strings.TrimSpace(string(res.Stdout)))
		return "", false
	}
	uuid := m[1]
	if ctx.Err() != nil {
		r.removeHalfImport(ctx, uuid)
		return "", false
	}
	_, err = r.nmChange(ctx, nmByUUID("modify", uuid, "connection.id", prefix+cfg.name, "+vpn.data", "username="+creds.user))
	if err != nil {
		r.failf("could not configure the imported %s: %v", cfg.name, err)
		r.removeHalfImport(ctx, uuid)
		return "", false
	}
	r.say("Imported " + prefix + cfg.name + ".")
	return uuid, ctx.Err() == nil
}

// removeHalfImport deletes a profile the import made and could not finish (E13, B9).
func (r *run) removeHalfImport(ctx context.Context, uuid string) {
	if _, err := r.nmChange(ctx, nmByUUID("delete", uuid)); err != nil {
		r.failf("could not remove the half-imported connection %s: %v", uuid, err)
		return
	}
	r.say("Removed the half-imported connection.")
}
