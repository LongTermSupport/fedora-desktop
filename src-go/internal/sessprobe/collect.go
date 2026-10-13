package sessprobe

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

const (
	defaultProcRoot   = "/proc"
	defaultSampleFor  = time.Second
	defaultClockTicks = 100
	unknownWord       = "unknown"
)

// Paner lists the panes of the tmux server (tmuxccy.Tmux does).
type Paner interface {
	Panes(ctx context.Context) ([]tmuxccy.Pane, error)
}

// Probe gathers the tables Detail joins. Zero values mean the real thing: /proc, the real clock
// and sleep, one second of sampling, and the 100 ticks a second every Linux kernel counts CPU
// time in (USER_HZ), which is what `getconf CLK_TCK` reports.
type Probe struct {
	Tmux       Paner
	Runner     execx.Runner
	ProcRoot   string
	Now        func() time.Time
	Sleep      func(time.Duration)
	SampleFor  time.Duration
	ClockTicks int64
}

// Detail is the extra columns of one session's picker row.
type Detail struct {
	Session string
	Network string
	Token   string
	Keys    string
	CPU     string
}

// Unknown is the Detail of a session the probe could not read: every column says so, because a
// blank one would read as "no network".
func Unknown(session string) Detail {
	return Detail{Session: session, Network: unknownWord, Token: unknownWord, Keys: unknownWord, CPU: unknownWord}
}

// Detail is the network, token, key and CPU columns for every session on the CCY server, in
// the server's pane order. The same probes whatever the number of sessions: tmux for the panes,
// one process table, two queries per engine that is actually running a CCY container (none at
// all when no session has one) and two CPU samples SampleFor apart. The picker rebuilds its rows
// on every loop, so this has to stay flat in the session count rather than shelling out per
// row. No server is no sessions: nothing is probed and nothing waited for.
func (p *Probe) Detail(ctx context.Context) ([]Detail, error) {
	panes, err := p.Tmux.Panes(ctx)
	if err != nil {
		return nil, err
	}
	if len(panes) == 0 {
		return nil, nil
	}
	table, err := execx.Combined(ctx, p.Runner, execx.Cmd{Name: "ps", Args: []string{"-ww", "-eo", "pid=,ppid=,args="}})
	if err != nil {
		return nil, fmt.Errorf("the process table could not be read, so no session can be matched to a container: %s: %w", strings.TrimSpace(table), err)
	}
	procs, err := ParseProcs(table)
	if err != nil {
		return nil, err
	}
	owners := SessionContainers(panes, procs)
	known, running, err := p.listEngines(ctx, owners)
	if err != nil {
		return nil, err
	}
	containerPIDs, err := p.inspectRunning(ctx, owners, running)
	if err != nil {
		return nil, err
	}
	roots := make([]Root, 0, len(panes)+len(owners))
	for _, pane := range panes {
		roots = append(roots, Root{Session: pane.Session, PID: pane.PID})
	}
	for _, o := range owners {
		if pid := containerPIDs[o.Container]; o.HasContainer() && pid != 0 {
			roots = append(roots, Root{Session: o.Session, PID: pid})
		}
	}
	sessions := SessionPIDs(roots, procs)
	cpu, err := p.cpuOf(sessions)
	if err != nil {
		return nil, err
	}
	rows := make([]Detail, 0, len(owners))
	for _, o := range owners {
		id := IdentityWords(o.Container, known)
		word, found := cpu[o.Session]
		if !found {
			word = unknownWord
		}
		rows = append(rows, Detail{Session: o.Session, Network: NetworkWord(o.Container, known), Token: id.Token, Keys: id.Keys, CPU: word})
	}
	return rows, nil
}

// engines are the distinct engines of the sessions that have a container, sorted.
func engines(owners []Owner) []string {
	var list []string
	for _, o := range owners {
		if o.HasContainer() && !slices.Contains(list, o.Engine) {
			list = append(list, o.Engine)
		}
	}
	sort.Strings(list)
	return list
}

// listEngines asks each engine in use for its CCY containers: every listed container's words,
// and the set of the running ones.
func (p *Probe) listEngines(ctx context.Context, owners []Owner) ([]Container, map[string]bool, error) {
	var known []Container
	running := map[string]bool{}
	for _, engine := range engines(owners) {
		// Every CCY container carries `--label ccy=true`, so this asks about those and nothing
		// else on the host.
		out, err := execx.Combined(ctx, p.Runner, execx.Cmd{Name: engine, Args: []string{"ps", "--filter", "label=ccy=true", "--format", DetailFormat}})
		if err != nil {
			return nil, nil, fmt.Errorf("%s ps failed: %s: %w", engine, strings.TrimSpace(out), err)
		}
		for _, c := range ParseEnginePS(out) {
			known = append(known, c)
			running[c.Name] = true
		}
	}
	return known, running, nil
}

// inspectRunning asks each engine for the host pid of the first process of the containers it
// has just listed as running: one that has exited has no processes to count.
func (p *Probe) inspectRunning(ctx context.Context, owners []Owner, running map[string]bool) (map[string]int, error) {
	asked := map[string][]string{}
	for _, o := range owners {
		if o.HasContainer() && running[o.Container] {
			asked[o.Engine] = append(asked[o.Engine], o.Container)
		}
	}
	pids := map[string]int{}
	for _, engine := range engines(owners) {
		names := asked[engine]
		if len(names) == 0 {
			continue
		}
		out, err := execx.Combined(ctx, p.Runner, execx.Cmd{Name: engine, Args: append([]string{"inspect", "--format", PIDFormat}, names...)})
		if err != nil {
			return nil, fmt.Errorf("%s inspect failed: %s: %w", engine, strings.TrimSpace(out), err)
		}
		parsed, parseErr := ParseInspect(out)
		if parseErr != nil {
			return nil, fmt.Errorf("%s inspect: %w", engine, parseErr)
		}
		maps.Copy(pids, parsed)
	}
	return pids, nil
}

// cpuOf samples every session's processes twice, SampleFor apart.
func (p *Probe) cpuOf(sessions []SessionProcs) (map[string]string, error) {
	var all []int
	for _, s := range sessions {
		all = append(all, s.PIDs...)
	}
	root := p.ProcRoot
	if root == "" {
		root = defaultProcRoot
	}
	before, err := ReadSample(root, all, p.now())
	if err != nil {
		return nil, err
	}
	p.sleep()
	after, err := ReadSample(root, all, p.now())
	if err != nil {
		return nil, err
	}
	hz := p.ClockTicks
	if hz == 0 {
		hz = defaultClockTicks
	}
	words, err := CPUWords(sessions, before.Ticks, after.Ticks, after.Micros-before.Micros, hz)
	if err != nil {
		return nil, err
	}
	cpu := make(map[string]string, len(words))
	for _, w := range words {
		cpu[w.Session] = w.Percent
	}
	return cpu, nil
}

func (p *Probe) now() time.Time {
	if p.Now == nil {
		return time.Now()
	}
	return p.Now()
}

func (p *Probe) sleep() {
	d := p.SampleFor
	if d == 0 {
		d = defaultSampleFor
	}
	if p.Sleep == nil {
		time.Sleep(d)
		return
	}
	p.Sleep(d)
}
