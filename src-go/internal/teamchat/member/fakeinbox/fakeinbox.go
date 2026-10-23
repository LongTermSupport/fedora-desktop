// Package fakeinbox is F2, the fake of a session's inbox socket (Plan 00209 SPEC-n3, B 2.15):
// a Unix listener that speaks the auth and user lines, records every frame, and can hold,
// refuse, and apply the 30 s duplicate filter and the 30-token, 0.5/s bucket that U01
// measured. A frame the filter or the bucket drops is accepted on the wire and never handed on
// (OnAccepted, where faketranscript is attached): that is how a held or dropped message looks
// to the link. It is a non-test package for the engine's, the link's and PT1's tests;
// production code never imports it. It never records a token.
package fakeinbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// Options configure the fake; a zero filter value takes U01's measure.
type Options struct {
	Token     string
	Clock     clock.Clock
	DupWindow time.Duration // 30 s
	Bucket    int           // 30
	PerSecond float64       // 0.5
}

// Frame is one line the fake read. Reason is "", "duplicate", "rate", "auth" or "framing".
type Frame struct {
	Auth     bool
	Content  string
	Accepted bool
	Reason   string
}

const (
	reasonDuplicate = "duplicate"
	reasonRate      = "rate"
	reasonAuth      = "auth"
	reasonFraming   = "framing"

	maxLine = 64 << 20
)

// Inbox is a running fake.
type Inbox struct {
	path string
	opt  Options
	l    *net.UnixListener
	wg   sync.WaitGroup

	mu         sync.Mutex
	frames     []Frame
	changed    chan struct{}
	onAccepted func(Frame)
	holds      int
	released   chan struct{}
	conns      map[net.Conn]bool
	seen       map[string]time.Duration
	tokens     float64
	filled     time.Duration
	refused    bool
	removed    bool
	closed     bool
}

// Start listens at path.
func Start(path string, opt Options) (*Inbox, error) {
	if opt.Token == "" || opt.Clock == nil {
		return nil, errors.New("fakeinbox: a token and a clock are required")
	}
	if opt.DupWindow < 0 || opt.Bucket < 0 || opt.PerSecond < 0 {
		return nil, fmt.Errorf("fakeinbox: negative filter options %+v", opt.filters())
	}
	if opt.DupWindow == 0 {
		opt.DupWindow = 30 * time.Second
	}
	if opt.Bucket == 0 {
		opt.Bucket = 30
	}
	if opt.PerSecond == 0 {
		opt.PerSecond = 0.5
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("fakeinbox: %w", err)
	}
	l.SetUnlinkOnClose(false)
	b := &Inbox{
		path: path, opt: opt, l: l,
		changed: make(chan struct{}), released: make(chan struct{}),
		conns: map[net.Conn]bool{}, seen: map[string]time.Duration{},
		tokens: float64(opt.Bucket), filled: opt.Clock.Boot(),
	}
	b.wg.Go(b.accept)
	return b, nil
}

// filters is the options without the token, for an error.
func (o Options) filters() string {
	return fmt.Sprintf("dup %s bucket %d per second %g", o.DupWindow, o.Bucket, o.PerSecond)
}

// Path is the socket's path.
func (b *Inbox) Path() string { return b.path }

// Frames is every frame read so far, in order.
func (b *Inbox) Frames() []Frame {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Frame(nil), b.frames...)
}

// Await returns once n frames are recorded, or with ctx's error.
func (b *Inbox) Await(ctx context.Context, n int) error {
	for {
		b.mu.Lock()
		got, changed := len(b.frames), b.changed
		b.mu.Unlock()
		if got >= n {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("fakeinbox: %d of %d frames: %w", got, n, ctx.Err())
		case <-changed:
		}
	}
}

// OnAccepted is called with each accepted user frame, before its connection is closed, so a
// link's Finish returns after the frame was handed on.
func (b *Inbox) OnAccepted(f func(Frame)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onAccepted = f
}

// Hold keeps accepting connections and reads nothing on any of them until every hold is
// released; release is idempotent.
func (b *Inbox) Hold() func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.holds++
	if b.holds == 1 {
		b.released = make(chan struct{})
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.holds--
			if b.holds == 0 {
				close(b.released)
			}
		})
	}
}

// Refuse closes the listener and keeps the path, so a connect is ECONNREFUSED.
func (b *Inbox) Refuse() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.refused || b.closed {
		return
	}
	b.refused = true
	if err := b.l.Close(); err != nil {
		panic("fakeinbox: close the listener: " + err.Error())
	}
}

// Remove unlinks the path, so a connect is ENOENT.
func (b *Inbox) Remove() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.removed {
		return
	}
	b.removed = true
	if err := os.Remove(b.path); err != nil {
		panic("fakeinbox: remove the socket: " + err.Error())
	}
}

