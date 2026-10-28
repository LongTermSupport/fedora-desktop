package containerimages

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"
)

// writeRecords writes c/storage's records under store, in the shapes c/storage writes them.
func writeRecords(t *testing.T, store string) {
	t.Helper()
	writeFile(t, filepath.Join(store, "overlay-layers", "layers.json"),
		`[{"id":"L1","created":"2025-01-01T00:00:00Z","diff-size":10},{"id":"L2","parent":"L1"},{"id":"L3","parent":"L2"},{"id":"CL","parent":"L3"}]`)
	writeFile(t, filepath.Join(store, "overlay-layers", "volatile-layers.json"), `[{"id":"VL","parent":"L2"}]`)
	writeFile(t, filepath.Join(store, "overlay-images", "images.json"),
		`[{"id":"I2","names":["localhost/app:1"],"layer":"L2","mapped-layers":["M2"]},{"id":"I3","layer":"L3"}]`)
	writeFile(t, filepath.Join(store, "overlay-containers", "containers.json"),
		`[{"id":"C1","names":["web"],"image":"I3","layer":"CL"}]`)
}

func TestReadRecordsReadsLayersImagesAndContainers(t *testing.T) {
	store := t.TempDir()
	writeRecords(t, store)
	r, err := readRecords(store)
	if err != nil {
		t.Fatal(err)
	}
	if r.parent["L2"] != "L1" || r.parent["VL"] != "L2" || len(r.parent) != 5 {
		t.Errorf("layers = %v", r.parent)
	}
	if len(r.images) != 2 || r.images[0].ID != "I2" || !slices.Equal(r.images[0].Names, []string{"localhost/app:1"}) ||
		!slices.Equal(r.images[0].MappedLayers, []string{"M2"}) {
		t.Errorf("images = %+v", r.images)
	}
	if len(r.containers) != 1 || r.containers[0].Image != "I3" || r.containers[0].Layer != "CL" || r.containers[0].Names[0] != "web" {
		t.Errorf("containers = %+v", r.containers)
	}
}

// An image's layers are its top layer and each mapped copy, with every parent.
func TestChainIsEveryLayerAnImageStandsOn(t *testing.T) {
	store := t.TempDir()
	writeRecords(t, store)
	r, err := readRecords(store)
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(r.chain("L3"))); !slices.Equal(got, []string{"L1", "L2", "L3"}) {
		t.Errorf("chain(L3) = %v", got)
	}
	img := r.images[0]
	if got := slices.Sorted(maps.Keys(r.imageChain(img)));!slices.Equal(got, []string{"L1", "L2", "M2"}) {
		t.Errorf("imageChain(I2) = %v", got)
	}
}

// A loop in the parents (a damaged record) ends the walk rather than looping.
func TestChainEndsOnALoop(t *testing.T) {
	r := records{parent: map[string]string{"A": "B", "B": "A"}}
	if got := slices.Sorted(maps.Keys(r.chain("A")));!slices.Equal(got, []string{"A", "B"}) {
		t.Fatalf("chain = %v", got)
	}
}

// A fresh store has no records yet: none is no record, never an error.
func TestReadRecordsOfAFreshStoreIsEmpty(t *testing.T) {
	r, err := readRecords(t.TempDir())
	if err != nil || len(r.parent) != 0 || len(r.images) != 0 || len(r.containers) != 0 {
		t.Fatalf("readRecords = %+v, %v", r, err)
	}
}

func TestReadRecordsRefusesARecordItCannotRead(t *testing.T) {
	store := t.TempDir()
	writeFile(t, filepath.Join(store, "overlay-images", "images.json"), "{not json")
	if _, err := readRecords(store); err == nil {
		t.Fatal("a damaged record read")
	}
}
