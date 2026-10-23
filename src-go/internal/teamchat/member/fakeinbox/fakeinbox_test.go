package fakeinbox_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/fakeinbox"
)

const (
	token = "fake-token"
	auth  = `{"type":"auth","token":"fake-token"}` + "\n"
)

func user(content string) string {
	return `{"type":"user","message":{"role":"user","content":` + strconv.Quote(content) + "}}\n"
}

type rig struct {
	b   *fakeinbox.Inbox
	clk *clock.Fake
}

func start(t *testing.T, opt fakeinbox.Options) rig {
	t.Helper()
	clk := clock.NewFake(time.Hour, 0)
	opt.Clock = clk
	if opt.Token == "" {
		opt.Token = token
	}
	b, err := fakeinbox.Start(filepath.Join(t.TempDir(), "7.sock"), opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	return rig{b: b, clk: clk}
}

// send is one delivery spoken by hand: the lines, a half-close and a read to EOF.
func send(t *testing.T, b *fakeinbox.Inbox, lines ...string) {
	t.Helper()
	c, err := net.Dial("unix", b.Path())
	if err != nil {
		t.Fatal(err)
	}
	uc, ok := c.(*net.UnixConn)
	if !ok {
		t.Fatalf("%T", c)
	}
	defer func() {
		if err := uc.Close(); err != nil {
			t.Error(err)
		}
	}()
	// One write, so a fake that refuses the auth line and closes cannot fail a second one.
	if _, err := io.WriteString(uc, strings.Join(lines, "")); err != nil {
		t.Fatal(err)
	}
	if err := uc.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, uc); err != nil {
		t.Fatal(err)
	}
}

func last(t *testing.T, b *fakeinbox.Inbox) fakeinbox.Frame {
	t.Helper()
	frames := b.Frames()
	if len(frames) == 0 {
		t.Fatal("no frames")
	}
	return frames[len(frames)-1]
}

func TestADeliveryIsRecordedAndHandedOn(t *testing.T) {
	r := start(t, fakeinbox.Options{})
	var handed []fakeinbox.Frame
	r.b.OnAccepted(func(f fakeinbox.Frame) { handed = append(handed, f) })
	send(t, r.b, auth, user("hello"))
	want := []fakeinbox.Frame{{Auth: true, Accepted: true}, {Content: "hello", Accepted: true}}
	if got := r.b.Frames(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("frames %+v, want %+v", got, want)
	}
	if fmt.Sprint(handed) != fmt.Sprint(want[1:]) {
		t.Fatalf("handed on %+v", handed)
	}
}

func TestAWrongTokenIsRefusedAndTheConnectionClosed(t *testing.T) {
	r := start(t, fakeinbox.Options{})
	send(t, r.b, `{"type":"auth","token":"other"}`+"\n", user("never"))
	frames := r.b.Frames()
	if len(frames) != 1 || !frames[0].Auth || frames[0].Accepted || frames[0].Reason != "auth" || frames[0].Content != "" {
		t.Fatalf("frames %+v, want one refused auth frame", frames)
	}
}

// An accepted frame is handed on before it is recorded, so a test that awaits the frame finds
// it handed on: the hand-on sees only the frames before it.
func TestAFrameIsHandedOnBeforeItIsRecorded(t *testing.T) {
	r := start(t, fakeinbox.Options{})
	handed := make(chan []fakeinbox.Frame, 1)
	r.b.OnAccepted(func(fakeinbox.Frame) { handed <- r.b.Frames() })
	send(t, r.b, auth, user("hello"))
	before := <-handed
	if want := []fakeinbox.Frame{{Auth: true, Accepted: true}}; fmt.Sprint(before) != fmt.Sprint(want) {
		t.Fatalf("frames when handed on %+v, want %+v", before, want)
	}
}

func TestFramingRefusals(t *testing.T) {
	cases := map[string][]string{
		"auth not JSON":        {"nope\n"},
		"auth of another type": {`{"type":"user","token":"fake-token"}` + "\n"},
		"auth unknown field":   {`{"type":"auth","token":"fake-token","x":1}` + "\n"},
		"auth trailing data":   {`{"type":"auth","token":"fake-token"} {}` + "\n"},
		"user not ASCII":       {auth, `{"type":"user","message":{"role":"user","content":"caf` + "é" + `"}}` + "\n"},
		"user wrong role":      {auth, `{"type":"user","message":{"role":"assistant","content":"x"}}` + "\n"},
		"user wrong type":      {auth, `{"type":"auth","message":{"role":"user","content":"x"}}` + "\n"},
		"user line cut":        {auth, `{"type":"user","message":{"role":"us`},
		"a line after":         {auth, user("x"), user("y")},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			r := start(t, fakeinbox.Options{})
			accepted := 0
			r.b.OnAccepted(func(fakeinbox.Frame) { accepted++ })
			send(t, r.b, lines...)
			if f := last(t, r.b); f.Accepted || f.Reason != "framing" {
				t.Fatalf("frames %+v, want a framing refusal last", r.b.Frames())
			}
			want := 0
			if name == "a line after" {
				want = 1
			}
			if accepted != want {
				t.Fatalf("handed on %d, want %d", accepted, want)
			}
		})
	}
}

func TestAnAuthAloneRecordsOnlyTheAuth(t *testing.T) {
	r := start(t, fakeinbox.Options{})
	send(t, r.b, auth)
	if got := r.b.Frames(); len(got) != 1 || !got[0].Accepted {
		t.Fatalf("frames %+v", got)
	}
}

