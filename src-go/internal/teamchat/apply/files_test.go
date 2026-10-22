package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func alphaID() paths.TeamID {
	id, err := paths.ParseTeamID("alpha")
	if err != nil {
		panic("fixture: " + err.Error())
	}
	return id
}

// fullRoster is a roster file using every key.
const fullRoster = `{"v":1,"team":"alpha","seq":41,"rev":12,"restore_epoch":3,` +
	`"counters":{"myrepo+desk.podman":3},` +
	`"members":{"myrepo.1+desk.podman":{"state":"active","kind":"podman","human_text":true,"since_ms":5,"cred":"password","cred_seq":39,"cred_ms":6,"needs":null},` +
	`"myrepo.2+desk.podman":{"state":"claimed","kind":"podman","human_text":false,"since_ms":7,"cred":null,"cred_seq":0,"cred_ms":0,"needs":"reissue"}},` +
	`"humans":{"alice":{"state":"active","since_ms":1,"revoked_seq":17,"needs":null},"bob":{"state":"removed","since_ms":2,"revoked_seq":3,"needs":"password"}},` +
	`"coordinator":"myrepo.1+desk.podman","coordinator_wanted":null,` +
	`"pending":{"op":"member.add","step":"account","since_ms":9,"handle":"myrepo.2+desk.podman"}}`

func TestRosterFileRoundTrip(t *testing.T) {
	f, err := DecodeRosterFile([]byte(fullRoster), alphaID())
	if err != nil {
		t.Fatalf("DecodeRosterFile: %v", err)
	}
	if f.Seq != 41 || f.Rev != 12 || f.RestoreEpoch != 3 || f.Counters["myrepo+desk.podman"] != 3 ||
		f.Members["myrepo.1+desk.podman"].CredSeq != 39 || f.Members["myrepo.2+desk.podman"].Needs != "reissue" ||
		f.Humans["bob"].State != HumanRemoved || f.Coordinator != "myrepo.1+desk.podman" ||
		f.Pending == nil || f.Pending.Handle != "myrepo.2+desk.podman" {
		t.Fatalf("decoded %+v", f)
	}
	if got := string(f.Encode()); got != fullRoster {
		t.Fatalf("Encode:\n%s\nwant:\n%s", got, fullRoster)
	}
}

