package ftpcamera

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// DefaultConfigPath is the file play-ftp-camera.yml writes: the checkout (for --pass), the
// rclone remote and the hotspot profile.
const DefaultConfigPath = "/etc/ftp-camera/config"

var configKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// readConfig reads the config as KEY=VALUE data, never as shell (E11). A missing file is an
// fs.ErrNotExist the caller words itself.
func readConfig(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	values := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !configKey.MatchString(key) {
			return nil, fmt.Errorf("%s:%d: not a KEY=VALUE line", path, n)
		}
		values[key] = unquote(value)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return values, nil
}

// unquote removes one pair of matching surrounding quotes, as the shell that used to source
// the file did for a plain quoted value.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// tunables are the weak-link and landing-zone thresholds the environment may override (E6).
type tunables struct {
	slowKBS     float64
	slowText    string
	repeat      int64
	rateMin     int64
	warnGB      int64
	warnAgeDays int64
}

// readTunables reads the five thresholds; problems are the ERROR lines for those that are
// not numbers of their kind.
func readTunables(getenv func(string) string) (tunables, []string) {
	tun := tunables{slowText: "500"}
	var problems []string
	if v := getenv("LINK_SLOW_KBS"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
			problems = append(problems, fmt.Sprintf("ERROR: LINK_SLOW_KBS must be a non-negative number, got '%s'", v))
		}
		tun.slowText = v
	}
	if f, err := strconv.ParseFloat(tun.slowText, 64); err == nil {
		tun.slowKBS = f
	}
	for _, w := range []struct {
		name string
		dst  *int64
		def  int64
	}{
		{"LINK_REPEAT_THRESHOLD", &tun.repeat, 3},
		{"LINK_RATE_MIN_BYTES", &tun.rateMin, 1000000},
		{"LANDING_WARN_GB", &tun.warnGB, 50},
		{"LANDING_WARN_AGE_DAYS", &tun.warnAgeDays, 7},
	} {
		*w.dst = w.def
		v := getenv(w.name)
		if v == "" {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			problems = append(problems, fmt.Sprintf("ERROR: %s must be a non-negative whole number, got '%s'", w.name, v))
			continue
		}
		*w.dst = n
	}
	return tun, problems
}
