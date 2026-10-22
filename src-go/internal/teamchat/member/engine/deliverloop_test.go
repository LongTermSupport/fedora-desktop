package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/fakeinbox"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/inbox"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
)

// DV1 end to end over the real dialer and fakeinbox: the auth line, then the user line carrying
// the delivery, written yes. The inbox never answers the auth line, so a wrong token is not seen
// by the link: the session refuses it, takes no user line, and only the transcript's proof
// (n3e-9) can find that delivery missing; the token is never in link.log.
func TestDeliveryOverTheInboxSocket(t *testing.T) {
	dw := &dworld{dial: newFakeDialer()}
	dw.world = newWorldWith(t, state.DeliverySocket, func(c *Config) {
		c.Inbox = inbox.NewDialer(c.Clock)
	}, func(*Engine) {})
	dw.transcript = filepath.Join(t.TempDir(), "session.jsonl")
	check(t, os.WriteFile(dw.transcript, nil, 0o600))
	// t.TempDir is short enough for a unix socket path (107 bytes).
	path := filepath.Join(t.TempDir(), "1.sock")
	ib, err := fakeinbox.Start(path, fakeinbox.Options{Token: testToken, Clock: dw.clk})
	check(t, err)
	t.Cleanup(func() { check(t, ib.Close()) })
	a := tid(t, "alpha")
	dw.live(a)
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	check(t, dw.e.SetSession(t.Context(), SessionFacts{Socket: path, Token: testToken, Transcript: dw.transcript}))
	check(t, ib.Await(t.Context(), 2))
	frames := ib.Frames()
	if auth := frames[0]; !auth.Auth || !auth.Accepted || auth.Content != "" {
		t.Fatalf("the auth frame: %+v", auth)
	}
	if user := frames[1]; user.Auth || !user.Accepted || !strings.Contains(user.Content, "1 message on alpha") || !strings.Contains(user.Content, evID(1)) {
		t.Fatalf("the user frame: %+v", user)
	}
	dw.until(func() bool {
		s := dw.e.teams[a].store
		return len(s.Awaiting) == 1 && s.Awaiting[0].Written == state.WrittenYes
	})
	const wrong = "wrong-token-0123"
	check(t, dw.e.SetSession(t.Context(), SessionFacts{Token: wrong}))
	dw.persist(a, humanItem(a, 2, startWall+1, "two"))
	dw.advance(paceWait)
	check(t, ib.Await(t.Context(), 3))
	if refused := ib.Frames()[2]; !refused.Auth || refused.Accepted || refused.Reason != "auth" {
		t.Fatalf("a wrong token's frame: %+v", refused)
	}
	if log := dw.log.String(); strings.Contains(log, testToken) || strings.Contains(log, wrong) {
		t.Fatal("link.log carries the inbox token")
	}
}
