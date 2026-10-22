package status

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

var (
	team = mustTeam("alpha")
	t0   = time.UnixMilli(1_800_000_000_000).UTC()
)

func mustTeam(s string) paths.TeamID {
	id, err := paths.ParseTeamID(s)
	if err != nil {
		panic(err)
	}
	return id
}

// example is C 3.11.3's document with real values.
const example = `{"v":1,"team":"alpha","name":"Alpha","origin":"v2","set":null,"migration":null,"seq":812,` +
	`"written_ms":1800000000000,"expires_ms":1800000300000,` +
	`"homeserver":{"unit":"active","since_ms":1799999000000,"nrestarts":0,"probe":"ok"},` +
	`"members":[{"handle":"web.1+desk.podman","kind":"podman","role":"coordinator","state":"active","rooms":{"main":"join","reports":"join","questions":"join"}},` +
	`{"handle":"web.2+box.lxc","kind":"lxc","role":"worker","state":"active","online":true,"rooms":{"main":"join","reports":"join"}}],` +
	`"humans":[{"name":"ann","state":"active","rooms":{"main":"join","reports":"invite"},"devices":2,"last_seen_ms":1799999999000}],` +
	`"backup":{"newest":{"id":7,"time_ms":1799990000000},"last_run":"ok"},` +
	`"disk":{"free_bytes":123456789,"free_pct":42},"restore":null,` +
	`"problems":[{"what":"a problem","fix":"ccy-teams apply alpha"}],` +
	`"events":[{"at_ms":1799999990000,"what":"restarted after three missed probes"}]}`

func TestDecodeTheExample(t *testing.T) {
	rec, err := Decode([]byte(example), team)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if rec.Seq != 812 || rec.Name != "Alpha" || len(rec.Members) != 2 || rec.Members[1].Online == nil || !*rec.Members[1].Online {
		t.Fatalf("decoded %+v", rec)
	}
	if rec.Members[0].Online != nil {
		t.Fatal("a podman member carries online")
	}
	out, err := rec.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	again, err := Decode(out, team)
	if err != nil || again.Seq != rec.Seq || string(mustEncode(t, again)) != string(out) {
		t.Fatalf("round trip: %v\n%s", err, out)
	}
}

