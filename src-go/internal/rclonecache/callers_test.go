package rclonecache

import (
	"os"
	"strings"
	"testing"
)

// rclone-tail is Go and runs this report in process (rclonetail's TestSnapshotNamesTheMount).
const rcloneCacheWarm = "../../../files/home/.local/bin/rclone-cache-warm"

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

// rclone-cache-warm runs under set -e (SPEC-rclone-cache-status section 4a) and names the
// mount, so its snapshot is the mount's summary line.
func TestRcloneCacheWarmNamesTheMount(t *testing.T) {
	data, err := os.ReadFile(rcloneCacheWarm)
	for _, line := range calls(t, rcloneCacheWarm, data, err) {
		if !strings.HasSuffix(line, ` "$MOUNT_NAME"`) {
			t.Errorf("rclone-cache-warm runs %q, not by mount name", line)
		}
	}
}