// Close stops the fake, ends every connection, held ones included, and unlinks the path.
func (b *Inbox) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	var errs []error
	if !b.refused {
		errs = append(errs, b.l.Close())
	}
	if !b.removed {
		errs = append(errs, os.Remove(b.path))
	}
	for c := range b.conns {
		errs = append(errs, c.Close())
		delete(b.conns, c)
	}
	if b.holds > 0 {
		b.holds = 0
		close(b.released)
	}
	b.mu.Unlock()
	b.wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("fakeinbox: close: %w", err)
	}
	return nil
}

func (b *Inbox) accept() {
	for {
		c, err := b.l.Accept()
		if err != nil {
			return
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			if err := c.Close(); err != nil {
				return
			}
			return
		}
		b.conns[c] = true
		b.mu.Unlock()
		b.wg.Go(func() { b.serve(c) })
	}
}

// serve reads one delivery: the auth line, the user line, then EOF. Its last frame, a refusal
// or a line after the user line, is recorded only once the connection is closed, so a test that
// awaits it finds the close done.
func (b *Inbox) serve(c net.Conn) {
	last, ok := b.converse(c)
	b.drop(c)
	if ok {
		b.record(last, false)
	}
}

// converse reads c's lines, recording each frame the delivery goes on after, and returns the
// frame that ends it, if one does.
func (b *Inbox) converse(c net.Conn) (Frame, bool) {
	r := bufio.NewReader(gated{b: b, r: c})
	line, ok := readLine(r)
	if !ok {
		return Frame{Auth: true, Reason: reasonFraming}, line != nil
	}
	var a struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if decode(line, &a) != nil || a.Type != "auth" {
		return Frame{Auth: true, Reason: reasonFraming}, true
	}
	if a.Token != b.opt.Token {
		return Frame{Auth: true, Reason: reasonAuth}, true
	}
	b.record(Frame{Auth: true, Accepted: true}, false)
	line, ok = readLine(r)
	if !ok {
		return Frame{Reason: reasonFraming}, line != nil
	}
	var u struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}
	if !ascii(line) || decode(line, &u) != nil || u.Type != "user" || u.Message.Role != "user" {
		return Frame{Reason: reasonFraming}, true
	}
	b.record(b.judge(u.Message.Content), true)
	if extra, err := r.Peek(1); len(extra) > 0 || !errors.Is(err, io.EOF) {
		return Frame{Reason: reasonFraming}, true
	}
	return Frame{}, false
}

// readLine is a whole newline-ended line (ok), a cut one (not ok, non-nil), or nothing.
func readLine(r *bufio.Reader) ([]byte, bool) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if err == nil {
			return line, true
		}
		if !errors.Is(err, bufio.ErrBufferFull) || len(line) > maxLine {
			return line, false
		}
	}
}

func decode(line []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(line))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.More() {
		return errors.New("trailing data")
	}
	return nil
}

func ascii(line []byte) bool {
	for _, c := range line {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// judge applies the duplicate filter, then the bucket, on the boot clock.
func (b *Inbox) judge(content string) Frame {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.opt.Clock.Boot()
	if at, ok := b.seen[content]; ok && now-at < b.opt.DupWindow {
		return Frame{Content: content, Reason: reasonDuplicate}
	}
	b.tokens = min(float64(b.opt.Bucket), b.tokens+(now-b.filled).Seconds()*b.opt.PerSecond)
	b.filled = now
	if b.tokens < 1 {
		return Frame{Content: content, Reason: reasonRate}
	}
	b.tokens--
	b.seen[content] = now
	return Frame{Content: content, Accepted: true}
}

// record hands an accepted frame on (when handOn) and only then appends it and wakes Await, so
// a test that awaits the frame finds it handed on.
func (b *Inbox) record(f Frame, handOn bool) {
	b.mu.Lock()
	on := b.onAccepted
	b.mu.Unlock()
	if handOn && f.Accepted && on != nil {
		on(f)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frames = append(b.frames, f)
	close(b.changed)
	b.changed = make(chan struct{})
}

func (b *Inbox) drop(c net.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.conns[c] {
		return
	}
	delete(b.conns, c)
	if err := c.Close(); err != nil {
		panic("fakeinbox: close a connection: " + err.Error())
	}
}

// gated reads only while no hold is in place.
type gated struct {
	b *Inbox
	r io.Reader
}

func (g gated) Read(p []byte) (int, error) {
	g.b.mu.Lock()
	held, released, closed := g.b.holds > 0, g.b.released, g.b.closed
	g.b.mu.Unlock()
	if held {
		<-released
	}
	if closed {
		return 0, net.ErrClosed
	}
	return g.r.Read(p)
}
