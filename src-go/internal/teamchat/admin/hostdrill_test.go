package admin

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// discarding is the engine whose discard removes the record, as restore.Discard does.
type discarding struct {
	*fakeRestorer

	root string
}

func (d discarding) Discard(ctx context.Context, lock *teamlock.Lock, team paths.TeamID) ([]byte, error) {
	out, err := d.fakeRestorer.Discard(ctx, lock, team)
	if err != nil {
		return out, err
	}
	return out, os.Remove(d.root + paths.RestoreRecord(team))
}

// drillHost is a host holding one marked scratch team, its restore recorded.
func drillHost(t *testing.T) (*restoreWorld, HostDrill, paths.TeamID) {
	t.Helper()
	bw := bareWorld(t)
	w := &restoreWorld{t: t, root: bw.root, sys: bw.sys()}
	w.engine = &fakeRestorer{t: t, sys: w.sys}
	id := scratchTeam(t)
	if err := os.MkdirAll(w.root+paths.AdminStateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := drill.WriteMarker(w.root, id, drill.Marker{Digest: mustDigest(t), StartedMS: t0ms}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.root+paths.RestoreRecord(id), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := HostDrill{Sys: w.sys, Engine: func(context.Context) (Restorer, error) {
		w.engines++
		return discarding{w.engine, w.root}, nil
	}}
	return w, h, id
}

func mustDigest(t *testing.T) paths.Digest {
	t.Helper()
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// --clean discards a scratch team's recorded restore through the engine, under the team lock,
// without the proof a restore needs, then removes the team with purge.
func TestHostDrillCleanDiscardsThenPurges(t *testing.T) {
	w, h, id := drillHost(t)
	cleaned, err := h.Clean(t.Context())
	if err != nil || !slices.Equal(cleaned, []paths.TeamID{id}) {
		t.Fatalf("Clean = %v, %v", cleaned, err)
	}
	if !slices.Equal(w.engine.calls, []string{"discard " + id.String()}) {
		t.Errorf("engine calls = %q", w.engine.calls)
	}
	if _, err := os.Stat(w.root + paths.AdminState(id)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the scratch team's state is still there: %v", err)
	}
}

func markerKept(t *testing.T, w *restoreWorld, id paths.TeamID) {
	t.Helper()
	if _, err := os.Stat(w.root + paths.DrillMarker(id)); err != nil {
		t.Errorf("the marker is gone: %v", err)
	}
}

// A discard that fails stops the clean-up, and the team stays for the next --clean.
func TestHostDrillCleanStopsAtAFailedDiscard(t *testing.T) {
	w, h, id := drillHost(t)
	w.engine.err = errors.New("the discard failed")
	cleaned, err := h.Clean(t.Context())
	if err == nil || !strings.Contains(err.Error(), "the discard failed") || len(cleaned) != 0 {
		t.Fatalf("Clean = %v, %v", cleaned, err)
	}
	markerKept(t, w, id)
}

// The machine rig the drill runs on is not built: a drill is refused (70), naming the issue,
// and touches nothing.
func TestHostDrillRunIsRefusedUntilItsRigIsBuilt(t *testing.T) {
	w, h, id := drillHost(t)
	out, err := h.Run(t.Context(), 8470)
	if !errors.Is(err, drill.ErrRefused) || !strings.Contains(err.Error(), "#324") || w.engines != 0 || out.Written {
		t.Fatalf("Run = %+v, %v, %d engines", out, err, w.engines)
	}
	markerKept(t, w, id)
}
