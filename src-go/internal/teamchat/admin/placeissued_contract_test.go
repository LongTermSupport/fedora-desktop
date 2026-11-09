package admin

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/placeissued"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// answerWithRealBundles is what `teamchat-admin member add` prints for args, over the world with
// the real seat bundle maker in place of the world's simplified one: the bytes an owner pipes to
// `teamchat seat place-issued` (Plan 00246 X21).
func answerWithRealBundles(t *testing.T, w *world, args ...string) string {
	t.Helper()
	sys := w.sys()
	sys.Bundles = seatBundles(t)
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), Env{
		Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Proc: rootProc(), Digest: digest, Sys: sys,
	})
	if code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	return stdout.String()
}

// appliedOverHTTPS approves and applies alpha with an https URL, the form a team reached from
// another machine declares: its seat bundles carry the team CA.
func (w *world) appliedOverHTTPS(t *testing.T) {
	t.Helper()
	doc := alphaDoc()
	team, ok := doc["team"].(map[string]any)
	if !ok {
		t.Fatal("alphaDoc has no team object")
	}
	team["url"] = httpsURL
	in := encodeDoc(t, doc)
	w.ok(t, in, "team", "approve", "--team", "alpha")
	w.ok(t, in, "team", "apply")
}

// The contract of X21: the answer of a member verb is what place-issued's Read accepts, for each
// credential kind, and Read finds the issued seat in it that seat.DecodeIssued reads.
func TestTheAnswerOfAMemberVerbIsWhatPlaceIssuedReads(t *testing.T) {
	for _, c := range []struct {
		seat, cred string
		kind       seat.Kind
		handle     string
	}{
		{"3", "password", seat.KindPassword, sampleHandle},
		{"1", "token", seat.KindToken, coord},
	} {
		w := newWorld(t)
		w.appliedOverHTTPS(t)
		answer := answerWithRealBundles(t, w, addArgs(c.seat, c.cred)...)
		is, err := placeissued.Read(strings.NewReader(answer))
		if err != nil {
			t.Fatalf("%s: Read refused the answer: %v\n%s", c.cred, err, redacted(answer))
		}
		if is.Kind != c.kind || is.Seat.Handle().String() != c.handle || is.Seat.Team.String() != "alpha" {
			t.Errorf("%s: read %s of %s on %s, want %s of %s", c.cred, is.Kind, is.Seat.Handle(), is.Seat.Team, c.kind, c.handle)
		}
		if is.Secret.Reveal() == "" {
			t.Errorf("%s: the issued seat has no credential", c.cred)
		}
	}
}

// redacted is the answer with its credential members cut out, so a failure shows the shape and
// never the secret.
func redacted(answer string) string {
	for _, key := range []string{`"password":"`, `"token":"`} {
		if i := strings.Index(answer, key); i >= 0 {
			rest := answer[i+len(key):]
			if j := strings.IndexByte(rest, '"'); j >= 0 {
				answer = answer[:i+len(key)] + "..." + rest[j:]
			}
		}
	}
	return answer
}
