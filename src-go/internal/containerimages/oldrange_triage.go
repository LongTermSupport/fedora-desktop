package containerimages

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
)

const (
	kindVolume = "volume"
	kindLayer  = "layer"
	kindPath   = "path"

	proposeRemove = "REMOVE"
	proposeReown  = "RE-OWN"
	proposeAsk    = "ASK"
	proposeHeld   = "HELD"

	actRemove = "remove"
	actReown  = "reown"
	actLeave  = "leave"
	actAsk    = "ask"
)

// kindOrder is the order items are listed in.
var kindOrder = []string{kindImage, kindCtr, kindVolume, kindLayer, kindPath}

// podmanView is what podman says of the store that c/storage's records do not: each
// container's state and volumes, and the volumes.
type podmanView struct {
	containers map[string]engine.StoreContainer // by id, the external ones too
	mounts     map[string][]string              // container id to the volumes it mounts
	volumes    map[string]bool
}

// item is one thing a decision is made about (O6-O9).
type item struct {
	kind, key string
	names     []string
	proposal  string
	why       string
	users     []string
	dirs      []string // the store directories holding its old-range paths
	held      []string // those a running container's overlay holds (O12), as a person reads them
	heldDirs  map[string]string
	paths     int
	size      int64
	samples   []string
	data      bool
	// unshiftable is an owner outside the old range, or one that does not fit the current one.
	unshiftable bool
	allowed     []string
	// children are the images built on an image; used is a container of any state on it or
	// mounting a volume; live is a running container doing so.
	children []string
	used     bool
	live     bool
	// depth is an image's number of layers, so podman removes a child before its parent;
	// external is a container another tool made (buildah's).
	depth    int
	external bool
}

// deadStates are the container states with nothing mounted; any other, and a container podman
// does not list, may have its overlay mounted.
var deadStates = []string{"exited", "stopped", "created", "configured", "storage"}

// ccyOwnTags are never proposed for removal (the plan's non-goal).
var ccyOwnTags = []string{"localhost/claude-yolo:base", "localhost/claude-yolo:latest", "claude-yolo:base", "claude-yolo:latest"}

type triager struct {
	scan  Scan
	rec   records
	pv    podmanView
	old   Range
	items map[string]*item // by kind and key
	// heldBy is each layer a live container's overlay holds, to that container's name.
	heldBy map[string]string
	chains map[string]map[string]bool
}

// triage is O5-O9: each old-range store directory attributed to what owns it, and a proposal
// for each item.
func triage(scan Scan, rec records, pv podmanView, old Range) []item {
	t := &triager{scan: scan, rec: rec, pv: pv, old: old, items: map[string]*item{}, heldBy: map[string]string{}, chains: map[string]map[string]bool{}}
	for _, img := range rec.images {
		t.chains[img.ID] = rec.imageChain(img)
	}
	t.findHeld()
	for _, d := range scan.Dirs {
		t.attribute(d)
	}
	t.propose()
	out := make([]item, 0, len(t.items))
	for _, it := range t.items {
		slices.Sort(it.dirs)
		slices.Sort(it.held)
		out = append(out, *it)
	}
	slices.SortFunc(out, func(a, b item) int {
		return cmp.Or(cmp.Compare(slices.Index(kindOrder, a.kind), slices.Index(kindOrder, b.kind)), cmp.Compare(a.key, b.key))
	})
	return out
}

// liveContainer is a container whose overlay may be mounted: one in no dead state, or one
// podman does not list.
func (t *triager) liveContainer(id string) bool {
	c, ok := t.pv.containers[id]
	return !ok || !slices.Contains(deadStates, c.State)
}

func containerName(c containerRecord) string {
	if len(c.Names) > 0 {
		return c.Names[0]
	}
	return short(c.ID)
}

// findHeld is O12: every layer under a live container's overlay, its own and its image's.
func (t *triager) findHeld() {
	for _, c := range t.rec.containers {
		if !t.liveContainer(c.ID) {
			continue
		}
		layers := t.rec.chain(c.Layer)
		maps.Copy(layers, t.chains[c.Image])
		for id := range layers {
			if _, seen := t.heldBy[id]; !seen {
				t.heldBy[id] = containerName(c)
			}
		}
	}
}

