package statusline

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
)

// SPEC section 8: every on-disk format the port reads is written here by the code that writes it
// in use, and the render must read each one as the Python does. testdata/write_formats.py runs
// the Python writers from this checkout: the session record (hooks.record_session_teams), the
// bundle (agent_bus.admin's bundle, unpacked by agent_bus.checkout.read_bundle), team.json
// (inbox.TeamState.save_team_record), the down mark (statusline.set_team_down) and applied.json
// (membership.write_applied). membership.json is written by the Go ccy-teams writer. A writer
// that changes its format so the port no longer reads it fails this test.
func TestReadsWhatTheRealWritersWrite(t *testing.T) {
	root := t.TempDir()
	writeFormats(t, root)
	desired := membership.Desired{
		V: 1, Session: "ccy-x-1", Kind: "ccy", Checkout: "/srv/x", Generation: 3, WrittenMS: 1,
		Seats: []membership.Seat{{Team: "alpha", Seat: "1"}}, Farewell: []membership.Farewell{},
	}
	if err := membership.Write(filepath.Join(root, "ccy", "membership"), desired); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		innerVar:        "1",
		"HOME":          filepath.Join(root, "home"),
		"PINGBUS_HOME":  filepath.Join(root, "pingbus"),
		sessionsDirVar:  filepath.Join(root, "sessions"),
		"PINGBUS_TEAMS": "alpha,beta",
		"COLUMNS":       "200",
	}
	in := Input{Env: env, Cwd: root, Stdin: []byte(`{"session_id":"sess-1"}`)}

	var stderr bytes.Buffer
	out, code := Run(context.Background(), in, &stderr)
	if want := "alpha (coordinator), beta (worker) (down)"; code != 0 || !strings.Contains(string(out), want) || stderr.Len() != 0 {
		t.Errorf("the session record, bundles, team records and down mark: exit %d, %q, stderr %q; want %q",
			code, out, stderr.String(), want)
	}

	delete(env, "PINGBUS_TEAMS")
	env[membershipVar] = filepath.Join(root, "ccy", "membership")
	stderr.Reset()
	out, code = Run(context.Background(), in, &stderr)
	if code != 0 || !strings.Contains(string(out), "alpha (coordinator)") || strings.Contains(string(out), "beta") || stderr.Len() != 0 {
		t.Errorf("membership.json and applied.json: exit %d, %q, stderr %q; want alpha alone", code, out, stderr.String())
	}
}

// writeFormats runs the Python writers into root, from the checkout this package is in.
func writeFormats(t *testing.T, root string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("testdata", "write_formats.py"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := execx.Cmd{Name: "python3", Args: []string{"-I", script, checkoutOf(t), root}}
	if _, err := (execx.OS{}).Run(context.Background(), cmd); err != nil {
		t.Fatalf("the Python writers: %v", err)
	}
}

// checkoutOf walks up from the package directory to the checkout that holds helpers/pingbus.
func checkoutOf(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, statErr := os.Stat(filepath.Join(dir, "helpers", "pingbus", "statusline.py"))
		if statErr == nil {
			return dir
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatal(statErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no checkout with helpers/pingbus above the package")
		}
		dir = parent
	}
}
