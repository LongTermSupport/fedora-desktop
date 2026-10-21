package ops

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// trace records, in order, each play run, each Changed and each Refresh of r.
func trace(r *opsRig) *[]string {
	events := &[]string{}
	r.ops.Changed = func() { *events = append(*events, "changed") }
	refresh := r.ops.Refresh
	r.ops.Refresh = func(ctx context.Context) (map[string]declare.Status, error) {
		*events = append(*events, "refresh")
		return refresh(ctx)
	}
	r.fake.OnFunc(strings.Fields(opsPlay), func(execx.Cmd) (execx.Result, error) {
		*events = append(*events, "play")
		return execx.Result{}, nil
	})
	return events
}

// toldBeforeTheReadBack says whether every play run was followed by a Changed before the next
// Refresh: what the open kept of the teams (SPEC-view.md V41) is not given to the check of what
// the play did.
func toldBeforeTheReadBack(events []string) bool {
	for i, e := range events {
		if e != "play" {
			continue
		}
		next := slices.IndexFunc(events[i+1:], func(e string) bool { return e == "changed" || e == "refresh" })
		if next < 0 || events[i+1+next] != "changed" {
			return false
		}
	}
	return slices.Contains(events, "play")
}

func TestAnApplysPlayIsSaidToHaveChangedTheTeamsBeforeTheyAreReadBack(t *testing.T) {
	r := newOpsRig(t)
	events := trace(r)
	r.statuses = []map[string]declare.Status{{"beta": st("outdated", "config")}, {"beta": st("current", "")}}
	if code := r.ops.Apply(context.Background(), []string{"beta"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !toldBeforeTheReadBack(*events) {
		t.Fatalf("events %q", *events)
	}
}

func TestARemovesPlayIsSaidToHaveChangedTheTeamsBeforeTheyAreReadBack(t *testing.T) {
	r := removeRig(t)
	events := trace(r)
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !toldBeforeTheReadBack(*events) {
		t.Fatalf("events %q", *events)
	}
}
