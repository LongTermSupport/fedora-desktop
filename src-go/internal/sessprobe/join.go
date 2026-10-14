// Package sessprobe works out what each running CCY or cc session is made of and how busy it
// is: which container (and engine) belongs to which tmux session, which processes make up a
// session, how much CPU they used over a sample, and the network, token and key words a picker
// row shows. The joins are pure over text-shaped tables so every shape is testable; the probe
// that gathers the tables and runs them is Collect.
package sessprobe

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// maxClientHops and maxProcessHops stop a malformed process table (a cycle, a pid that is its
// own parent) from spinning the climb to a pane or a root.
const (
	maxClientHops  = 32
	maxProcessHops = 64
)

// Proc is one line of the process table.
type Proc struct {
	PID  int
	PPID int
	Args string
}

// ParseProcs reads `ps -ww -eo pid=,ppid=,args=` output. Blank lines are skipped; a line that
// is not pid, ppid and a command is an error, not a guess.
func ParseProcs(text string) ([]Proc, error) {
	var procs []Proc
	for line := range strings.SplitSeq(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 3 {
			return nil, fmt.Errorf("unreadable process table line %q", line)
		}
		pid, pidErr := strconv.Atoi(fields[0])
		ppid, ppidErr := strconv.Atoi(fields[1])
		if pidErr != nil || ppidErr != nil {
			return nil, fmt.Errorf("unreadable process table line %q", line)
		}
		args := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		args = strings.TrimSpace(strings.TrimPrefix(args, fields[1]))
		procs = append(procs, Proc{PID: pid, PPID: ppid, Args: args})
	}
	return procs, nil
}

// Owner is the engine client found in a session's process tree, if any: Engine and Container
// are empty for a session with none (a cc session runs claude on the host).
type Owner struct {
	Session   string
	Engine    string
	Container string
}

// HasContainer reports whether an engine client was found.
func (o Owner) HasContainer() bool { return o.Container != "" }

// engineClient is a podman or docker command line that names a container.
type engineClient struct {
	engine, container string
}

// clientOf reads the engine and container from one command line. --name is not an engine-only
// flag, so the command itself has to be the engine; the container is the value of the first
// --name or --name= after it.
func clientOf(args string) (engineClient, bool) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return engineClient{}, false
	}
	engine := path.Base(fields[0])
	if engine != "podman" && engine != "docker" {
		return engineClient{}, false
	}
	for i := 1; i < len(fields); i++ {
		switch {
		case strings.HasPrefix(fields[i], "--name="):
			container := strings.TrimPrefix(fields[i], "--name=")
			return engineClient{engine, container}, container != ""
		case fields[i] == "--name":
			if i+1 >= len(fields) {
				return engineClient{}, false
			}
			return engineClient{engine, fields[i+1]}, true
		}
	}
	return engineClient{}, false
}

// climb walks from pid up the parent table until it reaches a pid in targets, giving up after
// maxHops steps, at pid 0, or at a pid the table does not know.
func climb(pid int, parent map[int]int, targets map[int]string, maxHops int) (string, bool) {
	walk := pid
	for hops := 0; walk != 0 && hops < maxHops; hops++ {
		if session, ok := targets[walk]; ok {
			return session, true
		}
		next, known := parent[walk]
		if !known {
			break
		}
		walk = next
	}
	return "", false
}

// SessionContainers says, for every session in panes in order of first appearance, which engine
// and container its process tree holds. Rootless podman re-executes itself in a user namespace,
// so one container legitimately appears twice in one tree; the first client found wins. A
// container whose client is outside every pane's tree belongs to no session here.
func SessionContainers(panes []tmuxccy.Pane, procs []Proc) []Owner {
	parent := make(map[int]int, len(procs))
	var clientPIDs []int
	client := map[int]engineClient{}
	for _, p := range procs {
		parent[p.PID] = p.PPID
		if c, ok := clientOf(p.Args); ok {
			client[p.PID] = c
			clientPIDs = append(clientPIDs, p.PID)
		}
	}
	sessionOf := map[int]string{}
	var order []string
	seen := map[string]bool{}
	for _, p := range panes {
		sessionOf[p.PID] = p.Session
		if !seen[p.Session] {
			seen[p.Session] = true
			order = append(order, p.Session)
		}
	}
	owner := map[string]engineClient{}
	for _, pid := range clientPIDs {
		if session, ok := climb(pid, parent, sessionOf, maxClientHops); ok {
			if _, taken := owner[session]; !taken {
				owner[session] = client[pid]
			}
		}
	}
	owners := make([]Owner, 0, len(order))
	for _, session := range order {
		c := owner[session]
		owners = append(owners, Owner{Session: session, Engine: c.engine, Container: c.container})
	}
	return owners
}

