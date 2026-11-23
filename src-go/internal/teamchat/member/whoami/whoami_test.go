package whoami

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/duty"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const runID = "00112233445566ff"

type world struct {
	t           *testing.T
	run         paths.Run
	env         map[string]string
	out, stderr bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	base := t.TempDir()
	home, xdg := filepath.Join(base, "h"), filepath.Join(base, "x")
	layout, err := paths.HomeLayout(home, xdg)
	if err != nil {
		t.Fatal(err)
	}
	id, err := paths.ParseRunID(runID)
	if err != nil {
		t.Fatal(err)
	}
	run := layout.Run(id)
	if err = os.MkdirAll(run.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	return &world{t: t, run: run, env: map[string]string{"HOME": home, "XDG_RUNTIME_DIR": xdg, paths.EnvRuntime: run.Dir()}}
}

func (w *world) status(run string, teams ...state.StatusTeam) {
	w.t.Helper()
	id, err := paths.ParseRunID(run)
	if err != nil {
		w.t.Fatal(err)
	}
	if teams == nil {
		teams = []state.StatusTeam{}
	}
	root, err := homefs.Open(w.run.Dir(), homefs.Private)
	if err != nil {
		w.t.Fatal(err)
	}
	defer func() {
		if cerr := root.Close(); cerr != nil {
			w.t.Error(cerr)
		}
	}()
	st := state.Status{Run: id, WrittenMS: 1_760_000_000_000, Desired: state.DesiredApplied, Teams: teams}
	if err := state.Write(root, w.run.Status(), st); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) whoami() term.ExitCode {
	w.t.Helper()
	e := cli.Env{Stdout: &w.out, Stderr: &w.stderr, Getenv: func(k string) string { return w.env[k] }}
	return Main(e, cli.Invocation{})
}

func row(t *testing.T, team, handle, role string, d duty.Duty) state.StatusTeam {
	t.Helper()
	id, err := paths.ParseTeamID(team)
	if err != nil {
		t.Fatal(err)
	}
	r := state.StatusTeam{Team: id, Delivery: state.DeliverySocket, Colour: "#2dd4bf"}
	if handle != "" {
		short := strings.SplitN(handle, "+", 2)[0]
		r.Handle, r.Member = &handle, &short
	}
	if role != "" {
		r.Role = &role
	}
	if d != "" {
		r.Duty = &d
	}
	return r
}

// The contract: one entry per held seat whose handle is known, its team, handle, role and duty;
// nothing else.
func TestPrintsEachHeldSeat(t *testing.T) {
	w := newWorld(t)
	w.status(runID,
		row(t, "alpha", "repo.1+desk.host", "worker", duty.Host),
		row(t, "beta", "repo.2+desk.host", "coordinator", ""),
		row(t, "gamma", "", "", ""))
	if code := w.whoami(); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %s", code, w.stderr.String())
	}
	want := `{"v":1,"teams":[{"team":"alpha","handle":"repo.1+desk.host","role":"worker","duty":"host"},` +
		`{"team":"beta","handle":"repo.2+desk.host","role":"coordinator","duty":null}]}` + "\n"
	if w.out.String() != want {
		t.Fatalf("stdout\n%s\nwant\n%s", w.out.String(), want)
	}
	if w.stderr.Len() != 0 {
		t.Fatalf("stderr %s", w.stderr.String())
	}
}

func TestNoSeatIsAnEmptyList(t *testing.T) {
	w := newWorld(t)
	w.status(runID)
	if code := w.whoami(); code != term.ExitOK || w.out.String() != `{"v":1,"teams":[]}`+"\n" {
		t.Fatalf("exit %d, stdout %s", code, w.out.String())
	}
}

func TestNoSessionIs69(t *testing.T) {
	w := newWorld(t)
	delete(w.env, paths.EnvRuntime)
	if code := w.whoami(); code != exits.ExitNoRuntime || w.out.Len() != 0 || !strings.Contains(w.stderr.String(), `"code":"no-session"`) {
		t.Fatalf("exit %d, stdout %s, stderr %s", code, w.out.String(), w.stderr.String())
	}
}

func TestNoStatusFileIs69NoLink(t *testing.T) {
	w := newWorld(t)
	code := w.whoami()
	if code != exits.ExitNoRuntime || w.out.Len() != 0 || !strings.Contains(w.stderr.String(), `"code":"no-link"`) ||
		!strings.Contains(w.stderr.String(), "status.json") {
		t.Fatalf("exit %d, stdout %s, stderr %s", code, w.out.String(), w.stderr.String())
	}
}

func TestAnotherRunsOrABrokenFileIs78(t *testing.T) {
	w := newWorld(t)
	w.status("0011223344556677")
	if code := w.whoami(); code != exits.ExitConfig || w.out.Len() != 0 || !strings.Contains(w.stderr.String(), `"code":"runtime"`) {
		t.Fatalf("another run's: exit %d, stderr %s", code, w.stderr.String())
	}
	w = newWorld(t)
	if err := os.WriteFile(w.run.Status(), []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.whoami(); code != exits.ExitConfig || w.out.Len() != 0 {
		t.Fatalf("broken: exit %d, stderr %s", code, w.stderr.String())
	}
}
