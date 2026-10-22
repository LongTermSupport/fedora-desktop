package seat

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// holdLock has a second process (flock(1) running cat) hold the lock at path, mode "-x"
// (exclusive) or "-s" (shared), until the returned release (also run at cleanup). flock is
// only proven across processes, so the holder is never this test's own descriptor.
func holdLock(t *testing.T, path, mode string) func() {
	t.Helper()
	stdinR, stdinW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := execx.OS{}.Run(context.Background(), execx.Cmd{
			Name: "flock", Args: []string{mode, path, "cat"}, Stdin: stdinR, Stdout: outW,
		})
		done <- errors.Join(err, outW.Close())
	}()
	if _, err := io.WriteString(stdinW, "locked\n"); err != nil {
		t.Fatal(err)
	}
	// cat echoes the line only once flock has the lock and has started it.
	line, err := bufio.NewReader(outR).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("holder: %q, %v", line, err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			if err := stdinW.Close(); err != nil {
				t.Error(err)
			}
			if err := <-done; err != nil {
				t.Errorf("holder: %v", err)
			}
		})
	}
	t.Cleanup(release)
	return release
}

func lockIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(realTemp(t), lockFile)
}

func TestProbeLockMissingIsFree(t *testing.T) {
	held, err := probeLock(lockIn(t))
	if err != nil || held {
		t.Fatalf("held %v, %v", held, err)
	}
}

func TestProbeLockUnheldIsFree(t *testing.T) {
	path := lockIn(t)
	fx{t}.writeFile(path, "seat 1")
	held, err := probeLock(path)
	if err != nil || held {
		t.Fatalf("held %v, %v", held, err)
	}
}

func TestProbeLockHeldByAnotherProcess(t *testing.T) {
	path := lockIn(t)
	fx{t}.writeFile(path, "seat 1")
	release := holdLock(t, path, "-x")
	held, err := probeLock(path)
	if err != nil || !held {
		t.Fatalf("held %v, %v", held, err)
	}
	release()
	if held, err = probeLock(path); err != nil || held {
		t.Fatalf("after release: held %v, %v", held, err)
	}
}

// v1 probes with a shared lock, so a shared holder is not seen.
func TestProbeLockSharedHolderIsFree(t *testing.T) {
	path := lockIn(t)
	fx{t}.writeFile(path, "")
	holdLock(t, path, "-s")
	held, err := probeLock(path)
	if err != nil || held {
		t.Fatalf("held %v, %v", held, err)
	}
}

// The probe lets go of its shared lock: another process can take the lock at once after it.
func TestProbeLockReleasesItsLock(t *testing.T) {
	path := lockIn(t)
	fx{t}.writeFile(path, "")
	if _, err := probeLock(path); err != nil {
		t.Fatal(err)
	}
	if _, err := (execx.OS{}).Run(context.Background(), execx.Cmd{Name: "flock", Args: []string{"-n", "-x", path, "true"}}); err != nil {
		t.Fatalf("lock still taken after the probe: %v", err)
	}
}

func TestProbeLockRefusals(t *testing.T) {
	for _, c := range []struct {
		about string
		make  func(f fx, path string)
		want  string
	}{
		{"symlink", func(f fx, path string) {
			f.writeFile(path+".real", "")
			f.symlink(filepath.Base(path)+".real", path)
		}, " is a symlink"},
		{"dangling symlink", func(f fx, path string) { f.symlink("nowhere", path) }, " is a symlink"},
		{"directory", fx.mkdirs, " is not a regular file"},
		{"fifo", fx.mkfifo, " is not a regular file"},
	} {
		t.Run(c.about, func(t *testing.T) {
			path := lockIn(t)
			c.make(fx{t}, path)
			_, err := probeLock(path)
			refusal, ok := errors.AsType[*RefusalError](err)
			if !ok || refusal.Msg != path+c.want {
				t.Fatalf("got %v, want %q", err, path+c.want)
			}
		})
	}
}

// v1's probe does not catch an open error other than ELOOP and ENOENT: a crash.
func TestProbeLockSocketIsACrash(t *testing.T) {
	path := lockIn(t)
	fx{t}.mksock(path)
	_, err := probeLock(path)
	if err == nil || isRefusal(err) || !strings.Contains(err.Error(), "no such device or address") {
		t.Fatalf("got %v", err)
	}
}

func TestProbeLockUnreadableIsACrash(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a mode-0000 file")
	}
	path := lockIn(t)
	fx{t}.writeFile(path, "")
	fx{t}.check(os.Chmod(path, 0))
	if _, err := probeLock(path); err == nil {
		t.Fatal("no error")
	}
}