// Root is a process at the top of a session's tree: its pane, or its container's first process.
type Root struct {
	Session string
	PID     int
}

// SessionProcs is every process at or under any of a session's roots, in process-table order.
type SessionProcs struct {
	Session string
	PIDs    []int
}

// SessionPIDs says which processes belong to which session, in order of the sessions' first
// appearance among the roots. A session's processes are everything under its pane AND everything
// under its container's first process: rootless podman starts the container under conmon, not
// under the podman client in the pane, so the pane's tree alone would show a busy ccy session as
// idle.
func SessionPIDs(roots []Root, procs []Proc) []SessionProcs {
	rootOf := map[int]string{}
	var order []string
	owned := map[string][]int{}
	listed := map[string]bool{}
	for _, r := range roots {
		rootOf[r.PID] = r.Session
		if !listed[r.Session] {
			listed[r.Session] = true
			order = append(order, r.Session)
		}
	}
	parent := make(map[int]int, len(procs))
	for _, p := range procs {
		parent[p.PID] = p.PPID
	}
	for _, p := range procs {
		if session, ok := climb(p.PID, parent, rootOf, maxProcessHops); ok {
			owned[session] = append(owned[session], p.PID)
		}
	}
	out := make([]SessionProcs, 0, len(order))
	for _, session := range order {
		out = append(out, SessionProcs{Session: session, PIDs: owned[session]})
	}
	return out
}

// Sample is the clock in microseconds and each still-running process's user plus system time in
// clock ticks, and the tick (counted from boot) each of them started at: a pid whose start time
// changed is another process that was given the number.
type Sample struct {
	Micros int64
	Ticks  map[int]int64
	Starts map[int]int64
}

// ReadSample reads /proc/<pid>/stat (under procRoot) for each pid at the moment at. A process
// that has exited since the table was read has no time left to count and is skipped.
func ReadSample(procRoot string, pids []int, at time.Time) (Sample, error) {
	s := Sample{Micros: at.UnixMicro(), Ticks: make(map[int]int64, len(pids)), Starts: make(map[int]int64, len(pids))}
	for _, pid := range pids {
		raw, err := os.ReadFile(filepath.Clean(filepath.Join(procRoot, strconv.Itoa(pid), "stat")))
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return Sample{}, fmt.Errorf("could not read the CPU time of process %d: %w", pid, err)
		}
		ticks, start, parseErr := statTimes(string(raw))
		if parseErr != nil {
			return Sample{}, fmt.Errorf("process %d: %w", pid, parseErr)
		}
		s.Ticks[pid], s.Starts[pid] = ticks, start
	}
	return s, nil
}

// statTimes is utime plus stime (fields 14 and 15) and starttime (field 22) of a
// /proc/<pid>/stat line. The command name, in brackets, may hold spaces and brackets: the fields
// are counted after the last ") ".
func statTimes(line string) (int64, int64, error) {
	cut := strings.LastIndex(line, ") ")
	if cut < 0 {
		return 0, 0, fmt.Errorf("unreadable stat line %q", strings.TrimSpace(line))
	}
	fields := strings.Fields(line[cut+2:])
	const utime, stime, starttime = 11, 12, 19
	if len(fields) <= starttime {
		return 0, 0, fmt.Errorf("unreadable stat line %q", strings.TrimSpace(line))
	}
	user, userErr := strconv.ParseInt(fields[utime], 10, 64)
	system, systemErr := strconv.ParseInt(fields[stime], 10, 64)
	began, beganErr := strconv.ParseInt(fields[starttime], 10, 64)
	if userErr != nil || systemErr != nil || beganErr != nil {
		return 0, 0, fmt.Errorf("unreadable stat line %q", strings.TrimSpace(line))
	}
	return user + system, began, nil
}

// SessionCPU is a session's share of CPU as the picker shows it, "N%".
type SessionCPU struct {
	Session string
	Percent string
}

// CPUWords is each session's CPU over a sample, as top shows it: 100% is one whole core. A
// process missing from before was born during the sample, so all its time is counted; one
// missing from after exited during it and its last share is lost, which is the price of sampling
// processes rather than a cgroup. The share is rounded half up in whole percent.
func CPUWords(sessions []SessionProcs, before, after map[int]int64, elapsedMicros, hz int64) ([]SessionCPU, error) {
	denominator := elapsedMicros * hz
	if denominator <= 0 {
		return nil, fmt.Errorf("a CPU sample needs time to pass and a tick rate; got %d us at %d ticks a second", elapsedMicros, hz)
	}
	out := make([]SessionCPU, 0, len(sessions))
	for _, s := range sessions {
		var used int64
		for _, pid := range s.PIDs {
			if now, ok := after[pid]; ok {
				used += now - before[pid]
			}
		}
		out = append(out, SessionCPU{Session: s.Session, Percent: strconv.FormatInt((used*100_000_000+denominator/2)/denominator, 10) + "%"})
	}
	return out, nil
}

