package backup

import (
	"context"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

var _ health.Backups = States(Deps{})

func TestStatesNoBackupsAndNoRun(t *testing.T) {
	w := newWorld(t)
	got, err := States(w.deps()).State(context.Background(), w.team)
	if err != nil || got.Newest != nil || got.LastRun != status.RunNone {
		t.Fatalf("%+v %v", got, err)
	}
}

// TestStatesNewestNeedsBothHalves is HB1: the newest is the highest number with its meta and
// its tar, and its time is the meta's.
func TestStatesNewestNeedsBothHalves(t *testing.T) {
	w := newWorld(t)
	w.file(paths.BackupDir(w.team), map[string][]byte{"2.tar": sampleTar(t, 2), "9.tar": []byte("x"), "x.tar": []byte("x")})
	got, err := States(w.deps()).State(context.Background(), w.team)
	if err != nil || got.Newest == nil || *got.Newest != (status.Newest{ID: 2, TimeMS: 1792600000000}) || got.LastRun != status.RunNone {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = w.take(); err != nil {
		t.Fatal(err)
	}
	got, err = States(w.deps()).State(context.Background(), w.team)
	if err != nil || *got.Newest != (status.Newest{ID: 4, TimeMS: newMetaTime * 1000}) || got.LastRun != status.RunOK {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStatesFailedRun(t *testing.T) {
	w := newWorld(t)
	w.units.states = []UnitState{{ActiveState: "failed"}}
	if _, err := w.take(); err == nil {
		t.Fatal("took a backup of a failed unit")
	}
	got, err := States(w.deps()).State(context.Background(), w.team)
	if err != nil || got.Newest != nil || got.LastRun != status.RunFailed {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStatesUnreadableIsAnError(t *testing.T) {
	cases := map[string]func(w *world){
		"a bad record": func(w *world) {
			w.file(paths.BackupDir(w.team), map[string][]byte{"last-run.json": []byte(`{"v":1,"result":"ok"}`)})
		},
		"a backup directory that is a file": func(w *world) {
			w.file(paths.AdminState(w.team), map[string][]byte{"backups": []byte("x")})
		},
		"a newest meta with no time": func(w *world) {
			w.meta(3, "x\n")
			w.file(paths.BackupDir(w.team), map[string][]byte{"3.tar": []byte("x")})
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			change(w)
			if _, err := States(w.deps()).State(context.Background(), w.team); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestStatesRefusesAStateTreeNotOwned(t *testing.T) {
	w := newWorld(t)
	d := w.deps()
	d.StateOwnerUID++
	if _, err := States(d).State(context.Background(), w.team); err == nil {
		t.Fatal("no error")
	}
}

func TestDecodeRunRefuses(t *testing.T) {
	for name, raw := range map[string]string{
		"ok with an error":     `{"v":1,"result":"ok","at_ms":1,"backup":2,"error":"x"}`,
		"ok with no backup":    `{"v":1,"result":"ok","at_ms":1,"backup":null,"error":null}`,
		"failed with a backup": `{"v":1,"result":"failed","at_ms":1,"backup":2,"error":"x"}`,
		"failed with no error": `{"v":1,"result":"failed","at_ms":1,"backup":null,"error":null}`,
		"two lines":            `{"v":1,"result":"failed","at_ms":1,"backup":null,"error":"a\nb"}`,
		"none":                 `{"v":1,"result":"none","at_ms":1,"backup":null,"error":null}`,
		"version 2":            `{"v":2,"result":"ok","at_ms":1,"backup":2,"error":null}`,
		"a negative time":      `{"v":1,"result":"ok","at_ms":-1,"backup":2,"error":null}`,
		"an extra key":         `{"v":1,"result":"ok","at_ms":1,"backup":2,"error":null,"x":1}`,
		"not json":             `{`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeRun([]byte(raw)); err == nil {
				t.Fatal("decoded")
			}
		})
	}
}

// TestListIsNewestFirst is BV3: every number either half names, newest first, usable only with
// both halves and a tar that validates, and the last run.
func TestListIsNewestFirst(t *testing.T) {
	w := newWorld(t)
	w.file(paths.BackupDir(w.team), map[string][]byte{"2.tar": sampleTar(t, 2), "3.tar": []byte("x"), "7.tar": sampleTar(t, 7)})
	if _, err := w.take(7); err != nil {
		t.Fatal(err)
	}
	l, err := List(w.deps(), w.team)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"team":"alpha","backups":[` +
		`{"id":7,"time_ms":1792663930000,"usable":false},` +
		`{"id":4,"time_ms":1792700000000,"usable":true},` +
		`{"id":3,"time_ms":1792650000000,"usable":false},` +
		`{"id":2,"time_ms":1792600000000,"usable":true}],` +
		`"last_run":{"v":1,"result":"ok","at_ms":1792710000123,"backup":4,"error":null}}`
	if got := string(l.Value(w.team).Compact()); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestListWithNothing(t *testing.T) {
	w := newWorld(t)
	remove(t, metaDir(w.root, w.team)+"/2")
	remove(t, metaDir(w.root, w.team)+"/3")
	l, err := List(w.deps(), w.team)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(l.Value(w.team).Compact()); got != `{"v":1,"team":"alpha","backups":[],"last_run":null}` {
		t.Fatalf("got %s", got)
	}
}

func TestListUnknownTimeIsNull(t *testing.T) {
	w := newWorld(t)
	w.meta(3, "x\n")
	w.file(paths.BackupDir(w.team), map[string][]byte{"9.tar": []byte("x")})
	l, err := List(w.deps(), w.team)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"team":"alpha","backups":[{"id":9,"time_ms":null,"usable":false},{"id":3,"time_ms":null,"usable":false},` +
		`{"id":2,"time_ms":1792600000000,"usable":false}],"last_run":null}`
	if got := string(l.Value(w.team).Compact()); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestListFailsOnAnUnreadableTree(t *testing.T) {
	w := newWorld(t)
	d := w.deps()
	d.StateOwnerUID++
	if _, err := List(d, w.team); err == nil {
		t.Fatal("listed")
	}
	w.file(paths.BackupDir(w.team), map[string][]byte{"last-run.json": []byte(`{`)})
	if _, err := List(w.deps(), w.team); err == nil {
		t.Fatal("listed")
	}
}
