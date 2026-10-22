package restore

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func team(t *testing.T, id string) paths.TeamID {
	t.Helper()
	tid, err := paths.ParseTeamID(id)
	if err != nil {
		t.Fatal(err)
	}
	return tid
}

// sampleRecord is a record with every field set.
func sampleRecord(t *testing.T) Record {
	t.Helper()
	r := roster(t, 21, 6, 4, map[string]apply.Member{h1: token(apply.MemberActive, 12)}, map[string]apply.Human{"ann": human(apply.HumanActive, 0)})
	return Record{
		Team: opsTeam(t), Backup: 7, Offline: false, FromBackup: true, Pre: 9, Step: StepRevert, Roster: r,
		Lists: Lists{
			Retire: []string{h2}, Revoke: []string{h1}, RevokeHumans: []string{"ann"}, Lock: []string{"bob"},
			Reissue: []string{h1}, Return: []string{h3}, Password: []string{"ann"}, Unknown: []string{"why"},
			Pending: "member.park of " + h1, Notes: []string{NoteElementPasswords},
		},
		Lost:   &Lost{Count: 3, FirstMS: 10, EndMS: 20, Truncated: true},
		Served: true, Stuck: "db r",
	}
}

// The record (n2g-D10) round-trips, every field, and the empty one too.
func TestRecordRoundTrips(t *testing.T) {
	for name, r := range map[string]Record{
		"full":    sampleRecord(t),
		"minimal": {Team: opsTeam(t), Backup: 1, Step: StepStaging, Roster: apply.NewRosterFile(opsTeam(t))},
	} {
		raw := EncodeRecord(r)
		got, err := DecodeRecord(raw, opsTeam(t))
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, raw)
		}
		if again := EncodeRecord(got); string(again) != string(raw) {
			t.Errorf("%s: round trip\n%s\n%s", name, raw, again)
		}
	}
}

func TestRecordEncodesTheSpecsShape(t *testing.T) {
	raw := string(EncodeRecord(Record{Team: opsTeam(t), Backup: 3, Step: StepStaged, Roster: apply.NewRosterFile(opsTeam(t))}))
	for _, want := range []string{`{"v":1,"team":"ops","backup":3,"offline":false,"from_backup":false,"pre":null,"step":"staged","roster":{`, `"lost":null,"served":false}`} {
		if !strings.Contains(raw, want) {
			t.Errorf("%s lacks %s", raw, want)
		}
	}
}

// The decoder is strict: every key, every value, and stuck only beside revert.
func TestRecordDecodeRefuses(t *testing.T) {
	good := string(EncodeRecord(sampleRecord(t)))
	for name, raw := range map[string]string{
		"not json":        "{",
		"another team":    strings.Replace(good, `"team":"ops"`, `"team":"dev"`, 1),
		"a version":       strings.Replace(good, `"v":1`, `"v":2`, 1),
		"an unknown key":  strings.Replace(good, `"served":true`, `"served":true,"x":1`, 1),
		"a step":          strings.Replace(good, `"step":"revert"`, `"step":"later"`, 1),
		"stuck elsewhere": strings.Replace(good, `"step":"revert"`, `"step":"staged"`, 1),
		"a backup of 0":   strings.Replace(good, `"backup":7`, `"backup":0`, 1),
		"a pre of 0":      strings.Replace(good, `"pre":9`, `"pre":0`, 1),
		"a bad handle":    strings.Replace(good, `"retire":["`+h2+`"]`, `"retire":["x y"]`, 1),
		"a bad human":     strings.Replace(good, `"lock":["bob"]`, `"lock":["Bob!"]`, 1),
		"a bad line":      strings.Replace(good, `"unknown":["why"]`, `"unknown":["a\nb"]`, 1),
		"a lost count":    strings.Replace(good, `"count":3`, `"count":-1`, 1),
		"a roster":        strings.Replace(good, `"seq":21`, `"seq":"x"`, 1),
		"a bool":          strings.Replace(good, `"offline":false`, `"offline":0`, 1),
		"a stuck":         strings.Replace(good, `"stuck":"db r"`, `"stuck":1`, 1),
		"a pending":       strings.Replace(good, `"pending":"member.park of `+h1+`"`, `"pending":3`, 1),
		"lists missing":   strings.Replace(good, `"lists":{`, `"listz":{`, 1),
		"a list":          strings.Replace(good, `"notes":[`, `"notes":3,"x":[`, 1),
		"too large":       good + strings.Repeat(" ", maxRecord),
	} {
		if _, err := DecodeRecord([]byte(raw), opsTeam(t)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}
