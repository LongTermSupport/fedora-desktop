package admin

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	v1 "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/migrate/v1"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// fakeHistory is the v1 history export seam: it records what it was asked and answers as set.
type fakeHistory struct {
	exports  []v1.Request
	verifies [][2]string
	result   v1.Result
	path     string
	err      error
}

func (f *fakeHistory) Export(_ context.Context, req v1.Request) (v1.Result, error) {
	if _, err := req.Progress.Write([]byte("progress\n")); err != nil {
		return v1.Result{}, err
	}
	req.Progress = nil
	f.exports = append(f.exports, req)
	return f.result, f.err
}

func (f *fakeHistory) Verify(team, stamp string) (string, error) {
	f.verifies = append(f.verifies, [2]string{team, stamp})
	return f.path, f.err
}

func runHistory(t *testing.T, h *fakeHistory, seamErr error, args ...string) outcome {
	t.Helper()
	sys := &System{HistoryExport: func() (HistoryExport, error) { return h, seamErr }}
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), Env{Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Proc: rootProc(), Digest: digest, Sys: sys})
	return outcome{code, stdout.String(), stderr.String()}
}

// E19: the record's path, then one line per room, on stdout; progress on stderr.
func TestMigrateExportPrintsTheRecord(t *testing.T) {
	h := &fakeHistory{result: v1.Result{Record: "/r/20261124T101112Z", Rooms: []v1.RoomCount{{Purpose: "main", RoomID: "!m", Events: 3}, {Purpose: "reports", RoomID: "!r", Events: 0}}}}
	got := runHistory(t, h, nil, "migrate", "export", "--team", "alpha")
	if got.code != term.ExitOK || got.stdout != "/r/20261124T101112Z\nmain !m 3\nreports !r 0\n" || got.stderr != "progress\n" {
		t.Fatalf("%+v", got)
	}
	if len(h.exports) != 1 || h.exports[0] != (v1.Request{Team: "alpha"}) {
		t.Errorf("%+v", h.exports)
	}
}

// E17: the copy's flags reach the export, and the copy's path is said on stderr by the export.
func TestMigrateExportCopy(t *testing.T) {
	h := &fakeHistory{result: v1.Result{Record: "/r/x", Copy: "/home/x/alpha-x"}}
	got := runHistory(t, h, nil, "migrate", "export", "--copy-to", "/c", "--team", "alpha", "--copy-owner", "someone")
	if got.code != term.ExitOK || got.stdout != "/r/x\n" {
		t.Fatalf("%+v", got)
	}
	if h.exports[0] != (v1.Request{Team: "alpha", CopyTo: "/c", CopyOwner: "someone"}) {
		t.Errorf("%+v", h.exports)
	}
}

// E1, E20: flag errors are 64, before any seam is reached.
func TestMigrateExportUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"no team":       {"migrate", "export"},
		"bad team":      {"migrate", "export", "--team", "Alpha"},
		"twice":         {"migrate", "export", "--team", "a", "--team", "a"},
		"unknown":       {"migrate", "export", "--team", "a", "--drill"},
		"stray":         {"migrate", "export", "--team", "a", "extra"},
		"copy alone":    {"migrate", "export", "--team", "a", "--copy-to", "/c"},
		"owner alone":   {"migrate", "export", "--team", "a", "--copy-owner", "u"},
		"no value":      {"migrate", "export", "--team"},
		"verify none":   {"migrate", "export-verify"},
		"verify stamp":  {"migrate", "export-verify", "--team", "a", "--export", "yesterday"},
		"verify copy":   {"migrate", "export-verify", "--team", "a", "--copy-to", "/c"},
		"verify stray":  {"migrate", "export-verify", "--team", "a", "x"},
		"verify twice":  {"migrate", "export-verify", "--team", "a", "--export", "20261124T101112Z", "--export", "20261124T101112Z"},
		"verify noteam": {"migrate", "export-verify", "--export", "20261124T101112Z"},
	} {
		h := &fakeHistory{}
		got := runHistory(t, h, nil, args...)
		if got.code != term.ExitUsage || got.stdout != "" || !strings.HasPrefix(got.stderr, "teamchat-admin: ") {
			t.Errorf("%s: %+v", name, got)
		}
		if len(h.exports)+len(h.verifies) != 0 {
			t.Errorf("%s: the seam was reached", name)
		}
	}
}

// E20: a refusal is 1 with the reason; a seam that cannot be built is 1 too.
func TestMigrateExportRefused(t *testing.T) {
	h := &fakeHistory{err: errors.New("no v1 team alpha on this host (never made, or purged)")}
	got := runHistory(t, h, nil, "migrate", "export", "--team", "alpha")
	if got.code != term.ExitFailure || got.stdout != "" || !strings.HasSuffix(got.stderr, "teamchat-admin: no v1 team alpha on this host (never made, or purged)\n") {
		t.Fatalf("%+v", got)
	}
	got = runHistory(t, h, errors.New("broken"), "migrate", "export", "--team", "alpha")
	if got.code != term.ExitFailure || !strings.Contains(got.stderr, "broken") {
		t.Fatalf("%+v", got)
	}
}

// E18: verify prints the record's path on success; each mismatch on its own line, exit 1.
func TestMigrateExportVerify(t *testing.T) {
	h := &fakeHistory{path: "/r/20261124T101112Z"}
	got := runHistory(t, h, nil, "migrate", "export-verify", "--team", "alpha")
	if got.code != term.ExitOK || got.stdout != "/r/20261124T101112Z\n" || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
	got = runHistory(t, h, nil, "migrate", "export-verify", "--team", "alpha", "--export", "20261124T101112Z")
	if got.code != term.ExitOK || h.verifies[1] != [2]string{"alpha", "20261124T101112Z"} || h.verifies[0] != [2]string{"alpha", ""} {
		t.Fatalf("%+v %v", got, h.verifies)
	}
	h.err = &v1.MismatchError{Record: "/r/x", Problems: []string{"a is changed", "b is missing"}}
	got = runHistory(t, h, nil, "migrate", "export-verify", "--team", "alpha")
	want := "teamchat-admin: the record /r/x does not match its digest:\n  a is changed\n  b is missing\n"
	if got.code != term.ExitFailure || got.stdout != "" || got.stderr != want {
		t.Fatalf("%+v", got)
	}
	h.err = errors.New("no export of v1 team alpha")
	got = runHistory(t, h, nil, "migrate", "export-verify", "--team", "alpha")
	if got.code != term.ExitFailure || got.stderr != "teamchat-admin: no export of v1 team alpha\n" {
		t.Fatalf("%+v", got)
	}
}

// A System with no history export seam says so (1).
func TestMigrateExportUnwired(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), Env{Args: []string{"migrate", "export", "--team", "alpha"}, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Proc: rootProc(), Digest: digest, Sys: &System{}})
	if code != term.ExitFailure || !strings.Contains(stderr.String(), "not wired") {
		t.Fatalf("%d %q", code, stderr.String())
	}
}

// The carry gate judges v2 teams; a v1 team's export is not one of its verbs.
func TestMigrateExportNotCarryGated(t *testing.T) {
	if !namesV1Team["migrate export"] || !namesV1Team["migrate export-verify"] {
		t.Fatal("the migrate export verbs are carry-gated")
	}
}