func TestRosterFileRefuses(t *testing.T) {
	alpha := alphaID()
	cases := map[string]string{
		"another team":      strings.Replace(fullRoster, `"team":"alpha"`, `"team":"beta"`, 1),
		"version":           strings.Replace(fullRoster, `"v":1`, `"v":2`, 1),
		"unknown key":       strings.Replace(fullRoster, `"seq":41`, `"seq":41,"x":1`, 1),
		"repeated key":      strings.Replace(fullRoster, `"seq":41`, `"seq":41,"seq":42`, 1),
		"negative seq":      strings.Replace(fullRoster, `"seq":41`, `"seq":-1`, 1),
		"member state":      strings.Replace(fullRoster, `"state":"active","kind"`, `"state":"gone","kind"`, 1),
		"member handle":     strings.Replace(fullRoster, `"myrepo.1+desk.podman":{`, `"myrepo+desk.podman":{`, 1),
		"member kind":       strings.Replace(fullRoster, `"kind":"podman"`, `"kind":""`, 1),
		"member cred":       strings.Replace(fullRoster, `"cred":"password"`, `"cred":"key"`, 1),
		"member needs":      strings.Replace(fullRoster, `"needs":"reissue"`, `"needs":"password"`, 1),
		"member key":        strings.Replace(fullRoster, `"cred_ms":6,`, ``, 1),
		"human name":        strings.Replace(fullRoster, `"alice":{`, `"admin":{`, 1),
		"human state":       strings.Replace(fullRoster, `"state":"removed"`, `"state":"parked"`, 1),
		"human needs":       strings.Replace(fullRoster, `"needs":"password"`, `"needs":"reissue"`, 1),
		"counter key":       strings.Replace(fullRoster, `"myrepo+desk.podman":3`, `"x":3`, 1),
		"counter value":     strings.Replace(fullRoster, `"myrepo+desk.podman":3`, `"myrepo+desk.podman":-3`, 1),
		"coordinator":       strings.Replace(fullRoster, `"coordinator":"myrepo.1+desk.podman"`, `"coordinator":"alice"`, 1),
		"pending handle":    strings.Replace(fullRoster, `,"handle":"myrepo.2+desk.podman"`, ``, 1),
		"pending team op":   strings.Replace(fullRoster, `"op":"member.add"`, `"op":"team.apply"`, 1),
		"pending empty op":  strings.Replace(fullRoster, `"op":"member.add"`, `"op":""`, 1),
		"string seq":        strings.Replace(fullRoster, `"seq":41`, `"seq":"41"`, 1),
		"not json":          "{",
		"not an object":     "[]",
		"members not a map": strings.Replace(fullRoster, `"counters":{"myrepo+desk.podman":3}`, `"counters":[]`, 1),
		"over the cap":      `{"v":1,"pad":"` + strings.Repeat("x", maxFile) + `"}`,
	}
	for name, raw := range cases {
		if raw == fullRoster {
			t.Fatalf("%s: the fixture edit did not apply", name)
		}
		if _, err := DecodeRosterFile([]byte(raw), alpha); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRosterFileRefusalQuotesAKey(t *testing.T) {
	raw := strings.Replace(fullRoster, `"seq":41`, `"seq":41,"x\nCHANGED\tforged":1`, 1)
	_, err := DecodeRosterFile([]byte(raw), alphaID())
	if err == nil || strings.ContainsAny(err.Error(), "\n\t") || !strings.Contains(err.Error(), `"x\nCHANGED\tforged"`) {
		t.Fatalf("a control character in a key: %q", err)
	}
}

func TestEmptyRosterFile(t *testing.T) {
	f := NewRosterFile(alphaID())
	want := `{"v":1,"team":"alpha","seq":0,"rev":0,"restore_epoch":0,"counters":{},"members":{},"humans":{},` +
		`"coordinator":null,"coordinator_wanted":null,"pending":null}`
	if got := string(f.Encode()); got != want {
		t.Fatalf("empty roster %s", got)
	}
	back, err := DecodeRosterFile([]byte(want), alphaID())
	if err != nil || back.Seq != 0 || len(back.Members) != 0 {
		t.Fatalf("decoded %+v, %v", back, err)
	}
}

func TestRoomsRecordRoundTrip(t *testing.T) {
	alpha := alphaID()
	ids := proto.RoomIDs{Main: "!" + strings.Repeat("a", 43), Reports: "!" + strings.Repeat("b", 43)}
	raw := EncodeRooms(alpha, ids)
	want := `{"v":1,"team":"alpha","main":"!` + strings.Repeat("a", 43) + `","reports":"!` + strings.Repeat("b", 43) + `"}`
	if string(raw) != want {
		t.Fatalf("EncodeRooms %s", raw)
	}
	back, err := DecodeRooms(raw, alpha)
	if err != nil || back != ids {
		t.Fatalf("DecodeRooms %+v, %v", back, err)
	}
	for name, bad := range map[string]string{
		"another team": `{"v":1,"team":"beta"}`,
		"version":      `{"v":2,"team":"alpha"}`,
		"bad id":       `{"v":1,"team":"alpha","main":"!x"}`,
		"unknown key":  `{"v":1,"team":"alpha","lobby":"!x"}`,
		"not json":     `{`,
		"same room":    `{"v":1,"team":"alpha","main":"!` + strings.Repeat("a", 43) + `","reports":"!` + strings.Repeat("a", 43) + `"}`,
	} {
		if _, err := DecodeRooms([]byte(bad), alpha); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStoreFiles(t *testing.T) {
	dir := t.TempDir()
	alpha := alphaID()
	s := &store{team: alpha, rosterPath: filepath.Join(dir, "roster.json"), roomsPath: filepath.Join(dir, "rooms.json")}
	if err := s.load(); err != nil {
		t.Fatalf("load with no files: %v", err)
	}
	if s.roster.Seq != 0 || s.rooms != (proto.RoomIDs{}) {
		t.Fatalf("no files: %+v %+v", s.roster, s.rooms)
	}
	s.roster.Humans["alice"] = Human{State: HumanActive}
	if err := s.saveRoster(); err != nil || s.roster.Seq != 1 {
		t.Fatalf("save: seq %d, %v", s.roster.Seq, err)
	}
	if err := s.saveRoster(); err != nil || s.roster.Seq != 2 {
		t.Fatalf("second save: seq %d, %v", s.roster.Seq, err)
	}
	s.rooms.Main = "!" + strings.Repeat("a", 43)
	if err := s.saveRooms(); err != nil {
		t.Fatal(err)
	}
	again := &store{team: alpha, rosterPath: s.rosterPath, roomsPath: s.roomsPath}
	if err := again.load(); err != nil || again.roster.Seq != 2 || again.rooms.Main != s.rooms.Main ||
		again.roster.Humans["alice"].State != HumanActive {
		t.Fatalf("reloaded %+v %+v, %v", again.roster, again.rooms, err)
	}
}

func TestStoreRefusesBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	again := &store{team: alphaID(), rosterPath: filepath.Join(dir, "roster.json"), roomsPath: filepath.Join(dir, "rooms.json")}
	s := again
	if err := os.WriteFile(s.roomsPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := again.load(); err == nil {
		t.Fatal("a broken rooms record was loaded")
	}
	if err := os.WriteFile(s.rosterPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := again.load(); err == nil {
		t.Fatal("a broken roster file was loaded")
	}
}
