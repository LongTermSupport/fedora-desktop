package ctl

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	roomsName  = "rooms.json"
	rosterName = "roster.json"
)

// swapWorld is the world after team apply: the three files a restore swaps, with their old
// content.
func swapWorld(t *testing.T) (*world, admin.RestoreFiles) {
	t.Helper()
	w := newWorld(t)
	w.write(tokenName, "old-token")
	w.write(roomsName, "old-rooms")
	w.write(rosterName, "old-roster")
	return w, w.mustOpen("").Restore
}

func (w *world) wantFiles(want map[string]string) {
	w.t.Helper()
	names := make([]string, 0, len(want))
	for name, content := range want {
		names = append(names, name)
		if got := w.text(name); got != content {
			w.t.Errorf("%s = %q, want %q", name, got, content)
		}
		if got := w.mode(name); got != 0o600 {
			w.t.Errorf("%s mode = %v, want 0600", name, got)
		}
	}
	slices.Sort(names)
	if got := w.entries(); !slices.Equal(got, names) {
		w.t.Errorf("ctl/<team>/ = %v, want %v", got, names)
	}
}

var afterSwap = map[string]string{
	tokenName: "new-token", roomsName: "new-rooms", rosterName: "new-roster",
	tokenName + previousSuffix: "old-token", roomsName + previousSuffix: "old-rooms", rosterName + previousSuffix: "old-roster",
}

func swap(t *testing.T, r admin.RestoreFiles) {
	t.Helper()
	if err := r.Swap([]byte("new-token"), []byte("new-rooms"), []byte("new-roster")); err != nil {
		t.Fatalf("Swap: %v", err)
	}
}

func TestSwapKeepsThePreviousFilesAndWritesTheNewOnes(t *testing.T) {
	w, r := swapWorld(t)
	swap(t, r)
	w.wantFiles(afterSwap)
	swap(t, r)
	w.wantFiles(afterSwap)
}

func TestSwapAfterACrashBeforeAnyWriteKeepsTheFirstPrevious(t *testing.T) {
	w, r := swapWorld(t)
	if err := os.Link(w.path(tokenName), w.path(tokenName+previousSuffix)); err != nil {
		t.Fatal(err)
	}
	swap(t, r)
	w.wantFiles(afterSwap)
}

func TestSwapRefusesAMissingFileBeforeChangingAny(t *testing.T) {
	w, r := swapWorld(t)
	if err := os.Remove(w.path(rosterName)); err != nil {
		t.Fatal(err)
	}
	if err := r.Swap([]byte("t"), []byte("r"), []byte("s")); err == nil {
		t.Fatal("a swap with no roster.json to keep was done")
	}
	w.wantFiles(map[string]string{tokenName: "old-token", roomsName: "old-rooms"})
}

func TestSwapSweepsAStaleTemporary(t *testing.T) {
	w, r := swapWorld(t)
	w.write("."+roomsName+".0011.tmp", "partial")
	swap(t, r)
	w.wantFiles(afterSwap)
}

func TestRevertPutsThePreviousFilesBack(t *testing.T) {
	w, r := swapWorld(t)
	swap(t, r)
	got, err := r.Revert()
	if err != nil || !slices.Equal(got, []string{tokenName, roomsName, rosterName}) {
		t.Fatalf("Revert = %v, %v", got, err)
	}
	old := map[string]string{tokenName: "old-token", roomsName: "old-rooms", rosterName: "old-roster"}
	w.wantFiles(old)
	if got, err = r.Revert(); err != nil || len(got) != 0 {
		t.Fatalf("Revert again = %v, %v", got, err)
	}
	w.wantFiles(old)
}

func TestFinishRemovesThePreviousFiles(t *testing.T) {
	w, r := swapWorld(t)
	swap(t, r)
	got, err := r.Finish()
	if err != nil || !slices.Equal(got, []string{tokenName, roomsName, rosterName}) {
		t.Fatalf("Finish = %v, %v", got, err)
	}
	fresh := map[string]string{tokenName: "new-token", roomsName: "new-rooms", rosterName: "new-roster"}
	w.wantFiles(fresh)
	if got, err = r.Finish(); err != nil || len(got) != 0 {
		t.Fatalf("Finish again = %v, %v", got, err)
	}
}