func mustEncode(t *testing.T, r Record) []byte {
	t.Helper()
	out, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDecodeRefuses(t *testing.T) {
	cases := map[string]string{
		"unknown key":          strings.Replace(example, `"seq":812`, `"seq":812,"extra":1`, 1),
		"repeated key":         strings.Replace(example, `"seq":812`, `"seq":812,"seq":813`, 1),
		"nested capital":       strings.Replace(example, `"handle":"web.1+desk.podman"`, `"Handle":"coord"`, 1),
		"missing key":          strings.Replace(example, `"disk":{"free_bytes":123456789,"free_pct":42},`, ``, 1),
		"another team":         strings.Replace(example, `"team":"alpha"`, `"team":"beta"`, 1),
		"version":              strings.Replace(example, `"v":1,"team"`, `"v":2,"team"`, 1),
		"origin":               strings.Replace(example, `"origin":"v2"`, `"origin":"v1"`, 1),
		"probe":                strings.Replace(example, `"probe":"ok"`, `"probe":"maybe"`, 1),
		"unit":                 strings.Replace(example, `"unit":"active"`, `"unit":"Active"`, 1),
		"membership":           strings.Replace(example, `"reports":"invite"`, `"reports":"joined"`, 1),
		"purpose":              strings.Replace(example, `"reports":"invite"`, `"lobby":"invite"`, 1),
		"last run":             strings.Replace(example, `"last_run":"ok"`, `"last_run":"great"`, 1),
		"online on podman":     strings.Replace(example, `"role":"coordinator","state":"active",`, `"role":"coordinator","state":"active","online":true,`, 1),
		"kind":                 strings.Replace(example, `"kind":"podman"`, `"kind":"Podman"`, 1),
		"role":                 strings.Replace(example, `"role":"worker"`, `"role":"boss"`, 1),
		"member state":         strings.Replace(example, `"handle":"web.1+desk.podman","kind":"podman","role":"coordinator","state":"active"`, `"handle":"web.1+desk.podman","kind":"podman","role":"coordinator","state":"gone"`, 1),
		"problem line":         strings.Replace(example, `"what":"a problem"`, `"what":"a\nproblem"`, 1),
		"empty fix":            strings.Replace(example, `"fix":"ccy-teams apply alpha"`, `"fix":""`, 1),
		"event line":           strings.Replace(example, `"what":"restarted after three missed probes"`, `"what":"x y"`, 1),
		"trailing":             example + ` {}`,
		"not an object":        `[]`,
		"expiry":               strings.Replace(example, `"expires_ms":1800000300000`, `"expires_ms":1800000300001`, 1),
		"negative seq":         strings.Replace(example, `"seq":812`, `"seq":-1`, 1),
		"pct":                  strings.Replace(example, `"free_pct":42`, `"free_pct":101`, 1),
		"set":                  strings.Replace(example, `"set":null`, `"set":"Not A Set"`, 1),
		"migration not object": strings.Replace(example, `"migration":null`, `"migration":[1]`, 1),
		"restore":              strings.Replace(example, `"restore":null`, `"restore":{"step":"x"}`, 1),
		"name":                 strings.Replace(example, `"name":"Alpha"`, `"name":""`, 1),
		"handle":               strings.Replace(example, `"handle":"web.2+box.lxc"`, `"handle":"LX 1"`, 1),
		"devices":              strings.Replace(example, `"devices":2`, `"devices":-2`, 1),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(doc), team); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Decode = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestDecodeKeepsAMigrationAsWritten(t *testing.T) {
	doc := strings.Replace(example, `"set":null,"migration":null`, `"set":"s1","migration":{"phase":"m3","accepted":{"ok":true,"report":"r"}}`, 1)
	rec, err := Decode([]byte(doc), team)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	out := string(mustEncode(t, rec))
	if !strings.Contains(out, `"migration":{"phase":"m3","accepted":{"ok":true,"report":"r"}}`) || !strings.Contains(out, `"set":"s1"`) {
		t.Fatalf("migration not kept: %s", out)
	}
}

func TestDecodeRefusesAnOversizeRecord(t *testing.T) {
	big := strings.Replace(example, `"name":"Alpha"`, `"name":"`+strings.Repeat("a", MaxSize)+`"`, 1)
	if _, err := Decode([]byte(big), team); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Decode = %v, want ErrInvalid", err)
	}
}

func TestDecodeRestore(t *testing.T) {
	doc := strings.Replace(example, `"restore":null`, `"restore":{"step":"staged","backup":7,"pre":8}`, 1)
	rec, err := Decode([]byte(doc), team)
	if err != nil || rec.Restore == nil || rec.Restore.Backup != 7 {
		t.Fatalf("Decode = %+v, %v", rec.Restore, err)
	}
}

func decoded(t *testing.T) Record {
	t.Helper()
	rec, err := Decode([]byte(example), team)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestNextRaisesSeqAndPrunesEvents(t *testing.T) {
	prev := decoded(t)
	prev.Events = nil
	for i := range 40 {
		prev.Events = append(prev.Events, Event{AtMS: t0.Add(-time.Duration(i) * time.Minute).UnixMilli(), What: "e"})
	}
	prev.Events = append(prev.Events, Event{AtMS: t0.Add(-25 * time.Hour).UnixMilli(), What: "old"})
	next := Next(&prev, team, t0)
	if next.Seq != 813 || next.WrittenMS != t0.UnixMilli() || next.ExpiresMS != t0.Add(Lifetime).UnixMilli() {
		t.Fatalf("next %d %d %d", next.Seq, next.WrittenMS, next.ExpiresMS)
	}
	if len(next.Events) != MaxEvents {
		t.Fatalf("%d events kept, want %d", len(next.Events), MaxEvents)
	}
	for _, e := range next.Events {
		if e.What == "old" {
			t.Fatal("an event older than 24 h was kept")
		}
	}
	if next.Events[0].AtMS != t0.UnixMilli() {
		t.Fatal("the newest events are not the ones kept")
	}
	if prev.Seq != 812 {
		t.Fatal("Next changed its input")
	}
}

func TestNextFromNothingIsSeqOne(t *testing.T) {
	next := Next(nil, team, t0)
	if next.Seq != 1 || next.Team != team || next.V != 1 || next.Origin != "" {
		t.Fatalf("next %+v", next)
	}
}

func TestWriteAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	rec := Next(nil, team, t0)
	rec.Name, rec.Origin = "Alpha", OriginV2
	rec.Homeserver = Homeserver{Unit: "active", Probe: ProbeOK}
	rec.Backup.LastRun = RunNone
	if err := Write(path, rec, -1); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != FileMode {
		t.Fatalf("mode %v, %v", info.Mode().Perm(), err)
	}
	got, found, err := Read(path, team)
	if err != nil || !found || got.Seq != 1 {
		t.Fatalf("Read = %+v, %v, %v", got, found, err)
	}
	_, found, err = Read(filepath.Join(t.TempDir(), "none.json"), team)
	if err != nil || found {
		t.Fatalf("a missing record: %v, %v", found, err)
	}
}

func TestWriteRefusesABadRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	rec := Next(nil, team, t0)
	if err := Write(path, rec, -1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Write of a record with no origin = %v, want ErrInvalid", err)
	}
	rec.Name, rec.Origin = "Alpha", OriginV2
	rec.Homeserver = Homeserver{Unit: "active", Probe: ProbeOK}
	rec.Backup.LastRun = RunNone
	rec.ExpiresMS++
	if err := Write(path, rec, -1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Write of a wrong expiry = %v, want ErrInvalid", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused record was written")
	}
}

