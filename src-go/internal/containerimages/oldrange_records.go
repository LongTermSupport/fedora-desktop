package containerimages

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// imageRecord, containerRecord and layerRecord are c/storage's own records of the store
// (overlay-images/images.json and the rest), matched by its JSON names.
type imageRecord struct {
	ID           string   `json:"id"`
	Names        []string `json:"names"`
	Layer        string   `json:"layer"`
	MappedLayers []string `json:"mapped-layers"`
}

type containerRecord struct {
	ID    string   `json:"id"`
	Names []string `json:"names"`
	Image string   `json:"image"`
	Layer string   `json:"layer"`
}

type layerRecord struct {
	ID     string `json:"id"`
	Parent string `json:"parent"`
}

// records is what c/storage knows of the store (O5).
type records struct {
	parent     map[string]string // every layer, to its parent ("" for a base layer)
	images     []imageRecord
	containers []containerRecord
}

// readJSONList reads one record file; a missing one is none (a fresh store has no records).
func readJSONList[T any](path string) ([]T, error) {
	text, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read c/storage's record %s: %w", path, err)
	}
	var out []T
	if err := json.Unmarshal(text, &out); err != nil {
		return nil, fmt.Errorf("cannot read c/storage's record %s: %w", path, err)
	}
	return out, nil
}

// readRecords reads the store's layers, images and containers, the volatile ones too.
func readRecords(store string) (records, error) {
	r := records{parent: map[string]string{}}
	for _, name := range []string{"layers.json", "volatile-layers.json"} {
		layers, err := readJSONList[layerRecord](filepath.Join(store, "overlay-layers", name))
		if err != nil {
			return r, err
		}
		for _, l := range layers {
			r.parent[l.ID] = l.Parent
		}
	}
	var err error
	if r.images, err = readJSONList[imageRecord](filepath.Join(store, "overlay-images", "images.json")); err != nil {
		return r, err
	}
	for _, name := range []string{"containers.json", "volatile-containers.json"} {
		cs, err := readJSONList[containerRecord](filepath.Join(store, "overlay-containers", name))
		if err != nil {
			return r, err
		}
		r.containers = append(r.containers, cs...)
	}
	return r, nil
}

// chain is the layer and every parent under it.
func (r records) chain(layer string) map[string]bool {
	out := map[string]bool{}
	for id := layer; id != "" && !out[id]; id = r.parent[id] {
		out[id] = true
	}
	return out
}

// imageChain is every layer an image stands on: its top layer's chain and each mapped copy's.
func (r records) imageChain(img imageRecord) map[string]bool {
	out := r.chain(img.Layer)
	for _, m := range img.MappedLayers {
		for id := range r.chain(m) {
			out[id] = true
		}
	}
	return out
}
