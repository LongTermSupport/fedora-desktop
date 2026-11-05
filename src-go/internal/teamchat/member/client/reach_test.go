package client

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// otherTeam is a team whose seat directory holds alpha's seat.json.
const otherTeam = "beta"

// seats is the world's state root, 0700, below its home.
func (w *world) seats() string {
	w.t.Helper()
	l, err := paths.HomeLayout(w.env["HOME"], w.env["XDG_RUNTIME_DIR"])
	if err != nil {
		w.t.Fatal(err)
	}
	if err := os.MkdirAll(l.State(), 0o700); err != nil {
		w.t.Fatal(err)
	}
	if err := os.Chmod(l.State(), 0o700); err != nil {
		w.t.Fatal(err)
	}
	return l.State()
}

// placeSeat writes alpha's seat.json, its homeserver at base, below the world's state root.
func (w *world) placeSeat(base string) {
	w.t.Helper()
	writeSeat(w.t, filepath.Join(w.seats(), hsTeam), bundleFor(w.t, base, deadRoom), "")
}

func (w *world) reach(team string) term.ExitCode {
	w.t.Helper()
	inv, err := cli.Parse([]string{"reach", "--team", team})
	if err != nil {
		w.t.Fatalf("parse: %v", err)
	}
	return reach(w.cliEnv(), inv, hsDeps{clock: w.clk, wait: noWait{}})
}

func TestReach(t *testing.T) {
	h := newHS(t)
	w := newWorld(t)
	w.placeSeat(h.srv.URL())
	if code := w.reach(hsTeam); code != term.ExitOK || w.stderr.Len() != 0 || w.stdout.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, w.stdout.String(), w.stderr.String())
	}
	for _, r := range h.srv.Requests() {
		if r.Path != "/_matrix/client/versions" {
			t.Errorf("reach asked %s %s", r.Method, r.Path)
		}
	}
}

func TestReachWithoutARuntimeIsTheHomeLayout(t *testing.T) {
	h := newHS(t)
	w := newWorld(t)
	w.placeSeat(h.srv.URL())
	delete(w.env, paths.EnvRuntime)
	if code := w.reach(hsTeam); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
}

func TestReachUnreachable(t *testing.T) {
	w := newWorld(t)
	w.placeSeat(deadURL(t))
	if code := w.reach(hsTeam); code != 20 {
		t.Fatalf("exit %d, want 20; stderr %q", code, w.stderr.String())
	}
	if e := w.errorObject(); e.Code != exits.CodeUnreachable || e.Team == nil || e.Team.String() != hsTeam {
		t.Errorf("error %+v", e)
	}
}

func TestReachTheBound(t *testing.T) {
	h := newHS(t)
	w := newWorld(t)
	w.placeSeat(h.srv.URL())
	inv, err := cli.Parse([]string{"reach", "--team", hsTeam})
	if err != nil {
		t.Fatal(err)
	}
	stalled := hsDeps{clock: w.clk, wait: noWait{}, armed: func() { w.clk.Advance(reachBound) }}
	if code := reach(w.cliEnv(), inv, stalled); code != 20 {
		t.Fatalf("exit %d, want 20 once the bound passes; stderr %q", code, w.stderr.String())
	}
}

func TestReachRefusals(t *testing.T) {
	cases := []struct {
		name, team, code string
		prepare          func(w *world)
	}{
		{"no seat", hsTeam, exits.CodeBundle, func(w *world) { w.seats() }},
		{"no state root", hsTeam, exits.CodeBundle, func(*world) {}},
		{"a shared state root", hsTeam, exits.CodeBundle, func(w *world) {
			if err := os.Chmod(w.seats(), 0o755); err != nil {
				w.t.Fatal(err)
			}
		}},
		{"another team's seat.json", otherTeam, exits.CodeBundle, func(w *world) {
			w.placeSeat(deadURL(w.t))
			if err := os.Rename(filepath.Join(w.seats(), hsTeam), filepath.Join(w.seats(), otherTeam)); err != nil {
				w.t.Fatal(err)
			}
		}},
		{"not a seat.json", hsTeam, exits.CodeBundle, func(w *world) {
			w.placeSeat(deadURL(w.t))
			writeFile(w.t, filepath.Join(w.seats(), hsTeam, "seat.json"), []byte(`{"v":1}`))
		}},
		{"a runtime that names no session", hsTeam, exits.CodeNoSession, func(w *world) {
			w.env[paths.EnvRuntime] = "/nowhere/" + runID
		}},
		{"no home", hsTeam, exits.CodeRuntime, func(w *world) {
			delete(w.env, paths.EnvRuntime)
			delete(w.env, "HOME")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.prepare(w)
			code := w.reach(tc.team)
			if e := w.errorObject(); e.Code != tc.code || e.Exit != code {
				t.Errorf("exit %d, error %+v; want %s", code, e, tc.code)
			}
		})
	}
}

func TestReachExported(t *testing.T) {
	h := newHS(t)
	w := newWorld(t)
	w.placeSeat(h.srv.URL())
	inv, err := cli.Parse([]string{"reach", "--team", hsTeam})
	if err != nil {
		t.Fatal(err)
	}
	if code := Reach(w.cliEnv(), inv); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
}
