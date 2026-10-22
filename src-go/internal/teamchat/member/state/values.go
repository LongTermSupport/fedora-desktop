package state

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The closed sets the member files share (B 2.3.3, 2.4.1, 2.9).
const (
	KindCCY   = "ccy"
	KindLXC   = "lxc"
	KindHost  = "host"
	KindOther = "other"

	InboundAccept = "accept"
	InboundRefuse = "refuse"

	LinkRunning    = "running"
	LinkRestarting = "restarting"
	LinkFailed     = "failed"

	DesiredApplied = "applied"
	DesiredRefused = "refused"

	DeliverySocket = "socket"
	DeliveryWait   = "wait"
	DeliveryNone   = "none"

	SourceStartup = "startup"
	SourceResume  = "resume"
	SourceClear   = "clear"
	SourceCompact = "compact"
)

// maxPID is the kernel's PID_MAX_LIMIT; maxSocketPath is what a Unix socket address holds.
const (
	maxPID        = 1 << 22
	maxSocketPath = 107
	// maxTeams bounds the teams one session holds (B 2.9's status.json, B 2.3.3's roles).
	maxTeams = 64
)

var (
	memberKinds   = []string{KindCCY, KindLXC, KindHost, KindOther}
	inbounds      = []string{InboundAccept, InboundRefuse}
	links         = []string{LinkRunning, LinkRestarting, LinkFailed}
	desiredStates = []string{DesiredApplied, DesiredRefused}
	deliveries    = []string{DeliverySocket, DeliveryWait, DeliveryNone}
	sources       = []string{SourceStartup, SourceResume, SourceClear, SourceCompact}
	// sessionIDRe is Claude Code's session id, a lowercase UUID.
	sessionIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// Process is a process by its pid and its start time (field 22 of /proc/<pid>/stat), so a
// reused pid is never taken for it (B 2.4.1 step 4).
type Process struct {
	PID        int   `json:"pid"`
	StartTicks int64 `json:"start_ticks"`
}

func (p Process) valid() bool { return p.PID > 0 && p.PID <= maxPID && p.StartTicks >= 0 }

func validSessionID(s string) bool { return sessionIDRe.MatchString(s) }

// cleanAbs is an absolute path with no `.`, `..` or empty segment.
func cleanAbs(path string) bool { return filepath.IsAbs(path) && filepath.Clean(path) == path }

// validTranscript is a session transcript's path: absolute, clean, a .jsonl file.
func validTranscript(path string) bool {
	return cleanAbs(path) && strings.HasSuffix(path, ".jsonl") && len(path) <= maxTranscriptPath
}

// validSocket is the inbox socket of the Claude Code process pid: `<dir>/cc-socks/<pid>.sock`
// (paths.InboxSocket), short enough to bind.
func validSocket(path string, pid int) bool {
	return cleanAbs(path) && len(path) <= maxSocketPath && strings.HasSuffix(path, "/cc-socks/"+strconv.Itoa(pid)+".sock")
}

// optionalOf is null or one of set.
func optionalOf(s *string, set []string) bool { return s == nil || slices.Contains(set, *s) }

// optionalMS is null or a time.
func optionalMS(ms *int64) bool { return ms == nil || validMS(*ms) }
