package membership

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sample() Desired {
	return Desired{
		V: 1, Session: "ccy-proj", Kind: "ccy", Checkout: "/p", Generation: 3, WrittenMS: 5,
		Seats: []Seat{{Team: "alpha", Seat: "1"}}, Farewell: []Farewell{{Team: "beta", Seat: "2", Reason: "removed"}},
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ccy", "membership", "ccy-proj")
	if err := Write(dir, sample()); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 3 || got.Seats[0].Seat != "1" || got.Farewell[0].Team != "beta" {
		t.Fatalf("read back %+v", got)
	}
}

func TestReadRefusesWhatTheHostDoesNotWrite(t *testing.T) {
	cases := map[string]string{
		"unknown key":  `{"v":1,"session":"s","kind":"ccy","checkout":"/p","generation":1,"written_ms":1,"seats":[],"farewell":[],"x":1}`,
		"version":      `{"v":2,"session":"s","kind":"ccy","checkout":"/p","generation":1,"written_ms":1,"seats":[],"farewell":[]}`,
		"bad seat":     `{"v":1,"session":"s","kind":"ccy","checkout":"/p","generation":1,"written_ms":1,"seats":[{"team":"alpha","seat":"0"}],"farewell":[]}`,
		"generation":   `{"v":1,"session":"s","kind":"ccy","checkout":"/p","generation":0,"written_ms":1,"seats":[],"farewell":[]}`,
		"two per team": `{"v":1,"session":"s","kind":"ccy","checkout":"/p","generation":1,"written_ms":1,"seats":[{"team":"alpha","seat":"1"},{"team":"alpha","seat":"2"}],"farewell":[]}`,
		"reason":       `{"v":1,"session":"s","kind":"ccy","checkout":"/p","generation":1,"written_ms":1,"seats":[],"farewell":[{"team":"alpha","seat":"1","reason":"why"}]}`,
		"trailing":     `{"v":1,"session":"s","kind":"ccy","checkout":"/p","generation":1,"written_ms":1,"seats":[],"farewell":[]} {}`,
	}
	for name, body := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, File), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(dir); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestReadOfAMissingFileIsNotExist(t *testing.T) {
	if _, err := Read(t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}

func TestRelaunchRequestIsWrittenPrivateAndRemovedByInit(t *testing.T) {
	r := newRig(t)
	dir := r.dir("ccy-proj")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteRelaunch(dir, 4, stamp); err != nil {
		t.Fatal(err)
	}
	body := readFile(t, filepath.Join(dir, RelaunchFile))
	if !strings.Contains(body, `"generation":4`) || !strings.Contains(body, `"v":1`) || mode(t, filepath.Join(dir, RelaunchFile)).Perm() != 0o600 {
		t.Fatalf("relaunch %q", body)
	}
	if code := r.init.Run(t.Context(), []string{"ccy-proj", "/p", "1@alpha"}); code != 0 {
		t.Fatalf("init exit %d: %s", code, r.ErrOut.String())
	}
	if _, err := os.Lstat(filepath.Join(dir, RelaunchFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a launch leaves no relaunch request behind: %v", err)
	}
}

func TestCanonicalJoinsSeatsInOrder(t *testing.T) {
	if got := Canonical([]Seat{{Team: "alpha", Seat: "1"}, {Team: "beta", Seat: "dev"}}); got != "1@alpha,dev@beta" {
		t.Fatalf("got %q", got)
	}
	if Canonical(nil) != "" {
		t.Fatal("no seats is the empty list")
	}
}

func TestValidSession(t *testing.T) {
	if !ValidSession("ccy-proj-2") || ValidSession("..") || ValidSession("a/b") || ValidSession("") {
		t.Fatal("session names")
	}
}
