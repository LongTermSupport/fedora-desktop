package nord

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
)

// need is what a command uses, so what it checks before it acts (E8).
type need struct {
	networkManager bool
	connect        bool
}

var (
	needNM      = need{networkManager: true}
	needConnect = need{networkManager: true, connect: true}
)

// credentials are the account's service credentials (E10), both stored in the profile: the
// username through an argv, the password only through nmcli's stdin (D4). hide replaces the
// password in text nmcli printed, before it is shown.
type credentials struct {
	user     string
	password execx.Secret
	hide     func(string) string
}

// feedVar names the password inside nord; nmcli reads it from its stdin, never by name.
const feedVar = "NORDVPN_LOGIN"

// check is E8-E10: every missing piece the command needs is said, then false. The credentials
// are read when the command connects.
func (r *run) check(ctx context.Context, n need) (credentials, bool) {
	ok := true
	if n.networkManager {
		ok = r.checkNetworkManager(ctx) && ok
	}
	if !n.connect {
		return credentials{}, ok
	}
	if _, err := r.env.LookPath("openvpn"); err != nil {
		r.failf("openvpn is not installed; run %s, which installs it.", play)
		ok = false
	}
	if _, err := r.configs(); err != nil {
		r.failf("%v.", err)
		ok = false
	}
	creds, problem := r.readCredentials()
	if problem != "" {
		r.failf("%s: %s; run %s, which writes it.", r.abbrev(r.path(accountFileRel)), problem, play)
		ok = false
	}
	return creds, ok
}

func (r *run) checkNetworkManager(ctx context.Context) bool {
	ok := true
	if _, err := r.env.LookPath(progNmcli); err != nil {
		r.failf("nmcli is not installed; nord manages its connections through NetworkManager.")
		ok = false
	}
	active, err := (&systemd.Systemd{Runner: r.runner}).UnitActive(ctx, "NetworkManager")
	switch {
	case err != nil:
		r.failf("could not ask systemd whether NetworkManager is running: %v", err)
		ok = false
	case !active:
		r.failf("NetworkManager is not running; nord manages its connections through it.")
		ok = false
	}
	return ok
}

// readCredentials is E10: the username on line 1 and the password on line 2, each less its line
// ending only; what is wrong with the file when it is not that.
func (r *run) readCredentials() (credentials, string) {
	data, err := os.ReadFile(r.path(accountFileRel))
	if errors.Is(err, fs.ErrNotExist) {
		return credentials{}, "missing"
	}
	if err != nil {
		return credentials{}, fmt.Sprintf("cannot be read (%v)", err)
	}
	lines := strings.Split(string(data), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	if lines[0] == "" {
		return credentials{}, "the username (line 1) is empty"
	}
	if len(lines) < 2 || lines[1] == "" {
		return credentials{}, "the password (line 2) is empty"
	}
	for _, extra := range lines[2:] {
		if strings.TrimSpace(extra) != "" {
			return credentials{}, "a third line; expected the username on line 1 and the password on line 2 only"
		}
	}
	secret, err := execx.NewSecret(feedVar, lines[1])
	if err != nil {
		return credentials{}, err.Error()
	}
	password := lines[1]
	hide := func(text string) string { return strings.ReplaceAll(text, password, "<password>") }
	return credentials{user: lines[0], password: secret, hide: hide}, ""
}
