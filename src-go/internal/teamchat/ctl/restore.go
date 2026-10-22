package ctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// previousSuffix names the copy a swap keeps of each file it replaces (RC2).
const previousSuffix = ".previous"

// stagedPoll is the wait between two versions probes of the staging server (RC1).
const stagedPoll = time.Second

// lostName is the export's name in ctl/<team>/ (RC3).
func lostName(team paths.TeamID, n int64) string {
	return filepath.Base(paths.RestoreLostFile(team, n))
}

// restoreFiles is admin.RestoreFiles over ctl/<team>/ at path: every op opens the directory
// again by openDir's rule and works relative to it.
type restoreFiles struct {
	path string
	team paths.TeamID
	rand io.Reader
}

var _ admin.RestoreFiles = restoreFiles{}

// Swap implements admin.RestoreFiles. Every file must be there before anything is changed.
func (r restoreFiles) Swap(token, rooms, roster []byte) error {
	names := r.swapped()
	return r.in(names, func(d *dir) error {
		for _, name := range names {
			if err := d.lstat(name); err != nil {
				return fmt.Errorf("%s cannot be kept as %s: %w", d.at(name), name+previousSuffix, err)
			}
		}
		for i, data := range [][]byte{token, rooms, roster} {
			if err := d.keep(names[i]); err != nil {
				return err
			}
			if err := d.replace(names[i], data, r.rand); err != nil {
				return err
			}
		}
		return nil
	})
}

// Finish implements admin.RestoreFiles.
func (r restoreFiles) Finish() ([]string, error) {
	return r.previous(func(d *dir, name string) error {
		if err := unix.Unlinkat(d.fd, name+previousSuffix, 0); err != nil {
			return fmt.Errorf("cannot delete %s: %w", d.at(name+previousSuffix), err)
		}
		return nil
	})
}

// Revert implements admin.RestoreFiles.
func (r restoreFiles) Revert() ([]string, error) {
	return r.previous(func(d *dir, name string) error {
		if err := unix.Renameat(d.fd, name+previousSuffix, d.fd, name); err != nil {
			return fmt.Errorf("cannot put %s back: %w", d.at(name), err)
		}
		return nil
	})
}

// WriteLost implements admin.RestoreFiles.
func (r restoreFiles) WriteLost(n int64, data []byte) error {
	name := lostName(r.team, n)
	return r.in([]string{name}, func(d *dir) error { return d.replace(name, data, r.rand) })
}

// swapped are the files a swap replaces, in order.
func (r restoreFiles) swapped() []string {
	return []string{tokenName, filepath.Base(paths.RoomsFile(r.team)), filepath.Base(paths.RosterFile(r.team))}
}

// in runs op on the directory, opened and swept of the temporaries of names first.
func (r restoreFiles) in(names []string, op func(d *dir) error) (err error) {
	d, err := openDir(r.path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.close()) }()
	if err = d.sweep(names...); err != nil {
		return err
	}
	return op(d)
}

// previous runs op on each swapped name whose `.previous` is there, then syncs the directory,
// answering the names it ran on.
func (r restoreFiles) previous(op func(d *dir, name string) error) ([]string, error) {
	var done []string
	err := r.in(r.swapped(), func(d *dir) error {
		for _, name := range r.swapped() {
			err := d.lstat(name + previousSuffix)
			if errors.Is(err, unix.ENOENT) {
				continue
			}
			if err == nil {
				err = op(d, name)
			}
			if err != nil {
				return err
			}
			done = append(done, name)
		}
		return d.sync()
	})
	if err != nil {
		return nil, err
	}
	return done, nil
}

// lstat is nil when name is there, not following a link.
func (d *dir) lstat(name string) error {
	var st unix.Stat_t
	return unix.Fstatat(d.fd, name, &st, unix.AT_SYMLINK_NOFOLLOW)
}

// keep links name to its `.previous` when that is absent, and syncs the directory, so the old
// content survives the replace that follows.
func (d *dir) keep(name string) error {
	err := d.lstat(name + previousSuffix)
	if err == nil {
		return nil
	}
	if !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("%s: %w", d.at(name+previousSuffix), err)
	}
	if err := unix.Linkat(d.fd, name, d.fd, name+previousSuffix, 0); err != nil {
		return fmt.Errorf("cannot keep %s as %s: %w", d.at(name), name+previousSuffix, err)
	}
	return d.sync()
}

// replace writes name, 0600, from a synced temporary file renamed over it, then syncs the
// directory; unlike create it replaces an existing name.
func (d *dir) replace(name string, data []byte, rnd io.Reader) error {
	temp, err := tempName(name, rnd)
	if err != nil {
		return err
	}
	fd, err := unix.Openat(d.fd, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, fileMode)
	if err != nil {
		return fmt.Errorf("cannot create a temporary file for %s: %w", d.at(name), err)
	}
	f := os.NewFile(uintptr(fd), d.at(temp))
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err == nil {
		err = unix.Renameat(d.fd, temp, d.fd, name)
	}
	if err != nil {
		return errors.Join(fmt.Errorf("cannot put %s in place: %w", d.at(name), err), unix.Unlinkat(d.fd, temp, 0))
	}
	return d.sync()
}

// staged is Open's Staged: the admin client of the staging server on port with token, given
// once it answers versions, probed every stagedPoll for up to admin.StagedWait.
func (cfg Config) staged(ctx context.Context, port int, token string) (*matrix.Admin, error) {
	anon, err := cfg.Admin("http://" + loopback + ":" + strconv.Itoa(port))
	if err != nil {
		return nil, fmt.Errorf("the staging server's admin client: %w", err)
	}
	client, err := anon.WithToken(token)
	if err != nil {
		return nil, fmt.Errorf("the staging server's admin client: %w", err)
	}
	deadline := cfg.Now().Add(admin.StagedWait)
	for {
		_, err := client.Versions(ctx)
		if err == nil {
			return client, nil
		}
		if ctx.Err() != nil || !cfg.Now().Before(deadline) {
			return nil, fmt.Errorf("the staging server on port %d did not answer versions within %v: %w", port, admin.StagedWait, err)
		}
		if err := cfg.Sleep(ctx, stagedPoll); err != nil {
			return nil, err
		}
	}
}

// sleep waits d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
