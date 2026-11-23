package ccylimits

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// meminfo64 is /proc/meminfo on a machine with exactly 64 GiB.
const meminfo64 = "MemTotal:       67108864 kB\nMemFree:        1000000 kB\n"

func TestPids(t *testing.T) {
	for value, want := range map[string]string{
		"": "16384", "256": "256", "4096": "4096", "16384": "16384", "32768": "32768",
		"0": "", "00": "", "-1": "", "max": "", "255": "", "32769": "", "99999": "", "100000": "",
		"04096": "", "4096 ": "", " 4096": "", "4096x": "", "1e4": "", "+4096": "",
	} {
		got, ok := Pids(value)
		if got != want || ok != (want != "") {
			t.Errorf("Pids(%q) = %q, %v; want %q", value, got, ok, want)
		}
	}
}

// The default is RAM less the desktop's 16 GiB, in MiB; a machine where that is under the
// floor is refused, naming CCY_MEMORY_MAX, and never given a value picked for it.
func TestMemoryFromMeminfo(t *testing.T) {
	for meminfo, want := range map[string]int{
		meminfo64:                                49152,
		"MemFree: 1 kB\nMemTotal: 33554432 kB\n": 16384,
		"MemTotal: 25165824 kB\n":                8192,
		"MemTotal: 20971520 kB\n":                4096,
	} {
		got, err := MemoryMiB("", []byte(meminfo))
		if err != nil || got != want {
			t.Errorf("MemoryMiB(%q) = %d, %v; want %d", meminfo, got, err, want)
		}
	}
	for _, meminfo := range []string{"MemTotal: 20971519 kB\n", "MemTotal: 16777216 kB\n", "MemTotal: 8388608 kB\n"} {
		_, err := MemoryMiB("", []byte(meminfo))
		if err == nil || !strings.Contains(err.Error(), MemoryEnv) || !strings.Contains(err.Error(), "floor") {
			t.Errorf("MemoryMiB(%q): %v; want a refusal naming %s and the floor", meminfo, err, MemoryEnv)
		}
	}
	for _, meminfo := range []string{"", "MemFree: 1 kB\n", "MemTotal: lots kB\n", "MemTotal: 0 kB\n"} {
		if _, err := MemoryMiB("", []byte(meminfo)); err == nil || !strings.Contains(err.Error(), "MemTotal") {
			t.Errorf("MemoryMiB(%q): %v; want a refusal naming MemTotal", meminfo, err)
		}
	}
}

// The override: bytes or podman's suffix form, from the floor to the machine's RAM; it cannot
// turn the cap off.
func TestMemoryOverride(t *testing.T) {
	for value, want := range map[string]int{
		"8g": 8192, "8G": 8192, "8192m": 8192, "8192M": 8192, "4g": 4096, "64g": 65536,
		"8388608k": 8192, "8589934592": 8192, "8589934592b": 8192, "8589935000": 8192, "40000m": 40000,
	} {
		got, err := MemoryMiB(value, []byte(meminfo64))
		if err != nil || got != want {
			t.Errorf("MemoryMiB(%q) = %d, %v; want %d", value, got, err, want)
		}
	}
	for _, value := range []string{
		"0", "-1", "max", "00", "3g", "4095m", "65g", "65537m", "1t", "8 g", " 8g", "8g ", "8gb", "8.5g",
		"08g", "+8g", "1e10", "9999999999999g", "8gi",
	} {
		_, err := MemoryMiB(value, []byte(meminfo64))
		if err == nil || !strings.Contains(err.Error(), MemoryEnv+"="+value+" ") {
			t.Errorf("MemoryMiB(%q): %v; want a refusal naming the value", value, err)
		}
	}
}

