package ccylaunch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixedClock is a clock for the tests: now, and file times as the file system gives them.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time                     { return c.now }
func (c fixedClock) ModTime(info os.FileInfo) time.Time { return info.ModTime() }

func TestSystemClockReadsTheFileTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2030, 1, 15, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := (SystemClock{}).ModTime(info); !got.Equal(stamp) {
		t.Fatalf("ModTime = %v, want %v", got, stamp)
	}
}

func TestSystemClockNowMoves(t *testing.T) {
	if (SystemClock{}).Now().Year() < 2020 {
		t.Fatal("the system clock is before 2020")
	}
}

func TestFrontWithoutAClockUsesTheSystemOne(t *testing.T) {
	if _, ok := (Front{}).clock().(SystemClock); !ok {
		t.Fatal("a front given no clock does not read the system's")
	}
	c := fixedClock{now: time.Date(2030, 1, 15, 12, 0, 0, 0, time.UTC)}
	if got := (Front{Clock: c}).clock(); got != c {
		t.Fatalf("the front's clock is %v, want the one it was given", got)
	}
}
