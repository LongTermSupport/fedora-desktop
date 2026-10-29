package containerimages

import (
	"slices"
	"strings"
	"testing"
)

func TestParseDecisionsReadsTheRangeAndOneActionPerItem(t *testing.T) {
	d, err := parseDecisions("# a comment\n\nold-range 100000 65536\nremove\timage\tI1\nreown volume v\nleave path overlay/x\n")
	if err != nil {
		t.Fatal(err)
	}
	if d.old != oldRange || d.actions[itemKey{kindImage, "I1"}] != actRemove || d.actions[itemKey{kindVolume, "v"}] != actReown ||
		d.actions[itemKey{kindPath, "overlay/x"}] != actLeave || len(d.actions) != 3 {
		t.Fatalf("parseDecisions = %+v", d)
	}
}

func TestParseDecisionsRefusesWhatItCannotRead(t *testing.T) {
	for _, text := range []string{
		"remove image I1\n", // no old-range line
		"old-range 100000 65536\nold-range 100000 65536\n",
		"old-range x 65536\n",
		"old-range 100000 65536\nremove image I1\nreown image I1\n",
		"old-range 100000 65536\nzap image I1\n",
		"old-range 100000 65536\nremove gizmo I1\n",
		"old-range 100000 65536\nremove image\n",
	} {
		if d, err := parseDecisions(text); err == nil {
			t.Errorf("parseDecisions(%q) = %+v", text, d)
		}
	}
}

// The proposal is a decisions file a host agent can edit: the range, then each item's proposed
// action (ask for ASK, leave for HELD) under a comment saying what it is and why.
func TestProposedDecisionsRoundTrip(t *testing.T) {
	w := newStore().layers("L1").image("I1", "L1", "docker.io/x:1").volume("v").old("overlay/L1", 100099, true).old("volumes/v", 100099, true)
	w.container("C", "live", "I1", "CL", "running")
	items := w.items()
	text := proposedDecisions(items, oldRange)
	if !strings.Contains(text, "# HELD image") || !strings.Contains(text, "docker.io/x:1") || !strings.Contains(text, "leave\timage\tI1") ||
		!strings.Contains(text, "ask\tvolume\tv") {
		t.Fatalf("proposal:\n%s", text)
	}
	d, err := parseDecisions(text)
	if err != nil || d.old != oldRange || len(d.actions) != len(items) {
		t.Fatalf("the proposal does not read back: %+v, %v\n%s", d, err, text)
	}
}

func decide(t *testing.T, w *storeWorld, text string) (Plan, error) {
	t.Helper()
	d, err := parseDecisions("old-range 100000 65536\n" + text)
	if err != nil {
		t.Fatal(err)
	}
	return makePlan(w.scan, w.items(), d)
}

// O10: a removed or kept image's old-range layers are re-owned (root), then podman removes the
// removed ones (the user), children before parents; volumes and containers likewise.
func TestPlanReownsFirstThenRemovesThroughPodman(t *testing.T) {
	w := newStore().layers("L1", "L2", "L3").image("BASE", "L2").image("CHILD", "L3").volume("v").
		old("overlay/L1", 100099, true).old("overlay/L3", 100099, true).old("volumes/v", 100099, false).
		old("overlay/STRAY", 100099, false)
	p, err := decide(t, w, "remove image BASE\nremove image CHILD\nremove volume v\nremove path overlay/STRAY\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := stepKeys(p.Reown); !slices.Equal(got, []string{"overlay/L1", "overlay/L3", "volumes/v"}) {
		t.Errorf("reown = %v", got)
	}
	if got := stepKeys(p.RootRemove); !slices.Equal(got, []string{"overlay/STRAY"}) {
		t.Errorf("root removes = %v", got)
	}
	if got := stepKeys(p.Remove); !slices.Equal(got, []string{"CHILD", "BASE", "v"}) {
		t.Errorf("podman removes = %v (children first)", got)
	}
	if p.Old != oldRange || p.Store != "/s" || p.Owners != owners() {
		t.Errorf("plan header = %+v", p)
	}
}

