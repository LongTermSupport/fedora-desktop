package fakeinbox

import (
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// closeSpy is the fake's end of a connection, which reads the fake's frames as it is closed.
type closeSpy struct {
	net.Conn

	b       *Inbox
	atClose []Frame
}

// Close is called by drop, which holds the fake's mu, so the frames are read without taking it.
func (c *closeSpy) Close() error {
	c.atClose = append([]Frame(nil), c.b.frames...)
	return c.Conn.Close()
}

// A delivery's last frame, a refusal or a line after the user line, is recorded only once its
// connection is closed, so a test that awaits the frame finds the close done.
func TestTheLastFrameIsRecordedOnceTheConnectionIsClosed(t *testing.T) {
	const good = `{"type":"auth","token":"fake-token"}` + "\n"
	user := `{"type":"user","message":{"role":"user","content":"x"}}` + "\n"
	for name, tc := range map[string]struct {
		lines   string
		atClose int
	}{
		"wrong token":     {`{"type":"auth","token":"other"}` + "\n", 0},
		"auth not JSON":   {"nope\n", 0},
		"auth cut":        {`{"type":"au`, 0},
		"user wrong role": {good + `{"type":"user","message":{"role":"assistant","content":"x"}}` + "\n", 1},
		"user line cut":   {good + `{"type":"user","mess`, 1},
		"a line after":    {good + user + user, 2},
	} {
		t.Run(name, func(t *testing.T) {
			b, err := Start(filepath.Join(t.TempDir(), "9.sock"), Options{Token: "fake-token", Clock: clock.NewFake(time.Hour, 0)})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := b.Close(); err != nil {
					t.Error(err)
				}
			})
			ours, theirs := net.Pipe()
			spy := &closeSpy{Conn: ours, b: b}
			b.mu.Lock()
			b.conns[spy] = true
			b.mu.Unlock()
			wrote := make(chan error, 1)
			go func() {
				_, err := io.WriteString(theirs, tc.lines)
				wrote <- errors.Join(err, theirs.Close())
			}()
			b.serve(spy)
			if err := <-wrote; err != nil && !errors.Is(err, io.ErrClosedPipe) {
				t.Fatal(err)
			}
			if len(spy.atClose) != tc.atClose || len(b.Frames()) != tc.atClose+1 {
				t.Fatalf("closed with frames %+v recorded, all %+v", spy.atClose, b.Frames())
			}
		})
	}
}
