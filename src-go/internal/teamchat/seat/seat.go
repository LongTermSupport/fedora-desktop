// Package seat is the owner's seat store of the v2 agent-team bus (Plan 00209 SPEC-n2c.md,
// C 3.7): the pool seat records and the bundle, the allocation lock, the credential seam, take,
// release and placement. Every path below the store root is opened by directory fd, through no
// link (SW1-SW3); every name comes from teamchat/paths.
package seat

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ErrBusy is a lock still held when its bounded wait ends (SL1, SX1).
var ErrBusy = errors.New("seat: busy")

// Clock is the store's time: now for its records, and the waits of its bounded retries.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// Config is what a store is opened from; every part is required.
type Config struct {
	// StateHome is $XDG_STATE_HOME, under which the seat store lives.
	StateHome string
	// RuntimeDir is $XDG_RUNTIME_DIR, under which the allocation lock lives.
	RuntimeDir string
	// Checkout is the checkout whose pool seats this store allocates.
	Checkout paths.CheckoutKey
	Clock    Clock
}

// Seat is one pool seat: its team and its handle (n2c-D1).
type Seat struct {
	Team paths.TeamID
	Name paths.SeatName
}

// Store is one owner's seat store for one checkout, its root held open by fd.
type Store struct {
	store     paths.Store
	checkout  paths.CheckoutKey
	root      *dir
	allocLock string
	clock     Clock
}

// Open opens the seat store, making its root 0700 when missing. A config that lacks a part, or
// a root that is not the owner's 0700 directory, is ErrConfig.
func Open(cfg Config) (*Store, error) {
	if cfg.Clock == nil || cfg.Checkout.String() == "" {
		return nil, fmt.Errorf("%w: a clock and a checkout are required", ErrConfig)
	}
	if !filepath.IsAbs(cfg.StateHome) || !filepath.IsAbs(cfg.RuntimeDir) {
		return nil, fmt.Errorf("%w: the state home and the runtime directory must be absolute", ErrConfig)
	}
	store, err := paths.SeatStore(cfg.StateHome)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	allocLock, err := paths.AllocLock(cfg.RuntimeDir, cfg.Checkout)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	root, err := openDir(store.Root(), true)
	if err != nil {
		return nil, err
	}
	return &Store{store: store, checkout: cfg.Checkout, root: root, allocLock: allocLock, clock: cfg.Clock}, nil
}

// Close releases the store root's fd.
func (s *Store) Close() error { return s.root.close() }
