package seatmodel

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func newStore(t *testing.T) paths.Store {
	t.Helper()
	store, err := paths.SeatStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func team(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seatName(t *testing.T, s string) paths.SeatName {
	t.Helper()
	n, err := paths.ParseSeatName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCheckTakesOneTokenOnly(t *testing.T) {
	for _, ok := range []string{"opus", "sonnet", "claude-opus-4-1-20250805", "opus[1m]"} {
		if err := Check(ok); err != nil {
			t.Fatalf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "opus 4", " opus", "opus\n", "-opus", "op\tus", "op\x00us"} {
		if err := Check(bad); !errors.Is(err, ErrModel) {
			t.Fatalf("%q: got %v", bad, err)
		}
	}
}

func TestReadWithNoFileIsNoModels(t *testing.T) {
	s, err := Read(newStore(t))
	if err != nil || len(s.Teams) != 0 {
		t.Fatalf("got %+v %v", s, err)
	}
}

func TestWriteThenReadKeepsEveryModelPrivately(t *testing.T) {
	store := newStore(t)
	var s Settings
	s.SetDefault(team(t, "alpha"), "opus")
	s.SetSeat(team(t, "alpha"), seatName(t, "repo.2+site.podman"), "sonnet")
	s.SetDefault(team(t, "beta"), "haiku")
	if err := Write(store, s); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(File(store))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info, err)
	}
	got, err := Read(store)
	if err != nil {
		t.Fatal(err)
	}
	if got.Teams["alpha"].Default != "opus" || got.Teams["alpha"].Seats["repo.2+site.podman"] != "sonnet" || got.Teams["beta"].Default != "haiku" {
		t.Fatalf("got %+v", got)
	}
}

func TestClearingTheLastModelOfATeamDropsTheTeam(t *testing.T) {
	var s Settings
	alpha, n := team(t, "alpha"), seatName(t, "repo.2+site.podman")
	s.SetDefault(alpha, "opus")
	s.SetSeat(alpha, n, "sonnet")
	s.SetSeat(alpha, n, "")
	if _, ok := s.Teams["alpha"].Seats[n.String()]; ok {
		t.Fatalf("seat kept: %+v", s)
	}
	s.SetDefault(alpha, "")
	if _, ok := s.Teams["alpha"]; ok {
		t.Fatalf("team kept: %+v", s)
	}
}

func TestReadRefusesAFileItCannotTrust(t *testing.T) {
	for name, body := range map[string]string{
		"not json":      "{",
		"unknown key":   `{"teams":{},"x":1}`,
		"bad team":      `{"teams":{"Not A Team!":{"default":"opus"}}}`,
		"bad seat":      `{"teams":{"alpha":{"seats":{"Not A Seat!":"opus"}}}}`,
		"bad model":     `{"teams":{"alpha":{"default":"two words"}}}`,
		"bad seat mdl":  `{"teams":{"alpha":{"seats":{"repo.1+site.podman":""}}}}`,
		"trailing data": `{"teams":{}} {}`,
	} {
		store := newStore(t)
		if err := os.MkdirAll(store.Root(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(File(store), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Read(store)
		if !errors.Is(err, ErrFile) || !strings.Contains(err.Error(), File(store)) {
			t.Fatalf("%s: got %v", name, err)
		}
	}
}

func TestReadSaysAnUnreadableFile(t *testing.T) {
	store := newStore(t)
	if err := os.MkdirAll(File(store), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(store); err == nil || errors.Is(err, ErrFile) {
		t.Fatalf("got %v", err)
	}
}

func TestWriteSaysWhereItCouldNot(t *testing.T) {
	store := newStore(t)
	if err := os.MkdirAll(filepath.Dir(store.Root()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Root(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(store, Settings{}); err == nil {
		t.Fatal("no error")
	}
}

func TestForTakesTheSeatsModelThenTheTeamsDefault(t *testing.T) {
	var s Settings
	alpha, beta := team(t, "alpha"), team(t, "beta")
	one, two := seatName(t, "repo.1+site.podman"), seatName(t, "repo.2+site.podman")
	s.SetDefault(alpha, "opus")
	s.SetSeat(alpha, two, "sonnet")

	if p := s.For([]Seat{{alpha, one}}); p.Model != "opus" || p.FromSeat || p.Seat.Name != one {
		t.Fatalf("default: %+v", p)
	}
	if p := s.For([]Seat{{alpha, two}}); p.Model != "sonnet" || !p.FromSeat {
		t.Fatalf("seat: %+v", p)
	}
	if p := s.For([]Seat{{beta, one}}); p.Model != "" {
		t.Fatalf("none: %+v", p)
	}
	// The first seat with a model decides; a later seat asking for another is named.
	p := s.For([]Seat{{beta, one}, {alpha, one}, {alpha, two}})
	if p.Model != "opus" || p.Seat.Team != alpha || len(p.Others) != 1 || !strings.Contains(p.Others[0], "sonnet") {
		t.Fatalf("first: %+v", p)
	}
	if p := s.For([]Seat{{alpha, one}, {alpha, one}}); len(p.Others) != 0 {
		t.Fatalf("same model named: %+v", p)
	}
}

func TestNoteSaysTheModelWhereItCameFromAndTheOverride(t *testing.T) {
	alpha, two := team(t, "alpha"), seatName(t, "repo.2+site.podman")
	seatPick := Pick{Model: "sonnet", Seat: Seat{alpha, two}, FromSeat: true, Others: []string{"opus (repo.1+site.podman of team beta)"}}
	note := seatPick.Note()
	for _, want := range []string{"sonnet", "repo.2+site.podman", "alpha", "--model", "opus (repo.1+site.podman of team beta)", "ccy-teams seats model"} {
		if !strings.Contains(note, want) {
			t.Fatalf("note %q lacks %q", note, want)
		}
	}
	if !strings.Contains((Pick{Model: "opus", Seat: Seat{alpha, two}}).Note(), "team alpha's model") {
		t.Fatal("team default not said")
	}
}

// cmdWorld is a Cmd over a temporary store and a fake seat resolver.
type cmdWorld struct {
	cmd            *Cmd
	store          paths.Store
	stdout, stderr *bytes.Buffer
	asked          []string
}

func newCmdWorld(t *testing.T) *cmdWorld {
	t.Helper()
	w := &cmdWorld{store: newStore(t), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	w.cmd = &Cmd{
		Stdout: w.stdout, Stderr: w.stderr,
		Store: func() (paths.Store, error) { return w.store, nil },
		Resolve: func(tm paths.TeamID, seat, kind string) (paths.SeatName, error) {
			w.asked = append(w.asked, seat+"@"+tm.String()+" "+kind)
			if seat == "9" {
				return paths.SeatName{}, errors.New("no pool seat of this checkout has seat 9")
			}
			return paths.ParseSeatName("repo." + seat + "+site." + kind)
		},
	}
	return w
}

func TestCmdSetsATeamsDefaultAndASeatsModel(t *testing.T) {
	w := newCmdWorld(t)
	if code := w.cmd.Run([]string{"alpha", "opus"}); code != term.ExitOK {
		t.Fatalf("team: %d %s", code, w.stderr)
	}
	if code := w.cmd.Run([]string{"2@alpha", "sonnet"}); code != term.ExitOK {
		t.Fatalf("seat: %d %s", code, w.stderr)
	}
	if code := w.cmd.Run([]string{"--host", "3@alpha", "haiku"}); code != term.ExitOK {
		t.Fatalf("host seat: %d %s", code, w.stderr)
	}
	if strings.Join(w.asked, ";") != "2@alpha podman;3@alpha host" {
		t.Fatalf("asked %q", w.asked)
	}
	s, err := Read(w.store)
	if err != nil {
		t.Fatal(err)
	}
	a := s.Teams["alpha"]
	if a.Default != "opus" || a.Seats["repo.2+site.podman"] != "sonnet" || a.Seats["repo.3+site.host"] != "haiku" {
		t.Fatalf("got %+v", s)
	}
	if !strings.Contains(w.stderr.String(), "next launch") || w.stdout.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", w.stdout, w.stderr)
	}
}

func TestCmdListsAndClears(t *testing.T) {
	w := newCmdWorld(t)
	for _, args := range [][]string{{"beta", "haiku"}, {"alpha", "opus"}, {"2@alpha", "sonnet"}} {
		if code := w.cmd.Run(args); code != term.ExitOK {
			t.Fatalf("%q: %d %s", args, code, w.stderr)
		}
	}
	if code := w.cmd.Run(nil); code != term.ExitOK {
		t.Fatalf("list: %d", code)
	}
	want := "alpha team opus\nalpha repo.2+site.podman sonnet\nbeta team haiku\n"
	if w.stdout.String() != want {
		t.Fatalf("list %q, want %q", w.stdout, want)
	}
	for _, args := range [][]string{{"2@alpha", "--clear"}, {"--clear", "beta"}} {
		if code := w.cmd.Run(args); code != term.ExitOK {
			t.Fatalf("%q: %d %s", args, code, w.stderr)
		}
	}
	w.stdout.Reset()
	if code := w.cmd.Run(nil); code != term.ExitOK || w.stdout.String() != "alpha team opus\n" {
		t.Fatalf("after clear %d %q", code, w.stdout)
	}
}

func TestCmdListOfNothingSaysSo(t *testing.T) {
	w := newCmdWorld(t)
	if code := w.cmd.Run(nil); code != term.ExitOK || w.stdout.Len() != 0 || !strings.Contains(w.stderr.String(), "account's default") {
		t.Fatalf("%d %q %q", code, w.stdout, w.stderr)
	}
}

func TestCmdRefusesWhatItCannotUse(t *testing.T) {
	for _, args := range [][]string{
		{"alpha"},
		{"alpha", "opus", "x"},
		{"alpha", "two words"},
		{"alpha", "--clear", "opus"},
		{"--host", "alpha", "opus"},
		{"--bogus", "alpha", "opus"},
		{"Not!", "opus"},
		{"x@Not!", "opus"},
		{"@alpha", "opus"},
		{"--clear"},
	} {
		w := newCmdWorld(t)
		if code := w.cmd.Run(args); code != term.ExitUsage || !strings.Contains(w.stderr.String(), "Usage: ") {
			t.Fatalf("%q: %d %q", args, code, w.stderr)
		}
		if _, err := os.Stat(File(w.store)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%q wrote the file: %v", args, err)
		}
	}
}

func TestCmdFailsWithTheCause(t *testing.T) {
	w := newCmdWorld(t)
	if code := w.cmd.Run([]string{"9@alpha", "opus"}); code != term.ExitFailure || !strings.Contains(w.stderr.String(), "seat 9") {
		t.Fatalf("resolve: %d %q", code, w.stderr)
	}
	w = newCmdWorld(t)
	w.cmd.Store = func() (paths.Store, error) { return paths.Store{}, errors.New("no state home") }
	if code := w.cmd.Run(nil); code != term.ExitFailure || !strings.Contains(w.stderr.String(), "no state home") {
		t.Fatalf("store: %d %q", code, w.stderr)
	}
	w = newCmdWorld(t)
	if err := os.MkdirAll(w.store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(File(w.store), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.cmd.Run([]string{"alpha", "opus"}); code != term.ExitFailure || !strings.Contains(w.stderr.String(), File(w.store)) {
		t.Fatalf("file: %d %q", code, w.stderr)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestCmdFailsWhenItCannotSpeak(t *testing.T) {
	w := newCmdWorld(t)
	if code := w.cmd.Run([]string{"alpha", "opus"}); code != term.ExitOK {
		t.Fatal(code)
	}
	w.cmd.Stdout = failWriter{}
	if code := w.cmd.Run(nil); code != term.ExitFailure {
		t.Fatalf("stdout: %d", code)
	}
	w.cmd.Stderr = failWriter{}
	if code := w.cmd.Run([]string{"alpha", "sonnet"}); code != term.ExitFailure {
		t.Fatalf("stderr: %d", code)
	}
}
