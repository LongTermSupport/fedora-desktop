package containerimages

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
)

var oldRange = Range{Start: 100000, Count: 65536}

// storeWorld is one store: c/storage's records, podman's view and the scan, built by a test.
type storeWorld struct {
	rec  records
	pv   podmanView
	scan Scan
}

func newStore() *storeWorld {
	return &storeWorld{
		rec:  records{parent: map[string]string{}},
		pv:   podmanView{containers: map[string]engine.StoreContainer{}, mounts: map[string][]string{}, volumes: map[string]bool{}},
		scan: Scan{Store: "/s", User: "joe", Owners: owners()},
	}
}

// layers records a chain of layers, each on the one before.
func (w *storeWorld) layers(ids ...string) *storeWorld {
	parent := ""
	for _, id := range ids {
		if _, ok := w.rec.parent[id]; !ok {
			w.rec.parent[id] = parent
		}
		parent = id
	}
	return w
}

func (w *storeWorld) image(id, top string, names ...string) *storeWorld {
	w.rec.images = append(w.rec.images, imageRecord{ID: id, Layer: top, Names: names})
	return w
}

func (w *storeWorld) container(id, name, image, layer, state string, volumes ...string) *storeWorld {
	w.rec.parent[layer] = ""
	for _, img := range w.rec.images {
		if img.ID == image {
			w.rec.parent[layer] = img.Layer
		}
	}
	w.rec.containers = append(w.rec.containers, containerRecord{ID: id, Names: []string{name}, Image: image, Layer: layer})
	w.pv.containers[id] = engine.StoreContainer{ID: id, Name: name, ImageID: image, State: state, External: state == "storage", Command: "claude"}
	w.pv.mounts[id] = volumes
	for _, v := range volumes {
		w.pv.volumes[v] = true
	}
	return w
}

func (w *storeWorld) volume(name string) *storeWorld {
	w.pv.volumes[name] = true
	return w
}

// old marks a store directory as holding n old-range paths owned by uid.
func (w *storeWorld) old(key string, uid int64, data bool) *storeWorld {
	w.scan.Dirs = append(w.scan.Dirs, StoreDir{
		Key: key, Paths: 1, UIDs: []int64{uid}, GIDs: []int64{uid}, Samples: []string{key + "/diff/var/cache/apt/archives/partial"},
		Size: 2048, Data: data,
	})
	return w
}

func (w *storeWorld) items() []item { return triage(w.scan, w.rec, w.pv, oldRange) }

func find(t *testing.T, items []item, kind, key string) item {
	t.Helper()
	for _, it := range items {
		if it.kind == kind && it.key == key {
			return it
		}
	}
	t.Fatalf("no %s %s among %+v", kind, key, items)
	return item{}
}

// O6: an image no container uses is REMOVE; the old-range directory is its layer's, and the apt
// partial directory inside is never an item of its own.
func TestAnUnusedImageIsRemove(t *testing.T) {
	items := newStore().layers("L1", "L2").image("I1", "L2", "docker.io/library/debian:12").old("overlay/L1", 100099, true).items()
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	it := items[0]
	if it.kind != kindImage || it.key != "I1" || it.proposal != proposeRemove || !slices.Equal(it.dirs, []string{"overlay/L1"}) ||
		!strings.Contains(it.why, "no container") || !slices.Equal(it.names, []string{"docker.io/library/debian:12"}) {
		t.Fatalf("item = %+v", it)
	}
}

// O6: a stopped container keeps its image (RE-OWN); a running one holds it (HELD), and so are
// its layers.
func TestAnImageAContainerUsesIsReownOrHeld(t *testing.T) {
	w := newStore().layers("L1", "L2").image("I1", "L2").layers("L1", "L3").image("I2", "L3").old("overlay/L1", 100099, true)
	w.container("C1", "stopped-one", "I1", "CL1", "exited").container("C2", "live-one", "I2", "CL2", "running")
	items := w.items()
	i1, i2 := find(t, items, kindImage, "I1"), find(t, items, kindImage, "I2")
	if i1.proposal != proposeReown || !strings.Contains(i1.why, "stopped-one") {
		t.Errorf("I1 = %+v", i1)
	}
	if i2.proposal != proposeHeld || !strings.Contains(i2.why, "live-one") {
		t.Errorf("I2 = %+v", i2)
	}
	// The shared layer is under a running container: held in both items.
	held := []string{"overlay/L1 (under running container live-one)"}
	if !slices.Equal(i1.held, held) || !slices.Equal(i2.held, held) {
		t.Errorf("held = %v and %v", i1.held, i2.held)
	}
}

// O6: an image another kept image is built on is RE-OWN; one only removed images build on
// goes with them.
func TestAnImageAKeptImageIsBuiltOnIsReown(t *testing.T) {
	w := newStore().layers("L1", "L2", "L3").image("BASE", "L2").image("CHILD", "L3").old("overlay/L1", 100099, true)
	if got := find(t, w.items(), kindImage, "BASE").proposal; got != proposeRemove {
		t.Fatalf("BASE under a removed child = %s", got)
	}
	w.container("C1", "web", "CHILD", "CL", "exited")
	base := find(t, w.items(), kindImage, "BASE")
	if base.proposal != proposeReown || !strings.Contains(base.why, "web") || base.used {
		t.Fatalf("BASE under a kept child = %+v", base)
	}
	// A running container on the child holds the base too.
	w2 := newStore().layers("L1", "L2", "L3").image("BASE", "L2").image("CHILD", "L3").old("overlay/L1", 100099, true)
	w2.container("C1", "web", "CHILD", "CL", "running")
	if got := find(t, w2.items(), kindImage, "BASE").proposal; got != proposeHeld {
		t.Fatalf("BASE under a running child = %s", got)
	}
}

