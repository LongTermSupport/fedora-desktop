// Package seat reads a checkout's seats as v1's `agent-bus seat list` reads them
// (CLAUDE/Plan/00209-agent-bus-v2-in-go/SPEC-view.md, V30-V35): the checkout top and its
// .claude/ccy, each a real directory owned by the user; the tree
// .claude/ccy/pingbus/seats/<team>/<seat>/ by team then seat in name order; each seat's
// member.json (member.go, config.read_member's rules) and the flock probe of its seat.lock
// (lock.go). It starts no process; a working directory's top level is asked of git through
// the TopLeveler the caller passes.
//
// Three outcomes, as v1 had them: a Listing whose Failures name each seat left out (v1's
// exit 78 with the other seats printed); a *RefusalError of the whole list (v1's exit 78 and no
// seats); any other error, where v1 raised an exception it did not catch and crashed
// (SPEC-view B-S3), such as an unreadable directory or a base_url urlsplit refuses.
package seat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// Seat is one readable seat: held while another process holds its seat.lock.
type Seat struct {
	Team   string
	Seat   string
	Held   bool
	Handle string
}

// Listing is a checkout's readable seats in v1's order, and a line for each seat left out:
// "seat <seat>@<team>: <reason>", v1's words.
type Listing struct {
	Seats    []Seat
	Failures []string
}

// RefusalError is v1's refusal in v1's words (its exit 78 class: CheckoutError, ConfigError
// or StateError): of the whole list from List and Checkout, of one seat inside them.
type RefusalError struct{ Msg string }

func (r *RefusalError) Error() string { return r.Msg }

func refusalf(format string, args ...any) *RefusalError {
	return &RefusalError{Msg: fmt.Sprintf(format, args...)}
}

// TopLeveler is git's answer for the top level of the work tree at dir ("" the working
// directory); gitx.Git is one.
type TopLeveler interface {
	TopLevel(ctx context.Context, dir string) (string, error)
}

// Checkout is the top a seat list reads (V30). A named dir is read at that folder with its
// symbolic links resolved, as git reports a top level, without asking git (B-S2); one that
// cannot be resolved is an error, as v1's process could not start there. "" is the working
// directory's git top level, refused as v1 refuses a folder outside a checkout.
func Checkout(ctx context.Context, git TopLeveler, dir string) (string, error) {
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("checkout %s: %w", dir, err)
		}
		top, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", fmt.Errorf("checkout %s: %w", dir, err)
		}
		return top, nil
	}
	top, err := git.TopLevel(ctx, "")
	if err == nil {
		return top, nil
	}
	if execx.IsNotInstalled(err) {
		return "", refusalf("git is not installed: a checkout cannot be found")
	}
	exit, ok := errors.AsType[*execx.ExitError](err)
	if !ok {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	// syscall.Getwd is the kernel's path, as python's os.getcwd; os.Getwd may answer $PWD.
	cwd, cwdErr := syscall.Getwd()
	if cwdErr != nil {
		return "", fmt.Errorf("the working directory: %w", cwdErr)
	}
	return "", refusalf("%s is not in a git checkout: run agent-bus seat in the ccy checkout (%s)",
		pathText(cwd), pycompat.Strip(exit.Stderr))
}

const (
	ccyDir   = ".claude/ccy"
	seatsDir = ".claude/ccy/pingbus/seats"
)

// List is the seats of the checkout at top, read as the user uid (checkout.seat_list).
func List(top string, uid int) (Listing, error) {
	if _, err := ownedDir(top, uid, "the checkout"); err != nil {
		return Listing{}, err
	}
	present, err := ownedDir(filepath.Join(top, ccyDir), uid, "ccy's directory")
	if err != nil {
		return Listing{}, err
	}
	if !present {
		return Listing{}, refusalf("%s has no %s/: launch ccy in it once, then again with --teams", pathText(top), ccyDir)
	}
	root := filepath.Join(top, seatsDir)
	refs, err := listSeats(root)
	if err != nil {
		return Listing{}, err
	}
	listing := Listing{Seats: []Seat{}, Failures: []string{}}
	for _, ref := range refs {
		seat, err := readSeat(filepath.Join(root, ref.team, ref.seat), ref)
		if refusal, refused := errors.AsType[*RefusalError](err); refused {
			listing.Failures = append(listing.Failures, "seat "+ref.seat+"@"+ref.team+": "+refusal.Msg)
			continue
		}
		if err != nil {
			return Listing{}, err
		}
		listing.Seats = append(listing.Seats, seat)
	}
	return listing, nil
}

func readSeat(dir string, ref seatRef) (Seat, error) {
	handle, err := readMember(ref.team, dir)
	if err != nil {
		return Seat{}, err
	}
	held, err := probeLock(filepath.Join(dir, lockFile))
	if err != nil {
		return Seat{}, err
	}
	return Seat{Team: ref.team, Seat: ref.seat, Held: held, Handle: handle}, nil
}

// ownedDir is checkout._real_dir: false when path is missing; refused unless a real
// directory owned by uid. A failure other than ENOENT is a crash, as in v1.
func ownedDir(path string, uid int, what string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lstat: %w", err)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return false, refusalf("%s is a symlink; %s must be a real directory", pathText(path), what)
	case !info.IsDir():
		return false, refusalf("%s is not a directory; %s must be one", pathText(path), what)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("lstat %s: no owner", pathText(path))
	}
	if int64(st.Uid) != int64(uid) {
		return false, refusalf("%s is owned by uid %d, not %d; %s must be yours", pathText(path), st.Uid, uid, what)
	}
	return true, nil
}

type seatRef struct{ team, seat string }

var (
	teamNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
	seatNameRE = regexp.MustCompile(`^(?:[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11})$`)
)

// listSeats is seat.list_seats: every seats/<team>/<seat>/ by team then seat in name order; no
// root is no seats; anything else in the tree refuses the whole list.
func listSeats(root string) ([]seatRef, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lstat: %w", err)
	}
	if !info.IsDir() {
		return nil, notADirectory(root)
	}
	teams, err := names(root)
	if err != nil {
		return nil, err
	}
	var refs []seatRef
	for _, team := range teams {
		dir := filepath.Join(root, team)
		if !teamNameRE.MatchString(team) {
			return nil, refusalf("%s is not a team's seats (not a team name)", pathText(dir))
		}
		seats, err := seatNames(dir)
		if err != nil {
			return nil, err
		}
		for _, seat := range seats {
			refs = append(refs, seatRef{team, seat})
		}
	}
	return refs, nil
}

func seatNames(teamDir string) ([]string, error) {
	if err := realDir(teamDir); err != nil {
		return nil, err
	}
	seats, err := names(teamDir)
	if err != nil {
		return nil, err
	}
	for _, seat := range seats {
		dir := filepath.Join(teamDir, seat)
		if !seatNameRE.MatchString(seat) {
			return nil, refusalf("%s is not a seat (not a seat name)", pathText(dir))
		}
		if err := realDir(dir); err != nil {
			return nil, err
		}
	}
	return seats, nil
}

// realDir is seat._real_dir: a directory, not a symlink; an lstat failure is a crash.
func realDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("lstat: %w", err)
	}
	if !info.IsDir() {
		return notADirectory(path)
	}
	return nil
}

func notADirectory(path string) *RefusalError {
	return refusalf("%s is not a directory (a symlink is refused)", pathText(path))
}

// names is a directory's entries in python's sorted() order; an unreadable directory is a
// crash, as in v1.
func names(dir string) ([]string, error) {
	f, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	entries, err := f.Readdirnames(-1)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", pathText(dir), err)
	}
	sortNames(entries)
	return entries, nil
}
