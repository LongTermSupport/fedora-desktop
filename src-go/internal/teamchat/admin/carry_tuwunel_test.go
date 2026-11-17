//go:build tuwunel

package admin

// SPEC-carry item 15 (RT-style) against a real Tuwunel (through `matrix/tuwunelhs`):
// `go test -tags tuwunel` with FAKEHS_TUWUNEL_BIN naming the binary. carryRound runs over the
// real server L on alpha's database: the seal's stop of the homeserver unit stops L by SIGTERM,
// so the export copies a database at rest; the guest's loopback start is the same binary on the
// imported database, which carry-check asks for the seal's tail, and which is served again
// after the check to read the tail and the roster event back.

import (
	"context"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// realCarryServer is a carry round's homeserver as real Tuwunel processes: the source's L, and
// one on the guest's imported database at a time.
type realCarryServer struct {
	t        *testing.T
	source   *realWorld
	imported *tuwunelhs.Server
}

func (s *realCarryServer) stopLive() error { return s.source.live.Stop(syscall.SIGTERM) }

// serveImported starts Tuwunel on the guest's imported database and backup paths, and gives the
// guest the carried admin token's client of it.
func (s *realCarryServer) serveImported(ctx context.Context, guest *world) error {
	team := alpha(s.t)
	hs := tuwunelhs.StartWith(ctx, s.t, server, options(guest.root, team, guest.root+paths.HomeserverPrivateDatabase(team)))
	s.imported = &hs
	a, err := matrix.NewAdmin(matrix.Config{BaseURL: hs.Base, Token: s.source.token, Clock: tuwunelhs.Clock{}})
	if err != nil {
		return err
	}
	s.t.Cleanup(a.Close)
	guest.base, guest.admin = hs.Base, a
	return nil
}

func (s *realCarryServer) stopImported() error {
	if s.imported == nil {
		return nil
	}
	hs := s.imported
	s.imported = nil
	return hs.Stop(syscall.SIGTERM)
}

// Item 15: alpha applied on L, carried through the four verbs to an empty guest; the database
// arrives byte for byte, carry-check finds the seal's tail on it, and served again it gives each
// room's newest event and the roster event as the seal read them.
func TestTuwunelCarryRoundRT(t *testing.T) {
	// The host runs Tuwunel under UMask=0077 (host/sandbox.go), so its files are the modes SX3 wants.
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })
	w := newRealWorld(t.Context(), t)
	in := encodeDoc(t, alphaDoc())
	w.ok(t.Context(), t, in, "team", "approve", "--team", "alpha")
	w.ok(t.Context(), t, in, "team", "apply")
	w.writeToken(t)
	carryRound(t, w.world, &realCarryServer{t: t, source: w})
}
