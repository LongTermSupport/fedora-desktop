package proto

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

func one64(n int64) pyjson.Number { return intNum(n) }

func minimalTeam() pyjson.Object {
	return pyjson.Object{
		"v": one64(1), "team": "team", "humans": []any{"@alice:server.test"},
		"roles": pyjson.Object{"@app.1+desk.podman:server.test": "orchestrator"},
		"repos": []any{}, "forge_api": "https://forge.server.test", "org": "example-org",
	}
}

func untrustedReason(err error) string {
	if u, ok := errors.AsType[*UntrustedError](err); ok {
		return u.Reason
	}
	return ""
}

func TestParseTeamRecordNormalisesRolesAndReadsOptionalKeys(t *testing.T) {
	rec, err := ParseTeamRecord(minimalTeam(), "server.test", "team")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Roles["@app.1+desk.podman:server.test"] != RoleCoordinator || rec.Org != "example-org" {
		t.Fatalf("%+v", rec)
	}
	ctx := rec.Context("server.test")
	if !ctx.IsHuman("@alice:server.test") || ctx.Org != "example-org" {
		t.Fatalf("%+v", ctx)
	}
}

func TestParseTeamRecordFailures(t *testing.T) {
	cases := map[string]func(pyjson.Object){
		"team record keys":    func(o pyjson.Object) { o["extra"] = true },
		"team record version": func(o pyjson.Object) { o["v"] = true },
		"team name":           func(o pyjson.Object) { o["team"] = "other" },
		"humans":              func(o pyjson.Object) { o["humans"] = []any{"@app.1+desk.podman:server.test"} },
		"forge_api":           func(o pyjson.Object) { o["forge_api"] = "http://x" },
		"org":                 func(o pyjson.Object) { o["org"] = pyjson.Null{} },
		"neither an org nor a repository is trusted": func(o pyjson.Object) { delete(o, "org") },
		"exclude": func(o pyjson.Object) { o["exclude"] = []any{"Bad"} },
	}
	for want, change := range cases {
		content := minimalTeam()
		change(content)
		if _, err := ParseTeamRecord(content, "server.test", "team"); untrustedReason(err) != want {
			t.Errorf("%s: got %v", want, err)
		}
	}
}

func TestParseTeamEventChecksTheEnvelope(t *testing.T) {
	event := pyjson.Object{"type": TypeTeam, "state_key": "", "sender": "@admin:server.test", "content": minimalTeam()}
	if _, err := ParseTeamEvent(event, "@admin:server.test", "server.test", "team"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseTeamEvent(event, "@other:server.test", "server.test", "team"); untrustedReason(err) != "team record event" {
		t.Fatal(err)
	}
}

func TestReadStatus(t *testing.T) {
	ctx := &Context{ServerName: "server.test", Roles: map[string]string{"@a:server.test": RoleWorker}}
	event := pyjson.Object{
		"type": TypeStatus, "state_key": "@a:server.test", "sender": "@a:server.test",
		"content": pyjson.Object{"v": one64(1), "state": "listening", "until": one64(7)},
	}
	if until, ok := ReadStatus(event, ctx); !ok || until != 7 {
		t.Fatal(until, ok)
	}
	event["content"] = pyjson.Object{"v": one64(1), "state": "listening", "until": pyjson.Number{Lit: "9223372036854775808", Kind: pyjson.Int}}
	if _, ok := ReadStatus(event, ctx); ok {
		t.Fatal("an until past int64 was read")
	}
}

func TestCheckPowerLevelsIsExact(t *testing.T) {
	humans := []string{"@bob:server.test", "@alice:server.test"}
	if err := CheckPowerLevels(ExpectedPowerLevels(humans), humans); err != nil {
		t.Fatal(err)
	}
	levels := ExpectedPowerLevels(humans)
	levels["users_default"] = pyjson.Number{Lit: "0.0", Kind: pyjson.Float}
	if untrustedReason(CheckPowerLevels(levels, humans)) != "power levels" {
		t.Fatal("a float level was trusted")
	}
}
