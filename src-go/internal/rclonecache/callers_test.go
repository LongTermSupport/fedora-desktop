package rclonecache

import (
	"os"
	"strings"
	"testing"
)

const (
	rcloneTail      = "../../../files/home/.local/bin/rclone-tail"
	rcloneCacheWarm = "../../../files/home/.local/bin/rclone-cache-warm"
)

// calls is every line of a caller script that runs rclone-cache-status.
func calls(t *testing.T, script string, data []byte, err error) []string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "rclone-cache-status ") || line == "rclone-cache-status" {
			found = append(found, line)
		}
	}
	if len(found) == 0 {
		t.Fatalf("%s runs rclone-cache-status nowhere: the caller test reads nothing", script)
	}
	return found
}

// The callers run under set -e (SPEC-rclone-cache-status section 4a): rclone-tail snapshots the
// mount summary only, so a directory walk's exit 1 (E19) can never end it.
func TestRcloneTailNeverWalksADirectory(t *testing.T) {
	data, err := os.ReadFile(rcloneTail)
	for _, line := range calls(t, rcloneTail, data, err) {
		if !strings.Contains(line, " --no-dir") {
			t.Errorf("rclone-tail runs %q without --no-dir", line)
		}
	}
}

// rclone-cache-warm names the mount, so its snapshot is the mount's summary line.
func TestRcloneCacheWarmNamesTheMount(t *testing.T) {
	data, err := os.ReadFile(rcloneCacheWarm)
	for _, line := range calls(t, rcloneCacheWarm, data, err) {
		if !strings.HasSuffix(line, ` "$MOUNT_NAME"`) {
			t.Errorf("rclone-cache-warm runs %q, not by mount name", line)
		}
	}
}
