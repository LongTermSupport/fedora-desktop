package inbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/fakeinbox"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/inbox"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// token is a value no other text in these tests holds, so finding it anywhere is a leak.
var token = strings.Repeat("tk9", 4)

type world struct {
	clk  *clock.Fake
	fake *fakeinbox.Inbox
	d    inbox.Dialer
	t    inbox.Target
}

func newWorld(t *testing.T, fakeToken string) world {
	t.Helper()
	clk := clock.NewFake(time.Hour, 1_760_000_000_000)
	path := filepath.Join(t.TempDir(), "4242.sock")
	fake, err := fakeinbox.Start(path, fakeinbox.Options{Token: fakeToken, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fake.Close(); err != nil {
			t.Error(err)
		}
	})
	return world{clk: clk, fake: fake, d: inbox.NewDialer(clk), t: inbox.Target{Socket: path, Token: token}}
}

// noToken fails the test when an error's text carries the token (B11).
func noToken(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), token) {
		t.Fatalf("the error carries the token: %v", err)
	}
}

func dial(t *testing.T, w world) inbox.Conn {
	t.Helper()
	c, err := w.d.Dial(t.Context(), w.t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}

func TestOneDeliveryRecordsTheAuthFrameThenTheContent(t *testing.T) {
	w := newWorld(t, token)
	c := dial(t, w)
	if err := c.WriteUser("teamchat delivery é ☃"); err != nil {
		t.Fatal(err)
	}
	if err := c.Finish(); err != nil {
		t.Fatal(err)
	}
	got := w.fake.Frames()
	want := []fakeinbox.Frame{
		{Auth: true, Accepted: true},
		{Content: "teamchat delivery é ☃", Accepted: true},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("frames %+v, want %+v", got, want)
	}
	if n := w.clk.Waiters(); n != 0 {
		t.Fatalf("%d phase timers left armed", n)
	}
}

func TestTheTargetNeverPrintsItsToken(t *testing.T) {
	target := inbox.Target{Socket: "/run/user/1000/cc-socks/1.sock", Token: token}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%T %v"} {
		out := fmt.Sprintf(verb, target)
		if strings.Contains(out, token) || strings.Contains(out, fmt.Sprintf("%x", token)) {
			t.Fatalf("%s printed the token: %s", verb, out)
		}
	}
	if s := target.String(); !strings.Contains(s, target.Socket) || !strings.Contains(s, "[redacted]") {
		t.Fatalf("String %q: want the socket and the redaction", s)
	}
	raw, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Fatalf("JSON printed the token: %s", raw)
	}
}

func TestAMissingSocketIsGone(t *testing.T) {
	w := newWorld(t, token)
	w.fake.Remove()
	_, err := w.d.Dial(t.Context(), w.t)
	if !errors.Is(err, inbox.ErrGone) || !errors.Is(err, unix.ENOENT) {
		t.Fatalf("got %v, want ErrGone for ENOENT", err)
	}
	noToken(t, err)
}

func TestARefusingSocketIsGone(t *testing.T) {
	w := newWorld(t, token)
	w.fake.Refuse()
	_, err := w.d.Dial(t.Context(), w.t)
	if !errors.Is(err, inbox.ErrGone) || !errors.Is(err, unix.ECONNREFUSED) {
		t.Fatalf("got %v, want ErrGone for ECONNREFUSED", err)
	}
	noToken(t, err)
}

func TestARegularFileAtThePathIsGone(t *testing.T) {
	clk := clock.NewFake(0, 0)
	path := filepath.Join(t.TempDir(), "1.sock")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := inbox.NewDialer(clk).Dial(t.Context(), inbox.Target{Socket: path, Token: token})
	if !errors.Is(err, inbox.ErrGone) || !errors.Is(err, unix.ENOTSOCK) {
		t.Fatalf("got %v, want ErrGone for ENOTSOCK", err)
	}
	noToken(t, err)
}

func TestASymlinkToALiveSocketIsNotFollowed(t *testing.T) {
	w := newWorld(t, token)
	link := filepath.Join(t.TempDir(), "2.sock")
	if err := os.Symlink(w.t.Socket, link); err != nil {
		t.Fatal(err)
	}
	_, err := w.d.Dial(t.Context(), inbox.Target{Socket: link, Token: token})
	if !errors.Is(err, inbox.ErrGone) || !errors.Is(err, unix.ENOTSOCK) {
		t.Fatalf("got %v, want ErrGone for a symlink", err)
	}
	if n := len(w.fake.Frames()); n != 0 {
		t.Fatalf("the fake behind the symlink saw %d frames", n)
	}
}

func TestAnInvalidTargetIsNeitherGoneNorRetry(t *testing.T) {
	d := inbox.NewDialer(clock.NewFake(0, 0))
	for _, target := range []inbox.Target{
		{Socket: "cc-socks/1.sock", Token: token},
		{Socket: "/run/user/1000/cc-socks/1.sock"},
	} {
		_, err := d.Dial(t.Context(), target)
		if err == nil || errors.Is(err, inbox.ErrGone) || errors.Is(err, inbox.ErrRetry) {
			t.Fatalf("%v: got %v, want a plain refusal", target, err)
		}
		noToken(t, err)
	}
}