// The engine queries: each CCY container's name, networks and the two labels the launcher puts
// on it, then the host pid of the first process of the ones that belong to a session. `|`
// separates the list's fields because podman renders a network list with spaces, and the key
// label is space-separated itself.
const (
	// DetailFormat is the --format of `<engine> ps --filter label=ccy=true`.
	DetailFormat = `{{.Names}}|{{.Networks}}|{{.Label "ccy-token"}}|{{.Label "ccy-ssh-keys"}}`
	// NamesFormat is the --format of the same query when only the running names are wanted.
	NamesFormat = `{{.Names}}`
	// PIDFormat is the --format of `<engine> inspect`.
	PIDFormat = `{{.Name}} {{.State.Pid}}`
)

const (
	noContainer = "no container"
	noWord      = "-"
	unlabelled  = "unlabelled"
)

// Container is one CCY container as the engine lists it.
type Container struct {
	Name     string
	Networks string
	Token    string
	Keys     string
}

// ParseEnginePS reads `<engine> ps --format DetailFormat` output: name|networks|token|keys, the
// keys taking the rest of the line. Lines without a name are skipped.
func ParseEnginePS(out string) []Container {
	var list []Container
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.SplitN(line, "|", 4)
		if parts[0] == "" {
			continue
		}
		c := Container{Name: parts[0]}
		if len(parts) > 1 {
			c.Networks = parts[1]
		}
		if len(parts) > 2 {
			c.Token = parts[2]
		}
		if len(parts) > 3 {
			c.Keys = parts[3]
		}
		list = append(list, c)
	}
	return list
}

func find(container string, known []Container) (Container, bool) {
	if container == "" {
		return Container{}, false
	}
	for _, c := range known {
		if c.Name == container {
			return c, true
		}
	}
	return Container{}, false
}

// NetworkWord is the one word a picker row shows for a session's network. container is empty
// when no container was found for the session. Three ordinary states get three distinct words,
// because a reader cannot act on an ambiguity:
//
//	<name>[,<name>]  what the ENGINE says the container is connected to right now
//	none             the container runs on no named network
//	no container     there is nothing to ask about: a cc session runs claude on the host, and
//	                 a ccy session outlives a container that has exited
//
// The fourth word, "unknown", belongs to the caller: it is what a FAILED probe shows.
func NetworkWord(container string, known []Container) string {
	c, ok := find(container, known)
	if !ok {
		return noContainer
	}
	// Podman's template renders a Go slice, "[a b]" or "[]"; Docker a comma string.
	raw := strings.TrimSpace(c.Networks)
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = raw[1 : len(raw)-1]
	}
	nets := strings.Fields(strings.ReplaceAll(raw, ",", " "))
	if len(nets) == 0 {
		return "none"
	}
	return strings.Join(nets, ",")
}

// Identity is the token name and the SSH keys a session's container was started with.
type Identity struct {
	Token string
	Keys  string
}

// IdentityWords is the token and key words for a session. The launcher labels every container
// with both (the token name or "none", and the key basenames space-separated or "none"); several
// keys are joined with commas so the column stays one word. "-" in both when there is no
// container: a cc session runs claude on the host, with no key mounted. A listed container with
// no labels at all says "unlabelled" rather than passing for "none".
func IdentityWords(container string, known []Container) Identity {
	c, ok := find(container, known)
	if !ok {
		return Identity{Token: noWord, Keys: noWord}
	}
	id := Identity{Token: c.Token, Keys: unlabelled}
	if id.Token == "" {
		id.Token = unlabelled
	}
	if keys := strings.Fields(c.Keys); len(keys) > 0 {
		id.Keys = strings.Join(keys, ",")
	}
	return id
}

// ParseInspect reads `<engine> inspect --format PIDFormat` output into the host pid of each
// container's first process. Docker names a container with a leading slash; podman does not.
func ParseInspect(out string) (map[string]int, error) {
	pids := map[string]int{}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, pidText, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			return nil, fmt.Errorf("unreadable inspect line %q", line)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(pidText))
		if err != nil {
			return nil, fmt.Errorf("unreadable inspect line %q: %w", line, err)
		}
		pids[strings.TrimPrefix(name, "/")] = pid
	}
	return pids, nil
}
