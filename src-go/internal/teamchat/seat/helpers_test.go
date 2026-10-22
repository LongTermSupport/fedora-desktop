package seat

import (
	"encoding/json"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	testHandle   = "repo.1+site.podman"
	testRoom     = "!" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	testDigest   = "0123456789abcdef"
	testIssuedMS = 1_700_000_000_000
	testBase     = "http://127.0.0.1:8448"
	loopback     = "127.0.0.1"
)

func mustTeam(t *testing.T) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	return team
}

func mustSession(t *testing.T, s string) paths.SessionName {
	t.Helper()
	n, err := paths.ParseSessionName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func mustDigest(t *testing.T) paths.Digest {
	t.Helper()
	d, err := paths.ParseDigest(testDigest)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustSecret(t *testing.T, s string) Secret {
	t.Helper()
	sec, err := ParseSecret(s)
	if err != nil {
		t.Fatal(err)
	}
	return sec
}

// testBundle is a pool seat.json for the test handle, its member URL on loopback.
func testBundle(t *testing.T) Bundle {
	t.Helper()
	team := mustTeam(t)
	server := paths.ServerName(team)
	return Bundle{
		V: 1, Team: team, ServerName: server, UserID: "@" + testHandle + ":" + server,
		AdminID: "@admin:" + server, MainRoom: testRoom, BaseURL: testBase,
		PlainHTTPHosts: []string{loopback}, DeviceID: paths.SeatDevice, IssuedMS: testIssuedMS,
		Issuer: mustDigest(t),
	}
}

func decodeJSON(t *testing.T, data string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		t.Fatalf("%s: %v", data, err)
	}
	return out
}
