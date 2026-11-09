// Package ghlogin is gh's github.com logins as this machine holds them, and each login's own
// token, so a command can act as one login (its GH_TOKEN) without `gh auth switch`: switching
// changes the active account under every other session that is running gh at the time.
package ghlogin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// Host is the one GitHub host the logins are read for.
const Host = "github.com"

// Status is what `gh auth status --json hosts` says of github.com.
type Status struct {
	Active  string   // the logged-in active account, "" for none
	Working []string // every account whose token is good, in gh's order
}

// LoggedIn reports whether login (in any case) is a working login.
func (s Status) LoggedIn(login string) bool { return s.Login(login) != "" }

// Login is the working login matching login, spelled as gh holds it; "" when none is.
func (s Status) Login(login string) string {
	for _, l := range s.Working {
		if strings.EqualFold(l, login) {
			return l
		}
	}
	return ""
}

// Parse reads gh's status JSON; anything else is refused.
func Parse(out []byte) (Status, error) {
	var doc struct {
		Hosts map[string][]struct {
			Login  string `json:"login"`
			Active bool   `json:"active"`
			State  string `json:"state"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return Status{}, fmt.Errorf("not gh's status JSON: %w", err)
	}
	if doc.Hosts == nil {
		return Status{}, errors.New("not gh's status JSON: no hosts")
	}
	var st Status
	for _, e := range doc.Hosts[Host] {
		if e.State != "success" {
			continue
		}
		st.Working = append(st.Working, e.Login)
		if e.Active {
			st.Active = e.Login
		}
	}
	return st, nil
}

// Read is gh's local view of the github.com accounts. A gh that refused is returned wrapping
// its *execx.ExitError, so a caller can say what gh said.
func Read(ctx context.Context, r execx.Runner) (Status, error) {
	res, err := r.Run(ctx, execx.Cmd{Name: "gh", Args: []string{"auth", "status", "--hostname", Host, "--json", "hosts"}})
	if err != nil {
		return Status{}, fmt.Errorf("gh auth status failed: %w", err)
	}
	st, err := Parse(res.Stdout)
	if err != nil {
		return Status{}, fmt.Errorf("gh auth status: %w: %q", err, strings.TrimSpace(string(res.Stdout)))
	}
	return st, nil
}

// Token is login's own gh token as GH_TOKEN, so a call given it acts as login and not as the
// active account.
func Token(ctx context.Context, r execx.Runner, login string) (execx.Secret, error) {
	res, err := r.Run(ctx, execx.Cmd{Name: "gh", Args: []string{"auth", "token", "--hostname", Host, "--user", login}})
	if err != nil {
		return execx.Secret{}, fmt.Errorf("gh holds no token for %s: %w", login, err)
	}
	s, err := execx.NewSecret("GH_TOKEN", strings.TrimSpace(string(res.Stdout)))
	if err != nil {
		return execx.Secret{}, fmt.Errorf("gh's token for %s: %w", login, err)
	}
	return s, nil
}
