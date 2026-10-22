package pinclock

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	epoch = 1894708800 // 2030-01-15T12:00:00Z
	shift = 1000
)

func TestNowIsTheEpoch(t *testing.T) {
	c := Clock{Epoch: epoch, Shift: shift}
	if got := c.Now().Unix(); got != epoch {
		t.Fatalf("Now = %d, want %d", got, epoch)
	}
}

// A time the machine gave a file during the run reads as the pinned now; any other is shifted
// back by Shift.
func TestPinnedShiftsMachineTimes(t *testing.T) {
	c := Clock{Epoch: epoch, Shift: shift}
	cases := []struct {
		machine, want int64
	}{
		{epoch + shift, epoch},
		{epoch + shift + RunWindow - 1, epoch},
		{epoch + shift + RunWindow, epoch + RunWindow},
		{epoch + shift - 86400, epoch - 86400},
	}
	for _, tc := range cases {
		if got := c.Pinned(tc.machine); got != tc.want {
			t.Errorf("Pinned(%d) = %d, want %d", tc.machine, got, tc.want)
		}
	}
}

type fakeInfo struct {
	os.FileInfo

	mtime time.Time
}

func (f fakeInfo) ModTime() time.Time { return f.mtime }

func TestModTimeIsPinned(t *testing.T) {
	c := Clock{Epoch: epoch, Shift: shift}
	got := c.ModTime(fakeInfo{mtime: time.Unix(epoch+shift+5, 0)})
	if got.Unix() != epoch {
		t.Fatalf("ModTime = %d, want %d", got.Unix(), epoch)
	}
}

func TestReadTakesTheFakesFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FakesFile), []byte(`{"epoch": 7, "shift": 3, "rules": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Epoch != 7 || c.Shift != 3 {
		t.Fatalf("Read = %+v, want epoch 7 shift 3", c)
	}
}

func TestReadRefusesAMissingOrBrokenFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir); err == nil {
		t.Fatal("Read of a sandbox with no fakes file succeeded")
	}
	if err := os.WriteFile(filepath.Join(dir, FakesFile), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil {
		t.Fatal("Read of a broken fakes file succeeded")
	}
	if err := os.WriteFile(filepath.Join(dir, FakesFile), []byte(`{"shift": 3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil {
		t.Fatal("Read of a fakes file with no epoch succeeded")
	}
}

func TestFromEnvNeedsTheSandbox(t *testing.T) {
	t.Setenv(SandboxEnv, "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv with no sandbox succeeded")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FakesFile), []byte(`{"epoch": 9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SandboxEnv, dir)
	c, err := FromEnv()
	if err != nil || c.Epoch != 9 {
		t.Fatalf("FromEnv = %+v, %v", c, err)
	}
}
