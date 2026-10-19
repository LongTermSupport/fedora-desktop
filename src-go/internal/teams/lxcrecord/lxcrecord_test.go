package lxcrecord

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The record helpers/agent_bus_lxc/members.py writes (SPEC-lxc-stopped-members.md L8), as it
// writes it.
const record = `{
 "members": [
  {"checkout": "/srv/a", "container": "container-a", "handle": "proj.1+box.lxc", "since": 1792000000,
   "state": "present", "status": "joined", "user": "agent", "why": ""},
  {"checkout": "/srv/b", "container": "container-b", "handle": "proj.2+box.lxc", "since": 1792000000,
   "state": "present", "status": "stopped", "user": "agent", "why": "LXC container container-b is STOPPED: kept"},
  {"checkout": "/srv/c", "container": "container-c", "handle": null, "since": 1792000000,
   "state": "present", "status": "deferred", "user": "agent", "why": "LXC container container-c is STOPPED: start it"},
  {"checkout": "/srv/d", "container": "container-d", "handle": "proj.4+box.lxc", "since": 1792000000,
   "state": "absent", "status": "deferred", "user": "agent", "why": "LXC container container-d is STOPPED: parked"}
 ],
 "team": "alpha",
 "version": 1
}
`

// write makes team alpha's record under dir.
func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, Dir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, Dir, "alpha.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadGivesEveryItem(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, record)
	rec, found, err := Read(dir, "alpha")
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if len(rec.Members) != 4 || rec.Members[1].Handle != "proj.2+box.lxc" || rec.Members[2].Handle != "" {
		t.Fatalf("members %+v", rec.Members)
	}
	if got := rec.Members[2].Label(); got != "agent@container-c" {
		t.Errorf("label %q", got)
	}
}

func TestNoRecordIsNotAnError(t *testing.T) {
	_, found, err := Read(t.TempDir(), "alpha")
	if err != nil || found {
		t.Fatalf("found %v, err %v", found, err)
	}
}

func TestAnUnreadableRecordIsAnErrorNamingIt(t *testing.T) {
	for name, body := range map[string]string{"not json": "{", "another version": `{"version": 2, "members": []}`} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, body)
			if _, _, err := Read(dir, "alpha"); err == nil || !strings.Contains(err.Error(), filepath.Join(dir, Dir, "alpha.json")) {
				t.Fatalf("err %v", err)
			}
		})
	}
}

func TestHeldBackIsTheSkippedAndDeferredInOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, record)
	rec, _, err := Read(dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	held := rec.HeldBack()
	got := make([]string, 0, len(held))
	for _, item := range held {
		got = append(got, item.Label()+" "+item.Words())
	}
	want := []string{
		"agent@container-b skipped (already a member): LXC container container-b is STOPPED: kept",
		"agent@container-c deferred: LXC container container-c is STOPPED: start it",
		"agent@container-d deferred (leaving): LXC container container-d is STOPPED: parked",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestFindIsTheItemOfAContainerAndUser(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, record)
	rec, _, err := Read(dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if item, ok := rec.Find("container-c", "agent"); !ok || item.Status != StatusDeferred {
		t.Fatalf("item %+v ok %v", item, ok)
	}
	if _, ok := rec.Find("container-c", "other"); ok {
		t.Fatal("found another user's item")
	}
}
