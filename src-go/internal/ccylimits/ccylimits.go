// Package ccylimits is the limit set every container ccy runs is started with, the Go copy of
// files/var/local/claude-yolo/lib/container-limits.bash (TestTheLibrarysValues holds the two
// together): the ccy launcher's Go front and ccc use it. Why each limit, and its overrides:
// docs/ccy.md, "Container limits".
//
//   - L1. The pids limit: CCY_PIDS_LIMIT, a whole number from 256 to 32768, else 16384.
//   - L2. The memory limit: CCY_MEMORY_MAX (bytes, or a number with k, m or g), from 2 GiB to
//     the machine's RAM; else the slice's MemoryMax, so a lone container may fill the slice and
//     the one host_vars setting the play writes it from (SliceSetting) sets both, for every
//     launch path, the restore unit's included. A slice unit with no MemoryMax=<n>M is refused
//     naming the play, and one under the floor or over the RAM naming the setting: no value
//     is picked. It cannot be turned off. --memory-reservation is 2 GiB below, or half the cap
//     where that is less: on cgroup v2 crun writes it to memory.low, a reclaim protection and
//     not a throttle, which inside agents.slice (no MemoryLow of its own) protects nothing; the
//     throttle is the slice's MemoryHigh.
//   - L3. The slice: every container runs in the user unit agents.slice, which caps all of them
//     together; a host without its unit file is refused, naming the play that installs it.
package ccylimits

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// The bounds, held to the library's by TestTheLibrarysValues.
const (
	PidsEnv     = "CCY_PIDS_LIMIT"
	PidsDefault = 16384
	PidsMin     = 256
	PidsMax     = 32768

	MemoryEnv = "CCY_MEMORY_MAX"
	// FloorMiB is the smallest cap a container may have: room for a session on a 4 GiB host.
	FloorMiB = 2048
	// SoftGapMiB is how far below the cap --memory-reservation (memory.low) is, at most.
	SoftGapMiB = 2048

	// Slice is the user slice every ccy container runs in.
	Slice = "agents.slice"
	// Play installs the slice's unit.
	Play = "playbooks/imports/play-claude-yolo.yml"
	// SliceSetting is the host_vars setting the play writes the slice's MemoryMax from.
	SliceSetting = "ccy_agents_slice_memory_max_mb"
)

const (
	kibPerMiB = 1024
	kibPerGiB = 1024 * 1024
	bytesKiB  = 1024
)

var (
	pidsForm   = regexp.MustCompile(`^[1-9][0-9]{0,4}$`)
	memoryForm = regexp.MustCompile(`^([1-9][0-9]{0,11})([bkmgBKMG]?)$`)
	// sliceMaxForm is the MemoryMax line the play's agents.slice template writes.
	sliceMaxForm = regexp.MustCompile(`^MemoryMax=([1-9][0-9]{0,8})M$`)
)

// Limits is one settled limit set.
type Limits struct {
	Pids      string
	MemoryMiB int
}

// Refused is a limit the library refuses, with its reason.
type Refused struct{ Why string }

func (r *Refused) Error() string { return r.Why }

// Run is `run --rm` with the whole limit set, then flags: the argv of every container ccy runs.
func (l Limits) Run(flags ...string) []string {
	return append([]string{
		"run", "--rm", "--cgroup-parent", Slice, "--pids-limit", l.Pids,
		"--memory", strconv.Itoa(l.MemoryMiB) + "m",
		"--memory-reservation", strconv.Itoa(l.MemoryMiB-min(SoftGapMiB, l.MemoryMiB/2)) + "m",
	}, flags...)
}