func TestReadRefusesALink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(filepath.Join(dir, "real"), []byte(example), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "real"), path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(path, team); err == nil {
		t.Fatal("a link was read")
	}
	if err := os.WriteFile(filepath.Join(dir, "bad"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(filepath.Join(dir, "bad"), team); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a bad record = %v, want ErrInvalid", err)
	}
}

func TestShown(t *testing.T) {
	rec := decoded(t)
	written := time.UnixMilli(rec.WrittenMS)

	fresh := Shown(rec, true, nil, written.Add(70*time.Second))
	if !fresh.Current || len(fresh.Problems) != 1 {
		t.Fatalf("fresh %+v", fresh)
	}
	expired := Shown(rec, true, nil, written.Add(Lifetime))
	if expired.Current || len(expired.Problems) != 2 || !strings.HasPrefix(expired.Problems[0].What, "health has not run since ") {
		t.Fatalf("expired %+v", expired)
	}
	missing := Shown(Record{}, false, nil, written)
	if missing.Current || len(missing.Problems) != 1 || missing.Problems[0].What != "no status record yet" {
		t.Fatalf("missing %+v", missing)
	}
	broken := Shown(Record{}, false, errors.New("boom"), written)
	if broken.Current || len(broken.Problems) != 1 || broken.Problems[0].What != "the status record cannot be read" {
		t.Fatalf("broken %+v", broken)
	}
}

func TestProblemAndEventChecks(t *testing.T) {
	if _, err := NewProblem("two\nlines", "fix"); !errors.Is(err, ErrInvalid) {
		t.Fatal("a two-line problem was made")
	}
	p, err := NewProblem("what", "fix")
	if err != nil || p.What != "what" {
		t.Fatalf("NewProblem = %+v, %v", p, err)
	}
}
