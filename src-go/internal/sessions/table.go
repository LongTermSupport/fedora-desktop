package sessions

import (
	"context"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const unknownColumn = "unknown"

// tableRow is one session as the table and the picker show it.
type tableRow struct {
	Name     string
	Attached int
	Dir      string
	Network  string
	Token    string
	Keys     string
	CPU      string
}

// elsewhere says whether another terminal is on the session. The picker acts on this, never
// on the row's text, so a directory or name that spells "open elsewhere" cannot fool it.
func (r tableRow) elsewhere() bool { return r.Attached != 0 }

// text is the row in the house format. A directory under home reads with "~".
func (r tableRow) text(home string) string {
	state := "detached"
	if r.elsewhere() {
		state = "open elsewhere"
	}
	return fullRow(r.Name, state, r.CPU, r.Network, r.Token, r.Keys, term.AbbrevHome(r.Dir, home))
}

// fullRow is the layout shared by the rows and the heading above them. The last column is not
// padded.
func fullRow(name, state, cpu, network, token, keys, dir string) string {
	return fmt.Sprintf("%-28s  %-15s  %5s  %-22s  %-16s  %-16s  %s", name, state, cpu, network, token, keys, dir)
}

// heading is the column titles over the rows.
func heading() string {
	return fullRow("SESSION", "STATE", "CPU", "NETWORK", "TOKEN", "SSH KEY", "DIRECTORY")
}

// rows is every session as a table row. The network, token, key and CPU columns come from one
// pass over the host, not one lookup per row; a probe that fails leaves them reading "unknown"
// on every row, because a blank one would be read as "none". The reason is on stderr. A
// session list that cannot be read is an error: it must not pass for "no sessions".
func (a *App) rows(ctx context.Context) ([]tableRow, error) {
	byName := map[string]tableRow{}
	details, err := a.Probe.Detail(ctx)
	if err != nil {
		a.failErr(err)
		a.sayf("The CPU, network, token and SSH key columns read 'unknown' for every session — see the error above.\n")
	}
	for _, d := range details {
		byName[d.Session] = tableRow{Network: d.Network, Token: d.Token, Keys: d.Keys, CPU: d.CPU}
	}
	sessions, err := a.Tmux.List(ctx)
	if err != nil {
		a.failErr(err)
		return nil, errReported
	}
	rows := make([]tableRow, 0, len(sessions))
	for _, s := range sessions {
		r, found := byName[s.Name]
		if !found {
			r = tableRow{Network: unknownColumn, Token: unknownColumn, Keys: unknownColumn, CPU: unknownColumn}
		}
		r.Name, r.Attached, r.Dir = s.Name, s.Attached, s.Path
		rows = append(rows, r)
	}
	return rows, nil
}

// list prints the heading and the rows on stdout, with no terminal needed.
func (a *App) list(ctx context.Context) term.ExitCode {
	rows, err := a.rows(ctx)
	if err != nil {
		return term.ExitFailure
	}
	if len(rows) == 0 {
		a.sayf("No CCY or cc sessions are running.\n")
		return term.ExitOK
	}
	home := a.Getenv("HOME")
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, r.text(home))
	}
	a.out(heading() + "\n" + strings.Join(lines, "\n") + "\n")
	return term.ExitOK
}
