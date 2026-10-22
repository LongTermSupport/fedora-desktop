package link

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const startWall = int64(1_700_000_000_000)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func tid(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	check(t, err)
	return id
}

func agent(tm paths.TeamID) string {
	return "@repo.1+site.podman:" + paths.ServerName(tm)
}

// until polls cond, yielding between tries, until it holds or the test's time is up.
func until(t *testing.T, cond func() bool) {
	t.Helper()
	for !cond() {
		select {
		case <-t.Context().Done():
			t.Fatal("the condition never held")
		default:
			runtime.Gosched()
		}
	}
}

// logBuf is link.log.
type logBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// fakeProc is a process table: each pid's parent and start ticks.
type fakeProc struct {
	mu      sync.Mutex
	parents map[int]int
	ticks   map[int]int64
	costErr error
}

func newFakeProc() *fakeProc { return &fakeProc{parents: map[int]int{}, ticks: map[int]int64{}} }

func (p *fakeProc) PPid(pid int) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parent, ok := p.parents[pid]
	if !ok {
		return 0, os.ErrNotExist
	}
	return parent, nil
}

func (p *fakeProc) StartTicks(pid int) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.ticks[pid]
	if !ok {
		return 0, os.ErrNotExist
	}
	return t, nil
}

func (p *fakeProc) Cost(pid int) (Cost, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.costErr != nil {
		return Cost{}, p.costErr
	}
	return Cost{RSSKiB: int64(pid), StartTicks: p.ticks[pid]}, nil
}

func (p *fakeProc) add(pid, parent int, ticks int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.parents[pid], p.ticks[pid] = parent, ticks
}

// home is a member home with its runtime directory below it, one root holding both.
type home struct {
	dir    string
	root   *homefs.Root
	layout paths.Layout
	run    paths.RunID
	clk    *clock.Fake
	log    *logBuf
}

func newHome(t *testing.T) *home {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "h")
	check(t, os.Mkdir(dir, 0o700))
	root, err := homefs.Open(dir, homefs.Private)
	check(t, err)
	t.Cleanup(func() { check(t, root.Close()) })
	layout, err := paths.HomeLayout(dir, dir+"/r")
	check(t, err)
	run, err := paths.ParseRunID("0123456789abcdef")
	check(t, err)
	check(t, root.MkdirAll(layout.Run(run).Dir()))
	check(t, root.MkdirAll(layout.State()))
	return &home{dir: dir, root: root, layout: layout, run: run, clk: clock.NewFake(time.Hour, startWall), log: &logBuf{}}
}