func TestTheDuplicateFilterHoldsThirtySeconds(t *testing.T) {
	r := start(t, fakeinbox.Options{})
	send(t, r.b, auth, user("same"))
	r.clk.Advance(29 * time.Second)
	send(t, r.b, auth, user("same"))
	if f := last(t, r.b); f.Accepted || f.Reason != "duplicate" || f.Content != "same" {
		t.Fatalf("inside 30 s: %+v", f)
	}
	send(t, r.b, auth, user("other"))
	if f := last(t, r.b); !f.Accepted {
		t.Fatalf("other content: %+v", f)
	}
	r.clk.Advance(time.Second)
	send(t, r.b, auth, user("same"))
	if f := last(t, r.b); !f.Accepted {
		t.Fatalf("after 30 s: %+v", f)
	}
}

func TestTheBucketDropsTheThirtyFirstOfABurst(t *testing.T) {
	r := start(t, fakeinbox.Options{})
	for i := range 30 {
		send(t, r.b, auth, user("burst "+strconv.Itoa(i)))
		if f := last(t, r.b); !f.Accepted {
			t.Fatalf("frame %d: %+v", i, f)
		}
	}
	send(t, r.b, auth, user("burst 30"))
	if f := last(t, r.b); f.Accepted || f.Reason != "rate" {
		t.Fatalf("31st: %+v", f)
	}
	r.clk.Advance(1999 * time.Millisecond)
	send(t, r.b, auth, user("burst 31"))
	if f := last(t, r.b); f.Reason != "rate" {
		t.Fatalf("before 2 s: %+v", f)
	}
	r.clk.Advance(time.Millisecond)
	send(t, r.b, auth, user("burst 32"))
	if f := last(t, r.b); !f.Accepted {
		t.Fatalf("2 s later: %+v", f)
	}
	send(t, r.b, auth, user("burst 33"))
	if f := last(t, r.b); f.Reason != "rate" {
		t.Fatalf("the bucket refilled more than one: %+v", f)
	}
}

func TestOptionsSetTheFilters(t *testing.T) {
	r := start(t, fakeinbox.Options{DupWindow: time.Second, Bucket: 1, PerSecond: 1})
	send(t, r.b, auth, user("a"))
	send(t, r.b, auth, user("b"))
	if f := last(t, r.b); f.Reason != "rate" {
		t.Fatalf("bucket of one: %+v", f)
	}
	r.clk.Advance(time.Second)
	send(t, r.b, auth, user("a"))
	if f := last(t, r.b); !f.Accepted {
		t.Fatalf("a one-second window and a one-per-second refill: %+v", f)
	}
}

func TestStartRefusesBadOptions(t *testing.T) {
	clk := clock.NewFake(0, 0)
	dir := t.TempDir()
	for name, opt := range map[string]fakeinbox.Options{
		"no token":      {Clock: clk},
		"no clock":      {Token: token},
		"negative rate": {Token: token, Clock: clk, PerSecond: -1},
		"negative dup":  {Token: token, Clock: clk, DupWindow: -time.Second},
		"negative cap":  {Token: token, Clock: clk, Bucket: -1},
	} {
		if b, err := fakeinbox.Start(filepath.Join(dir, "x.sock"), opt); err == nil {
			t.Fatalf("%s: started %v", name, b.Path())
		}
	}
	if b, err := fakeinbox.Start(filepath.Join(dir, "missing", "x.sock"), fakeinbox.Options{Token: token, Clock: clk}); err == nil {
		t.Fatalf("started in a missing directory: %v", b.Path())
	}
}

func TestHoldReadsNothingUntilReleased(t *testing.T) {
	r := start(t, fakeinbox.Options{})
	release := r.b.Hold()
	c, err := net.Dial("unix", r.b.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(c, auth+user("held")); err != nil {
		t.Fatal(err)
	}
	if n := len(r.b.Frames()); n != 0 {
		t.Fatalf("%d frames while held", n)
	}
	release()
	release()
	if err := r.b.Await(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if f := last(t, r.b); !f.Accepted || f.Content != "held" {
		t.Fatalf("after release: %+v", f)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAwaitEndsWithItsContext(t *testing.T) {
	r := start(t, fakeinbox.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.b.Await(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestRefuseKeepsThePathAndRefuses(t *testing.T) {
	r := start(t, fakeinbox.Options{})
	r.b.Refuse()
	r.b.Refuse()
	if _, err := os.Lstat(r.b.Path()); err != nil {
		t.Fatalf("the path went: %v", err)
	}
	if c, err := net.Dial("unix", r.b.Path()); !errors.Is(err, unix.ECONNREFUSED) {
		t.Fatalf("got %v %v, want ECONNREFUSED", c, err)
	}
}

func TestRemoveUnlinksThePath(t *testing.T) {
	r := start(t, fakeinbox.Options{})
	r.b.Remove()
	r.b.Remove()
	if c, err := net.Dial("unix", r.b.Path()); !errors.Is(err, unix.ENOENT) {
		t.Fatalf("got %v %v, want ENOENT", c, err)
	}
}

func TestCloseEndsAHeldConnectionAndRemovesThePath(t *testing.T) {
	clk := clock.NewFake(0, 0)
	b, err := fakeinbox.Start(filepath.Join(t.TempDir(), "8.sock"), fakeinbox.Options{Token: token, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	b.Hold()
	c, err := net.Dial("unix", b.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("a second close: %v", err)
	}
	if _, err := os.Lstat(b.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the path is left: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}