// One argument set for every run: the slice, the pids limit and both memory limits, the soft
// one 2 GiB below the hard one.
func TestRun(t *testing.T) {
	l := Limits{Pids: "16384", MemoryMiB: 49152}
	want := []string{
		"run", "--rm", "--cgroup-parent", "agents.slice", "--pids-limit", "16384",
		"--memory", "49152m", "--memory-reservation", "47104m", "--entrypoint", "claude",
	}
	if got := l.Run("--entrypoint", "claude"); !slices.Equal(got, want) {
		t.Fatalf("Run = %q, want %q", got, want)
	}
}

func TestSettle(t *testing.T) {
	config := t.TempDir()
	unit := filepath.Join(config, "systemd", "user", Slice)
	env := func(kv map[string]string) func(string) string {
		return func(name string) string { return kv[name] }
	}
	base := map[string]string{"HOME": "/nonexistent", "XDG_CONFIG_HOME": config}
	if _, err := Settle(env(base), []byte(meminfo64)); err == nil || !strings.Contains(err.Error(), Play) || !strings.Contains(err.Error(), unit) {
		t.Fatalf("no slice unit: %v; want a refusal naming %s and %s", err, unit, Play)
	}
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unit, []byte("[Slice]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Settle(env(base), []byte(meminfo64))
	if err != nil || got != (Limits{Pids: "16384", MemoryMiB: 49152}) {
		t.Fatalf("Settle = %+v, %v", got, err)
	}
	over := map[string]string{"XDG_CONFIG_HOME": config, PidsEnv: "8192", MemoryEnv: "8g"}
	if got, err = Settle(env(over), []byte(meminfo64)); err != nil || got != (Limits{Pids: "8192", MemoryMiB: 8192}) {
		t.Fatalf("Settle with overrides = %+v, %v", got, err)
	}
	home := map[string]string{"HOME": filepath.Dir(config), "XDG_CONFIG_HOME": ""}
	if err = os.Rename(config, filepath.Join(filepath.Dir(config), ".config")); err != nil {
		t.Fatal(err)
	}
	if _, err = Settle(env(home), []byte(meminfo64)); err != nil {
		t.Fatalf("Settle under HOME/.config: %v", err)
	}
	if _, err = Settle(env(map[string]string{"XDG_CONFIG_HOME": config, PidsEnv: "max"}), []byte(meminfo64)); err == nil || !strings.Contains(err.Error(), PidsEnv+"=max") {
		t.Fatalf("a refused pids limit: %v", err)
	}
	var refused *Refused
	if !errors.As(err, &refused) {
		t.Fatalf("a refusal is not a *Refused: %T", err)
	}
}

// The bounds, the floor, the reserve and the slice are lib/container-limits.bash's: a value the
// library refused and Go accepted would start a container the bash would not.
func TestTheLibrarysValues(t *testing.T) {
	text := checkoutFile(t, "files/var/local/claude-yolo/lib/container-limits.bash")
	for name, want := range map[string]string{
		"CCY_PIDS_LIMIT_DEFAULT": strconv.Itoa(PidsDefault), "CCY_PIDS_LIMIT_MIN": strconv.Itoa(PidsMin),
		"CCY_PIDS_LIMIT_MAX": strconv.Itoa(PidsMax), "CCY_MEMORY_DESKTOP_MIB": strconv.Itoa(DesktopMiB),
		"CCY_MEMORY_FLOOR_MIB": strconv.Itoa(FloorMiB), "CCY_MEMORY_SOFT_GAP_MIB": strconv.Itoa(SoftGapMiB),
		"CCY_AGENTS_SLICE": Slice,
	} {
		m := regexp.MustCompile(`(?m)^` + name + `=(\S+)$`).FindStringSubmatch(text)
		if m == nil || m[1] != want {
			t.Errorf("the library's %s is %v; Go has %s", name, m, want)
		}
	}
	for _, form := range []string{pidsForm.String(), memoryForm.String()} {
		if !strings.Contains(text, `=~ `+form+` ]]`) {
			t.Errorf("the library does not hold the form %s", form)
		}
	}
}

// checkoutFile is a file of this checkout, read from the module's parent.
func checkoutFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