func (t *triager) get(kind, key string) *item {
	k := kind + "\x00" + key
	if it := t.items[k]; it != nil {
		return it
	}
	it := &item{kind: kind, key: key}
	t.items[k] = it
	return it
}

func (t *triager) unshiftable(d StoreDir) bool {
	for _, uid := range d.UIDs {
		if _, ok := shift(uid, t.old, t.scan.Owners.SubUID); !ok {
			return true
		}
	}
	for _, gid := range d.GIDs {
		if _, ok := shift(gid, t.old, t.scan.Owners.SubGID); t.old.Has(gid) && !ok {
			return true
		}
	}
	return false
}

// add puts a directory's figures in an item.
func (t *triager) add(it *item, d StoreDir) {
	it.dirs = append(it.dirs, d.Key)
	it.paths += d.Paths
	it.size += d.Size
	it.data = it.data || d.Data
	it.unshiftable = it.unshiftable || t.unshiftable(d)
	it.samples = append(it.samples, d.Samples...)
	if layer, ok := strings.CutPrefix(d.Key, "overlay/"); ok {
		if by, held := t.heldBy[layer]; held {
			it.held = append(it.held, d.Key+" (under running container "+by+")")
			if it.heldDirs == nil {
				it.heldDirs = map[string]string{}
			}
			it.heldDirs[d.Key] = "under running container " + by
		}
	}
}

// attribute is O5: a directory belongs to a container's own layer, the images whose chains
// hold its layer, a volume, a recorded layer nothing uses, or nothing known.
func (t *triager) attribute(d StoreDir) {
	if layer, ok := strings.CutPrefix(d.Key, "overlay/"); ok {
		for _, c := range t.rec.containers {
			if c.Layer == layer {
				t.add(t.get(kindCtr, c.ID), d)
				return
			}
		}
		if _, recorded := t.rec.parent[layer]; recorded {
			found := false
			for _, img := range t.rec.images {
				if t.chains[img.ID][layer] {
					t.add(t.get(kindImage, img.ID), d)
					found = true
				}
			}
			if !found {
				t.add(t.get(kindLayer, layer), d)
			}
			return
		}
	}
	if name, ok := strings.CutPrefix(d.Key, "volumes/"); ok && t.pv.volumes[name] {
		t.add(t.get(kindVolume, name), d)
		return
	}
	t.add(t.get(kindPath, d.Key), d)
}

func (t *triager) propose() {
	for _, it := range t.items {
		switch it.kind {
		case kindImage:
			t.proposeImage(it)
		case kindCtr:
			t.proposeContainer(it)
		case kindVolume:
			t.proposeVolume(it)
		case kindLayer:
			it.allowed = []string{actReown, actLeave}
			it.proposal, it.why = proposeAsk, "a recorded layer no image and no container uses"
		default:
			it.allowed = []string{actReown, actLeave}
			if rootRemovable(it.key) {
				it.allowed = append(it.allowed, actRemove)
			}
			it.proposal, it.why = proposeAsk, "no record of c/storage or podman names this directory"
		}
		if it.unshiftable && it.proposal != proposeHeld {
			it.proposal, it.why = proposeAsk, "owned outside both the old and the current range, so it cannot be shifted; "+it.why
		}
	}
	t.settleImages()
}

// rootRemovable is a directory root may remove (O13): one under overlay/ or volumes/ that no
// record names, never the link directory or the temporary directories the repair owns.
func rootRemovable(key string) bool {
	if name, ok := strings.CutPrefix(key, "overlay/"); ok {
		return name != "l" && name != "tempdirs"
	}
	return strings.HasPrefix(key, "volumes/")
}

func (t *triager) imageRecord(id string) imageRecord {
	for _, r := range t.rec.images {
		if r.ID == id {
			return r
		}
	}
	return imageRecord{ID: id}
}

