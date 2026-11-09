package placeissued

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	testHandle = "repo.1+site.podman"
	testRoom   = "!" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	testDigest = "0123456789abcdef"
	testBase   = "http://127.0.0.1:8448"
	testIssued = 1_700_000_000_000
)

// secretOne and secretTwo are credentials random for each run (gate B: only random test keys);
// every refusal test looks for them on the output.
var secretOne, secretTwo = rand.Text(), rand.Text()

func mustTeam(t *testing.T) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	return team
}

// bundleAt is the test handle's pool seat.json with its member URL at base.
func bundleAt(t *testing.T, base string) seat.Bundle {
	t.Helper()
	team := mustTeam(t)
	server := paths.ServerName(team)
	digest, err := paths.ParseDigest(testDigest)
	if err != nil {
		t.Fatal(err)
	}
	return seat.Bundle{
		V: seat.BundleVersion, Team: team, ServerName: server, UserID: "@" + testHandle + ":" + server,
		AdminID: "@admin:" + server, MainRoom: testRoom, BaseURL: base, DeviceID: paths.SeatDevice,
		IssuedMS: testIssued, Issuer: digest,
	}
}

// issuedDoc is the bare issued document of the test handle: a seat.json and one credential.
func issuedDoc(t *testing.T, base string, kind seat.Kind, secret string) []byte {
	t.Helper()
	sec, err := seat.ParseSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := seat.Issued{Seat: bundleAt(t, base), Kind: kind, Secret: sec}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// passwordDoc is the test handle's issued document with a password, on the loopback test URL.
func passwordDoc(t *testing.T, secret string) []byte {
	t.Helper()
	return issuedDoc(t, testBase, seat.KindPassword, secret)
}

// answerDoc is the answer of a member verb around an issued document: what teamchat-admin
// prints, its bundle the issued document.
func answerDoc(t *testing.T, issued []byte, more map[string]any) []byte {
	t.Helper()
	answer := map[string]any{
		"v": 1, "changed": []string{"member " + testHandle + " issued"}, "handle": testHandle,
		"bundle": json.RawMessage(issued),
	}
	for k, v := range more {
		if v == nil {
			delete(answer, k)
			continue
		}
		answer[k] = v
	}
	doc, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// edit is doc after f has changed its top-level members and the seat's.
func edit(t *testing.T, doc []byte, f func(top, seatObj map[string]any)) []byte {
	t.Helper()
	var top map[string]any
	if err := json.Unmarshal(doc, &top); err != nil {
		t.Fatal(err)
	}
	seatObj, ok := top["seat"].(map[string]any)
	if !ok {
		t.Fatal("the document has no seat object")
	}
	f(top, seatObj)
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// failureOf is err's *FailureError, which every refusal of this package is.
func failureOf(t *testing.T, err error) *FailureError {
	t.Helper()
	f, ok := errors.AsType[*FailureError](err)
	if !ok {
		t.Fatalf("%v is not a *FailureError", err)
	}
	return f
}

// reported is what Report writes for err: the exit, stderr, and the error object that ends it.
func reported(t *testing.T, err error) (term.ExitCode, string, exits.Error) {
	t.Helper()
	var stderr bytes.Buffer
	code := Report(&stderr, err)
	lines := bytes.Split(bytes.TrimSuffix(stderr.Bytes(), []byte("\n")), []byte("\n"))
	obj, decodeErr := exits.Decode(lines[len(lines)-1])
	if decodeErr != nil {
		t.Fatalf("the last line of stderr is not an error object: %v", decodeErr)
	}
	return code, stderr.String(), obj
}