func TestWriteLostWritesTheExportAtomically(t *testing.T) {
	w, r := swapWorld(t)
	lost := lostName(w.team, 7)
	if lost != "restore-7-lost.jsonl" || w.dir+"/"+lost != w.cfg.Root+paths.RestoreLostFile(w.team, 7) {
		t.Fatalf("lost name = %s", lost)
	}
	for _, content := range []string{"{\"a\":1}\n", "{\"b\":2}\n"} {
		if err := r.WriteLost(7, []byte(content)); err != nil {
			t.Fatalf("WriteLost: %v", err)
		}
		w.wantFiles(map[string]string{tokenName: "old-token", roomsName: "old-rooms", rosterName: "old-roster", lost: content})
	}
}

func TestTheRestoreFilesKeepTheDirectoryRule(t *testing.T) {
	w, r := swapWorld(t)
	w.mkdir(w.dir, 0o750)
	_, ferr := r.Finish()
	_, rerr := r.Revert()
	for name, err := range map[string]error{
		"swap": r.Swap(nil, nil, nil), "finish": ferr, "revert": rerr, "lost": r.WriteLost(1, nil),
	} {
		if err == nil {
			t.Errorf("%s ran in a directory of mode 0750", name)
		}
	}
}

func TestRestoreFilesFailOnWhatTheyCannotRemoveOrReplace(t *testing.T) {
	w, r := swapWorld(t)
	swap(t, r)
	if err := os.Remove(w.path(rosterName + previousSuffix)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(w.path(rosterName+previousSuffix), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Finish(); err == nil {
		t.Error("Finish removed a directory named roster.json.previous")
	}
	if err := r.WriteLost(1, []byte("x")); err != nil {
		t.Fatalf("WriteLost: %v", err)
	}
	if err := os.Mkdir(w.path(lostName(w.team, 2)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.path(lostName(w.team, 2)+"/x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteLost(2, []byte("x")); err == nil {
		t.Error("WriteLost replaced a directory that is not empty")
	}
}

func TestTheDefaultSleepEndsWithItsContext(t *testing.T) {
	if err := sleep(t.Context(), 0); err != nil {
		t.Errorf("sleep(0) = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled sleep = %v", err)
	}
}

// silentPort is a loopback port nothing listens on: tcpmux, which no test host serves.
const silentPort = 1

// clock is a clock that Sleep moves on.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func (c *clock) sleep(ctx context.Context, d time.Duration) error {
	c.at = c.at.Add(d)
	return ctx.Err()
}

func TestStagedIsAnAdminClientOfTheStagingServerWithTheGivenToken(t *testing.T) {
	w := newWorld(t)
	w.write(tokenName, "live-token")
	token, err := w.hs.MintToken(w.hs.AddUser("admin", "", true))
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.mustOpen("").Staged(t.Context(), w.port, token)
	if err != nil {
		t.Fatalf("Staged: %v", err)
	}
	id, err := a.Whoami(t.Context())
	if err != nil || id.UserID != w.adminID() {
		t.Fatalf("whoami = %+v, %v", id, err)
	}
}

func TestStagedWaitsUpToAMinuteForVersions(t *testing.T) {
	w := newWorld(t)
	w.write(tokenName, "live-token")
	c := &clock{at: fixedNow()}
	w.cfg.Now, w.cfg.Sleep = c.now, c.sleep
	o := w.mustOpen("")
	_, err := o.Staged(t.Context(), silentPort, "staged-token")
	if !errors.Is(err, matrix.ErrUnreachable) {
		t.Fatalf("Staged of a server that never answers = %v", err)
	}
	if waited := c.at.Sub(fixedNow()); waited < admin.StagedWait || waited > admin.StagedWait+stagedPoll {
		t.Errorf("waited %v", waited)
	}
	if _, err := o.Staged(t.Context(), w.port, "bad token"); err == nil {
		t.Error("a token with a space was taken")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := o.Staged(ctx, w.port, "staged-token"); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled wait = %v", err)
	}
}