// proposeImage is O6, before the images built on it are settled.
func (t *triager) proposeImage(it *item) {
	it.allowed = []string{actRemove, actReown, actLeave}
	img := t.imageRecord(it.key)
	it.names = img.Names
	it.depth = len(t.chains[img.ID])
	for _, r := range t.rec.images {
		if r.ID != img.ID && img.Layer != "" && t.chains[r.ID][img.Layer] {
			it.children = append(it.children, r.ID)
		}
	}
	slices.Sort(it.children)
	var stopped, live []string
	for _, c := range t.rec.containers {
		if c.Image != img.ID && !slices.Contains(it.children, c.Image) {
			continue
		}
		state := cmp.Or(t.pv.containers[c.ID].State, "not listed by podman")
		it.users = append(it.users, "container "+containerName(c)+" ("+state+")")
		it.used = it.used || c.Image == img.ID
		if t.liveContainer(c.ID) {
			live = append(live, containerName(c))
		} else {
			stopped = append(stopped, containerName(c))
		}
	}
	tagged := ""
	if len(img.Names) > 0 {
		tagged = "; tagged " + strings.Join(img.Names, ", ") + " (a pull or a build makes it again)"
	}
	switch {
	case len(live) > 0:
		it.live = true
		it.proposal, it.why = proposeHeld, "used by running container "+strings.Join(live, ", ")+" (itself or an image built on it)"
	case len(stopped) > 0:
		it.proposal, it.why = proposeReown, "used by stopped container "+strings.Join(stopped, ", ")+" (itself or an image built on it)"
	case slices.ContainsFunc(img.Names, func(n string) bool { return slices.Contains(ccyOwnTags, n) }):
		it.proposal, it.why = proposeReown, "ccy's own base or latest image, never removed"
	default:
		it.proposal, it.why = proposeRemove, "no container uses it"+tagged
	}
}

// settleImages keeps (RE-OWN) every image a kept image is built on, until nothing changes: a
// child that is not an item has no old-range layer of its own and is kept.
func (t *triager) settleImages() {
	for changed := true; changed; {
		changed = false
		for _, it := range t.items {
			if it.kind != kindImage || it.proposal != proposeRemove {
				continue
			}
			for _, child := range it.children {
				c := t.items[kindImage+"\x00"+child]
				if c == nil || c.proposal != proposeRemove {
					it.proposal, it.why = proposeReown, "image "+short(child)+" is built on it and is kept"
					changed = true
					break
				}
			}
		}
	}
}

// proposeContainer is O7.
func (t *triager) proposeContainer(it *item) {
	it.allowed = []string{actRemove, actReown, actLeave}
	for _, c := range t.rec.containers {
		if c.ID == it.key {
			it.names = []string{containerName(c)}
		}
	}
	pc, listed := t.pv.containers[it.key]
	it.external = pc.External
	switch {
	case !listed:
		it.live = true
		it.proposal, it.why = proposeHeld, "podman does not list this container, so whether it runs is unknown"
	case t.liveContainer(it.key):
		it.live = true
		it.proposal, it.why = proposeHeld, "the container is "+pc.State+": its own layer is its running state"
	case pc.External && pc.Command == "buildah":
		it.proposal, it.why = proposeRemove, "a buildah working container left by a build that did not finish"
	default:
		it.proposal, it.why = proposeReown, "a "+pc.State+" container: its own layer is its state"
	}
}

// proposeVolume is O8.
func (t *triager) proposeVolume(it *item) {
	it.allowed = []string{actRemove, actReown, actLeave}
	var live, all []string
	for id, vols := range t.pv.mounts {
		if !slices.Contains(vols, it.key) {
			continue
		}
		c := t.pv.containers[id]
		name := cmp.Or(c.Name, short(id))
		all = append(all, name)
		it.users = append(it.users, "container "+name+" ("+c.State+")")
		if t.liveContainer(id) {
			live = append(live, name)
		}
	}
	slices.Sort(all)
	slices.Sort(live)
	slices.Sort(it.users)
	switch {
	case len(live) > 0:
		it.live = true
		it.proposal, it.why = proposeHeld, "mounted by running container "+strings.Join(live, ", ")
	case len(all) > 0:
		it.used = true
		it.proposal, it.why = proposeReown, "mounted by container "+strings.Join(all, ", ")
	case it.data:
		it.proposal, it.why = proposeAsk, "no container mounts it, and it holds data"
	default:
		it.proposal, it.why = proposeRemove, "no container mounts it, and it holds no data"
	}
}
