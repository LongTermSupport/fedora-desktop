package ftpcamera

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// E11: data, not shell; comments and blanks skipped; one pair of quotes removed.
func TestReadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "# Auto-generated\n\nREPO_PATH=/home/u/repo\nRCLONE_REMOTE='lib:/PHOTO'\nHOTSPOT_CONNECTION=\"My Hotspot\"\nEMPTY=\nCMD=$(rm -rf /)\n")
	got, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"REPO_PATH": "/home/u/repo", "RCLONE_REMOTE": "lib:/PHOTO", "HOTSPOT_CONNECTION": "My Hotspot",
		"EMPTY": "", "CMD": "$(rm -rf /)",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestReadConfigRefusesALineThatIsNotAPair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "A=1\nexport B\n")
	_, err := readConfig(path)
	if err == nil || err.Error() != path+":2: not a KEY=VALUE line" {
		t.Fatalf("got %v", err)
	}
	writeFile(t, path, "1A=x\n")
	if _, err := readConfig(path); err == nil {
		t.Fatal("a key starting with a digit was accepted")
	}
}

func TestReadConfigMissingIsNotExist(t *testing.T) {
	_, err := readConfig(filepath.Join(t.TempDir(), "none"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}

// E6: defaults, overrides, refusals.
func TestReadTunables(t *testing.T) {
	env := map[string]string{"LINK_SLOW_KBS": "250.5", "LANDING_WARN_GB": "", "LINK_REPEAT_THRESHOLD": "4"}
	tun, problems := readTunables(func(k string) string { return env[k] })
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if tun.slowKBS != 250.5 || tun.repeat != 4 || tun.rateMin != 1000000 || tun.warnGB != 50 || tun.warnAgeDays != 7 {
		t.Errorf("got %+v", tun)
	}
	if tun.slowText != "250.5" {
		t.Errorf("the limit is printed as given: %q", tun.slowText)
	}
	env = map[string]string{"LINK_SLOW_KBS": "fast", "LANDING_WARN_AGE_DAYS": "-1", "LINK_RATE_MIN_BYTES": "1e6"}
	_, problems = readTunables(func(k string) string { return env[k] })
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"ERROR: LINK_SLOW_KBS must be a non-negative number, got 'fast'",
		"ERROR: LANDING_WARN_AGE_DAYS must be a non-negative whole number, got '-1'",
		"ERROR: LINK_RATE_MIN_BYTES must be a non-negative whole number, got '1e6'",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
	env = map[string]string{"LINK_SLOW_KBS": "NaN"}
	if _, problems = readTunables(func(k string) string { return env[k] }); len(problems) != 1 {
		t.Errorf("NaN accepted: %v", problems)
	}
}