// Pids is L1: the pids limit value settles to, and false where the library refuses it.
func Pids(value string) (string, bool) {
	if value == "" {
		return strconv.Itoa(PidsDefault), true
	}
	if !pidsForm.MatchString(value) {
		return "", false
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < PidsMin || n > PidsMax {
		return "", false
	}
	return value, true
}

// MemoryMiB is L2: the cap in MiB for the override (empty for none) on a machine whose
// /proc/meminfo is meminfo and whose agents.slice unit is slice.
func MemoryMiB(override string, meminfo, slice []byte) (int, error) {
	total, ok := memTotalKiB(meminfo)
	if !ok {
		return 0, &Refused{Why: "could not read MemTotal from /proc/meminfo, so no memory cap can be set for a ccy container"}
	}
	if override == "" {
		return sliceMaxMiB(slice, total)
	}
	refused := &Refused{Why: fmt.Sprintf("%s=%s is not a memory cap: bytes, or a number with k, m or g, from %dm to this machine's %d MiB of RAM; it cannot be turned off (docs/ccy.md, \"Container limits\")",
		MemoryEnv, override, FloorMiB, total/kibPerMiB)}
	m := memoryForm.FindStringSubmatch(override)
	if m == nil {
		return 0, refused
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, refused
	}
	var kib int
	switch m[2] {
	case "", "b", "B":
		kib = n / bytesKiB
	case "k", "K":
		kib = n
	case "m", "M":
		kib = n * kibPerMiB
	default:
		kib = n * kibPerGiB
	}
	if kib < FloorMiB*kibPerMiB || kib > total {
		return 0, refused
	}
	return kib / kibPerMiB, nil
}

// sliceMaxMiB is the slice unit's MemoryMax in MiB, the last line in the play's form (systemd
// reads the last), held between the floor and the machine's totalKiB of RAM.
func sliceMaxMiB(slice []byte, totalKiB int) (int, error) {
	mib := 0
	s := bufio.NewScanner(bytes.NewReader(slice))
	for s.Scan() {
		if m := sliceMaxForm.FindStringSubmatch(s.Text()); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				return 0, &Refused{Why: fmt.Sprintf("%s's MemoryMax=%sM is not a number: %v", Slice, m[1], err)}
			}
			mib = n
		}
	}
	if mib == 0 {
		return 0, &Refused{Why: fmt.Sprintf("%s has no MemoryMax=<MiB>M line, which a container's default memory cap is read from. Deploy it with %s, then launch again (or set %s on this host)", Slice, Play, MemoryEnv)}
	}
	if mib < FloorMiB || mib*kibPerMiB > totalKiB {
		return 0, &Refused{Why: fmt.Sprintf("%s's MemoryMax is %d MiB, a container's default memory cap, which must be from the %d MiB floor to this machine's %d MiB of RAM. Set %s in this host's host_vars to the MiB all ccy containers together may use and run %s (docs/ccy.md, \"Container limits\")",
			Slice, mib, FloorMiB, totalKiB/kibPerMiB, SliceSetting, Play)}
	}
	return mib, nil
}

// memTotalKiB is MemTotal from /proc/meminfo, in KiB.
func memTotalKiB(meminfo []byte) (int, bool) {
	s := bufio.NewScanner(bytes.NewReader(meminfo))
	for s.Scan() {
		f := bytes.Fields(s.Bytes())
		if len(f) >= 2 && string(f[0]) == "MemTotal:" {
			n, err := strconv.Atoi(string(f[1]))
			return n, err == nil && n > 0
		}
	}
	return 0, false
}

// SliceUnit is where the play installs the slice's unit: the user's systemd directory.
func SliceUnit(getenv func(string) string) string {
	config := getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(getenv("HOME"), ".config")
	}
	return filepath.Join(config, "systemd", "user", Slice)
}

// Settle is L1 to L3 for the environment getenv reads, on a machine whose /proc/meminfo is
// meminfo. A refusal is a *Refused.
func Settle(getenv func(string) string, meminfo []byte) (Limits, error) {
	pids, ok := Pids(getenv(PidsEnv))
	if !ok {
		return Limits{}, &Refused{Why: fmt.Sprintf("%s=%s is not a pids limit: a whole number from %d to %d", PidsEnv, getenv(PidsEnv), PidsMin, PidsMax)}
	}
	unit := SliceUnit(getenv)
	slice, err := readRegular(unit)
	if err != nil {
		return Limits{}, &Refused{Why: fmt.Sprintf("%s is not installed (%s is missing): every ccy container runs in it, so that all of them together cannot take the host's memory. Deploy it with %s, then launch again", Slice, unit, Play)}
	}
	memory, err := MemoryMiB(getenv(MemoryEnv), meminfo, slice)
	if err != nil {
		return Limits{}, err
	}
	return Limits{Pids: pids, MemoryMiB: memory}, nil
}

// readRegular is the content of path, which must be a regular file.
func readRegular(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return os.ReadFile(path)
}