func stepKeys(steps []PlanStep) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.Key)
	}
	return out
}

// O12: a layer under a running container is never in the plan, whatever was decided for the
// image that also uses it; it is named as held.
func TestPlanLeavesWhatARunningContainerHolds(t *testing.T) {
	w := newStore().layers("L1", "L2").image("KEPT", "L2").layers("L1", "L3").image("LIVE", "L3").
		old("overlay/L1", 100099, true).old("overlay/L2", 100099, true)
	w.container("C", "live", "LIVE", "CL", "running")
	p, err := decide(t, w, "reown image KEPT\nleave image LIVE\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := stepKeys(p.Reown); !slices.Equal(got, []string{"overlay/L2"}) {
		t.Errorf("reown = %v", got)
	}
	if got := stepKeys(p.Held); !slices.Equal(got, []string{"overlay/L1"}) {
		t.Errorf("held = %v", got)
	}
	if !slices.Equal(p.Left, []string{"image LIVE"}) {
		t.Errorf("left = %v", p.Left)
	}
}

// O11: a decisions file that misses an item, keeps an ASK, names what is not an item, or would
// act on what is held, used or unshiftable, is refused whole.
func TestPlanRefusesAnUnsafeOrIncompleteDecision(t *testing.T) {
	base := func() *storeWorld {
		w := newStore().layers("L1").image("USED", "L1").layers("L2").image("FREE", "L2").layers("L3").image("LIVE", "L3").
			volume("mounted").volume("data").old("overlay/L1", 100099, true).old("overlay/L2", 100099, true).
			old("overlay/L3", 100099, true).old("volumes/mounted", 100099, true).old("volumes/data", 100099, true).
			old("overlay-containers/x", 100099, false)
		w.container("C1", "stopped", "USED", "CL1", "exited", "mounted").container("C2", "live", "LIVE", "CL2", "running")
		w.layers("L9").image("ODD", "L9").old("overlay/L9", 65534, true)
		return w
	}
	ok := "reown image USED\nremove image FREE\nleave image LIVE\nleave image ODD\nreown volume mounted\nremove volume data\nleave path overlay-containers/x\n"
	if _, err := decide(t, base(), ok); err != nil {
		t.Fatalf("the safe decisions were refused: %v", err)
	}
	for name, text := range map[string]string{
		"an item missing":         strings.Replace(ok, "remove image FREE\n", "", 1),
		"still ask":               strings.Replace(ok, "remove volume data", "ask volume data", 1),
		"not an item":             ok + "remove image NOPE\n",
		"remove a used image":     strings.Replace(ok, "reown image USED", "remove image USED", 1),
		"touch a held image":      strings.Replace(ok, "leave image LIVE", "reown image LIVE", 1),
		"remove a mounted volume": strings.Replace(ok, "reown volume mounted", "remove volume mounted", 1),
		"shift an unshiftable":    strings.Replace(ok, "leave image ODD", "reown image ODD", 1),
		"remove a record dir":     strings.Replace(ok, "leave path overlay-containers/x", "remove path overlay-containers/x", 1),
	} {
		if _, err := decide(t, base(), text); err == nil {
			t.Errorf("%s: the plan was made", name)
		}
	}
}

// O6: an image may be removed only with every image built on it.
func TestPlanRefusesAParentWithoutItsChildren(t *testing.T) {
	w := newStore().layers("L1", "L2", "L3").image("BASE", "L2").image("CHILD", "L3").old("overlay/L1", 100099, true)
	if _, err := decide(t, w, "remove image BASE\nreown image CHILD\n"); err == nil {
		t.Fatal("the parent of a kept child was planned for removal")
	}
}

func TestPlanRefusesAnOldRangeOverTheMappedOnes(t *testing.T) {
	w := newStore().layers("L1").image("I", "L1").old("overlay/L1", 100099, true)
	d, err := parseDecisions("old-range 524288 10\nremove image I\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := makePlan(w.scan, w.items(), d); err == nil {
		t.Fatal("an old range over the current one was planned")
	}
}