func TestAnEndedContextIsRetry(t *testing.T) {
	w := newWorld(t, token)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := w.d.Dial(ctx, w.t)
	if !errors.Is(err, inbox.ErrRetry) {
		t.Fatalf("got %v, want ErrRetry", err)
	}
}

func TestAWrongTokenIsRetryAndNothingIsWritten(t *testing.T) {
	w := newWorld(t, "another-token")
	c := dial(t, w)
	if err := w.fake.Await(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	err := c.WriteUser("hello")
	if !errors.Is(err, inbox.ErrRetry) {
		t.Fatalf("got %v, want ErrRetry", err)
	}
	noToken(t, err)
	got := w.fake.Frames()
	if len(got) != 1 || !got[0].Auth || got[0].Accepted || got[0].Reason != "auth" {
		t.Fatalf("frames %+v, want one refused auth frame", got)
	}
}

func TestAFinishThatTimesOutLeavesTheWriteCounted(t *testing.T) {
	w := newWorld(t, token)
	release := w.fake.Hold()
	c := dial(t, w)
	if err := c.WriteUser("counted"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Finish() }()
	if err := w.clk.AwaitDue(t.Context(), inbox.PhaseTimeout); err != nil {
		t.Fatal(err)
	}
	w.clk.Advance(inbox.PhaseTimeout - time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("finished before its phase ran out: %v", err)
	default:
	}
	w.clk.Advance(time.Millisecond)
	if err := <-done; err == nil || errors.Is(err, inbox.ErrPartial) || errors.Is(err, inbox.ErrRetry) {
		t.Fatalf("got %v, want a timed-out finish that is neither partial nor retry", err)
	}
	release()
	if err := w.fake.Await(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	got := w.fake.Frames()
	if !got[1].Accepted || got[1].Content != "counted" {
		t.Fatalf("frames %+v: the full write did not arrive", got)
	}
}

func TestTheUserLineIsWrittenOnce(t *testing.T) {
	w := newWorld(t, token)
	c := dial(t, w)
	if err := c.WriteUser("one"); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteUser("two"); err == nil || errors.Is(err, inbox.ErrRetry) || errors.Is(err, inbox.ErrPartial) {
		t.Fatalf("got %v, want a refusal of a second user line", err)
	}
}

func TestAWriteAfterCloseIsRetry(t *testing.T) {
	w := newWorld(t, token)
	c, err := w.d.Dial(t.Context(), w.t)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("a second close: %v", err)
	}
	if err := c.WriteUser("late"); !errors.Is(err, inbox.ErrRetry) {
		t.Fatalf("got %v, want ErrRetry", err)
	}
}

func TestUserLineIsCompactASCIIJSONOfTheEnvelope(t *testing.T) {
	if got, want := string(inbox.UserLine("a")), `{"type":"user","message":{"role":"user","content":"a"}}`+"\n"; got != want {
		t.Fatalf("UserLine %q, want %q", got, want)
	}
	for _, envelope := range []string{
		"plain",
		"café ☃ \U0001F600 \U0000202E rtl",
		"line one\nline two\t\"quoted\" \\ back",
		"invalid \xff byte",
		strings.Repeat("é", 3000),
	} {
		line := inbox.UserLine(envelope)
		if line[len(line)-1] != '\n' {
			t.Fatalf("%q: no newline", envelope)
		}
		for i, b := range line {
			if b >= 0x80 {
				t.Fatalf("%q: byte %d is 0x%x, not ASCII", envelope, i, b)
			}
		}
		var decoded struct {
			Type    string `json:"type"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &decoded); err != nil {
			t.Fatalf("%q: %v", envelope, err)
		}
		if decoded.Type != "user" || decoded.Message.Role != "user" {
			t.Fatalf("%q: decoded %+v", envelope, decoded)
		}
		if want := strings.ToValidUTF8(envelope, "�"); decoded.Message.Content != want {
			t.Fatalf("%q: content %q", envelope, decoded.Message.Content)
		}
		frame := len(`{"type":"user","message":{"role":"user","content":}}`) + 1
		if got := len(line) - frame; got != proto.WireBytes(envelope) {
			t.Fatalf("%q: content takes %d bytes, proto.WireBytes says %d", envelope, got, proto.WireBytes(envelope))
		}
	}
}

func TestDialReachesAListenerItDidNotStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "3.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := l.Close(); cerr != nil {
			t.Error(cerr)
		}
	}()
	got := make(chan string, 1)
	go func() {
		conn, aerr := l.Accept()
		if aerr != nil {
			got <- aerr.Error()
			return
		}
		buf := make([]byte, 512)
		n, rerr := conn.Read(buf)
		if rerr != nil {
			got <- rerr.Error()
		} else {
			got <- string(buf[:n])
		}
		if cerr := conn.Close(); cerr != nil {
			t.Error(cerr)
		}
	}()
	c, err := inbox.NewDialer(clock.NewFake(0, 0)).Dial(t.Context(), inbox.Target{Socket: path, Token: "té"})
	if err != nil {
		t.Fatal(err)
	}
	if line := <-got; line != "{\"type\":\"auth\",\"token\":\"t\x5cu00e9\"}\n" {
		t.Fatalf("auth line %q", line)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}