// ccy's own base and latest are never proposed for removal (the plan's non-goal).
func TestCcysOwnTagsAreReown(t *testing.T) {
	for _, tag := range []string{"localhost/claude-yolo:base", "localhost/claude-yolo:latest"} {
		it := newStore().layers("L1").image("I", "L1", tag).old("overlay/L1", 100099, true).items()[0]
		if it.proposal != proposeReown {
			t.Errorf("%s = %+v", tag, it)
		}
	}
}

// O7: a container's own layer: RE-OWN while stopped, HELD while running, REMOVE for a buildah
// leftover, HELD when podman does not list it (whether it runs is unknown).
func TestAContainersOwnLayer(t *testing.T) {
	cases := []struct {
		state, command string
		want           string
	}{
		{"exited", "claude", proposeReown},
		{"created", "claude", proposeReown},
		{"running", "claude", proposeHeld},
		{"paused", "claude", proposeHeld},
		{"storage", "buildah", proposeRemove},
	}
	for _, c := range cases {
		w := newStore().layers("L1").image("I", "L1").container("C", "box", "I", "CL", c.state).old("overlay/CL", 100099, true)
		ctr := w.pv.containers["C"]
		ctr.Command = c.command
		w.pv.containers["C"] = ctr
		it := find(t, w.items(), kindCtr, "C")
		if it.proposal != c.want || !slices.Equal(it.dirs, []string{"overlay/CL"}) {
			t.Errorf("%s %s = %+v, want %s", c.state, c.command, it, c.want)
		}
	}
	w := newStore().layers("L1").image("I", "L1").container("C", "box", "I", "CL", "exited").old("overlay/CL", 100099, true)
	delete(w.pv.containers, "C")
	if it := find(t, w.items(), kindCtr, "C"); it.proposal != proposeHeld {
		t.Errorf("a container podman does not list = %+v", it)
	}
}

// O8: a volume a container mounts is RE-OWN (HELD while it runs), an unmounted empty one
// REMOVE, an unmounted one with data ASK.
func TestAVolume(t *testing.T) {
	w := newStore().layers("L1").image("I", "L1").
		container("C1", "db", "I", "CL1", "exited", "mounted").
		container("C2", "live", "I", "CL2", "running", "live-mounted").
		volume("empty").volume("data").
		old("volumes/mounted", 100998, true).old("volumes/live-mounted", 100998, true).
		old("volumes/empty", 100998, false).old("volumes/data", 100998, true)
	items := w.items()
	for key, want := range map[string]string{"mounted": proposeReown, "live-mounted": proposeHeld, "empty": proposeRemove, "data": proposeAsk} {
		if it := find(t, items, kindVolume, key); it.proposal != want {
			t.Errorf("volume %s = %+v, want %s", key, it, want)
		}
	}
	if it := find(t, items, kindVolume, "mounted"); !slices.Equal(it.users, []string{"container db (exited)"}) {
		t.Errorf("users = %v", it.users)
	}
}

// O9: anything not attributed to an image, container or volume is ASK; an owner outside both
// ranges makes any item ASK.
func TestWhatCannotBeAttributedIsAsk(t *testing.T) {
	w := newStore().layers("ORPHAN").old("overlay/ORPHAN", 100099, false).old("overlay/UNKNOWN", 100099, false).
		old("volumes/gone", 100099, true).old("overlay-containers/x", 100099, false)
	items := w.items()
	if it := find(t, items, kindLayer, "ORPHAN"); it.proposal != proposeAsk || slices.Contains(it.allowed, actRemove) {
		t.Errorf("orphan layer = %+v", it)
	}
	for _, key := range []string{"overlay/UNKNOWN", "volumes/gone", "overlay-containers/x"} {
		it := find(t, items, kindPath, key)
		if it.proposal != proposeAsk {
			t.Errorf("%s = %+v", key, it)
		}
		if removable := slices.Contains(it.allowed, actRemove); removable != (key != "overlay-containers/x") {
			t.Errorf("%s may be removed: %v", key, removable)
		}
	}
	if it := newStore().layers("L1").image("I", "L1").old("overlay/L1", 65534, true).items()[0]; it.proposal != proposeAsk || !it.unshiftable {
		t.Errorf("an image owned by 65534 = %+v", it)
	}
}

func TestPathsTheRepairOwnsAreNeverRemovedByRoot(t *testing.T) {
	items := newStore().old("overlay/l", 100099, false).old("overlay/tempdirs", 100099, false).items()
	for _, it := range items {
		if slices.Contains(it.allowed, actRemove) {
			t.Errorf("%s may be removed", it.key)
		}
	}
}

func TestItemsAreInAFixedOrder(t *testing.T) {
	w := newStore().layers("L1").image("I2", "L1").image("I1", "L1").volume("v").old("volumes/v", 100099, false).old("overlay/L1", 100099, true)
	items := w.items()
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.kind+" "+it.key)
	}
	if !slices.Equal(keys, []string{"image I1", "image I2", "volume v"}) {
		t.Fatalf("order = %v", keys)
	}
}
